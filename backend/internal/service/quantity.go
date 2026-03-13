package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

const (
	// QuantitySourceAdminManual marks one quantity delta that was created
	// directly by an administrator through the backend API.
	QuantitySourceAdminManual = "admin_manual"
)

// QuantityService owns the append-only quantity ledger used to prepare future
// billing and redeem-code flows without coupling them to one fixed product type.
type QuantityService struct {
	db Store
}

// AdminCreateQuantityRecordRequest describes one administrator-authored ledger
// mutation for a target user.
type AdminCreateQuantityRecordRequest struct {
	ResourceKey   string     `json:"resource_key"`
	Scope         string     `json:"scope"`
	Delta         int        `json:"delta"`
	Source        string     `json:"source"`
	Reason        string     `json:"reason"`
	ReferenceType string     `json:"reference_type"`
	ReferenceID   string     `json:"reference_id"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

// NewQuantityService constructs the quantity-ledger service.
func NewQuantityService(db Store) *QuantityService {
	return &QuantityService{db: db}
}

// ListMyQuantityRecords returns the current user's full quantity ledger.
func (s *QuantityService) ListMyQuantityRecords(ctx context.Context, user model.User) ([]model.QuantityRecord, error) {
	return s.db.ListQuantityRecordsByUser(ctx, user.ID)
}

// ListMyQuantityBalances returns the current user's non-expired balances
// grouped by resource key and scope.
func (s *QuantityService) ListMyQuantityBalances(ctx context.Context, user model.User) ([]model.QuantityBalance, error) {
	return s.db.ListQuantityBalancesByUser(ctx, user.ID, time.Now().UTC())
}

// ListQuantityRecordsForUser returns the target user's ledger for administrator
// inspection after first confirming the user exists.
func (s *QuantityService) ListQuantityRecordsForUser(ctx context.Context, userID int64) ([]model.QuantityRecord, error) {
	if _, err := s.requireQuantityTargetUser(ctx, userID); err != nil {
		return nil, err
	}
	return s.db.ListQuantityRecordsByUser(ctx, userID)
}

// ListQuantityBalancesForUser returns the target user's current non-expired
// balances for administrator inspection.
func (s *QuantityService) ListQuantityBalancesForUser(ctx context.Context, userID int64) ([]model.QuantityBalance, error) {
	if _, err := s.requireQuantityTargetUser(ctx, userID); err != nil {
		return nil, err
	}
	return s.db.ListQuantityBalancesByUser(ctx, userID, time.Now().UTC())
}

// CreateQuantityRecord appends one validated ledger delta for the target user.
func (s *QuantityService) CreateQuantityRecord(ctx context.Context, actor model.User, userID int64, request AdminCreateQuantityRecordRequest) (model.QuantityRecord, error) {
	targetUser, err := s.requireQuantityTargetUser(ctx, userID)
	if err != nil {
		return model.QuantityRecord{}, err
	}

	resourceKey := normalizeQuantityKey(strings.TrimSpace(request.ResourceKey))
	if resourceKey == "" {
		return model.QuantityRecord{}, ValidationError("resource_key is required and must contain only lowercase letters, digits, underscores, dots, or hyphens")
	}

	source := normalizeQuantityKey(strings.TrimSpace(request.Source))
	if strings.TrimSpace(request.Source) == "" {
		source = QuantitySourceAdminManual
	} else if source == "" {
		return model.QuantityRecord{}, ValidationError("source may only contain lowercase letters, digits, underscores, dots, or hyphens")
	}

	referenceType := normalizeOptionalQuantityKey(strings.TrimSpace(request.ReferenceType))
	if strings.TrimSpace(request.ReferenceType) != "" && referenceType == "" {
		return model.QuantityRecord{}, ValidationError("reference_type may only contain lowercase letters, digits, underscores, dots, or hyphens")
	}

	scope := strings.ToLower(strings.TrimSpace(request.Scope))
	reason := strings.TrimSpace(request.Reason)
	referenceID := strings.TrimSpace(request.ReferenceID)
	if request.Delta == 0 {
		return model.QuantityRecord{}, ValidationError("delta must not be 0")
	}
	if reason == "" {
		return model.QuantityRecord{}, ValidationError("reason is required")
	}

	var expiresAt *time.Time
	if request.ExpiresAt != nil {
		value := request.ExpiresAt.UTC()
		if !value.After(time.Now().UTC()) {
			return model.QuantityRecord{}, ValidationError("expires_at must be in the future")
		}
		expiresAt = &value
	}

	item, err := s.db.CreateQuantityRecord(ctx, storage.CreateQuantityRecordInput{
		UserID:          targetUser.ID,
		ResourceKey:     resourceKey,
		Scope:           scope,
		Delta:           request.Delta,
		Source:          source,
		Reason:          reason,
		ReferenceType:   referenceType,
		ReferenceID:     referenceID,
		ExpiresAt:       expiresAt,
		CreatedByUserID: &actor.ID,
	})
	if err != nil {
		return model.QuantityRecord{}, InternalError("failed to create quantity record", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"quantity_record_id": item.ID,
		"target_user_id":     item.UserID,
		"resource_key":       item.ResourceKey,
		"scope":              item.Scope,
		"delta":              item.Delta,
		"source":             item.Source,
		"reference_type":     item.ReferenceType,
		"reference_id":       item.ReferenceID,
	})
	logAuditWriteFailure("admin.quantity_record.create", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.quantity_record.create",
		ResourceType: "quantity_record",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}))

	return item, nil
}

// requireQuantityTargetUser resolves one target user id into the persisted user
// row and normalizes not-found errors for the HTTP layer.
func (s *QuantityService) requireQuantityTargetUser(ctx context.Context, userID int64) (model.User, error) {
	user, err := s.db.GetUserByID(ctx, userID)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.User{}, NotFoundError("target user not found")
		}
		return model.User{}, InternalError("failed to load target user", err)
	}
	return user, nil
}

// normalizeQuantityKey validates one stored ledger token. The same restricted
// character set is used for resource keys and sources so later billing code can
// safely match on exact machine-readable identifiers.
func normalizeQuantityKey(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return ""
	}
	for _, runeValue := range normalized {
		switch {
		case runeValue >= 'a' && runeValue <= 'z':
		case runeValue >= '0' && runeValue <= '9':
		case runeValue == '_', runeValue == '-', runeValue == '.':
		default:
			return ""
		}
	}
	return normalized
}

// normalizeOptionalQuantityKey mirrors normalizeQuantityKey but preserves the
// empty value for optional fields such as reference_type.
func normalizeOptionalQuantityKey(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	return normalizeQuantityKey(trimmed)
}
