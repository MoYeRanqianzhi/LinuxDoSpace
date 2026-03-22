package mailrelay

import (
	"context"
	"testing"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

type queueStoreStub struct {
	enqueuedInputs []storage.EnqueueMailDeliveryBatchInput
}

func (s *queueStoreStub) EnqueueMailDeliveryBatch(ctx context.Context, input storage.EnqueueMailDeliveryBatchInput) ([]model.MailDeliveryJob, error) {
	s.enqueuedInputs = append(s.enqueuedInputs, input)
	return nil, nil
}

func (s *queueStoreStub) ClaimMailDeliveryJobs(ctx context.Context, input storage.ClaimMailDeliveryJobsInput) ([]model.MailDeliveryJob, error) {
	return nil, nil
}

func (s *queueStoreStub) MarkMailDeliveryJobDelivered(ctx context.Context, input storage.MarkMailDeliveryJobDeliveredInput) (model.MailDeliveryJob, error) {
	return model.MailDeliveryJob{}, nil
}

func (s *queueStoreStub) MarkMailDeliveryJobRetry(ctx context.Context, input storage.MarkMailDeliveryJobRetryInput) (model.MailDeliveryJob, error) {
	return model.MailDeliveryJob{}, nil
}

func (s *queueStoreStub) MarkMailDeliveryJobFailed(ctx context.Context, input storage.MarkMailDeliveryJobFailedInput) (model.MailDeliveryJob, error) {
	return model.MailDeliveryJob{}, nil
}

func (s *queueStoreStub) CleanupMailDeliveryJobs(ctx context.Context, input storage.CleanupMailDeliveryJobsInput) (int64, error) {
	return 0, nil
}

// TestPersistentQueueEnqueuesConnectedAPITokenTargets verifies that token
// targets now travel through the durable queue instead of being published
// directly before the SMTP transaction commits.
func TestPersistentQueueEnqueuesConnectedAPITokenTargets(t *testing.T) {
	store := &queueStoreStub{}
	hub := NewTokenStreamHub()
	subscription, err := hub.Subscribe("ldt_token")
	if err != nil {
		t.Fatalf("subscribe token stream: %v", err)
	}
	defer subscription.Cancel()

	queue := &PersistentQueue{
		store:       store,
		hub:         hub,
		maxAttempts: 5,
		now: func() time.Time {
			return time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
		},
	}

	err = queue.Enqueue(context.Background(), EnqueueRequest{
		OriginalEnvelopeFrom: "sender@example.com",
		RawMessage:           []byte("Subject: test\r\n\r\nbody"),
		Groups: []groupedRecipients{
			{
				TargetKind:          model.EmailRouteTargetKindAPIToken,
				TargetTokenPublicID: "ldt_token",
				OriginalRecipients:  []string{"one@alice.linuxdo.space"},
				OwnerUserIDs:        []int64{10},
			},
		},
	})
	if err != nil {
		t.Fatalf("enqueue connected token target: %v", err)
	}
	if len(store.enqueuedInputs) != 1 {
		t.Fatalf("expected one durable enqueue call, got %d", len(store.enqueuedInputs))
	}
	if got := store.enqueuedInputs[0].Groups[0].TargetRecipients[0]; got != encodeAPITokenDeliveryTarget("ldt_token") {
		t.Fatalf("expected encoded token target, got %q", got)
	}
}

// TestPersistentQueueDropsDisconnectedAPITokenTargets verifies that ephemeral
// token targets are skipped entirely when no live client is connected.
func TestPersistentQueueDropsDisconnectedAPITokenTargets(t *testing.T) {
	store := &queueStoreStub{}
	queue := &PersistentQueue{
		store:       store,
		hub:         NewTokenStreamHub(),
		maxAttempts: 5,
		now: func() time.Time {
			return time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
		},
	}

	err := queue.Enqueue(context.Background(), EnqueueRequest{
		OriginalEnvelopeFrom: "sender@example.com",
		RawMessage:           []byte("Subject: test\r\n\r\nbody"),
		Groups: []groupedRecipients{
			{
				TargetKind:          model.EmailRouteTargetKindAPIToken,
				TargetTokenPublicID: "ldt_token",
				OriginalRecipients:  []string{"one@alice.linuxdo.space"},
				OwnerUserIDs:        []int64{10},
			},
		},
	})
	if err != nil {
		t.Fatalf("enqueue disconnected token target: %v", err)
	}
	if len(store.enqueuedInputs) != 0 {
		t.Fatalf("expected disconnected token target to be skipped, got %d enqueue calls", len(store.enqueuedInputs))
	}
}
