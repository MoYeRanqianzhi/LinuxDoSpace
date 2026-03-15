package httpapi

import (
	"context"
	"sync/atomic"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
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

const (
	adminPasswordBucketSession = "session"
	adminPasswordBucketIP      = "client_ip"
)

// adminPasswordAttemptStore is the narrow persistence contract needed by the
// admin second-factor limiter.
type adminPasswordAttemptStore interface {
	GetAdminPasswordAttempt(ctx context.Context, bucketType string, bucketKey string) (model.AdminPasswordAttempt, error)
	RegisterAdminPasswordFailure(ctx context.Context, bucketType string, bucketKey string, maxFailures int, blockDuration time.Duration, now time.Time) (model.AdminPasswordAttempt, error)
	DeleteAdminPasswordAttempt(ctx context.Context, bucketType string, bucketKey string) error
	DeleteStaleAdminPasswordAttempts(ctx context.Context, cutoff time.Time, now time.Time) error
}

// adminPasswordLimiter tracks sensitive admin-password verification failures by
// both session ID and client IP so attackers cannot brute-force the endpoint by
// rotating only one side of the request identity.
type adminPasswordLimiter struct {
	store         adminPasswordAttemptStore
	maxFailures   int
	blockDuration time.Duration
	stateTTL      time.Duration
	lastCleanupAt atomic.Int64
}

// newAdminPasswordLimiter constructs one in-memory limiter tuned for the admin
// second-factor password endpoint.
func newAdminPasswordLimiter(store adminPasswordAttemptStore, maxFailures int, blockDuration time.Duration, stateTTL time.Duration) *adminPasswordLimiter {
	return &adminPasswordLimiter{
		store:         store,
		maxFailures:   maxFailures,
		blockDuration: blockDuration,
		stateTTL:      stateTTL,
	}
}

// Check reports whether the current session or client IP is still inside a
// temporary lockout window and returns the remaining block duration.
func (l *adminPasswordLimiter) Check(ctx context.Context, sessionID string, clientIP string, now time.Time) (time.Duration, bool) {
	if l == nil {
		return 0, false
	}

	l.cleanup(ctx, now)
	blockedUntil := l.maxBlockedUntil(ctx, now, sessionID, clientIP)
	if blockedUntil.IsZero() {
		return 0, false
	}
	return blockedUntil.Sub(now), true
}

// RegisterFailure increments the failed-attempt counters for the current
// session and client IP after one incorrect admin password submission.
func (l *adminPasswordLimiter) RegisterFailure(ctx context.Context, sessionID string, clientIP string, now time.Time) {
	if l == nil {
		return
	}

	l.cleanup(ctx, now)
	l.registerFailureForKey(ctx, adminPasswordBucketSession, sessionID, now)
	l.registerFailureForKey(ctx, adminPasswordBucketIP, clientIP, now)
}

// Reset clears the limiter state for the current session and client IP after a
// successful password verification so legitimate admins are not penalized by
// earlier mistakes.
func (l *adminPasswordLimiter) Reset(ctx context.Context, sessionID string, clientIP string) {
	if l == nil {
		return
	}

	_ = l.deleteAttempt(ctx, adminPasswordBucketSession, sessionID)
	_ = l.deleteAttempt(ctx, adminPasswordBucketIP, clientIP)
}

// maxBlockedUntil returns the later block boundary across the two identity buckets.
func (l *adminPasswordLimiter) maxBlockedUntil(ctx context.Context, now time.Time, sessionID string, clientIP string) time.Time {
	var blockedUntil time.Time

	if state, ok := l.loadAttempt(ctx, adminPasswordBucketSession, sessionID, now); ok && state.BlockedUntil != nil && state.BlockedUntil.After(now) {
		blockedUntil = *state.BlockedUntil
	}
	if state, ok := l.loadAttempt(ctx, adminPasswordBucketIP, clientIP, now); ok && state.BlockedUntil != nil && state.BlockedUntil.After(blockedUntil) {
		blockedUntil = *state.BlockedUntil
	}

	return blockedUntil
}

// registerFailureForKey records one failed attempt for the target limiter
// bucket. Storage errors are ignored so the password endpoint still fails
// closed for the caller.
func (l *adminPasswordLimiter) registerFailureForKey(ctx context.Context, bucketType string, bucketKey string, now time.Time) {
	if l.store == nil || bucketKey == "" {
		return
	}
	_, _ = l.store.RegisterAdminPasswordFailure(ctx, bucketType, bucketKey, l.maxFailures, l.blockDuration, now)
}

// cleanup discards long-idle limiter buckets so memory usage stays bounded.
func (l *adminPasswordLimiter) cleanup(ctx context.Context, now time.Time) {
	if l.store == nil {
		return
	}

	cleanupInterval := l.stateTTL / 4
	if cleanupInterval <= 0 {
		cleanupInterval = time.Minute
	}

	lastCleanupAt := l.lastCleanupAt.Load()
	nowUnix := now.UTC().UnixNano()
	if lastCleanupAt != 0 && nowUnix-lastCleanupAt < cleanupInterval.Nanoseconds() {
		return
	}
	if !l.lastCleanupAt.CompareAndSwap(lastCleanupAt, nowUnix) {
		return
	}

	_ = l.store.DeleteStaleAdminPasswordAttempts(ctx, now.Add(-l.stateTTL), now)
}

// loadAttempt fetches one limiter bucket and normalizes expired block windows.
func (l *adminPasswordLimiter) loadAttempt(ctx context.Context, bucketType string, bucketKey string, now time.Time) (model.AdminPasswordAttempt, bool) {
	if l.store == nil || bucketKey == "" {
		return model.AdminPasswordAttempt{}, false
	}
	item, err := l.store.GetAdminPasswordAttempt(ctx, bucketType, bucketKey)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.AdminPasswordAttempt{}, false
		}
		return model.AdminPasswordAttempt{}, false
	}
	if item.BlockedUntil != nil && !item.BlockedUntil.After(now) {
		return item, true
	}
	return item, true
}

// deleteAttempt removes one persisted limiter bucket when verification
// succeeds.
func (l *adminPasswordLimiter) deleteAttempt(ctx context.Context, bucketType string, bucketKey string) error {
	if l.store == nil || bucketKey == "" {
		return nil
	}
	return l.store.DeleteAdminPasswordAttempt(ctx, bucketType, bucketKey)
}
