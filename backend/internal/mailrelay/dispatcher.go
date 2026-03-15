package mailrelay

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

// Dispatcher moves durable queued mail-delivery jobs from the database to the
// outbound SMTP forwarder with bounded worker concurrency and retry backoff.
type Dispatcher struct {
	store              QueueStore
	forwarder          MessageForwarder
	logger             interface{ Printf(string, ...any) }
	workers            int
	pollInterval       time.Duration
	leaseDuration      time.Duration
	retryBaseDelay     time.Duration
	retryMaxDelay      time.Duration
	cleanupInterval    time.Duration
	deliveredRetention time.Duration
	failedRetention    time.Duration
	forwardTimeout     time.Duration
	storageTimeout     time.Duration
	now                func() time.Time
}

// NewDispatcher constructs the bounded background worker pool that drains the
// durable mail queue and retries failed jobs safely.
func NewDispatcher(mail config.MailConfig, store QueueStore, forwarder MessageForwarder, logger *log.Logger) *Dispatcher {
	if logger == nil {
		logger = log.Default()
	}

	return &Dispatcher{
		store:              store,
		forwarder:          forwarder,
		logger:             logger,
		workers:            mail.QueueWorkers,
		pollInterval:       mail.QueuePollInterval,
		leaseDuration:      mail.QueueLeaseDuration,
		retryBaseDelay:     mail.RetryBaseDelay,
		retryMaxDelay:      mail.RetryMaxDelay,
		cleanupInterval:    mail.CleanupInterval,
		deliveredRetention: mail.DeliveredRetention,
		failedRetention:    mail.FailedRetention,
		forwardTimeout:     deriveForwardTimeout(mail),
		storageTimeout:     mail.EnqueueTimeout,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// Run starts the worker pool and blocks until the caller cancels the context.
func (d *Dispatcher) Run(ctx context.Context) {
	if d == nil || d.store == nil || d.forwarder == nil {
		return
	}

	var waitGroup sync.WaitGroup
	for workerIndex := 0; workerIndex < d.workers; workerIndex++ {
		waitGroup.Add(1)
		go func(workerNumber int) {
			defer waitGroup.Done()
			d.workerLoop(ctx, workerNumber+1)
		}(workerIndex)
	}

	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		d.cleanupLoop(ctx)
	}()

	<-ctx.Done()
	waitGroup.Wait()
}

// workerLoop keeps claiming and processing one job at a time so outbound SMTP
// concurrency stays bounded by the configured worker count.
func (d *Dispatcher) workerLoop(ctx context.Context, workerNumber int) {
	for {
		if ctx.Err() != nil {
			return
		}

		job, claimed := d.claimOneJob(ctx)
		if !claimed {
			if !sleepContext(ctx, d.pollInterval) {
				return
			}
			continue
		}

		d.processJob(ctx, workerNumber, job)
	}
}

// cleanupLoop periodically deletes old terminal jobs and orphaned message blobs
// so the durable queue cannot grow without bound.
func (d *Dispatcher) cleanupLoop(ctx context.Context) {
	for {
		if !sleepContext(ctx, d.cleanupInterval) {
			return
		}

		cleanupErr := d.retryStorageOperation(ctx, func(operationCtx context.Context) error {
			deletedJobs, err := d.store.CleanupMailDeliveryJobs(operationCtx, storage.CleanupMailDeliveryJobsInput{
				DeliveredBefore: d.now().Add(-d.deliveredRetention),
				FailedBefore:    d.now().Add(-d.failedRetention),
			})
			if err == nil && deletedJobs > 0 {
				d.logger.Printf("linuxdospace mail relay cleanup removed %d terminal jobs", deletedJobs)
			}
			return err
		})
		if cleanupErr != nil && ctx.Err() == nil {
			d.logger.Printf("linuxdospace mail relay cleanup failed: %v", cleanupErr)
		}
	}
}

// claimOneJob asks storage for one ready queued job or one stale processing job
// whose previous lease already expired.
func (d *Dispatcher) claimOneJob(ctx context.Context) (model.MailDeliveryJob, bool) {
	var jobs []model.MailDeliveryJob
	claimErr := d.retryStorageOperation(ctx, func(operationCtx context.Context) error {
		claimedJobs, err := d.store.ClaimMailDeliveryJobs(operationCtx, storage.ClaimMailDeliveryJobsInput{
			Limit:         1,
			LeaseDuration: d.leaseDuration,
			Now:           d.now(),
		})
		if err != nil {
			return err
		}
		jobs = claimedJobs
		return nil
	})
	if claimErr != nil {
		if ctx.Err() == nil {
			d.logger.Printf("linuxdospace mail relay claim failed: %v", claimErr)
		}
		return model.MailDeliveryJob{}, false
	}
	if len(jobs) == 0 {
		return model.MailDeliveryJob{}, false
	}
	return jobs[0], true
}

// processJob performs the network delivery and then persists the appropriate
// queue outcome, including retries or terminal refunds.
func (d *Dispatcher) processJob(ctx context.Context, workerNumber int, job model.MailDeliveryJob) {
	forwardCtx, cancel := context.WithTimeout(ctx, d.forwardTimeout)
	err := d.forwarder.Forward(forwardCtx, ForwardRequest{
		OriginalEnvelopeFrom: job.OriginalEnvelopeFrom,
		OriginalEnvelopeTo:   append([]string(nil), job.OriginalRecipients...),
		TargetRecipients:     append([]string(nil), job.TargetRecipients...),
		RawMessage:           append([]byte(nil), job.RawMessage...),
	})
	cancel()

	if err == nil {
		if markErr := d.retryStorageOperation(ctx, func(operationCtx context.Context) error {
			_, updateErr := d.store.MarkMailDeliveryJobDelivered(operationCtx, storage.MarkMailDeliveryJobDeliveredInput{
				ID:          job.ID,
				DeliveredAt: d.now(),
			})
			return updateErr
		}); markErr != nil && ctx.Err() == nil {
			d.logger.Printf("linuxdospace mail relay worker=%d failed to persist delivered job=%d: %v", workerNumber, job.ID, markErr)
		}
		return
	}

	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		return
	}

	if job.AttemptCount >= job.MaxAttempts {
		if markErr := d.retryStorageOperation(ctx, func(operationCtx context.Context) error {
			_, updateErr := d.store.MarkMailDeliveryJobFailed(operationCtx, storage.MarkMailDeliveryJobFailedInput{
				ID:        job.ID,
				LastError: err.Error(),
				FailedAt:  d.now(),
			})
			return updateErr
		}); markErr != nil && ctx.Err() == nil {
			d.logger.Printf("linuxdospace mail relay worker=%d failed to persist terminal failure for job=%d: %v", workerNumber, job.ID, markErr)
			return
		}
		d.logger.Printf("linuxdospace mail relay worker=%d permanently failed job=%d attempts=%d err=%v", workerNumber, job.ID, job.AttemptCount, err)
		return
	}

	nextAttemptAt := d.now().Add(d.calculateRetryDelay(job.AttemptCount))
	if markErr := d.retryStorageOperation(ctx, func(operationCtx context.Context) error {
		_, updateErr := d.store.MarkMailDeliveryJobRetry(operationCtx, storage.MarkMailDeliveryJobRetryInput{
			ID:            job.ID,
			LastError:     err.Error(),
			NextAttemptAt: nextAttemptAt,
			UpdatedAt:     d.now(),
		})
		return updateErr
	}); markErr != nil && ctx.Err() == nil {
		d.logger.Printf("linuxdospace mail relay worker=%d failed to persist retry state for job=%d: %v", workerNumber, job.ID, markErr)
		return
	}

	d.logger.Printf(
		"linuxdospace mail relay worker=%d scheduled retry for job=%d attempt=%d/%d next_attempt_at=%s err=%v",
		workerNumber,
		job.ID,
		job.AttemptCount,
		job.MaxAttempts,
		nextAttemptAt.Format(time.RFC3339),
		err,
	)
}

// retryStorageOperation retries short storage updates a few times so brief
// database hiccups do not immediately turn into duplicate SMTP deliveries.
func (d *Dispatcher) retryStorageOperation(ctx context.Context, operation func(context.Context) error) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		operationCtx, cancel := context.WithTimeout(ctx, d.storageTimeout)
		err := operation(operationCtx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err

		if attempt == 2 {
			break
		}
		if !sleepContext(ctx, time.Duration(attempt+1)*time.Second) {
			return ctx.Err()
		}
	}
	return lastErr
}

// calculateRetryDelay applies capped exponential backoff so large retry storms
// cannot hammer one broken upstream on a tight loop.
func (d *Dispatcher) calculateRetryDelay(attemptCount int) time.Duration {
	delay := d.retryBaseDelay
	if delay <= 0 {
		delay = time.Second
	}

	for step := 1; step < attemptCount; step++ {
		if delay >= d.retryMaxDelay {
			return d.retryMaxDelay
		}
		if delay > d.retryMaxDelay/2 {
			return d.retryMaxDelay
		}
		delay *= 2
	}
	if delay > d.retryMaxDelay && d.retryMaxDelay > 0 {
		return d.retryMaxDelay
	}
	return delay
}

// sleepContext waits for one interval or until the caller cancels the context.
func sleepContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
