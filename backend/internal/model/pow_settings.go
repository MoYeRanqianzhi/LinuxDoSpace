package model

import "time"

// POWGlobalSettings stores the administrator-controlled feature flags and base
// reward range that govern the whole proof-of-work system.
type POWGlobalSettings struct {
	ID                          int64     `json:"id"`
	Enabled                     bool      `json:"enabled"`
	DefaultDailyCompletionLimit int       `json:"default_daily_completion_limit"`
	BaseRewardMin               int       `json:"base_reward_min"`
	BaseRewardMax               int       `json:"base_reward_max"`
	CreatedAt                   time.Time `json:"created_at"`
	UpdatedAt                   time.Time `json:"updated_at"`
}

// POWBenefitSettings stores the administrator-controlled on/off switch for one
// user-visible PoW benefit target.
type POWBenefitSettings struct {
	Key       string    `json:"key"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// POWDifficultySettings stores the administrator-controlled on/off switch for
// one supported difficulty level.
type POWDifficultySettings struct {
	Difficulty int       `json:"difficulty"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// POWUserSettings stores one optional per-user override for how many PoW
// rewards may be claimed in one UTC day.
type POWUserSettings struct {
	UserID                       int64     `json:"user_id"`
	DailyCompletionLimitOverride *int      `json:"daily_completion_limit_override,omitempty"`
	CreatedAt                    time.Time `json:"created_at"`
	UpdatedAt                    time.Time `json:"updated_at"`
}
