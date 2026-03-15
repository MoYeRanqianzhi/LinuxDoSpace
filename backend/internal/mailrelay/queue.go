package mailrelay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

// QueueStore is the minimum durable-queue contract the mail relay needs from
// the shared storage layer.
type QueueStore interface {
	EnqueueMailDeliveryBatch(ctx context.Context, input storage.EnqueueMailDeliveryBatchInput) ([]model.MailDeliveryJob, error)
	ClaimMailDeliveryJobs(ctx context.Context, input storage.ClaimMailDeliveryJobsInput) ([]model.MailDeliveryJob, error)
	MarkMailDeliveryJobDelivered(ctx context.Context, input storage.MarkMailDeliveryJobDeliveredInput) (model.MailDeliveryJob, error)
	MarkMailDeliveryJobRetry(ctx context.Context, input storage.MarkMailDeliveryJobRetryInput) (model.MailDeliveryJob, error)
	MarkMailDeliveryJobFailed(ctx context.Context, input storage.MarkMailDeliveryJobFailedInput) (model.MailDeliveryJob, error)
	CleanupMailDeliveryJobs(ctx context.Context, input storage.CleanupMailDeliveryJobsInput) (int64, error)
}

// DeliveryQueue accepts one fully resolved SMTP message and persists durable
// outbound jobs without performing the network delivery inline.
type DeliveryQueue interface {
	Enqueue(ctx context.Context, request EnqueueRequest) error
}

// EnqueueRequest is the normalized SMTP transaction payload sent to the durable
// queue after all recipients were resolved successfully.
type EnqueueRequest struct {
	OriginalEnvelopeFrom string
	RawMessage           []byte
	Groups               []groupedRecipients
}

// PersistentQueue translates SMTP session data into storage-layer batch input
// and maps storage errors back into relay-specific semantics.
type PersistentQueue struct {
	store       QueueStore
	maxAttempts int
	now         func() time.Time
}

// NewPersistentQueue constructs the queue adapter used by the SMTP listener.
func NewPersistentQueue(mail config.MailConfig, store QueueStore) *PersistentQueue {
	return &PersistentQueue{
		store:       store,
		maxAttempts: mail.MaxAttempts,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// Enqueue persists one SMTP message and every derived final target group in a
// single storage transaction.
func (q *PersistentQueue) Enqueue(ctx context.Context, request EnqueueRequest) error {
	if q == nil || q.store == nil {
		return fmt.Errorf("mail delivery queue store is not configured")
	}
	if len(request.RawMessage) == 0 {
		return fmt.Errorf("mail delivery queue raw message is empty")
	}
	if len(request.Groups) == 0 {
		return fmt.Errorf("mail delivery queue groups are empty")
	}

	groups := make([]storage.EnqueueMailDeliveryGroupInput, 0, len(request.Groups))
	for _, group := range request.Groups {
		groups = append(groups, storage.EnqueueMailDeliveryGroupInput{
			OriginalRecipients:   append([]string(nil), group.OriginalRecipients...),
			TargetRecipients:     []string{group.TargetEmail},
			CatchAllOwnerUserIDs: append([]int64(nil), group.CatchAllOwnerUserIDs...),
		})
	}

	_, err := q.store.EnqueueMailDeliveryBatch(ctx, storage.EnqueueMailDeliveryBatchInput{
		OriginalEnvelopeFrom: request.OriginalEnvelopeFrom,
		RawMessage:           append([]byte(nil), request.RawMessage...),
		Groups:               groups,
		MaxAttempts:          q.maxAttempts,
		QueuedAt:             q.now(),
	})
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, storage.ErrEmailCatchAllDailyLimitExceeded):
		return ErrCatchAllDailyLimitExceeded
	case errors.Is(err, storage.ErrEmailCatchAllInsufficientRemainingCount):
		return ErrCatchAllAccessUnavailable
	default:
		return fmt.Errorf("enqueue durable mail delivery batch: %w", err)
	}
}
