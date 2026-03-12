package mailrelay

import (
	"context"
	"errors"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

const catchAllPermissionPolicyKey = "email_catch_all"

var (
	// ErrCatchAllAccessUnavailable means the route exists but the owner has
	// neither an active subscription window nor enough remaining prepaid count.
	ErrCatchAllAccessUnavailable = errors.New("catch-all access is unavailable")

	// ErrCatchAllDailyLimitExceeded means the owner already reached today's
	// effective per-user cap and new catch-all deliveries must wait for the next
	// UTC day.
	ErrCatchAllDailyLimitExceeded = errors.New("catch-all daily limit exceeded")
)

// CatchAllAccessStore is the minimum storage contract required to enforce
// catch-all runtime billing state during SMTP delivery.
type CatchAllAccessStore interface {
	GetPermissionPolicy(ctx context.Context, key string) (model.PermissionPolicy, error)
	ConsumeEmailCatchAll(ctx context.Context, input storage.ConsumeEmailCatchAllInput) (model.EmailCatchAllConsumeResult, error)
}

// CatchAllAccessManager reserves catch-all delivery allowance for accepted SMTP
// recipients before the message is forwarded upstream.
type CatchAllAccessManager interface {
	Consume(ctx context.Context, userID int64, count int64) error
}

// DBCatchAllAccessManager enforces catch-all delivery limits directly against
// the shared database state.
type DBCatchAllAccessManager struct {
	store CatchAllAccessStore
	now   func() time.Time
}

// NewDBCatchAllAccessManager constructs the database-backed access manager used
// by the built-in SMTP relay.
func NewDBCatchAllAccessManager(store CatchAllAccessStore) *DBCatchAllAccessManager {
	return &DBCatchAllAccessManager{
		store: store,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// Consume reserves catch-all delivery allowance for one owner.
func (m *DBCatchAllAccessManager) Consume(ctx context.Context, userID int64, count int64) error {
	if m == nil || m.store == nil || count <= 0 {
		return nil
	}

	policy, err := m.store.GetPermissionPolicy(ctx, catchAllPermissionPolicyKey)
	if err != nil {
		return err
	}

	defaultDailyLimit := policy.DefaultDailyLimit
	if defaultDailyLimit <= 0 {
		defaultDailyLimit = 1_000_000
	}

	_, err = m.store.ConsumeEmailCatchAll(ctx, storage.ConsumeEmailCatchAllInput{
		UserID:            userID,
		Count:             count,
		DefaultDailyLimit: defaultDailyLimit,
		Now:               m.now(),
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
		return err
	}
}
