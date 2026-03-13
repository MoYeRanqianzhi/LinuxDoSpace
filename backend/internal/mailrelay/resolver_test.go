package mailrelay

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"linuxdospace/backend/internal/model"
)

// fakeResolverStore keeps the mail relay unit tests focused on route
// resolution instead of a concrete SQL backend.
type fakeResolverStore struct {
	routes  map[string]model.EmailRoute
	targets map[string]model.EmailTarget
}

// GetEmailRouteByAddress returns one in-memory route keyed by domain + prefix.
func (f *fakeResolverStore) GetEmailRouteByAddress(ctx context.Context, rootDomain string, prefix string) (model.EmailRoute, error) {
	key := strings.ToLower(strings.TrimSpace(rootDomain)) + "|" + strings.ToLower(strings.TrimSpace(prefix))
	item, ok := f.routes[key]
	if !ok {
		return model.EmailRoute{}, sql.ErrNoRows
	}
	return item, nil
}

// GetEmailTargetByEmail returns one in-memory target binding keyed by email.
func (f *fakeResolverStore) GetEmailTargetByEmail(ctx context.Context, email string) (model.EmailTarget, error) {
	key := strings.ToLower(strings.TrimSpace(email))
	item, ok := f.targets[key]
	if !ok {
		return model.EmailTarget{}, sql.ErrNoRows
	}
	return item, nil
}

// TestDBResolverPrefersExactRoute verifies that an exact mailbox row is used
// before the domain catch-all route, even when both exist.
func TestDBResolverPrefersExactRoute(t *testing.T) {
	verifiedAt := time.Now().UTC()
	resolver := NewDBResolver(&fakeResolverStore{
		routes: map[string]model.EmailRoute{
			"linuxdo.space|alice": {
				ID:          1,
				OwnerUserID: 10,
				RootDomain:  "linuxdo.space",
				Prefix:      "alice",
				TargetEmail: "exact@example.com",
				Enabled:     true,
			},
			"linuxdo.space|catch-all": {
				ID:          2,
				OwnerUserID: 10,
				RootDomain:  "linuxdo.space",
				Prefix:      catchAllRoutePrefix,
				TargetEmail: "catchall@example.com",
				Enabled:     true,
			},
		},
		targets: map[string]model.EmailTarget{
			"exact@example.com": {
				ID:          11,
				OwnerUserID: 10,
				Email:       "exact@example.com",
				VerifiedAt:  &verifiedAt,
			},
			"catchall@example.com": {
				ID:          12,
				OwnerUserID: 10,
				Email:       "catchall@example.com",
				VerifiedAt:  &verifiedAt,
			},
		},
	})

	result, err := resolver.ResolveRecipient(context.Background(), "Alice@LinuxDo.Space")
	if err != nil {
		t.Fatalf("resolve exact recipient: %v", err)
	}
	if result.TargetEmail != "exact@example.com" {
		t.Fatalf("expected exact target email, got %q", result.TargetEmail)
	}
	if result.UsedCatchAll {
		t.Fatalf("expected exact route, but resolver reported catch-all")
	}
}

// TestDBResolverFallsBackToCatchAll verifies that the relay still delivers mail
// for unmatched local-parts when a catch-all route exists for that domain.
func TestDBResolverFallsBackToCatchAll(t *testing.T) {
	verifiedAt := time.Now().UTC()
	resolver := NewDBResolver(&fakeResolverStore{
		routes: map[string]model.EmailRoute{
			"alice.linuxdo.space|catch-all": {
				ID:          2,
				OwnerUserID: 10,
				RootDomain:  "alice.linuxdo.space",
				Prefix:      catchAllRoutePrefix,
				TargetEmail: "catchall@example.com",
				Enabled:     true,
			},
		},
		targets: map[string]model.EmailTarget{
			"catchall@example.com": {
				ID:          12,
				OwnerUserID: 10,
				Email:       "catchall@example.com",
				VerifiedAt:  &verifiedAt,
			},
		},
	})

	result, err := resolver.ResolveRecipient(context.Background(), "notice@alice.linuxdo.space")
	if err != nil {
		t.Fatalf("resolve catch-all recipient: %v", err)
	}
	if result.TargetEmail != "catchall@example.com" {
		t.Fatalf("expected catch-all target email, got %q", result.TargetEmail)
	}
	if !result.UsedCatchAll {
		t.Fatalf("expected catch-all route to be used")
	}
}

// TestDBResolverRejectsMismatchedTargetOwnership verifies that the relay fails
// closed if a route points at a target inbox already bound to another user.
func TestDBResolverRejectsMismatchedTargetOwnership(t *testing.T) {
	verifiedAt := time.Now().UTC()
	resolver := NewDBResolver(&fakeResolverStore{
		routes: map[string]model.EmailRoute{
			"linuxdo.space|alice": {
				ID:          1,
				OwnerUserID: 10,
				RootDomain:  "linuxdo.space",
				Prefix:      "alice",
				TargetEmail: "shared@example.com",
				Enabled:     true,
			},
		},
		targets: map[string]model.EmailTarget{
			"shared@example.com": {
				ID:          11,
				OwnerUserID: 99,
				Email:       "shared@example.com",
				VerifiedAt:  &verifiedAt,
			},
		},
	})

	_, err := resolver.ResolveRecipient(context.Background(), "alice@linuxdo.space")
	if !errors.Is(err, ErrTargetOwnershipMismatch) {
		t.Fatalf("expected target ownership mismatch, got %v", err)
	}
}

// TestBuildForwardMessageAddsTraceHeaders verifies that the outbound forwarder
// writes the relay marker and original envelope headers above the raw message.
func TestBuildForwardMessageAddsTraceHeaders(t *testing.T) {
	raw := []byte("From: sender@example.com\r\nSubject: Test\r\n\r\nhello")

	message, err := buildForwardMessage(raw, "bounce@example.com", []string{"alice@linuxdo.space"})
	if err != nil {
		t.Fatalf("build forward message: %v", err)
	}

	serialized := string(message)
	if !strings.Contains(serialized, "X-LinuxDoSpace-Relay: 1\r\n") {
		t.Fatalf("expected relay marker header, got %q", serialized)
	}
	if !strings.Contains(serialized, "X-LinuxDoSpace-Original-Envelope-From: bounce@example.com\r\n") {
		t.Fatalf("expected original envelope from header, got %q", serialized)
	}
	if !strings.Contains(serialized, "X-LinuxDoSpace-Original-Envelope-To: alice@linuxdo.space\r\n") {
		t.Fatalf("expected original envelope to header, got %q", serialized)
	}
	if !strings.Contains(serialized, "\r\nFrom: sender@example.com\r\n") {
		t.Fatalf("expected original message headers to remain after relay headers, got %q", serialized)
	}
}

// TestBuildForwardMessageRejectsRelayLoop verifies that the relay does not
// forward a message that already passed through LinuxDoSpace once.
func TestBuildForwardMessageRejectsRelayLoop(t *testing.T) {
	raw := []byte("X-LinuxDoSpace-Relay: 1\r\nFrom: sender@example.com\r\n\r\nhello")

	_, err := buildForwardMessage(raw, "", []string{"alice@linuxdo.space"})
	if !errors.Is(err, ErrRelayLoopDetected) {
		t.Fatalf("expected relay loop detection, got %v", err)
	}
}
