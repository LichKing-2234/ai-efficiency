package attributionpool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionrequestclaim"
	"github.com/ai-efficiency/backend/ent/attributionusagepool"
	"github.com/ai-efficiency/backend/ent/attributionusagepoolcommit"
)

const LedgerEpoch = "shadow_v2"

const CoverageGapModel = "unresolved"

type commitRef struct {
	repoConfigID int
	commitSHA    string
}

type contribution struct {
	key         string
	userID      int
	model       string
	bucketStart time.Time
	commits     []commitRef
	input       int64
	output      int64
	cacheCreate int64
	cacheRead   int64
	total       int64
}

// MaterializeGroup moves every reconciled Request in a hot group to the pool
// implied by the group's latest canonical allocation. The caller supplies the
// transaction-scoped Ent client when this must commit with another mutation.
func MaterializeGroup(ctx context.Context, client *ent.Client, groupID int, now time.Time) error {
	if client == nil || groupID <= 0 {
		return fmt.Errorf("materialize attribution group: client and group_id are required")
	}
	ids, err := client.AttributionRequestClaim.Query().Where(
		attributionrequestclaim.ClaimGroupIDEQ(groupID),
		attributionrequestclaim.StatusEQ(attributionrequestclaim.StatusReconciled),
	).Order(ent.Asc(attributionrequestclaim.FieldID)).IDs(ctx)
	if err != nil {
		return fmt.Errorf("query reconciled group claims: %w", err)
	}
	for _, id := range ids {
		if err := MaterializeRequestClaim(ctx, client, id, now); err != nil {
			return err
		}
	}
	return nil
}

// MaterializeRequestClaim atomically moves one official Request contribution.
// The hot claim keeps only the internal pool pointer; long-lived rows never
// contain the upstream Request ID.
func MaterializeRequestClaim(ctx context.Context, client *ent.Client, claimID int, now time.Time) error {
	if client == nil || claimID <= 0 {
		return fmt.Errorf("materialize request claim: client and claim_id are required")
	}
	claim, err := client.AttributionRequestClaim.Get(ctx, claimID)
	if err != nil {
		return fmt.Errorf("load request claim for materialization: %w", err)
	}
	if claim.Status != attributionrequestclaim.StatusReconciled {
		return fmt.Errorf("request claim %d is not reconciled", claimID)
	}
	group, err := client.AttributionClaimGroup.Get(ctx, claim.ClaimGroupID)
	if err != nil {
		return fmt.Errorf("load claim group for materialization: %w", err)
	}
	desired, err := canonicalContribution(group.UserID, group.CommitAllocations, claim)
	if err != nil {
		return fmt.Errorf("build request claim contribution: %w", err)
	}
	pool, err := ensurePool(ctx, client, desired)
	if err != nil {
		return err
	}
	if claim.MaterializedPoolID != nil && *claim.MaterializedPoolID == pool.ID {
		return ensureCommitRelations(ctx, client, pool.ID, desired.commits)
	}

	oldPoolID := 0
	pointerPredicate := attributionrequestclaim.MaterializedPoolIDIsNil()
	if claim.MaterializedPoolID != nil {
		oldPoolID = *claim.MaterializedPoolID
		pointerPredicate = attributionrequestclaim.MaterializedPoolIDEQ(oldPoolID)
		if err := subtractContribution(ctx, client, oldPoolID, desiredFromClaim(claim)); err != nil {
			return err
		}
	}
	if err := addContribution(ctx, client, pool.ID, desired); err != nil {
		return err
	}
	updated, err := client.AttributionRequestClaim.Update().Where(
		attributionrequestclaim.IDEQ(claim.ID), pointerPredicate,
	).SetMaterializedPoolID(pool.ID).SetMaterializedAt(now.UTC()).Save(ctx)
	if err != nil {
		return fmt.Errorf("link request claim to usage pool: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("request claim materialization raced: updated=%d", updated)
	}
	if err := ensureCommitRelations(ctx, client, pool.ID, desired.commits); err != nil {
		return err
	}
	if oldPoolID > 0 {
		return deleteEmptyPool(ctx, client, oldPoolID)
	}
	return nil
}

// MaterializeCoverageGaps stores unresolved Requests as zero-Token coverage
// only. The group receive bucket is used because no upstream usage time or
// requested model exists for an unresolved Request.
func MaterializeCoverageGaps(ctx context.Context, client *ent.Client, groupID, count int) error {
	if client == nil || groupID <= 0 || count <= 0 {
		return fmt.Errorf("materialize coverage gaps: client, group_id, and positive count are required")
	}
	group, err := client.AttributionClaimGroup.Get(ctx, groupID)
	if err != nil {
		return fmt.Errorf("load claim group for coverage gaps: %w", err)
	}
	commits, err := canonicalCommits(group.CommitAllocations)
	if err != nil {
		return fmt.Errorf("build coverage gap contribution: %w", err)
	}
	bucket := group.CreatedAt.UTC().Truncate(15 * time.Minute)
	parts := []string{strconv.Itoa(group.UserID), CoverageGapModel, bucket.Format(time.RFC3339)}
	for _, commit := range commits {
		parts = append(parts, fmt.Sprintf("%d:%s", commit.repoConfigID, commit.commitSHA))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	value := contribution{
		key: hex.EncodeToString(sum[:]), userID: group.UserID, model: CoverageGapModel,
		bucketStart: bucket, commits: commits,
	}
	pool, err := ensurePool(ctx, client, value)
	if err != nil {
		return err
	}
	updated, err := client.AttributionUsagePool.Update().Where(
		attributionusagepool.IDEQ(pool.ID), attributionusagepool.CoverageGapCountLTE(math.MaxInt-count),
	).AddCoverageGapCount(count).Save(ctx)
	if err != nil {
		return fmt.Errorf("add attribution pool coverage gaps: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("add attribution pool coverage gaps: updated=%d", updated)
	}
	return ensureCommitRelations(ctx, client, pool.ID, commits)
}

func canonicalContribution(userID int, allocations []map[string]any, claim *ent.AttributionRequestClaim) (contribution, error) {
	if userID <= 0 || claim == nil || strings.TrimSpace(claim.RequestedModel) == "" || claim.UsageAt == nil || claim.UsageAt.IsZero() {
		return contribution{}, fmt.Errorf("user, requested model, and usage time are required")
	}
	if claim.InputTokens < 0 || claim.OutputTokens < 0 || claim.CacheCreationTokens < 0 || claim.CacheReadTokens < 0 || claim.TotalTokens < 0 {
		return contribution{}, fmt.Errorf("official Token components must be non-negative")
	}
	if claim.InputTokens > math.MaxInt64-claim.OutputTokens || claim.InputTokens+claim.OutputTokens > math.MaxInt64-claim.CacheCreationTokens || claim.InputTokens+claim.OutputTokens+claim.CacheCreationTokens > math.MaxInt64-claim.CacheReadTokens || claim.InputTokens+claim.OutputTokens+claim.CacheCreationTokens+claim.CacheReadTokens != claim.TotalTokens {
		return contribution{}, fmt.Errorf("official Token total is inconsistent")
	}
	commits, err := canonicalCommits(allocations)
	if err != nil {
		return contribution{}, err
	}
	bucket := claim.UsageAt.UTC().Truncate(15 * time.Minute)
	model := strings.TrimSpace(claim.RequestedModel)
	parts := []string{strconv.Itoa(userID), model, bucket.Format(time.RFC3339)}
	for _, commit := range commits {
		parts = append(parts, fmt.Sprintf("%d:%s", commit.repoConfigID, commit.commitSHA))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return contribution{
		key: hex.EncodeToString(sum[:]), userID: userID, model: model, bucketStart: bucket, commits: commits,
		input: claim.InputTokens, output: claim.OutputTokens, cacheCreate: claim.CacheCreationTokens,
		cacheRead: claim.CacheReadTokens, total: claim.TotalTokens,
	}, nil
}

func canonicalCommits(allocations []map[string]any) ([]commitRef, error) {
	unique := make(map[string]commitRef, len(allocations))
	for _, allocation := range allocations {
		repoID, ok := integerValue(allocation["repo_config_id"])
		sha, _ := allocation["commit_sha"].(string)
		sha = strings.TrimSpace(sha)
		if !ok || repoID <= 0 || sha == "" {
			return nil, fmt.Errorf("counting allocation repository and commit are required")
		}
		key := fmt.Sprintf("%d:%s", repoID, sha)
		unique[key] = commitRef{repoConfigID: repoID, commitSHA: sha}
	}
	if len(unique) == 0 {
		return nil, fmt.Errorf("at least one counting commit is required")
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	commits := make([]commitRef, 0, len(keys))
	for _, key := range keys {
		commits = append(commits, unique[key])
	}
	return commits, nil
}

func integerValue(value any) (int, bool) {
	switch value := value.(type) {
	case int:
		return value, true
	case int64:
		return int(value), int64(int(value)) == value
	case float64:
		return int(value), value == float64(int(value))
	default:
		return 0, false
	}
}

func ensurePool(ctx context.Context, client *ent.Client, value contribution) (*ent.AttributionUsagePool, error) {
	pool, err := client.AttributionUsagePool.Query().Where(attributionusagepool.CanonicalPoolKeyEQ(value.key)).Only(ctx)
	if ent.IsNotFound(err) {
		pool, err = client.AttributionUsagePool.Create().
			SetCanonicalPoolKey(value.key).SetLedgerEpoch(LedgerEpoch).SetUserID(value.userID).
			SetRequestedModel(value.model).SetBucketStartUtc(value.bucketStart).Save(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("ensure attribution usage pool: %w", err)
	}
	if pool.LedgerEpoch != LedgerEpoch || pool.UserID != value.userID || pool.RequestedModel != value.model || !pool.BucketStartUtc.Equal(value.bucketStart) {
		return nil, fmt.Errorf("canonical attribution usage pool conflict")
	}
	return pool, nil
}

func addContribution(ctx context.Context, client *ent.Client, poolID int, value contribution) error {
	updated, err := client.AttributionUsagePool.Update().Where(
		attributionusagepool.IDEQ(poolID),
		attributionusagepool.InputTokensLTE(math.MaxInt64-value.input),
		attributionusagepool.OutputTokensLTE(math.MaxInt64-value.output),
		attributionusagepool.CacheCreationTokensLTE(math.MaxInt64-value.cacheCreate),
		attributionusagepool.CacheReadTokensLTE(math.MaxInt64-value.cacheRead),
		attributionusagepool.TotalTokensLTE(math.MaxInt64-value.total),
		attributionusagepool.RequestCountLT(math.MaxInt),
	).AddInputTokens(value.input).AddOutputTokens(value.output).AddCacheCreationTokens(value.cacheCreate).
		AddCacheReadTokens(value.cacheRead).AddTotalTokens(value.total).AddRequestCount(1).Save(ctx)
	if err != nil {
		return fmt.Errorf("add attribution pool contribution: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("add attribution pool contribution: updated=%d", updated)
	}
	return nil
}

func desiredFromClaim(claim *ent.AttributionRequestClaim) contribution {
	return contribution{input: claim.InputTokens, output: claim.OutputTokens, cacheCreate: claim.CacheCreationTokens, cacheRead: claim.CacheReadTokens, total: claim.TotalTokens}
}

func subtractContribution(ctx context.Context, client *ent.Client, poolID int, value contribution) error {
	updated, err := client.AttributionUsagePool.Update().Where(
		attributionusagepool.IDEQ(poolID), attributionusagepool.InputTokensGTE(value.input),
		attributionusagepool.OutputTokensGTE(value.output), attributionusagepool.CacheCreationTokensGTE(value.cacheCreate),
		attributionusagepool.CacheReadTokensGTE(value.cacheRead), attributionusagepool.TotalTokensGTE(value.total),
		attributionusagepool.RequestCountGT(0),
	).AddInputTokens(-value.input).AddOutputTokens(-value.output).AddCacheCreationTokens(-value.cacheCreate).
		AddCacheReadTokens(-value.cacheRead).AddTotalTokens(-value.total).AddRequestCount(-1).Save(ctx)
	if err != nil {
		return fmt.Errorf("subtract attribution pool contribution: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("subtract attribution pool contribution: updated=%d", updated)
	}
	return nil
}

func ensureCommitRelations(ctx context.Context, client *ent.Client, poolID int, commits []commitRef) error {
	kind := attributionusagepoolcommit.RelationKindDirect
	if len(commits) > 1 {
		kind = attributionusagepoolcommit.RelationKindShared
	}
	for _, commit := range commits {
		relation, err := client.AttributionUsagePoolCommit.Query().Where(
			attributionusagepoolcommit.PoolIDEQ(poolID), attributionusagepoolcommit.RepoConfigIDEQ(commit.repoConfigID),
			attributionusagepoolcommit.CommitShaEQ(commit.commitSHA),
		).Only(ctx)
		if err == nil {
			if relation.RelationKind != kind {
				return fmt.Errorf("attribution pool commit relation conflict")
			}
			continue
		}
		if !ent.IsNotFound(err) {
			return fmt.Errorf("query attribution pool commit relation: %w", err)
		}
		if _, err := client.AttributionUsagePoolCommit.Create().SetPoolID(poolID).SetRepoConfigID(commit.repoConfigID).
			SetCommitSha(commit.commitSHA).SetRelationKind(kind).Save(ctx); err != nil {
			return fmt.Errorf("ensure attribution pool commit relation: %w", err)
		}
	}
	return nil
}

func deleteEmptyPool(ctx context.Context, client *ent.Client, poolID int) error {
	pool, err := client.AttributionUsagePool.Get(ctx, poolID)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load prior attribution pool: %w", err)
	}
	if pool.RequestCount != 0 {
		return nil
	}
	if pool.InputTokens != 0 || pool.OutputTokens != 0 || pool.CacheCreationTokens != 0 || pool.CacheReadTokens != 0 || pool.TotalTokens != 0 {
		return fmt.Errorf("empty attribution pool retains Token")
	}
	if _, err := client.AttributionUsagePoolCommit.Delete().Where(attributionusagepoolcommit.PoolIDEQ(poolID)).Exec(ctx); err != nil {
		return fmt.Errorf("delete prior attribution pool relations: %w", err)
	}
	if err := client.AttributionUsagePool.DeleteOneID(poolID).Exec(ctx); err != nil {
		return fmt.Errorf("delete prior attribution pool: %w", err)
	}
	return nil
}
