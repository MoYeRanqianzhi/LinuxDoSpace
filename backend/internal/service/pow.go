package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"math/bits"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/security"
	"linuxdospace/backend/internal/storage"
)

const (
	// powDefaultDailyCompletionLimit keeps the feature safe even before an
	// administrator touches the persisted PoW settings.
	powDefaultDailyCompletionLimit = 5

	// powDefaultBaseRewardMin and powDefaultBaseRewardMax define the fallback
	// random reward window before the persisted PoW settings are loaded.
	powDefaultBaseRewardMin = 5
	powDefaultBaseRewardMax = 10

	// powArgon2Variant identifies the exact browser/backend hash function used
	// to solve and verify the puzzle.
	powArgon2Variant = "argon2id"

	// powArgon2MemoryKiB keeps each trial expensive enough to matter while still
	// practical inside a browser worker.
	powArgon2MemoryKiB = 128

	// powArgon2Iterations controls how many Argon2 rounds each nonce trial uses.
	powArgon2Iterations = 1

	// powArgon2Parallelism keeps the browser-side worker implementation simple
	// and predictable across devices.
	powArgon2Parallelism = 1

	// powArgon2HashLength defines the byte length of the generated hash used for
	// leading-zero-bit verification.
	powArgon2HashLength = 16

	// powSaltBytes controls how much random salt entropy is embedded in each
	// backend-generated challenge.
	powSaltBytes = 16
)

var powSupportedDifficulties = []int{3, 6, 9, 12}

// powBenefitDefinition describes one currently supported PoW reward target.
// The struct stays separate from the database model so the frontend can later
// expose more reward categories without redesigning the API shape.
type powBenefitDefinition struct {
	Key         string
	DisplayName string
	Description string
	ResourceKey string
	Scope       string
	RewardUnit  string
}

var powBenefitCatalog = []powBenefitDefinition{
	{
		Key:         model.POWBenefitEmailCatchAllRemainingCount,
		DisplayName: "邮箱泛解析次数",
		Description: "解开算力谜题后会直接增加邮箱泛解析的剩余可用次数。当前先只开放这一项福利，后续会继续扩展到更多权益。",
		ResourceKey: QuantityResourceEmailCatchAllRemainingCount,
		Scope:       PermissionKeyEmailCatchAll,
		RewardUnit:  "次",
	},
}

// POWService owns user-bound challenge generation, browser-side proof-of-work
// verification, and reward issuance.
type POWService struct {
	cfg config.Config
	db  Store
}

// POWBenefitOptionView describes one selectable reward target returned to the
// frontend. The UI currently renders a dropdown even though only one option is
// enabled, which keeps the contract extensible.
type POWBenefitOptionView struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	RewardUnit  string `json:"reward_unit"`
	Enabled     bool   `json:"enabled"`
}

// POWDifficultyOptionView describes one selectable proof-of-work difficulty.
// Difficulty is measured in leading zero bits, not hexadecimal digits.
type POWDifficultyOptionView struct {
	Value            int    `json:"value"`
	Label            string `json:"label"`
	Description      string `json:"description"`
	RewardMultiplier int    `json:"reward_multiplier"`
	Enabled          bool   `json:"enabled"`
}

// POWChallengeView exposes the active or already-claimed challenge row in the
// exact shape the frontend worker needs to solve and display it.
type POWChallengeView struct {
	ID                 int64      `json:"id"`
	BenefitKey         string     `json:"benefit_key"`
	BenefitDisplayName string     `json:"benefit_display_name"`
	Difficulty         int        `json:"difficulty"`
	BaseReward         int        `json:"base_reward"`
	RewardQuantity     int        `json:"reward_quantity"`
	RewardUnit         string     `json:"reward_unit"`
	ChallengeToken     string     `json:"challenge_token"`
	SaltHex            string     `json:"salt_hex"`
	Argon2Variant      string     `json:"argon2_variant"`
	Argon2MemoryKiB    uint32     `json:"argon2_memory_kib"`
	Argon2Iterations   uint32     `json:"argon2_iterations"`
	Argon2Parallelism  uint8      `json:"argon2_parallelism"`
	Argon2HashLength   uint32     `json:"argon2_hash_length"`
	Status             string     `json:"status"`
	ClaimedAt          *time.Time `json:"claimed_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// POWStatusView returns the current PoW dashboard state rendered under the LDC
// section on the frontend.
type POWStatusView struct {
	FeatureEnabled        bool                      `json:"feature_enabled"`
	Benefits              []POWBenefitOptionView    `json:"benefits"`
	DifficultyOptions     []POWDifficultyOptionView `json:"difficulty_options"`
	MaxDailyCompletions   int                       `json:"max_daily_completions"`
	CompletedToday        int                       `json:"completed_today"`
	RemainingToday        int                       `json:"remaining_today"`
	CurrentRemainingCount int64                     `json:"current_remaining_count"`
	CurrentChallenge      *POWChallengeView         `json:"current_challenge,omitempty"`
}

// GeneratePOWChallengeRequest describes one authenticated request to replace
// the current challenge with a new reward target and difficulty.
type GeneratePOWChallengeRequest struct {
	BenefitKey string `json:"benefit_key"`
	Difficulty int    `json:"difficulty"`
}

// SubmitPOWChallengeRequest describes one browser-computed nonce candidate sent
// back to the backend for trusted verification and reward issuance.
type SubmitPOWChallengeRequest struct {
	ChallengeID int64  `json:"challenge_id"`
	Nonce       string `json:"nonce"`
}

// SubmitPOWChallengeResult describes the final reward grant after a valid
// nonce is verified and the claim transaction commits.
type SubmitPOWChallengeResult struct {
	Challenge             POWChallengeView `json:"challenge"`
	GrantedQuantity       int              `json:"granted_quantity"`
	RewardUnit            string           `json:"reward_unit"`
	CurrentRemainingCount int64            `json:"current_remaining_count"`
	CompletedToday        int              `json:"completed_today"`
	RemainingToday        int              `json:"remaining_today"`
}

// NewPOWService constructs the proof-of-work reward service.
func NewPOWService(cfg config.Config, db Store) *POWService {
	return &POWService{cfg: cfg, db: db}
}

// GetMyStatus returns the current active challenge, daily claim counters, and
// selectable reward metadata for the authenticated user.
func (s *POWService) GetMyStatus(ctx context.Context, user model.User) (POWStatusView, error) {
	runtimeSettings, err := s.loadRuntimeSettings(ctx)
	if err != nil {
		return POWStatusView{}, err
	}
	effectiveDailyLimit, err := s.loadEffectiveDailyCompletionLimit(ctx, user.ID, runtimeSettings.Global)
	if err != nil {
		return POWStatusView{}, err
	}
	completedToday, err := s.countCompletedToday(ctx, user.ID, time.Now().UTC())
	if err != nil {
		return POWStatusView{}, err
	}

	var currentChallenge *POWChallengeView
	challenge, err := s.db.GetActivePOWChallengeByUser(ctx, user.ID)
	switch {
	case err == nil:
		view := s.challengeView(challenge)
		currentChallenge = &view
	case storage.IsNotFound(err):
		currentChallenge = nil
	default:
		return POWStatusView{}, InternalError("failed to load current proof-of-work challenge", err)
	}

	currentRemainingCount, err := s.loadCatchAllRemainingCount(ctx, user.ID)
	if err != nil {
		return POWStatusView{}, err
	}

	remainingToday := effectiveDailyLimit - completedToday
	if remainingToday < 0 {
		remainingToday = 0
	}

	return POWStatusView{
		FeatureEnabled:        runtimeSettings.Global.Enabled,
		Benefits:              s.benefitOptions(runtimeSettings),
		DifficultyOptions:     s.difficultyOptions(runtimeSettings),
		MaxDailyCompletions:   effectiveDailyLimit,
		CompletedToday:        completedToday,
		RemainingToday:        remainingToday,
		CurrentRemainingCount: currentRemainingCount,
		CurrentChallenge:      currentChallenge,
	}, nil
}

// CreateChallenge replaces any older active challenge with one freshly
// generated puzzle for the current user.
func (s *POWService) CreateChallenge(ctx context.Context, user model.User, request GeneratePOWChallengeRequest) (POWChallengeView, error) {
	runtimeSettings, err := s.loadRuntimeSettings(ctx)
	if err != nil {
		return POWChallengeView{}, err
	}
	if !runtimeSettings.Global.Enabled {
		return POWChallengeView{}, ForbiddenError("proof-of-work welfare is currently disabled")
	}

	benefit, err := s.requireBenefitDefinition(request.BenefitKey, runtimeSettings)
	if err != nil {
		return POWChallengeView{}, err
	}
	difficulty, err := s.requireEnabledDifficulty(request.Difficulty, runtimeSettings)
	if err != nil {
		return POWChallengeView{}, err
	}
	effectiveDailyLimit, err := s.loadEffectiveDailyCompletionLimit(ctx, user.ID, runtimeSettings.Global)
	if err != nil {
		return POWChallengeView{}, err
	}

	completedToday, countErr := s.countCompletedToday(ctx, user.ID, time.Now().UTC())
	if countErr != nil {
		return POWChallengeView{}, countErr
	}
	if completedToday >= effectiveDailyLimit {
		return POWChallengeView{}, TooManyRequestsError(fmt.Sprintf("你今天已经完成了 %d 次 PoW 福利领取，请明天再来。", effectiveDailyLimit))
	}

	saltHex, err := randomHex(powSaltBytes)
	if err != nil {
		return POWChallengeView{}, InternalError("failed to generate proof-of-work salt", err)
	}
	challengeToken, err := security.RandomToken(18)
	if err != nil {
		return POWChallengeView{}, InternalError("failed to generate proof-of-work token", err)
	}

	item, err := s.db.CreateOrReplacePOWChallenge(ctx, storage.CreateOrReplacePOWChallengeInput{
		UserID:            user.ID,
		BenefitKey:        benefit.Key,
		ResourceKey:       benefit.ResourceKey,
		Scope:             benefit.Scope,
		Difficulty:        difficulty,
		BaseReward:        0,
		RewardQuantity:    0,
		RewardUnit:        benefit.RewardUnit,
		ChallengeToken:    challengeToken,
		SaltHex:           saltHex,
		Argon2Variant:     powArgon2Variant,
		Argon2MemoryKiB:   powArgon2MemoryKiB,
		Argon2Iterations:  powArgon2Iterations,
		Argon2Parallelism: powArgon2Parallelism,
		Argon2HashLength:  powArgon2HashLength,
		CreatedAt:         time.Now().UTC(),
	})
	if err != nil {
		return POWChallengeView{}, InternalError("failed to persist proof-of-work challenge", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"challenge_id":     item.ID,
		"benefit_key":      item.BenefitKey,
		"difficulty":       item.Difficulty,
		"challenge_status": item.Status,
	})
	logAuditWriteFailure("pow.challenge.create", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "pow.challenge.create",
		ResourceType: "pow_challenge",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}))

	return s.challengeView(item), nil
}

// SubmitChallenge verifies one browser-computed nonce against the current
// active challenge and grants the reward atomically when the hash satisfies the
// difficulty target.
func (s *POWService) SubmitChallenge(ctx context.Context, user model.User, request SubmitPOWChallengeRequest) (SubmitPOWChallengeResult, error) {
	if request.ChallengeID <= 0 {
		return SubmitPOWChallengeResult{}, ValidationError("challenge_id is required")
	}

	nonce := strings.TrimSpace(request.Nonce)
	if nonce == "" {
		return SubmitPOWChallengeResult{}, ValidationError("nonce is required")
	}
	if len(nonce) > 128 {
		return SubmitPOWChallengeResult{}, ValidationError("nonce is too long")
	}

	runtimeSettings, err := s.loadRuntimeSettings(ctx)
	if err != nil {
		return SubmitPOWChallengeResult{}, err
	}
	if !runtimeSettings.Global.Enabled {
		return SubmitPOWChallengeResult{}, ForbiddenError("proof-of-work welfare is currently disabled")
	}

	challenge, err := s.db.GetActivePOWChallengeByUser(ctx, user.ID)
	if err != nil {
		if storage.IsNotFound(err) {
			return SubmitPOWChallengeResult{}, NotFoundError("当前没有可提交的 PoW 题目，请先重新生成。")
		}
		return SubmitPOWChallengeResult{}, InternalError("failed to load active proof-of-work challenge", err)
	}
	if challenge.ID != request.ChallengeID {
		return SubmitPOWChallengeResult{}, ConflictError("当前题目已经变化，请重新获取最新题目后再提交。")
	}
	if _, err := s.requireBenefitDefinition(challenge.BenefitKey, runtimeSettings); err != nil {
		return SubmitPOWChallengeResult{}, err
	}
	if _, err := s.requireEnabledDifficulty(challenge.Difficulty, runtimeSettings); err != nil {
		return SubmitPOWChallengeResult{}, err
	}

	hashHex, solveErr := verifyPOWChallenge(challenge, nonce)
	if solveErr != nil {
		return SubmitPOWChallengeResult{}, solveErr
	}
	baseReward, err := randomIntInRange(runtimeSettings.Global.BaseRewardMin, runtimeSettings.Global.BaseRewardMax)
	if err != nil {
		return SubmitPOWChallengeResult{}, InternalError("failed to generate proof-of-work reward", err)
	}
	rewardQuantity := baseReward * challenge.Difficulty
	effectiveDailyLimit, err := s.loadEffectiveDailyCompletionLimit(ctx, user.ID, runtimeSettings.Global)
	if err != nil {
		return SubmitPOWChallengeResult{}, err
	}

	now := time.Now().UTC()
	startOfDay := utcStartOfDay(now)
	endOfDay := startOfDay.Add(24 * time.Hour)

	claimedChallenge, access, claimErr := s.db.ClaimPOWChallengeReward(ctx, storage.ClaimPOWChallengeRewardInput{
		UserID:               user.ID,
		ChallengeID:          challenge.ID,
		BaseReward:           baseReward,
		RewardQuantity:       rewardQuantity,
		SolutionNonce:        nonce,
		SolutionHashHex:      hashHex,
		ClaimedAt:            now,
		DailyWindowStart:     startOfDay,
		DailyWindowEnd:       endOfDay,
		MaxDailyCompletions:  effectiveDailyLimit,
		QuantityRecordReason: buildPOWRewardReason(challenge.Difficulty, baseReward, rewardQuantity, challenge.RewardUnit),
	})
	if claimErr != nil {
		switch {
		case claimErr == storage.ErrPOWChallengeDailyLimitExceeded:
			return SubmitPOWChallengeResult{}, TooManyRequestsError(fmt.Sprintf("你今天已经完成了 %d 次 PoW 福利领取，请明天再来。", effectiveDailyLimit))
		case claimErr == storage.ErrPOWChallengeNotActive:
			return SubmitPOWChallengeResult{}, ConflictError("当前题目已经失效，请重新生成新的题目。")
		default:
			return SubmitPOWChallengeResult{}, InternalError("failed to claim proof-of-work reward", claimErr)
		}
	}

	completedToday, err := s.countCompletedToday(ctx, user.ID, now)
	if err != nil {
		return SubmitPOWChallengeResult{}, err
	}
	remainingToday := effectiveDailyLimit - completedToday
	if remainingToday < 0 {
		remainingToday = 0
	}

	metadata, _ := json.Marshal(map[string]any{
		"challenge_id":            claimedChallenge.ID,
		"benefit_key":             claimedChallenge.BenefitKey,
		"difficulty":              claimedChallenge.Difficulty,
		"granted_quantity":        claimedChallenge.RewardQuantity,
		"current_remaining_count": access.RemainingCount,
	})
	logAuditWriteFailure("pow.challenge.claim", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "pow.challenge.claim",
		ResourceType: "pow_challenge",
		ResourceID:   strconv.FormatInt(claimedChallenge.ID, 10),
		MetadataJSON: string(metadata),
	}))

	return SubmitPOWChallengeResult{
		Challenge:             s.challengeView(claimedChallenge),
		GrantedQuantity:       claimedChallenge.RewardQuantity,
		RewardUnit:            claimedChallenge.RewardUnit,
		CurrentRemainingCount: access.RemainingCount,
		CompletedToday:        completedToday,
		RemainingToday:        remainingToday,
	}, nil
}

// challengeView converts one storage model row into the narrower API contract
// used by the frontend worker and dashboard.
func (s *POWService) challengeView(item model.POWChallenge) POWChallengeView {
	benefit, found := lookupPOWBenefitDefinition(item.BenefitKey)
	if !found {
		benefit = powBenefitDefinition{DisplayName: item.BenefitKey, RewardUnit: item.RewardUnit}
	}
	return POWChallengeView{
		ID:                 item.ID,
		BenefitKey:         item.BenefitKey,
		BenefitDisplayName: benefit.DisplayName,
		Difficulty:         item.Difficulty,
		BaseReward:         item.BaseReward,
		RewardQuantity:     item.RewardQuantity,
		RewardUnit:         item.RewardUnit,
		ChallengeToken:     item.ChallengeToken,
		SaltHex:            item.SaltHex,
		Argon2Variant:      item.Argon2Variant,
		Argon2MemoryKiB:    item.Argon2MemoryKiB,
		Argon2Iterations:   item.Argon2Iterations,
		Argon2Parallelism:  item.Argon2Parallelism,
		Argon2HashLength:   item.Argon2HashLength,
		Status:             item.Status,
		ClaimedAt:          item.ClaimedAt,
		CreatedAt:          item.CreatedAt,
		UpdatedAt:          item.UpdatedAt,
	}
}

// countCompletedToday returns how many PoW rewards the current user already
// claimed during the UTC day.
func (s *POWService) countCompletedToday(ctx context.Context, userID int64, now time.Time) (int, error) {
	start := utcStartOfDay(now.UTC())
	end := start.Add(24 * time.Hour)
	count, err := s.db.CountClaimedPOWChallengesByUser(ctx, userID, start, end)
	if err != nil {
		return 0, InternalError("failed to count today's proof-of-work rewards", err)
	}
	return count, nil
}

// loadCatchAllRemainingCount reads the current mutable remaining count so the
// PoW dashboard can immediately show the reward effect.
func (s *POWService) loadCatchAllRemainingCount(ctx context.Context, userID int64) (int64, error) {
	access, err := s.db.GetEmailCatchAllAccessByUser(ctx, userID)
	if err != nil {
		if storage.IsNotFound(err) {
			return 0, nil
		}
		return 0, InternalError("failed to load catch-all access state", err)
	}
	return access.RemainingCount, nil
}

// verifyPOWChallenge recomputes the Argon2 hash for one submitted nonce and
// confirms that the result satisfies the configured leading-zero-bit target.
func verifyPOWChallenge(challenge model.POWChallenge, nonce string) (string, error) {
	if challenge.Argon2Variant != powArgon2Variant {
		return "", ValidationError("unsupported proof-of-work variant")
	}

	salt, err := hex.DecodeString(strings.TrimSpace(challenge.SaltHex))
	if err != nil {
		return "", InternalError("failed to decode proof-of-work salt", err)
	}

	hashBytes := argon2.IDKey(
		[]byte(challenge.ChallengeToken+":"+nonce),
		salt,
		challenge.Argon2Iterations,
		challenge.Argon2MemoryKiB,
		challenge.Argon2Parallelism,
		challenge.Argon2HashLength,
	)
	if countLeadingZeroBits(hashBytes) < challenge.Difficulty {
		return "", ValidationError("提交的 nonce 未满足当前题目的难度要求。")
	}

	return hex.EncodeToString(hashBytes), nil
}

// countLeadingZeroBits mirrors the browser worker's verification rule so both
// sides agree on whether one Argon2 output solves the challenge.
func countLeadingZeroBits(hashBytes []byte) int {
	total := 0
	for _, value := range hashBytes {
		if value == 0 {
			total += 8
			continue
		}
		total += bits.LeadingZeros8(value)
		break
	}
	return total
}

// buildPOWRewardReason keeps quantity-ledger history self-explanatory once the
// reward is granted.
func buildPOWRewardReason(difficulty int, baseReward int, rewardQuantity int, rewardUnit string) string {
	return fmt.Sprintf("PoW 福利奖励：难度 %d，基础奖励 %d，发放 %d%s", difficulty, baseReward, rewardQuantity, rewardUnit)
}

// normalizePOWDifficulty rejects unsupported difficulty values so frontend
// tampering cannot silently create custom reward multipliers.
func normalizePOWDifficulty(raw int) (int, error) {
	for _, difficulty := range powSupportedDifficulties {
		if raw == difficulty {
			return raw, nil
		}
	}
	return 0, ValidationError("difficulty must be one of 3, 6, 9, or 12")
}

// randomIntInRange returns one cryptographically strong integer inside the
// inclusive range [minValue, maxValue].
func randomIntInRange(minValue int, maxValue int) (int, error) {
	if minValue > maxValue {
		return 0, fmt.Errorf("invalid integer range")
	}
	span := maxValue - minValue + 1
	value, err := rand.Int(rand.Reader, big.NewInt(int64(span)))
	if err != nil {
		return 0, err
	}
	return minValue + int(value.Int64()), nil
}

// randomHex returns one random byte slice encoded as lowercase hexadecimal.
func randomHex(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// utcStartOfDay returns the canonical start of the UTC day containing `value`.
func utcStartOfDay(value time.Time) time.Time {
	utcValue := value.UTC()
	return time.Date(utcValue.Year(), utcValue.Month(), utcValue.Day(), 0, 0, 0, 0, time.UTC)
}
