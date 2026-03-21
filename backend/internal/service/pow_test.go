package service

import (
	"context"
	"encoding/hex"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/timeutil"
)

// TestPOWServiceCreateAndClaimChallenge verifies the full happy path from
// challenge generation to reward grant and quantity-ledger persistence.
func TestPOWServiceCreateAndClaimChallenge(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 981, "alice")

	service := NewPOWService(newPermissionEmailTestConfig(), store)

	createdChallenge, err := service.CreateChallenge(ctx, user, GeneratePOWChallengeRequest{
		BenefitKey: model.POWBenefitEmailCatchAllRemainingCount,
		Difficulty: 3,
	})
	if err != nil {
		t.Fatalf("create pow challenge: %v", err)
	}
	if createdChallenge.BenefitKey != model.POWBenefitEmailCatchAllRemainingCount {
		t.Fatalf("expected email catch-all remaining-count benefit, got %+v", createdChallenge)
	}
	if createdChallenge.BaseReward != 0 || createdChallenge.RewardQuantity != 0 {
		t.Fatalf("expected unrevealed reward before solving, got %+v", createdChallenge)
	}

	nonce := solvePOWChallenge(t, createdChallenge)
	result, err := service.SubmitChallenge(ctx, user, SubmitPOWChallengeRequest{
		ChallengeID: createdChallenge.ID,
		Nonce:       nonce,
	})
	if err != nil {
		t.Fatalf("submit pow challenge: %v", err)
	}
	if result.GrantedQuantity < 15 || result.GrantedQuantity > 30 {
		t.Fatalf("expected difficulty-3 reward between 15 and 30, got %d", result.GrantedQuantity)
	}

	if result.CurrentRemainingCount != int64(result.GrantedQuantity) {
		t.Fatalf("expected remaining count %d, got %d", result.GrantedQuantity, result.CurrentRemainingCount)
	}
	if result.CurrentPermanentRemainingCount != 0 {
		t.Fatalf("expected permanent remaining count 0 after PoW reward, got %d", result.CurrentPermanentRemainingCount)
	}
	if result.CurrentTemporaryRewardCount != int64(result.GrantedQuantity) {
		t.Fatalf("expected temporary reward count %d, got %d", result.GrantedQuantity, result.CurrentTemporaryRewardCount)
	}
	expectedExpiry := timeutil.NextShanghaiMidnightUTC(time.Now().UTC())
	if result.CurrentTemporaryRewardExpiresAt == nil {
		t.Fatalf("expected temporary reward expiry to be set")
	}
	if !result.CurrentTemporaryRewardExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("expected temporary reward expiry %s, got %s", expectedExpiry.Format(time.RFC3339), result.CurrentTemporaryRewardExpiresAt.Format(time.RFC3339))
	}
	if result.CompletedToday != 1 || result.RemainingToday != 4 {
		t.Fatalf("unexpected day counters after claim: %+v", result)
	}

	access, err := store.GetEmailCatchAllAccessByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("load catch-all access after pow claim: %v", err)
	}
	if access.RemainingCount != 0 {
		t.Fatalf("expected persisted permanent remaining count 0, got %d", access.RemainingCount)
	}
	if access.TemporaryRewardCount != int64(result.GrantedQuantity) {
		t.Fatalf("expected persisted temporary reward count %d, got %d", result.GrantedQuantity, access.TemporaryRewardCount)
	}
	if access.TemporaryRewardExpiresAt == nil {
		t.Fatalf("expected persisted temporary reward expiry to be set")
	}
	if !access.TemporaryRewardExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("expected persisted temporary reward expiry %s, got %s", expectedExpiry.Format(time.RFC3339), access.TemporaryRewardExpiresAt.Format(time.RFC3339))
	}

	records, err := store.ListQuantityRecordsByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list quantity records after pow claim: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one quantity record, got %d", len(records))
	}
	if records[0].ResourceKey != QuantityResourceEmailCatchAllRemainingCount {
		t.Fatalf("expected remaining-count ledger entry, got %+v", records[0])
	}
	if records[0].Delta != result.GrantedQuantity {
		t.Fatalf("expected quantity delta %d, got %d", result.GrantedQuantity, records[0].Delta)
	}
	if records[0].ExpiresAt == nil {
		t.Fatalf("expected quantity record to carry temporary reward expiry")
	}
	if !records[0].ExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("expected quantity record expiry %s, got %s", expectedExpiry.Format(time.RFC3339), records[0].ExpiresAt.Format(time.RFC3339))
	}

	status, err := service.GetMyStatus(ctx, user)
	if err != nil {
		t.Fatalf("get pow status after claim: %v", err)
	}
	if status.CurrentChallenge != nil {
		t.Fatalf("expected no active challenge after successful claim, got %+v", status.CurrentChallenge)
	}
	if status.CurrentRemainingCount != int64(result.GrantedQuantity) {
		t.Fatalf("expected status remaining count %d, got %d", result.GrantedQuantity, status.CurrentRemainingCount)
	}
	if status.CurrentPermanentRemainingCount != 0 {
		t.Fatalf("expected status permanent remaining count 0, got %d", status.CurrentPermanentRemainingCount)
	}
	if status.CurrentTemporaryRewardCount != int64(result.GrantedQuantity) {
		t.Fatalf("expected status temporary reward count %d, got %d", result.GrantedQuantity, status.CurrentTemporaryRewardCount)
	}
	if status.CurrentTemporaryRewardExpiresAt == nil || !status.CurrentTemporaryRewardExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("expected status temporary reward expiry %s, got %+v", expectedExpiry.Format(time.RFC3339), status.CurrentTemporaryRewardExpiresAt)
	}
	if status.CompletedToday != 1 || status.RemainingToday != 4 {
		t.Fatalf("unexpected status counters after claim: %+v", status)
	}
}

// TestPOWServiceNewChallengeSupersedesOldOne verifies that each user can only
// keep one active puzzle at a time and stale solutions are rejected.
func TestPOWServiceNewChallengeSupersedesOldOne(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 982, "alice")

	service := NewPOWService(newPermissionEmailTestConfig(), store)

	firstChallenge, err := service.CreateChallenge(ctx, user, GeneratePOWChallengeRequest{
		BenefitKey: model.POWBenefitEmailCatchAllRemainingCount,
		Difficulty: 3,
	})
	if err != nil {
		t.Fatalf("create first pow challenge: %v", err)
	}
	secondChallenge, err := service.CreateChallenge(ctx, user, GeneratePOWChallengeRequest{
		BenefitKey: model.POWBenefitEmailCatchAllRemainingCount,
		Difficulty: 3,
	})
	if err != nil {
		t.Fatalf("create second pow challenge: %v", err)
	}

	if firstChallenge.ID == secondChallenge.ID {
		t.Fatalf("expected the replacement challenge to have a new id")
	}

	staleNonce := solvePOWChallenge(t, firstChallenge)
	if _, err := service.SubmitChallenge(ctx, user, SubmitPOWChallengeRequest{
		ChallengeID: firstChallenge.ID,
		Nonce:       staleNonce,
	}); err == nil {
		t.Fatalf("expected stale challenge submission to be rejected")
	} else if NormalizeError(err).Code != "conflict" {
		t.Fatalf("expected conflict error for stale challenge, got %v", err)
	}
}

// TestPOWServiceStopsAfterFiveDailyClaims verifies that one user cannot keep
// farming the PoW reward beyond the configured Shanghai-local day cap.
func TestPOWServiceStopsAfterFiveDailyClaims(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 983, "alice")

	service := NewPOWService(newPermissionEmailTestConfig(), store)

	for attempt := 1; attempt <= 5; attempt++ {
		challenge, err := service.CreateChallenge(ctx, user, GeneratePOWChallengeRequest{
			BenefitKey: model.POWBenefitEmailCatchAllRemainingCount,
			Difficulty: 3,
		})
		if err != nil {
			t.Fatalf("create pow challenge attempt %d: %v", attempt, err)
		}

		nonce := solvePOWChallenge(t, challenge)
		if _, err := service.SubmitChallenge(ctx, user, SubmitPOWChallengeRequest{
			ChallengeID: challenge.ID,
			Nonce:       nonce,
		}); err != nil {
			t.Fatalf("submit pow challenge attempt %d: %v", attempt, err)
		}
	}

	status, err := service.GetMyStatus(ctx, user)
	if err != nil {
		t.Fatalf("get pow status after 5 claims: %v", err)
	}
	if status.CompletedToday != 5 || status.RemainingToday != 0 {
		t.Fatalf("expected the day cap to be exhausted, got %+v", status)
	}

	if _, err := service.CreateChallenge(ctx, user, GeneratePOWChallengeRequest{
		BenefitKey: model.POWBenefitEmailCatchAllRemainingCount,
		Difficulty: 3,
	}); err == nil {
		t.Fatalf("expected a sixth pow challenge creation to be rejected")
	} else if NormalizeError(err).Code != "too_many_requests" {
		t.Fatalf("expected too_many_requests after 5 claims, got %v", err)
	}
}

// TestPOWServiceConcurrentClaimOnlyGrantsOnce verifies that concurrent submits
// for the same challenge cannot both receive the reward.
func TestPOWServiceConcurrentClaimOnlyGrantsOnce(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 984, "alice")

	service := NewPOWService(newPermissionEmailTestConfig(), store)
	challenge, err := service.CreateChallenge(ctx, user, GeneratePOWChallengeRequest{
		BenefitKey: model.POWBenefitEmailCatchAllRemainingCount,
		Difficulty: 3,
	})
	if err != nil {
		t.Fatalf("create pow challenge: %v", err)
	}
	nonce := solvePOWChallenge(t, challenge)

	type claimResult struct {
		result SubmitPOWChallengeResult
		err    error
	}

	results := make(chan claimResult, 2)
	var waitGroup sync.WaitGroup
	for attempt := 0; attempt < 2; attempt++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result, submitErr := service.SubmitChallenge(ctx, user, SubmitPOWChallengeRequest{
				ChallengeID: challenge.ID,
				Nonce:       nonce,
			})
			results <- claimResult{result: result, err: submitErr}
		}()
	}
	waitGroup.Wait()
	close(results)

	successCount := 0
	conflictCount := 0
	for item := range results {
		if item.err == nil {
			successCount++
			continue
		}
		if NormalizeError(item.err).Code == "conflict" {
			conflictCount++
			continue
		}
		t.Fatalf("expected concurrent loser to receive conflict, got %v", item.err)
	}

	if successCount != 1 {
		t.Fatalf("expected exactly one successful concurrent claim, got %d", successCount)
	}
	if conflictCount != 1 {
		t.Fatalf("expected exactly one conflicting concurrent claim, got %d", conflictCount)
	}

	status, err := service.GetMyStatus(ctx, user)
	if err != nil {
		t.Fatalf("get pow status after concurrent claim: %v", err)
	}
	if status.CompletedToday != 1 {
		t.Fatalf("expected completed_today=1 after concurrent claim, got %d", status.CompletedToday)
	}
}

// solvePOWChallenge performs the same browser-side hashing loop used by the
// frontend worker so service tests can exercise the real verification path.
func solvePOWChallenge(t *testing.T, challenge POWChallengeView) string {
	t.Helper()

	salt, err := hex.DecodeString(challenge.SaltHex)
	if err != nil {
		t.Fatalf("decode challenge salt: %v", err)
	}

	for nonceValue := 0; nonceValue < 200000; nonceValue++ {
		nonce := strconv.Itoa(nonceValue)
		hashBytes := argon2.IDKey(
			[]byte(challenge.ChallengeToken+":"+nonce),
			salt,
			challenge.Argon2Iterations,
			challenge.Argon2MemoryKiB,
			challenge.Argon2Parallelism,
			challenge.Argon2HashLength,
		)
		if countLeadingZeroBits(hashBytes) >= challenge.Difficulty {
			return nonce
		}
	}

	t.Fatalf("failed to solve challenge %+v inside nonce limit", challenge)
	return ""
}
