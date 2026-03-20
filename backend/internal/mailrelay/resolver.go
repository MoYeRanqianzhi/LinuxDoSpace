package mailrelay

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

const (
	// catchAllRoutePrefix reuses the existing database contract already used by
	// the service layer: exact mailbox aliases are stored by local-part, while
	// one namespace catch-all route is stored under the synthetic `catch-all`
	// prefix for the routed domain.
	catchAllRoutePrefix = "catch-all"
)

var (
	// ErrInvalidRecipient means the SMTP envelope recipient is malformed and
	// therefore cannot be matched against any saved route.
	ErrInvalidRecipient = errors.New("invalid recipient address")

	// ErrNoMatchingRoute means neither an exact mailbox route nor a namespace
	// catch-all route exists for the requested recipient.
	ErrNoMatchingRoute = errors.New("no matching route")

	// ErrRouteDisabled means a database route exists but is intentionally turned
	// off, so the relay must fail closed instead of forwarding silently.
	ErrRouteDisabled = errors.New("matching route is disabled")

	// ErrRouteHasNoTarget means the stored route exists but has no target inbox.
	ErrRouteHasNoTarget = errors.New("matching route has no target email")

	// ErrTargetNotVerified means the target email is known locally but has not
	// completed the ownership-verification workflow yet.
	ErrTargetNotVerified = errors.New("target email is not verified")

	// ErrTargetOwnershipMismatch means the target inbox is bound to another user
	// and should never be used by the current route owner.
	ErrTargetOwnershipMismatch = errors.New("target email belongs to another user")

	// ErrTargetTokenUnavailable means the stored API-token target is missing,
	// revoked, or no longer allowed to receive email stream traffic.
	ErrTargetTokenUnavailable = errors.New("target api token is unavailable")

	// ErrForwardingDailyLimitExceeded means one route owner already consumed the
	// hidden per-day forwarding cap enforced by the local SMTP relay.
	ErrForwardingDailyLimitExceeded = errors.New("mail forwarding daily limit exceeded")
)

// ResolverStore is the minimum storage contract required by the database-backed
// recipient resolver. Both SQLite and PostgreSQL stores already satisfy it.
type ResolverStore interface {
	GetEmailRouteByAddress(ctx context.Context, rootDomain string, prefix string) (model.EmailRoute, error)
	GetEmailTargetByEmail(ctx context.Context, email string) (model.EmailTarget, error)
	GetAPITokenByPublicID(ctx context.Context, publicID string) (model.APIToken, error)
}

// RecipientResolver turns one SMTP envelope recipient into the target inbox
// that should ultimately receive the forwarded message.
type RecipientResolver interface {
	ResolveRecipient(ctx context.Context, recipient string) (ResolvedRecipient, error)
}

// DBResolver resolves recipients purely from the local database so LinuxDoSpace
// no longer depends on Cloudflare Email Routing for subdomain catch-all mail.
type DBResolver struct {
	store ResolverStore
}

// ResolvedRecipient is the routing result for one accepted SMTP recipient.
type ResolvedRecipient struct {
	OriginalRecipient   string
	LocalPart           string
	Domain              string
	TargetKind          string
	TargetEmail         string
	TargetTokenPublicID string
	RouteID             int64
	RouteOwnerUserID    int64
	RoutePrefix         string
	UsedCatchAll        bool
}

// NewDBResolver constructs the database-backed recipient resolver used by the
// built-in SMTP relay.
func NewDBResolver(store ResolverStore) *DBResolver {
	return &DBResolver{store: store}
}

// ResolveRecipient accepts one envelope recipient, normalizes it, tries an
// exact mailbox lookup first, and only falls back to the namespace catch-all
// row when no exact route exists.
func (r *DBResolver) ResolveRecipient(ctx context.Context, recipient string) (ResolvedRecipient, error) {
	localPart, domain, err := normalizeEnvelopeAddress(recipient)
	if err != nil {
		return ResolvedRecipient{}, err
	}

	exactRoute, err := r.store.GetEmailRouteByAddress(ctx, domain, localPart)
	switch {
	case err == nil:
		return r.resolveStoredRoute(ctx, recipient, localPart, domain, exactRoute, false)
	case storage.IsNotFound(err):
		// Continue to the catch-all lookup below.
	default:
		return ResolvedRecipient{}, fmt.Errorf("load exact route for %s: %w", recipient, err)
	}

	catchAllRoute, err := r.store.GetEmailRouteByAddress(ctx, domain, catchAllRoutePrefix)
	switch {
	case err == nil:
		return r.resolveStoredRoute(ctx, recipient, localPart, domain, catchAllRoute, true)
	case storage.IsNotFound(err):
		return ResolvedRecipient{}, fmt.Errorf("%w: %s", ErrNoMatchingRoute, recipient)
	default:
		return ResolvedRecipient{}, fmt.Errorf("load catch-all route for %s: %w", recipient, err)
	}
}

// resolveStoredRoute validates the stored route state and enforces target-email
// ownership rules before the SMTP server accepts the recipient.
func (r *DBResolver) resolveStoredRoute(ctx context.Context, originalRecipient string, localPart string, domain string, route model.EmailRoute, usedCatchAll bool) (ResolvedRecipient, error) {
	if !route.Enabled {
		return ResolvedRecipient{}, fmt.Errorf("%w: %s", ErrRouteDisabled, originalRecipient)
	}

	targetKind := strings.TrimSpace(route.TargetKind)
	if targetKind == "" {
		targetKind = model.EmailRouteTargetKindEmail
	}

	targetEmail := ""
	targetTokenPublicID := ""
	switch targetKind {
	case model.EmailRouteTargetKindAPIToken:
		targetTokenPublicID = strings.TrimSpace(route.TargetTokenPublicID)
		if targetTokenPublicID == "" {
			return ResolvedRecipient{}, fmt.Errorf("%w: %s", ErrTargetTokenUnavailable, originalRecipient)
		}
		token, err := r.store.GetAPITokenByPublicID(ctx, targetTokenPublicID)
		switch {
		case err == nil:
			if token.OwnerUserID != route.OwnerUserID {
				return ResolvedRecipient{}, fmt.Errorf("%w: route owner=%d token owner=%d token=%s", ErrTargetOwnershipMismatch, route.OwnerUserID, token.OwnerUserID, targetTokenPublicID)
			}
			if token.RevokedAt != nil || !apiTokenSupportsEmail(token) {
				return ResolvedRecipient{}, fmt.Errorf("%w: %s", ErrTargetTokenUnavailable, targetTokenPublicID)
			}
		case storage.IsNotFound(err):
			return ResolvedRecipient{}, fmt.Errorf("%w: %s", ErrTargetTokenUnavailable, targetTokenPublicID)
		default:
			return ResolvedRecipient{}, fmt.Errorf("load target api token for %s: %w", targetTokenPublicID, err)
		}
	default:
		targetKind = model.EmailRouteTargetKindEmail
		targetEmail = strings.ToLower(strings.TrimSpace(route.TargetEmail))
		if targetEmail == "" {
			return ResolvedRecipient{}, fmt.Errorf("%w: %s", ErrRouteHasNoTarget, originalRecipient)
		}

		target, err := r.store.GetEmailTargetByEmail(ctx, targetEmail)
		switch {
		case err == nil:
			if target.OwnerUserID != route.OwnerUserID {
				return ResolvedRecipient{}, fmt.Errorf("%w: route owner=%d target owner=%d target=%s", ErrTargetOwnershipMismatch, route.OwnerUserID, target.OwnerUserID, targetEmail)
			}
			if target.VerifiedAt == nil {
				return ResolvedRecipient{}, fmt.Errorf("%w: %s", ErrTargetNotVerified, targetEmail)
			}
		case storage.IsNotFound(err):
			// Legacy administrator-managed routes may legitimately point at a target
			// address that predates the email_targets ownership table.
		default:
			return ResolvedRecipient{}, fmt.Errorf("load target email binding for %s: %w", targetEmail, err)
		}
	}

	return ResolvedRecipient{
		OriginalRecipient:   strings.ToLower(strings.TrimSpace(originalRecipient)),
		LocalPart:           localPart,
		Domain:              domain,
		TargetKind:          targetKind,
		TargetEmail:         targetEmail,
		TargetTokenPublicID: targetTokenPublicID,
		RouteID:             route.ID,
		RouteOwnerUserID:    route.OwnerUserID,
		RoutePrefix:         route.Prefix,
		UsedCatchAll:        usedCatchAll,
	}, nil
}

func apiTokenSupportsEmail(token model.APIToken) bool {
	for _, scope := range token.Scopes {
		if strings.EqualFold(strings.TrimSpace(scope), model.APITokenScopeEmail) {
			return true
		}
	}
	return false
}

// normalizeEnvelopeAddress converts one SMTP envelope address into the exact
// normalized local-part + domain pair used by the database.
func normalizeEnvelopeAddress(raw string) (string, string, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.TrimPrefix(normalized, "<")
	normalized = strings.TrimSuffix(normalized, ">")

	localPart, domain, ok := strings.Cut(normalized, "@")
	if !ok || strings.TrimSpace(localPart) == "" || strings.TrimSpace(domain) == "" {
		return "", "", fmt.Errorf("%w: %s", ErrInvalidRecipient, raw)
	}
	if strings.ContainsAny(localPart, " \r\n\t") || strings.ContainsAny(domain, " \r\n\t") {
		return "", "", fmt.Errorf("%w: %s", ErrInvalidRecipient, raw)
	}

	return localPart, domain, nil
}
