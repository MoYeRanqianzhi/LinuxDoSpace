package mailrelay

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	smtp "github.com/emersion/go-smtp"
)

type recordingDeliveryQueue struct {
	err error

	mu       sync.Mutex
	requests []EnqueueRequest
}

func (q *recordingDeliveryQueue) Enqueue(ctx context.Context, request EnqueueRequest) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.requests = append(q.requests, request)
	return q.err
}

// TestSMTPDataEnqueuesOneDurableGroup verifies that multiple aliases resolved
// to the same target mailbox become one queued delivery group instead of one
// synchronous outbound forward per alias.
func TestSMTPDataEnqueuesOneDurableGroup(t *testing.T) {
	queue := &recordingDeliveryQueue{}
	session := &smtpSession{
		queue:          queue,
		logger:         log.Default(),
		enqueueTimeout: time.Second,
		ingressSlots:   make(chan struct{}, 1),
		recipients: []ResolvedRecipient{
			{
				OriginalRecipient: "one@alice.linuxdo.space",
				TargetEmail:       "target@example.com",
				RouteOwnerUserID:  11,
				UsedCatchAll:      true,
			},
			{
				OriginalRecipient: "two@alice.linuxdo.space",
				TargetEmail:       "target@example.com",
				RouteOwnerUserID:  11,
				UsedCatchAll:      true,
			},
		},
	}

	if err := session.Data(strings.NewReader("Subject: test\r\n\r\nbody")); err != nil {
		t.Fatalf("smtp data should queue successfully, got %v", err)
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.requests) != 1 {
		t.Fatalf("expected one queued request, got %d", len(queue.requests))
	}
	if len(queue.requests[0].Groups) != 1 {
		t.Fatalf("expected one grouped target, got %d", len(queue.requests[0].Groups))
	}
	group := queue.requests[0].Groups[0]
	if group.TargetEmail != "target@example.com" {
		t.Fatalf("expected target email target@example.com, got %q", group.TargetEmail)
	}
	if len(group.OriginalRecipients) != 2 {
		t.Fatalf("expected two original recipients in one group, got %+v", group.OriginalRecipients)
	}
	if len(group.CatchAllOwnerUserIDs) != 1 || group.CatchAllOwnerUserIDs[0] != 11 {
		t.Fatalf("expected one catch-all owner 11, got %+v", group.CatchAllOwnerUserIDs)
	}
}

// TestSMTPDataMapsCatchAllQueueErrors verifies that deterministic catch-all
// denials still surface as the correct SMTP DATA status codes after the queue
// refactor.
func TestSMTPDataMapsCatchAllQueueErrors(t *testing.T) {
	queue := &recordingDeliveryQueue{err: ErrCatchAllAccessUnavailable}
	session := &smtpSession{
		queue:          queue,
		logger:         log.Default(),
		enqueueTimeout: time.Second,
		ingressSlots:   make(chan struct{}, 1),
		recipients: []ResolvedRecipient{
			{
				OriginalRecipient: "one@alice.linuxdo.space",
				TargetEmail:       "target@example.com",
				RouteOwnerUserID:  11,
				UsedCatchAll:      true,
			},
		},
	}

	err := session.Data(strings.NewReader("Subject: test\r\n\r\nbody"))
	if err == nil {
		t.Fatalf("expected smtp data to fail when the durable queue rejects catch-all access")
	}

	var smtpErr *smtp.SMTPError
	if !errors.As(err, &smtpErr) {
		t.Fatalf("expected smtp error, got %T %v", err, err)
	}
	if smtpErr.Code != 550 {
		t.Fatalf("expected smtp 550 for unavailable catch-all access, got %d", smtpErr.Code)
	}
}

// TestSMTPDataAppliesIngressBackpressure verifies that the relay fails fast
// instead of reading unlimited message bodies once all ingress slots are busy.
func TestSMTPDataAppliesIngressBackpressure(t *testing.T) {
	slots := make(chan struct{}, 1)
	slots <- struct{}{}

	session := &smtpSession{
		queue:          &recordingDeliveryQueue{},
		logger:         log.Default(),
		enqueueTimeout: 25 * time.Millisecond,
		ingressSlots:   slots,
		recipients: []ResolvedRecipient{
			{
				OriginalRecipient: "one@alice.linuxdo.space",
				TargetEmail:       "target@example.com",
			},
		},
	}

	err := session.Data(strings.NewReader("Subject: test\r\n\r\nbody"))
	if err == nil {
		t.Fatalf("expected smtp data to fail when all ingress slots are busy")
	}

	var smtpErr *smtp.SMTPError
	if !errors.As(err, &smtpErr) {
		t.Fatalf("expected smtp error, got %T %v", err, err)
	}
	if smtpErr.Code != 451 {
		t.Fatalf("expected smtp 451 for ingress backpressure, got %d", smtpErr.Code)
	}
}
