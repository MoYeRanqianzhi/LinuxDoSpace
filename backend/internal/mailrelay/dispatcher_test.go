package mailrelay

import (
	"context"
	"errors"
	"log"
	"sync"
	"testing"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

type dispatcherStoreStub struct {
	mu sync.Mutex

	deliveredInputs []storage.MarkMailDeliveryJobDeliveredInput
	retryInputs     []storage.MarkMailDeliveryJobRetryInput
	failedInputs    []storage.MarkMailDeliveryJobFailedInput
}

func (s *dispatcherStoreStub) EnqueueMailDeliveryBatch(ctx context.Context, input storage.EnqueueMailDeliveryBatchInput) ([]model.MailDeliveryJob, error) {
	return nil, errors.New("not implemented")
}

func (s *dispatcherStoreStub) ClaimMailDeliveryJobs(ctx context.Context, input storage.ClaimMailDeliveryJobsInput) ([]model.MailDeliveryJob, error) {
	return nil, nil
}

func (s *dispatcherStoreStub) MarkMailDeliveryJobDelivered(ctx context.Context, input storage.MarkMailDeliveryJobDeliveredInput) (model.MailDeliveryJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveredInputs = append(s.deliveredInputs, input)
	return model.MailDeliveryJob{ID: input.ID, Status: model.MailDeliveryJobStatusDelivered}, nil
}

func (s *dispatcherStoreStub) MarkMailDeliveryJobRetry(ctx context.Context, input storage.MarkMailDeliveryJobRetryInput) (model.MailDeliveryJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retryInputs = append(s.retryInputs, input)
	return model.MailDeliveryJob{ID: input.ID, Status: model.MailDeliveryJobStatusQueued}, nil
}

func (s *dispatcherStoreStub) MarkMailDeliveryJobFailed(ctx context.Context, input storage.MarkMailDeliveryJobFailedInput) (model.MailDeliveryJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failedInputs = append(s.failedInputs, input)
	return model.MailDeliveryJob{ID: input.ID, Status: model.MailDeliveryJobStatusFailed}, nil
}

func (s *dispatcherStoreStub) CleanupMailDeliveryJobs(ctx context.Context, input storage.CleanupMailDeliveryJobsInput) (int64, error) {
	return 0, nil
}

type dispatcherForwarderStub struct {
	err error
}

func (f *dispatcherForwarderStub) Forward(ctx context.Context, request ForwardRequest) error {
	return f.err
}

// TestDispatcherMarksDeliveredOnSuccess verifies that a successful outbound
// SMTP forward persists the delivered terminal state.
func TestDispatcherMarksDeliveredOnSuccess(t *testing.T) {
	store := &dispatcherStoreStub{}
	dispatcher := &Dispatcher{
		store:          store,
		forwarder:      &dispatcherForwarderStub{},
		logger:         log.Default(),
		retryBaseDelay: time.Second,
		retryMaxDelay:  time.Minute,
		storageTimeout: time.Second,
		now: func() time.Time {
			return time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
		},
	}

	dispatcher.processJob(context.Background(), 1, model.MailDeliveryJob{
		ID:                   10,
		OriginalEnvelopeFrom: "sender@example.com",
		OriginalRecipients:   []string{"one@alice.linuxdo.space"},
		TargetRecipients:     []string{"target@example.com"},
		RawMessage:           []byte("Subject: test\r\n\r\nbody"),
		AttemptCount:         1,
		MaxAttempts:          3,
	})

	if len(store.deliveredInputs) != 1 {
		t.Fatalf("expected one delivered update, got %d", len(store.deliveredInputs))
	}
	if len(store.retryInputs) != 0 {
		t.Fatalf("expected no retry update on success, got %d", len(store.retryInputs))
	}
	if len(store.failedInputs) != 0 {
		t.Fatalf("expected no failed update on success, got %d", len(store.failedInputs))
	}
}

// TestDispatcherSchedulesRetryBeforeMaxAttempts verifies that transient forward
// failures return the job to the queue instead of refunding immediately.
func TestDispatcherSchedulesRetryBeforeMaxAttempts(t *testing.T) {
	store := &dispatcherStoreStub{}
	dispatcher := &Dispatcher{
		store:          store,
		forwarder:      &dispatcherForwarderStub{err: errors.New("temporary smtp failure")},
		logger:         log.Default(),
		retryBaseDelay: 5 * time.Second,
		retryMaxDelay:  time.Minute,
		storageTimeout: time.Second,
		now: func() time.Time {
			return time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
		},
	}

	dispatcher.processJob(context.Background(), 1, model.MailDeliveryJob{
		ID:                   20,
		OriginalEnvelopeFrom: "sender@example.com",
		OriginalRecipients:   []string{"one@alice.linuxdo.space"},
		TargetRecipients:     []string{"target@example.com"},
		RawMessage:           []byte("Subject: test\r\n\r\nbody"),
		AttemptCount:         1,
		MaxAttempts:          3,
	})

	if len(store.retryInputs) != 1 {
		t.Fatalf("expected one retry update, got %d", len(store.retryInputs))
	}
	if len(store.failedInputs) != 0 {
		t.Fatalf("expected no terminal failure before max attempts, got %d", len(store.failedInputs))
	}
	if !store.retryInputs[0].NextAttemptAt.After(dispatcher.now()) {
		t.Fatalf("expected retry next_attempt_at to be in the future, got %s", store.retryInputs[0].NextAttemptAt.Format(time.RFC3339))
	}
}

// TestDispatcherMarksFailedAtMaxAttempts verifies that the last allowed retry
// converts the job into a terminal failure instead of requeueing forever.
func TestDispatcherMarksFailedAtMaxAttempts(t *testing.T) {
	store := &dispatcherStoreStub{}
	dispatcher := &Dispatcher{
		store:          store,
		forwarder:      &dispatcherForwarderStub{err: errors.New("permanent smtp failure")},
		logger:         log.Default(),
		retryBaseDelay: 5 * time.Second,
		retryMaxDelay:  time.Minute,
		storageTimeout: time.Second,
		now: func() time.Time {
			return time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
		},
	}

	dispatcher.processJob(context.Background(), 1, model.MailDeliveryJob{
		ID:                   30,
		OriginalEnvelopeFrom: "sender@example.com",
		OriginalRecipients:   []string{"one@alice.linuxdo.space"},
		TargetRecipients:     []string{"target@example.com"},
		RawMessage:           []byte("Subject: test\r\n\r\nbody"),
		AttemptCount:         3,
		MaxAttempts:          3,
	})

	if len(store.failedInputs) != 1 {
		t.Fatalf("expected one terminal failure update, got %d", len(store.failedInputs))
	}
	if len(store.retryInputs) != 0 {
		t.Fatalf("expected no retry update at max attempts, got %d", len(store.retryInputs))
	}
}
