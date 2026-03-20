package mailrelay

import (
	"encoding/base64"
	"strings"
	"sync"
	"time"
)

const tokenStreamBufferSize = 128

// TokenStreamEvent is the NDJSON payload delivered to one connected API token.
type TokenStreamEvent struct {
	Type                 string   `json:"type"`
	TokenPublicID        string   `json:"token_public_id,omitempty"`
	OriginalEnvelopeFrom string   `json:"original_envelope_from,omitempty"`
	OriginalRecipients   []string `json:"original_recipients,omitempty"`
	ReceivedAt           string   `json:"received_at,omitempty"`
	RawMessageBase64     string   `json:"raw_message_base64,omitempty"`
}

// TokenMailEvent is the in-memory event published by the SMTP relay for one
// API-token target.
type TokenMailEvent struct {
	TokenPublicID        string
	OriginalEnvelopeFrom string
	OriginalRecipients   []string
	ReceivedAt           time.Time
	RawMessage           []byte
}

// ToStreamEvent converts one broker event into the wire payload used by the SDKs.
func (e TokenMailEvent) ToStreamEvent() TokenStreamEvent {
	return TokenStreamEvent{
		Type:                 "mail",
		TokenPublicID:        strings.TrimSpace(e.TokenPublicID),
		OriginalEnvelopeFrom: strings.TrimSpace(e.OriginalEnvelopeFrom),
		OriginalRecipients:   append([]string(nil), e.OriginalRecipients...),
		ReceivedAt:           e.ReceivedAt.UTC().Format(time.RFC3339),
		RawMessageBase64:     base64.StdEncoding.EncodeToString(e.RawMessage),
	}
}

// TokenStreamHub keeps live in-memory subscriptions for API tokens that want
// to receive mail through the HTTPS NDJSON stream.
type TokenStreamHub struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[string]map[uint64]chan TokenMailEvent
}

// NewTokenStreamHub constructs the in-memory broker used by the HTTP stream
// endpoint and the SMTP relay.
func NewTokenStreamHub() *TokenStreamHub {
	return &TokenStreamHub{
		subscribers: make(map[string]map[uint64]chan TokenMailEvent),
	}
}

// Subscribe registers one live stream consumer for the given token public id.
func (h *TokenStreamHub) Subscribe(tokenPublicID string) (<-chan TokenMailEvent, func()) {
	normalizedID := strings.TrimSpace(tokenPublicID)
	channel := make(chan TokenMailEvent, tokenStreamBufferSize)

	h.mu.Lock()
	h.nextID++
	subscriptionID := h.nextID
	if h.subscribers[normalizedID] == nil {
		h.subscribers[normalizedID] = make(map[uint64]chan TokenMailEvent)
	}
	h.subscribers[normalizedID][subscriptionID] = channel
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		tokenSubscribers := h.subscribers[normalizedID]
		if tokenSubscribers == nil {
			return
		}
		if existing, ok := tokenSubscribers[subscriptionID]; ok {
			delete(tokenSubscribers, subscriptionID)
			close(existing)
		}
		if len(tokenSubscribers) == 0 {
			delete(h.subscribers, normalizedID)
		}
	}

	return channel, cancel
}

// Publish broadcasts one mail event to every currently connected client for
// the given token. If no client is connected, the event is dropped immediately.
func (h *TokenStreamHub) Publish(event TokenMailEvent) int {
	if h == nil {
		return 0
	}

	normalizedID := strings.TrimSpace(event.TokenPublicID)
	if normalizedID == "" {
		return 0
	}

	h.mu.RLock()
	tokenSubscribers := h.subscribers[normalizedID]
	listeners := make([]chan TokenMailEvent, 0, len(tokenSubscribers))
	for _, channel := range tokenSubscribers {
		listeners = append(listeners, channel)
	}
	h.mu.RUnlock()

	delivered := 0
	for _, channel := range listeners {
		select {
		case channel <- event:
			delivered++
		default:
		}
	}
	return delivered
}
