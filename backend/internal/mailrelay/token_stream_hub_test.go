package mailrelay

import "testing"

// TestTokenStreamHubAllowsOnlyOneActiveSubscription verifies that a single API
// token can own only one live NDJSON stream at a time.
func TestTokenStreamHubAllowsOnlyOneActiveSubscription(t *testing.T) {
	hub := NewTokenStreamHub()

	first, err := hub.Subscribe("ldt_token")
	if err != nil {
		t.Fatalf("subscribe first token stream: %v", err)
	}
	defer first.Cancel()

	second, err := hub.Subscribe("ldt_token")
	if err != ErrTokenStreamAlreadyConnected {
		t.Fatalf("expected ErrTokenStreamAlreadyConnected, got subscription=%v err=%v", second, err)
	}
}

// TestTokenStreamHubDisconnectTokenClosesSubscription verifies that token
// revocation can actively terminate the live stream.
func TestTokenStreamHubDisconnectTokenClosesSubscription(t *testing.T) {
	hub := NewTokenStreamHub()

	subscription, err := hub.Subscribe("ldt_token")
	if err != nil {
		t.Fatalf("subscribe token stream: %v", err)
	}

	if disconnected := hub.DisconnectToken("ldt_token"); disconnected != 1 {
		t.Fatalf("expected one disconnected subscriber, got %d", disconnected)
	}

	select {
	case <-subscription.Done():
	default:
		t.Fatalf("expected subscription done channel to be closed after disconnect")
	}
}
