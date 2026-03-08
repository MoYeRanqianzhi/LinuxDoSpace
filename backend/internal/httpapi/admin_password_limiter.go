package httpapi

import (
	"sync"
	"time"
)

const (
	// adminPasswordMaxFailures limits how many incorrect second-factor password
	// attempts one session or client IP may make before the endpoint blocks them.
	adminPasswordMaxFailures = 5

	// adminPasswordBlockDuration is the enforced quiet period after one session or
	// client IP reaches the failed-attempt threshold.
	adminPasswordBlockDuration = 15 * time.Minute

	// adminPasswordStateTTL controls when stale limiter buckets are discarded from
	// memory so the process does not retain dead sessions forever.
	adminPasswordStateTTL = time.Hour
)

// adminPasswordAttemptState keeps the mutable counters for one limiter bucket.
type adminPasswordAttemptState struct {
	FailureCount int
	BlockedUntil time.Time
	LastSeenAt   time.Time
}

// adminPasswordLimiter tracks sensitive admin-password verification failures by
// both session ID and client IP so attackers cannot brute-force the endpoint by
// rotating only one side of the request identity.
type adminPasswordLimiter struct {
	mu            sync.Mutex
	maxFailures   int
	blockDuration time.Duration
	stateTTL      time.Duration
	bySessionID   map[string]adminPasswordAttemptState
	byClientIP    map[string]adminPasswordAttemptState
}

// newAdminPasswordLimiter constructs one in-memory limiter tuned for the admin
// second-factor password endpoint.
func newAdminPasswordLimiter(maxFailures int, blockDuration time.Duration, stateTTL time.Duration) *adminPasswordLimiter {
	return &adminPasswordLimiter{
		maxFailures:   maxFailures,
		blockDuration: blockDuration,
		stateTTL:      stateTTL,
		bySessionID:   make(map[string]adminPasswordAttemptState),
		byClientIP:    make(map[string]adminPasswordAttemptState),
	}
}

// Check reports whether the current session or client IP is still inside a
// temporary lockout window and returns the remaining block duration.
func (l *adminPasswordLimiter) Check(sessionID string, clientIP string, now time.Time) (time.Duration, bool) {
	if l == nil {
		return 0, false
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanup(now)
	blockedUntil := l.maxBlockedUntil(now, sessionID, clientIP)
	if blockedUntil.IsZero() {
		return 0, false
	}
	return blockedUntil.Sub(now), true
}

// RegisterFailure increments the failed-attempt counters for the current
// session and client IP after one incorrect admin password submission.
func (l *adminPasswordLimiter) RegisterFailure(sessionID string, clientIP string, now time.Time) {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanup(now)
	l.bySessionID = l.registerFailureForMap(l.bySessionID, sessionID, now)
	l.byClientIP = l.registerFailureForMap(l.byClientIP, clientIP, now)
}

// Reset clears the limiter state for the current session and client IP after a
// successful password verification so legitimate admins are not penalized by
// earlier mistakes.
func (l *adminPasswordLimiter) Reset(sessionID string, clientIP string) {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.bySessionID, sessionID)
	delete(l.byClientIP, clientIP)
}

// maxBlockedUntil returns the later block boundary across the two identity buckets.
func (l *adminPasswordLimiter) maxBlockedUntil(now time.Time, sessionID string, clientIP string) time.Time {
	var blockedUntil time.Time

	if state, ok := l.bySessionID[sessionID]; ok && state.BlockedUntil.After(now) {
		blockedUntil = state.BlockedUntil
	}
	if state, ok := l.byClientIP[clientIP]; ok && state.BlockedUntil.After(blockedUntil) {
		blockedUntil = state.BlockedUntil
	}

	return blockedUntil
}

// registerFailureForMap mutates one limiter bucket map in place and returns it
// so callers can share the same logic across session-ID and client-IP tracking.
func (l *adminPasswordLimiter) registerFailureForMap(items map[string]adminPasswordAttemptState, key string, now time.Time) map[string]adminPasswordAttemptState {
	if key == "" {
		return items
	}

	state := l.normalizeState(items[key], now)
	state.FailureCount++
	state.LastSeenAt = now
	if state.FailureCount >= l.maxFailures {
		state.FailureCount = 0
		state.BlockedUntil = now.Add(l.blockDuration)
	}
	items[key] = state
	return items
}

// cleanup discards long-idle limiter buckets so memory usage stays bounded.
func (l *adminPasswordLimiter) cleanup(now time.Time) {
	cutoff := now.Add(-l.stateTTL)
	for key, state := range l.bySessionID {
		normalized := l.normalizeState(state, now)
		if normalized.LastSeenAt.Before(cutoff) && !normalized.BlockedUntil.After(now) {
			delete(l.bySessionID, key)
			continue
		}
		l.bySessionID[key] = normalized
	}
	for key, state := range l.byClientIP {
		normalized := l.normalizeState(state, now)
		if normalized.LastSeenAt.Before(cutoff) && !normalized.BlockedUntil.After(now) {
			delete(l.byClientIP, key)
			continue
		}
		l.byClientIP[key] = normalized
	}
}

// normalizeState resets one expired block window so new failures start a fresh count.
func (l *adminPasswordLimiter) normalizeState(state adminPasswordAttemptState, now time.Time) adminPasswordAttemptState {
	if !state.BlockedUntil.IsZero() && !state.BlockedUntil.After(now) {
		state.BlockedUntil = time.Time{}
		state.FailureCount = 0
	}
	return state
}
