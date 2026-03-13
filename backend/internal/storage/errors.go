package storage

import (
	"database/sql"
	"errors"
)

var (
	// ErrEmailCatchAllDailyLimitExceeded means one catch-all delivery would push
	// the user over the effective single-day maximum.
	ErrEmailCatchAllDailyLimitExceeded = errors.New("email catch-all daily limit exceeded")

	// ErrEmailCatchAllInsufficientRemainingCount means the user has no active
	// subscription and not enough prepaid remaining count to accept the mail.
	ErrEmailCatchAllInsufficientRemainingCount = errors.New("email catch-all remaining count is insufficient")
)

// IsNotFound reports whether one storage call failed only because the target
// row does not exist.
//
// The helper currently normalizes to `sql.ErrNoRows` so existing SQLite code
// and future PostgreSQL code can share one storage-agnostic check.
func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
