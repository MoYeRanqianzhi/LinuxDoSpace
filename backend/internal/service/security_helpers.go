package service

import (
	"crypto/subtle"
	"log"
	"strings"
	"time"

	"linuxdospace/backend/internal/config"

	"golang.org/x/crypto/bcrypt"
)

// AdminVerificationIsFresh reports whether one administrator password check is
// still inside the configured freshness window.
func AdminVerificationIsFresh(verifiedAt *time.Time, ttl time.Duration, now time.Time) bool {
	if verifiedAt == nil {
		return false
	}
	if ttl <= 0 {
		return false
	}
	return verifiedAt.UTC().Add(ttl).After(now.UTC())
}

// logAuditWriteFailure downgrades post-success audit write failures to operator
// logs so successful user-visible mutations do not turn into false 500 errors.
func logAuditWriteFailure(action string, err error) {
	if err == nil {
		return
	}
	log.Printf("audit log write failed for %s: %v", action, err)
}

// logPostMutationFailure keeps non-critical post-success bookkeeping failures
// from surfacing as false-negative 500 responses after the main mutation
// already committed.
func logPostMutationFailure(action string, err error) {
	if err == nil {
		return
	}
	log.Printf("post-mutation bookkeeping failed for %s: %v", action, err)
}

// VerifyAdminPasswordAgainstConfig validates the submitted administrator
// second-factor password against either the per-admin bcrypt hash map or the
// legacy shared plaintext fallback. Per-admin hashes win when configured for
// the current username so deployments can roll out stronger isolation without
// breaking the existing login form.
func VerifyAdminPasswordAgainstConfig(app config.AppConfig, username string, password string) bool {
	normalizedUsername := strings.ToLower(strings.TrimSpace(username))
	if normalizedUsername == "" || strings.TrimSpace(password) == "" {
		return false
	}

	if expectedHash, ok := app.AdminPasswordHashes[normalizedUsername]; ok {
		return bcrypt.CompareHashAndPassword([]byte(expectedHash), []byte(password)) == nil
	}

	expected := strings.TrimSpace(app.AdminPassword)
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(password), []byte(expected)) == 1
}
