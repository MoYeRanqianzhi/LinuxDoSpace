package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

// powRuntimeSettings collects the persisted PoW configuration needed by one
// request path so feature checks stay consistent inside that request.
type powRuntimeSettings struct {
	Global            model.POWGlobalSettings
	BenefitByKey      map[string]model.POWBenefitSettings
	DifficultyByValue map[int]model.POWDifficultySettings
}

// AdminPOWGlobalSettingsView is the administrator-facing PoW feature summary.
type AdminPOWGlobalSettingsView struct {
	Enabled                     bool      `json:"enabled"`
	DefaultDailyCompletionLimit int       `json:"default_daily_completion_limit"`
	BaseRewardMin               int       `json:"base_reward_min"`
	BaseRewardMax               int       `json:"base_reward_max"`
	CreatedAt                   time.Time `json:"created_at"`
	UpdatedAt                   time.Time `json:"updated_at"`
}

// AdminPOWBenefitSettingsView describes one benefit row on the administrator PoW page.
type AdminPOWBenefitSettingsView struct {
	Key         string    `json:"key"`
	DisplayName string    `json:"display_name"`
	Description string    `json:"description"`
	RewardUnit  string    `json:"reward_unit"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AdminPOWDifficultySettingsView describes one difficulty row on the administrator PoW page.
type AdminPOWDifficultySettingsView struct {
	Difficulty       int       `json:"difficulty"`
	Label            string    `json:"label"`
	Description      string    `json:"description"`
	RewardMultiplier int       `json:"reward_multiplier"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// AdminPOWSettingsView groups the whole PoW configuration editor payload.
type AdminPOWSettingsView struct {
	Global       AdminPOWGlobalSettingsView       `json:"global"`
	Benefits     []AdminPOWBenefitSettingsView    `json:"benefits"`
	Difficulties []AdminPOWDifficultySettingsView `json:"difficulties"`
}

// AdminUpdatePOWGlobalSettingsRequest describes one administrator update to the
// global PoW feature configuration.
type AdminUpdatePOWGlobalSettingsRequest struct {
	Enabled                     *bool `json:"enabled,omitempty"`
	DefaultDailyCompletionLimit *int  `json:"default_daily_completion_limit,omitempty"`
	BaseRewardMin               *int  `json:"base_reward_min,omitempty"`
	BaseRewardMax               *int  `json:"base_reward_max,omitempty"`
}

// AdminUpdatePOWBenefitSettingsRequest describes one administrator toggle for a
// single benefit row.
type AdminUpdatePOWBenefitSettingsRequest struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// AdminUpdatePOWDifficultySettingsRequest describes one administrator toggle
// for a single difficulty row.
type AdminUpdatePOWDifficultySettingsRequest struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// AdminPOWUserSettingsView describes the current per-user daily PoW settings.
type AdminPOWUserSettingsView struct {
	UserID                        int64     `json:"user_id"`
	DailyCompletionLimitOverride  *int      `json:"daily_completion_limit_override,omitempty"`
	EffectiveDailyCompletionLimit int       `json:"effective_daily_completion_limit"`
	CompletedToday                int       `json:"completed_today"`
	RemainingToday                int       `json:"remaining_today"`
	CreatedAt                     time.Time `json:"created_at"`
	UpdatedAt                     time.Time `json:"updated_at"`
}

// AdminUpdatePOWUserSettingsRequest describes one administrator-managed per-user
// daily completion limit override.
type AdminUpdatePOWUserSettingsRequest struct {
	DailyCompletionLimitOverride      *int `json:"daily_completion_limit_override,omitempty"`
	ClearDailyCompletionLimitOverride bool `json:"clear_daily_completion_limit_override"`
}

func (s *POWService) loadRuntimeSettings(ctx context.Context) (powRuntimeSettings, error) {
	globalSettings, err := s.loadGlobalSettings(ctx)
	if err != nil {
		return powRuntimeSettings{}, err
	}

	benefits, err := s.db.ListPOWBenefitSettings(ctx)
	if err != nil {
		return powRuntimeSettings{}, InternalError("failed to load proof-of-work benefit settings", err)
	}
	benefitByKey := make(map[string]model.POWBenefitSettings, len(benefits))
	for _, item := range benefits {
		benefitByKey[item.Key] = item
	}

	difficulties, err := s.db.ListPOWDifficultySettings(ctx)
	if err != nil {
		return powRuntimeSettings{}, InternalError("failed to load proof-of-work difficulty settings", err)
	}
	difficultyByValue := make(map[int]model.POWDifficultySettings, len(difficulties))
	for _, item := range difficulties {
		difficultyByValue[item.Difficulty] = item
	}

	return powRuntimeSettings{
		Global:            globalSettings,
		BenefitByKey:      benefitByKey,
		DifficultyByValue: difficultyByValue,
	}, nil
}

func (s *POWService) loadGlobalSettings(ctx context.Context) (model.POWGlobalSettings, error) {
	item, err := s.db.GetPOWGlobalSettings(ctx)
	if err == nil {
		return item, nil
	}
	if !storage.IsNotFound(err) {
		return model.POWGlobalSettings{}, InternalError("failed to load proof-of-work global settings", err)
	}
	now := time.Now().UTC()
	return model.POWGlobalSettings{
		ID:                          1,
		Enabled:                     true,
		DefaultDailyCompletionLimit: powDefaultDailyCompletionLimit,
		BaseRewardMin:               powDefaultBaseRewardMin,
		BaseRewardMax:               powDefaultBaseRewardMax,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}, nil
}

func (s *POWService) loadEffectiveDailyCompletionLimit(ctx context.Context, userID int64, global model.POWGlobalSettings) (int, error) {
	userSettings, err := s.db.GetPOWUserSettings(ctx, userID)
	switch {
	case err == nil:
		if userSettings.DailyCompletionLimitOverride != nil && *userSettings.DailyCompletionLimitOverride > 0 {
			return *userSettings.DailyCompletionLimitOverride, nil
		}
	case storage.IsNotFound(err):
	default:
		return 0, InternalError("failed to load user proof-of-work settings", err)
	}

	if global.DefaultDailyCompletionLimit > 0 {
		return global.DefaultDailyCompletionLimit, nil
	}
	return powDefaultDailyCompletionLimit, nil
}

func (s *POWService) benefitOptions(settings powRuntimeSettings) []POWBenefitOptionView {
	items := make([]POWBenefitOptionView, 0, len(powBenefitCatalog))
	for _, benefit := range powBenefitCatalog {
		itemSettings, exists := settings.BenefitByKey[benefit.Key]
		enabled := !exists || itemSettings.Enabled
		items = append(items, POWBenefitOptionView{
			Key:         benefit.Key,
			DisplayName: benefit.DisplayName,
			Description: benefit.Description,
			RewardUnit:  benefit.RewardUnit,
			Enabled:     enabled,
		})
	}
	return items
}

func (s *POWService) difficultyOptions(settings powRuntimeSettings) []POWDifficultyOptionView {
	items := make([]POWDifficultyOptionView, 0, len(powSupportedDifficulties))
	for _, difficulty := range powSupportedDifficulties {
		itemSettings, exists := settings.DifficultyByValue[difficulty]
		enabled := !exists || itemSettings.Enabled
		items = append(items, POWDifficultyOptionView{
			Value:            difficulty,
			Label:            fmt.Sprintf("难度 %d", difficulty),
			Description:      fmt.Sprintf("要求 Argon2 输出至少有 %d 个前导零 bit，奖励倍数也是 %d。", difficulty, difficulty),
			RewardMultiplier: difficulty,
			Enabled:          enabled,
		})
	}
	return items
}

func lookupPOWBenefitDefinition(key string) (powBenefitDefinition, bool) {
	normalizedKey := strings.TrimSpace(key)
	for _, benefit := range powBenefitCatalog {
		if benefit.Key == normalizedKey {
			return benefit, true
		}
	}
	return powBenefitDefinition{}, false
}

func (s *POWService) requireBenefitDefinition(key string, settings powRuntimeSettings) (powBenefitDefinition, error) {
	benefit, ok := lookupPOWBenefitDefinition(key)
	if !ok {
		return powBenefitDefinition{}, ValidationError("unsupported proof-of-work benefit")
	}
	if itemSettings, exists := settings.BenefitByKey[benefit.Key]; exists && !itemSettings.Enabled {
		return powBenefitDefinition{}, ForbiddenError("the selected proof-of-work benefit is currently disabled")
	}
	return benefit, nil
}

func (s *POWService) requireEnabledDifficulty(raw int, settings powRuntimeSettings) (int, error) {
	difficulty, err := normalizePOWDifficulty(raw)
	if err != nil {
		return 0, err
	}
	if itemSettings, exists := settings.DifficultyByValue[difficulty]; exists && !itemSettings.Enabled {
		return 0, ForbiddenError("the selected proof-of-work difficulty is currently disabled")
	}
	return difficulty, nil
}

// GetAdminSettings returns the full administrator-facing PoW settings payload.
func (s *POWService) GetAdminSettings(ctx context.Context) (AdminPOWSettingsView, error) {
	settings, err := s.loadRuntimeSettings(ctx)
	if err != nil {
		return AdminPOWSettingsView{}, err
	}

	benefitViews := make([]AdminPOWBenefitSettingsView, 0, len(powBenefitCatalog))
	for _, benefit := range powBenefitCatalog {
		itemSettings := settings.BenefitByKey[benefit.Key]
		enabled := true
		if itemSettings.Key != "" {
			enabled = itemSettings.Enabled
		}
		benefitViews = append(benefitViews, AdminPOWBenefitSettingsView{
			Key:         benefit.Key,
			DisplayName: benefit.DisplayName,
			Description: benefit.Description,
			RewardUnit:  benefit.RewardUnit,
			Enabled:     enabled,
			CreatedAt:   itemSettings.CreatedAt,
			UpdatedAt:   itemSettings.UpdatedAt,
		})
	}

	difficultyViews := make([]AdminPOWDifficultySettingsView, 0, len(powSupportedDifficulties))
	for _, difficulty := range powSupportedDifficulties {
		itemSettings := settings.DifficultyByValue[difficulty]
		enabled := itemSettings.Enabled
		if itemSettings.Difficulty == 0 {
			enabled = true
		}
		difficultyViews = append(difficultyViews, AdminPOWDifficultySettingsView{
			Difficulty:       difficulty,
			Label:            fmt.Sprintf("难度 %d", difficulty),
			Description:      fmt.Sprintf("启用后，用户可以选择 %d 位难度，奖励倍率固定为 %d。", difficulty, difficulty),
			RewardMultiplier: difficulty,
			Enabled:          enabled,
			CreatedAt:        itemSettings.CreatedAt,
			UpdatedAt:        itemSettings.UpdatedAt,
		})
	}

	return AdminPOWSettingsView{
		Global: AdminPOWGlobalSettingsView{
			Enabled:                     settings.Global.Enabled,
			DefaultDailyCompletionLimit: settings.Global.DefaultDailyCompletionLimit,
			BaseRewardMin:               settings.Global.BaseRewardMin,
			BaseRewardMax:               settings.Global.BaseRewardMax,
			CreatedAt:                   settings.Global.CreatedAt,
			UpdatedAt:                   settings.Global.UpdatedAt,
		},
		Benefits:     benefitViews,
		Difficulties: difficultyViews,
	}, nil
}

// UpdateAdminGlobalSettings writes one administrator-authored global PoW configuration update.
func (s *POWService) UpdateAdminGlobalSettings(ctx context.Context, actor model.User, request AdminUpdatePOWGlobalSettingsRequest) (AdminPOWGlobalSettingsView, error) {
	current, err := s.loadGlobalSettings(ctx)
	if err != nil {
		return AdminPOWGlobalSettingsView{}, err
	}

	if request.Enabled != nil {
		current.Enabled = *request.Enabled
	}
	if request.DefaultDailyCompletionLimit != nil {
		if *request.DefaultDailyCompletionLimit <= 0 {
			return AdminPOWGlobalSettingsView{}, ValidationError("default_daily_completion_limit must be greater than 0")
		}
		current.DefaultDailyCompletionLimit = *request.DefaultDailyCompletionLimit
	}
	if request.BaseRewardMin != nil {
		if *request.BaseRewardMin <= 0 {
			return AdminPOWGlobalSettingsView{}, ValidationError("base_reward_min must be greater than 0")
		}
		current.BaseRewardMin = *request.BaseRewardMin
	}
	if request.BaseRewardMax != nil {
		if *request.BaseRewardMax <= 0 {
			return AdminPOWGlobalSettingsView{}, ValidationError("base_reward_max must be greater than 0")
		}
		current.BaseRewardMax = *request.BaseRewardMax
	}
	if current.BaseRewardMin > current.BaseRewardMax {
		return AdminPOWGlobalSettingsView{}, ValidationError("base_reward_min must not exceed base_reward_max")
	}

	updated, err := s.db.UpsertPOWGlobalSettings(ctx, storage.UpsertPOWGlobalSettingsInput{
		Enabled:                     current.Enabled,
		DefaultDailyCompletionLimit: current.DefaultDailyCompletionLimit,
		BaseRewardMin:               current.BaseRewardMin,
		BaseRewardMax:               current.BaseRewardMax,
	})
	if err != nil {
		return AdminPOWGlobalSettingsView{}, InternalError("failed to update proof-of-work global settings", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"enabled":                        updated.Enabled,
		"default_daily_completion_limit": updated.DefaultDailyCompletionLimit,
		"base_reward_min":                updated.BaseRewardMin,
		"base_reward_max":                updated.BaseRewardMax,
	})
	logAuditWriteFailure("admin.pow.settings.update", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.pow.settings.update",
		ResourceType: "pow_global_settings",
		ResourceID:   strconv.FormatInt(updated.ID, 10),
		MetadataJSON: string(metadata),
	}))

	return AdminPOWGlobalSettingsView{
		Enabled:                     updated.Enabled,
		DefaultDailyCompletionLimit: updated.DefaultDailyCompletionLimit,
		BaseRewardMin:               updated.BaseRewardMin,
		BaseRewardMax:               updated.BaseRewardMax,
		CreatedAt:                   updated.CreatedAt,
		UpdatedAt:                   updated.UpdatedAt,
	}, nil
}

// UpdateAdminBenefitSettings updates one administrator-visible PoW benefit toggle.
func (s *POWService) UpdateAdminBenefitSettings(ctx context.Context, actor model.User, benefitKey string, request AdminUpdatePOWBenefitSettingsRequest) (AdminPOWBenefitSettingsView, error) {
	benefitDefinition, ok := lookupPOWBenefitDefinition(benefitKey)
	if !ok {
		return AdminPOWBenefitSettingsView{}, ValidationError("unsupported proof-of-work benefit")
	}
	if request.Enabled == nil {
		return AdminPOWBenefitSettingsView{}, ValidationError("enabled is required")
	}

	updated, err := s.db.UpsertPOWBenefitSettings(ctx, storage.UpsertPOWBenefitSettingsInput{
		Key:     benefitDefinition.Key,
		Enabled: *request.Enabled,
	})
	if err != nil {
		return AdminPOWBenefitSettingsView{}, InternalError("failed to update proof-of-work benefit settings", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"benefit_key": updated.Key,
		"enabled":     updated.Enabled,
	})
	logAuditWriteFailure("admin.pow.benefit.update", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.pow.benefit.update",
		ResourceType: "pow_benefit_settings",
		ResourceID:   updated.Key,
		MetadataJSON: string(metadata),
	}))

	return AdminPOWBenefitSettingsView{
		Key:         benefitDefinition.Key,
		DisplayName: benefitDefinition.DisplayName,
		Description: benefitDefinition.Description,
		RewardUnit:  benefitDefinition.RewardUnit,
		Enabled:     updated.Enabled,
		CreatedAt:   updated.CreatedAt,
		UpdatedAt:   updated.UpdatedAt,
	}, nil
}

// UpdateAdminDifficultySettings updates one administrator-visible PoW difficulty toggle.
func (s *POWService) UpdateAdminDifficultySettings(ctx context.Context, actor model.User, difficulty int, request AdminUpdatePOWDifficultySettingsRequest) (AdminPOWDifficultySettingsView, error) {
	if _, err := normalizePOWDifficulty(difficulty); err != nil {
		return AdminPOWDifficultySettingsView{}, err
	}
	if request.Enabled == nil {
		return AdminPOWDifficultySettingsView{}, ValidationError("enabled is required")
	}

	updated, err := s.db.UpsertPOWDifficultySettings(ctx, storage.UpsertPOWDifficultySettingsInput{
		Difficulty: difficulty,
		Enabled:    *request.Enabled,
	})
	if err != nil {
		return AdminPOWDifficultySettingsView{}, InternalError("failed to update proof-of-work difficulty settings", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"difficulty": updated.Difficulty,
		"enabled":    updated.Enabled,
	})
	logAuditWriteFailure("admin.pow.difficulty.update", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.pow.difficulty.update",
		ResourceType: "pow_difficulty_settings",
		ResourceID:   strconv.Itoa(updated.Difficulty),
		MetadataJSON: string(metadata),
	}))

	return AdminPOWDifficultySettingsView{
		Difficulty:       updated.Difficulty,
		Label:            fmt.Sprintf("难度 %d", updated.Difficulty),
		Description:      fmt.Sprintf("启用后，用户可以选择 %d 位难度，奖励倍率固定为 %d。", updated.Difficulty, updated.Difficulty),
		RewardMultiplier: updated.Difficulty,
		Enabled:          updated.Enabled,
		CreatedAt:        updated.CreatedAt,
		UpdatedAt:        updated.UpdatedAt,
	}, nil
}

// GetUserSettingsForAdmin returns one target user's current PoW daily settings.
func (s *POWService) GetUserSettingsForAdmin(ctx context.Context, userID int64) (AdminPOWUserSettingsView, error) {
	if _, err := s.db.GetUserByID(ctx, userID); err != nil {
		if storage.IsNotFound(err) {
			return AdminPOWUserSettingsView{}, NotFoundError("target user not found")
		}
		return AdminPOWUserSettingsView{}, InternalError("failed to load target user", err)
	}

	globalSettings, err := s.loadGlobalSettings(ctx)
	if err != nil {
		return AdminPOWUserSettingsView{}, err
	}
	effectiveLimit, err := s.loadEffectiveDailyCompletionLimit(ctx, userID, globalSettings)
	if err != nil {
		return AdminPOWUserSettingsView{}, err
	}
	completedToday, err := s.countCompletedToday(ctx, userID, time.Now().UTC())
	if err != nil {
		return AdminPOWUserSettingsView{}, err
	}

	item, err := s.db.GetPOWUserSettings(ctx, userID)
	switch {
	case err == nil:
	case storage.IsNotFound(err):
		now := time.Now().UTC()
		item = model.POWUserSettings{UserID: userID, CreatedAt: now, UpdatedAt: now}
	default:
		return AdminPOWUserSettingsView{}, InternalError("failed to load user proof-of-work settings", err)
	}

	remainingToday := effectiveLimit - completedToday
	if remainingToday < 0 {
		remainingToday = 0
	}

	return AdminPOWUserSettingsView{
		UserID:                        userID,
		DailyCompletionLimitOverride:  item.DailyCompletionLimitOverride,
		EffectiveDailyCompletionLimit: effectiveLimit,
		CompletedToday:                completedToday,
		RemainingToday:                remainingToday,
		CreatedAt:                     item.CreatedAt,
		UpdatedAt:                     item.UpdatedAt,
	}, nil
}

// UpdateUserSettingsForAdmin updates one target user's PoW per-day completion override.
func (s *POWService) UpdateUserSettingsForAdmin(ctx context.Context, actor model.User, userID int64, request AdminUpdatePOWUserSettingsRequest) (AdminPOWUserSettingsView, error) {
	if _, err := s.db.GetUserByID(ctx, userID); err != nil {
		if storage.IsNotFound(err) {
			return AdminPOWUserSettingsView{}, NotFoundError("target user not found")
		}
		return AdminPOWUserSettingsView{}, InternalError("failed to load target user", err)
	}

	var override *int
	if !request.ClearDailyCompletionLimitOverride {
		override = request.DailyCompletionLimitOverride
	}
	if override != nil && *override <= 0 {
		return AdminPOWUserSettingsView{}, ValidationError("daily_completion_limit_override must be greater than 0")
	}

	updated, err := s.db.UpsertPOWUserSettings(ctx, storage.UpsertPOWUserSettingsInput{
		UserID:                       userID,
		DailyCompletionLimitOverride: override,
	})
	if err != nil {
		return AdminPOWUserSettingsView{}, InternalError("failed to update user proof-of-work settings", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"target_user_id":                  userID,
		"daily_completion_limit_override": updated.DailyCompletionLimitOverride,
	})
	logAuditWriteFailure("admin.pow.user_settings.update", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.pow.user_settings.update",
		ResourceType: "pow_user_settings",
		ResourceID:   strconv.FormatInt(userID, 10),
		MetadataJSON: string(metadata),
	}))

	return s.GetUserSettingsForAdmin(ctx, userID)
}
