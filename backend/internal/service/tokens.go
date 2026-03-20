package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"linuxdospace/backend/internal/mailrelay"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/security"
	"linuxdospace/backend/internal/storage"
)

const (
	// APITokenTargetTypeEmail means the route forwards to one verified external mailbox.
	APITokenTargetTypeEmail = model.EmailRouteTargetKindEmail

	// APITokenTargetTypeAPIToken means the route forwards to one live API token stream.
	APITokenTargetTypeAPIToken = model.EmailRouteTargetKindAPIToken
)

// UserAPITokenView is the public frontend representation of one user-managed token.
type UserAPITokenView struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	PublicID     string     `json:"public_id"`
	Scopes       []string   `json:"scopes"`
	EmailEnabled bool       `json:"email_enabled"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
}

// CreateMyAPITokenRequest describes one new token request from the public frontend.
type CreateMyAPITokenRequest struct {
	Name         string `json:"name"`
	EmailEnabled bool   `json:"email_enabled"`
}

// CreateMyAPITokenResult returns the persisted token metadata and the one-time raw secret.
type CreateMyAPITokenResult struct {
	Token    UserAPITokenView `json:"token"`
	RawToken string           `json:"raw_token"`
}

// TokenService owns user-managed API tokens and the live email stream hub.
type TokenService struct {
	db  Store
	hub *mailrelay.TokenStreamHub
}

// NewTokenService creates the user-facing token-management service.
func NewTokenService(db Store, hub *mailrelay.TokenStreamHub) *TokenService {
	return &TokenService{db: db, hub: hub}
}

// ListMyAPITokens returns the current user's tokens, newest first.
func (s *TokenService) ListMyAPITokens(ctx context.Context, user model.User) ([]UserAPITokenView, error) {
	items, err := s.db.ListAPITokensByOwner(ctx, user.ID)
	if err != nil {
		return nil, InternalError("failed to list api tokens", err)
	}

	views := make([]UserAPITokenView, 0, len(items))
	for _, item := range items {
		views = append(views, userAPITokenFromModel(item))
	}
	return views, nil
}

// CreateMyAPIToken issues one new bearer token for the current user.
func (s *TokenService) CreateMyAPIToken(ctx context.Context, user model.User, request CreateMyAPITokenRequest) (CreateMyAPITokenResult, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return CreateMyAPITokenResult{}, ValidationError("token name must not be empty")
	}
	if len([]rune(name)) > 64 {
		return CreateMyAPITokenResult{}, ValidationError("token name must be 64 characters or fewer")
	}

	scopes := []string{model.APITokenScopeEmail}

	publicSuffix, err := security.RandomToken(8)
	if err != nil {
		return CreateMyAPITokenResult{}, InternalError("failed to generate token public id", err)
	}
	secret, err := security.RandomToken(32)
	if err != nil {
		return CreateMyAPITokenResult{}, InternalError("failed to generate token secret", err)
	}

	publicID := "ldt_" + strings.ToLower(strings.Trim(strings.ReplaceAll(publicSuffix, "-", ""), "_"))
	rawToken := "lds_pat_" + publicID + "." + secret
	tokenHash := hashAPIToken(rawToken)

	item, err := s.db.CreateAPIToken(ctx, storage.CreateAPITokenInput{
		OwnerUserID: user.ID,
		Name:        name,
		PublicID:    publicID,
		TokenHash:   tokenHash,
		Scopes:      scopes,
	})
	if err != nil {
		return CreateMyAPITokenResult{}, InternalError("failed to create api token", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"api_token_id": item.ID,
		"public_id":    item.PublicID,
		"scopes":       item.Scopes,
	})
	logAuditWriteFailure("api_token.create", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "api_token.create",
		ResourceType: "api_token",
		ResourceID:   item.PublicID,
		MetadataJSON: string(metadata),
	}))

	return CreateMyAPITokenResult{
		Token:    userAPITokenFromModel(item),
		RawToken: rawToken,
	}, nil
}

// RevokeMyAPIToken disables one existing token owned by the current user.
func (s *TokenService) RevokeMyAPIToken(ctx context.Context, user model.User, publicID string) (UserAPITokenView, error) {
	item, err := s.db.GetAPITokenByPublicID(ctx, strings.TrimSpace(publicID))
	if err != nil {
		if storage.IsNotFound(err) {
			return UserAPITokenView{}, NotFoundError("api token not found")
		}
		return UserAPITokenView{}, InternalError("failed to load api token", err)
	}
	if item.OwnerUserID != user.ID {
		return UserAPITokenView{}, NotFoundError("api token not found")
	}
	if item.RevokedAt != nil {
		return userAPITokenFromModel(item), nil
	}

	now := time.Now().UTC()
	item, err = s.db.UpdateAPIToken(ctx, storage.UpdateAPITokenInput{
		ID:        item.ID,
		RevokedAt: &now,
	})
	if err != nil {
		return UserAPITokenView{}, InternalError("failed to revoke api token", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"api_token_id": item.ID,
		"public_id":    item.PublicID,
	})
	logAuditWriteFailure("api_token.revoke", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "api_token.revoke",
		ResourceType: "api_token",
		ResourceID:   item.PublicID,
		MetadataJSON: string(metadata),
	}))

	return userAPITokenFromModel(item), nil
}

// AuthenticateEmailStreamToken validates one raw bearer token for the live email stream.
func (s *TokenService) AuthenticateEmailStreamToken(ctx context.Context, rawToken string) (model.APIToken, error) {
	tokenHash := hashAPIToken(rawToken)
	item, err := s.db.GetAPITokenByTokenHash(ctx, tokenHash)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.APIToken{}, UnauthorizedError("invalid api token")
		}
		return model.APIToken{}, InternalError("failed to load api token", err)
	}
	if item.RevokedAt != nil {
		return model.APIToken{}, UnauthorizedError("api token has been revoked")
	}
	if !apiTokenHasScope(item, model.APITokenScopeEmail) {
		return model.APIToken{}, ForbiddenError("api token does not allow email streaming")
	}

	now := time.Now().UTC()
	if _, err := s.db.UpdateAPIToken(ctx, storage.UpdateAPITokenInput{
		ID:         item.ID,
		LastUsedAt: &now,
	}); err != nil {
		return model.APIToken{}, InternalError("failed to update api token last_used_at", err)
	}
	item.LastUsedAt = &now
	return item, nil
}

// Hub returns the live email-stream broker used by the HTTP stream endpoint and SMTP relay.
func (s *TokenService) Hub() *mailrelay.TokenStreamHub {
	return s.hub
}

// RequireOwnedEmailAPIToken validates that the given token exists, belongs to the
// current user, is active, and supports the email stream capability.
func (s *TokenService) RequireOwnedEmailAPIToken(ctx context.Context, user model.User, publicID string) (model.APIToken, error) {
	item, err := s.db.GetAPITokenByPublicID(ctx, strings.TrimSpace(publicID))
	if err != nil {
		if storage.IsNotFound(err) {
			return model.APIToken{}, ValidationError("target_token_public_id is invalid")
		}
		return model.APIToken{}, InternalError("failed to load target api token", err)
	}
	if item.OwnerUserID != user.ID {
		return model.APIToken{}, ValidationError("target api token does not belong to the current user")
	}
	if item.RevokedAt != nil {
		return model.APIToken{}, ValidationError("target api token has been revoked")
	}
	if !apiTokenHasScope(item, model.APITokenScopeEmail) {
		return model.APIToken{}, ValidationError("target api token does not allow email streaming")
	}
	return item, nil
}

func userAPITokenFromModel(item model.APIToken) UserAPITokenView {
	return UserAPITokenView{
		ID:           item.ID,
		Name:         item.Name,
		PublicID:     item.PublicID,
		Scopes:       append([]string(nil), item.Scopes...),
		EmailEnabled: apiTokenHasScope(item, model.APITokenScopeEmail),
		LastUsedAt:   item.LastUsedAt,
		CreatedAt:    item.CreatedAt,
		UpdatedAt:    item.UpdatedAt,
		RevokedAt:    item.RevokedAt,
	}
}

func apiTokenHasScope(item model.APIToken, scope string) bool {
	normalizedScope := strings.ToLower(strings.TrimSpace(scope))
	if normalizedScope == "" {
		return false
	}
	for _, itemScope := range item.Scopes {
		if strings.ToLower(strings.TrimSpace(itemScope)) == normalizedScope {
			return true
		}
	}
	return false
}

func hashAPIToken(rawToken string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(rawToken)))
	return hex.EncodeToString(sum[:])
}
