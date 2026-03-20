package service

import (
	"context"
	"strings"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

type resolvedRouteTarget struct {
	TargetType          string
	TargetEmail         string
	TargetTokenPublicID string
	TargetTokenName     string
	TargetDisplay       string
	Configured          bool
}

func (s *PermissionService) resolveOwnedRouteTarget(
	ctx context.Context,
	user model.User,
	targetType string,
	targetEmail string,
	targetTokenPublicID string,
	allowEmpty bool,
) (resolvedRouteTarget, error) {
	normalizedType := normalizeRouteTargetType(targetType, targetTokenPublicID)
	switch normalizedType {
	case model.EmailRouteTargetKindAPIToken:
		if !s.cfg.UsesDatabaseMailRelay() {
			return resolvedRouteTarget{}, UnavailableError("api token email targets require the database mail relay mode", nil)
		}
		publicID := strings.TrimSpace(targetTokenPublicID)
		if publicID == "" {
			if allowEmpty {
				return resolvedRouteTarget{TargetType: model.EmailRouteTargetKindEmail}, nil
			}
			return resolvedRouteTarget{}, ValidationError("target_token_public_id must not be empty")
		}

		token, err := s.requireOwnedEmailCapableAPIToken(ctx, user, publicID)
		if err != nil {
			return resolvedRouteTarget{}, err
		}
		return resolvedRouteTarget{
			TargetType:          model.EmailRouteTargetKindAPIToken,
			TargetTokenPublicID: token.PublicID,
			TargetTokenName:     token.Name,
			TargetDisplay:       token.Name,
			Configured:          true,
		}, nil
	default:
		normalizedEmail, err := normalizeTargetEmail(targetEmail, allowEmpty)
		if err != nil {
			return resolvedRouteTarget{}, err
		}
		if normalizedEmail == "" {
			return resolvedRouteTarget{TargetType: model.EmailRouteTargetKindEmail}, nil
		}
		target, targetErr := s.requireVerifiedOwnedEmailTarget(ctx, user, normalizedEmail)
		if targetErr != nil {
			return resolvedRouteTarget{}, targetErr
		}
		return resolvedRouteTarget{
			TargetType:    model.EmailRouteTargetKindEmail,
			TargetEmail:   target.Email,
			TargetDisplay: target.Email,
			Configured:    true,
		}, nil
	}
}

func (s *PermissionService) requireOwnedEmailCapableAPIToken(ctx context.Context, user model.User, publicID string) (model.APIToken, error) {
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

func normalizeRouteTargetType(targetType string, targetTokenPublicID string) string {
	normalized := strings.ToLower(strings.TrimSpace(targetType))
	if normalized == "" && strings.TrimSpace(targetTokenPublicID) != "" {
		return model.EmailRouteTargetKindAPIToken
	}
	switch normalized {
	case model.EmailRouteTargetKindAPIToken:
		return model.EmailRouteTargetKindAPIToken
	default:
		return model.EmailRouteTargetKindEmail
	}
}

func routeTargetDisplayFromModel(route model.EmailRoute, token *model.APIToken) string {
	if normalizeRouteTargetType(route.TargetKind, route.TargetTokenPublicID) == model.EmailRouteTargetKindAPIToken {
		if token != nil && strings.TrimSpace(token.Name) != "" {
			return token.Name
		}
		if strings.TrimSpace(route.TargetTokenPublicID) != "" {
			return strings.TrimSpace(route.TargetTokenPublicID)
		}
		return ""
	}
	return strings.TrimSpace(route.TargetEmail)
}

func routeHasConfiguredTarget(route model.EmailRoute) bool {
	return strings.TrimSpace(route.TargetEmail) != "" || strings.TrimSpace(route.TargetTokenPublicID) != ""
}
