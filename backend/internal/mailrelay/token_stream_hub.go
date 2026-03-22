package mailrelay

import (
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"
)

const tokenStreamBufferSize = 128

// ErrTokenStreamAlreadyConnected reports that the same API token already owns
// one active NDJSON stream connection.
var ErrTokenStreamAlreadyConnected = errors.New("api token stream is already connected")

// TokenStreamEvent is the NDJSON payload delivered to one connected API token.
type TokenStreamEvent struct {
	Type                 string   `json:"type"`
	TokenPublicID        string   `json:"token_public_id,omitempty"`
	OwnerUsername        string   `json:"owner_username,omitempty"`
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
	subscribers map[string]map[uint64]*tokenStreamSubscriber
}

type tokenStreamSubscriber struct {
	events chan TokenMailEvent
	done   chan struct{}

	mu     sync.RWMutex
	closed bool
}

func newTokenStreamSubscriber() *tokenStreamSubscriber {
	return &tokenStreamSubscriber{
		events: make(chan TokenMailEvent, tokenStreamBufferSize),
		done:   make(chan struct{}),
	}
}

func (s *tokenStreamSubscriber) Events() <-chan TokenMailEvent {
	return s.events
}

func (s *tokenStreamSubscriber) Done() <-chan struct{} {
	return s.done
}

func (s *tokenStreamSubscriber) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.done)
}

func (s *tokenStreamSubscriber) TrySend(event TokenMailEvent) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return false
	}
	select {
	case s.events <- event:
		return true
	default:
		return false
	}
}

// TokenStreamSubscription is the lifecycle handle returned to one active API
// token mail stream client.
type TokenStreamSubscription struct {
	hub            *TokenStreamHub
	tokenPublicID  string
	subscriptionID uint64
	subscriber     *tokenStreamSubscriber
}

func (s *TokenStreamSubscription) Events() <-chan TokenMailEvent {
	if s == nil || s.subscriber == nil {
		return nil
	}
	return s.subscriber.Events()
}

func (s *TokenStreamSubscription) Done() <-chan struct{} {
	if s == nil || s.subscriber == nil {
		return nil
	}
	return s.subscriber.Done()
}

func (s *TokenStreamSubscription) Cancel() {
	if s == nil || s.hub == nil {
		return
	}
	s.hub.removeSubscription(s.tokenPublicID, s.subscriptionID)
}

// NewTokenStreamHub constructs the in-memory broker used by the HTTP stream
// endpoint and the SMTP relay.
func NewTokenStreamHub() *TokenStreamHub {
	return &TokenStreamHub{
		subscribers: make(map[string]map[uint64]*tokenStreamSubscriber),
	}
}

// Subscribe registers one live stream consumer for the given token public id.
func (h *TokenStreamHub) Subscribe(tokenPublicID string) (*TokenStreamSubscription, error) {
	normalizedID := strings.TrimSpace(tokenPublicID)
	if normalizedID == "" {
		return nil, ErrTokenStreamAlreadyConnected
	}
	subscriber := newTokenStreamSubscriber()

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subscribers[normalizedID]) > 0 {
		return nil, ErrTokenStreamAlreadyConnected
	}
	h.nextID++
	subscriptionID := h.nextID
	if h.subscribers[normalizedID] == nil {
		h.subscribers[normalizedID] = make(map[uint64]*tokenStreamSubscriber)
	}
	h.subscribers[normalizedID][subscriptionID] = subscriber

	return &TokenStreamSubscription{
		hub:            h,
		tokenPublicID:  normalizedID,
		subscriptionID: subscriptionID,
		subscriber:     subscriber,
	}, nil
}

func (h *TokenStreamHub) removeSubscription(tokenPublicID string, subscriptionID uint64) {
	normalizedID := strings.TrimSpace(tokenPublicID)
	if normalizedID == "" {
		return
	}

	h.mu.Lock()
	tokenSubscribers := h.subscribers[normalizedID]
	if tokenSubscribers == nil {
		h.mu.Unlock()
		return
	}
	existing := tokenSubscribers[subscriptionID]
	delete(tokenSubscribers, subscriptionID)
	if len(tokenSubscribers) == 0 {
		delete(h.subscribers, normalizedID)
	}
	h.mu.Unlock()

	if existing != nil {
		existing.Close()
	}
}

// DisconnectToken closes every active stream subscriber for one API token.
func (h *TokenStreamHub) DisconnectToken(tokenPublicID string) int {
	if h == nil {
		return 0
	}

	normalizedID := strings.TrimSpace(tokenPublicID)
	if normalizedID == "" {
		return 0
	}

	h.mu.Lock()
	tokenSubscribers := h.subscribers[normalizedID]
	if len(tokenSubscribers) == 0 {
		h.mu.Unlock()
		return 0
	}
	snapshot := make([]*tokenStreamSubscriber, 0, len(tokenSubscribers))
	for subscriptionID, subscriber := range tokenSubscribers {
		snapshot = append(snapshot, subscriber)
		delete(tokenSubscribers, subscriptionID)
	}
	delete(h.subscribers, normalizedID)
	h.mu.Unlock()

	for _, subscriber := range snapshot {
		subscriber.Close()
	}
	return len(snapshot)
}

// SubscriberCount reports how many live consumers are currently attached to
// the given token public id.
func (h *TokenStreamHub) SubscriberCount(tokenPublicID string) int {
	if h == nil {
		return 0
	}

	normalizedID := strings.TrimSpace(tokenPublicID)
	if normalizedID == "" {
		return 0
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers[normalizedID])
}

// HasSubscribers is the convenient boolean form used by the SMTP queue when it
// decides whether an ephemeral API-token target should be accepted or dropped.
func (h *TokenStreamHub) HasSubscribers(tokenPublicID string) bool {
	return h.SubscriberCount(tokenPublicID) > 0
}

// Publish broadcasts one mail event to every currently connected client for
// the given token. If no client is connected, the event is dropped immediately.
func (h *TokenStreamHub) Publish(event TokenMailEvent) (int, int) {
	if h == nil {
		return 0, 0
	}

	normalizedID := strings.TrimSpace(event.TokenPublicID)
	if normalizedID == "" {
		return 0, 0
	}

	h.mu.RLock()
	tokenSubscribers := h.subscribers[normalizedID]
	listeners := make([]*tokenStreamSubscriber, 0, len(tokenSubscribers))
	for _, subscriber := range tokenSubscribers {
		listeners = append(listeners, subscriber)
	}
	h.mu.RUnlock()

	delivered := 0
	for _, subscriber := range listeners {
		if subscriber.TrySend(event) {
			delivered++
		}
	}
	return delivered, len(listeners)
}
