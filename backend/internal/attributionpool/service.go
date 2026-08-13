package attributionpool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionclaimgroup"
	"github.com/ai-efficiency/backend/ent/attributionrequestclaim"
	"github.com/ai-efficiency/backend/ent/attributionusagepool"
	"github.com/ai-efficiency/backend/ent/attributionusagepoolcommit"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/commitrewrite"
)

const CoverageGapModel = "unresolved"

type commitRef struct {
	repoConfigID int
	commitSHA    string
}

type contribution struct {
	key         string
	ledgerEpoch string
	providerID  int
	userID      int
	model       string
	bucketStart time.Time
	commits     []commitRef
	input       int64
	output      int64
	cacheCreate int64
	cacheRead   int64
	total       int64
	count       int
}

// ApplyLocalGroupChange atomically replaces one Codex-local turn aggregate.
// The hot claim stores only 15-minute aggregates; no Request identity is
// created for this path.
func ApplyLocalGroupChange(ctx context.Context, client *ent.Client, ledgerEpoch string, providerID, userID int, oldAllocations, oldUsage, newAllocations, newUsage []map[string]any) error {
	oldContributions, err := localGroupContributions(ledgerEpoch, providerID, userID, oldAllocations, oldUsage)
	if err != nil {
		return fmt.Errorf("build prior local attribution contribution: %w", err)
	}
	newContributions, err := localGroupContributions(ledgerEpoch, providerID, userID, newAllocations, newUsage)
	if err != nil {
		return fmt.Errorf("build local attribution contribution: %w", err)
	}
	newByKey := make(map[string]contribution, len(newContributions))
	for _, value := range newContributions {
		newByKey[value.key] = value
	}
	orphaned := make(map[string]commitRef)
	oldPoolIDs := make([]int, 0, len(oldContributions))
	for _, value := range oldContributions {
		pool, err := client.AttributionUsagePool.Query().Where(attributionusagepool.CanonicalPoolKeyEQ(value.key)).Only(ctx)
		if err != nil {
			return fmt.Errorf("load prior local attribution pool: %w", err)
		}
		if replacement, ok := newByKey[value.key]; ok {
			delta, err := localContributionGrowth(value, replacement)
			if err != nil {
				return err
			}
			if delta.input != 0 || delta.output != 0 || delta.cacheCreate != 0 || delta.cacheRead != 0 || delta.total != 0 || delta.count != 0 {
				if err := addContribution(ctx, client, pool.ID, delta); err != nil {
					return err
				}
			}
			if err := ensureAllCommitRelations(ctx, client, pool.ID, userID, replacement.commits); err != nil {
				return err
			}
			delete(newByKey, value.key)
			continue
		}
		relations, err := client.AttributionUsagePoolCommit.Query().Where(
			attributionusagepoolcommit.PoolIDEQ(pool.ID), attributionusagepoolcommit.OrphanedEQ(true),
		).All(ctx)
		if err != nil {
			return fmt.Errorf("load prior local attribution orphan relations: %w", err)
		}
		for _, relation := range relations {
			key := fmt.Sprintf("%d:%s", relation.RepoConfigID, relation.CommitSha)
			orphaned[key] = commitRef{repoConfigID: relation.RepoConfigID, commitSHA: relation.CommitSha}
		}
		if err := subtractContribution(ctx, client, pool.ID, value); err != nil {
			return err
		}
		oldPoolIDs = append(oldPoolIDs, pool.ID)
	}
	for _, poolID := range oldPoolIDs {
		if err := deleteEmptyPool(ctx, client, poolID); err != nil {
			return err
		}
	}
	for _, value := range newContributions {
		if _, ok := newByKey[value.key]; !ok {
			continue
		}
		pool, err := ensurePool(ctx, client, value)
		if err != nil {
			return err
		}
		if err := addContribution(ctx, client, pool.ID, value); err != nil {
			return err
		}
		if err := ensureAllCommitRelations(ctx, client, pool.ID, userID, value.commits); err != nil {
			return err
		}
		if err := preserveOrphanedRelations(ctx, client, pool.ID, orphaned); err != nil {
			return err
		}
	}
	return nil
}

func preserveOrphanedRelations(ctx context.Context, client *ent.Client, poolID int, orphaned map[string]commitRef) error {
	for _, commit := range orphaned {
		updated, err := client.AttributionUsagePoolCommit.Update().Where(
			attributionusagepoolcommit.PoolIDEQ(poolID), attributionusagepoolcommit.RepoConfigIDEQ(commit.repoConfigID),
			attributionusagepoolcommit.CommitShaEQ(commit.commitSHA),
		).SetOrphaned(true).Save(ctx)
		if err != nil {
			return fmt.Errorf("preserve local attribution orphan relation: %w", err)
		}
		if updated > 1 {
			return fmt.Errorf("preserve local attribution orphan relation: updated=%d", updated)
		}
	}
	return nil
}

func localContributionGrowth(old, next contribution) (contribution, error) {
	if old.key != next.key || next.input < old.input || next.output < old.output || next.cacheCreate < old.cacheCreate ||
		next.cacheRead < old.cacheRead || next.total < old.total || next.count < old.count {
		return contribution{}, fmt.Errorf("local attribution contribution regressed")
	}
	next.input -= old.input
	next.output -= old.output
	next.cacheCreate -= old.cacheCreate
	next.cacheRead -= old.cacheRead
	next.total -= old.total
	next.count -= old.count
	return next, nil
}

type localUsage struct {
	RequestedModel      string    `json:"requested_model"`
	BucketStartUTC      time.Time `json:"bucket_start_utc"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	CacheCreationTokens int64     `json:"cache_creation_tokens"`
	CacheReadTokens     int64     `json:"cache_read_tokens"`
	TotalTokens         int64     `json:"total_tokens"`
	RequestCount        int       `json:"request_count"`
}

func localGroupContributions(ledgerEpoch string, providerID, userID int, allocations, rawUsage []map[string]any) ([]contribution, error) {
	if len(rawUsage) == 0 {
		return nil, nil
	}
	commits, err := canonicalCommits(allocations)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(rawUsage)
	if err != nil {
		return nil, fmt.Errorf("marshal local usage: %w", err)
	}
	var usage []localUsage
	if err := json.Unmarshal(payload, &usage); err != nil {
		return nil, fmt.Errorf("decode local usage: %w", err)
	}
	result := make([]contribution, 0, len(usage))
	seen := make(map[string]struct{}, len(usage))
	for _, item := range usage {
		item.RequestedModel = strings.TrimSpace(item.RequestedModel)
		item.BucketStartUTC = item.BucketStartUTC.UTC()
		if item.RequestedModel == "" || item.BucketStartUTC.IsZero() || !item.BucketStartUTC.Equal(item.BucketStartUTC.Truncate(15*time.Minute)) || item.RequestCount <= 0 {
			return nil, fmt.Errorf("local usage model, aligned UTC bucket, and positive request count are required")
		}
		if item.InputTokens < 0 || item.OutputTokens < 0 || item.CacheCreationTokens < 0 || item.CacheReadTokens < 0 || item.TotalTokens <= 0 ||
			item.InputTokens > math.MaxInt64-item.OutputTokens || item.InputTokens+item.OutputTokens > math.MaxInt64-item.CacheCreationTokens ||
			item.InputTokens+item.OutputTokens+item.CacheCreationTokens > math.MaxInt64-item.CacheReadTokens ||
			item.InputTokens+item.OutputTokens+item.CacheCreationTokens+item.CacheReadTokens != item.TotalTokens {
			return nil, fmt.Errorf("local usage Token total is inconsistent")
		}
		key := canonicalPoolKey(ledgerEpoch, providerID, userID, item.RequestedModel, item.BucketStartUTC, commits)
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("local usage bucket is duplicated")
		}
		seen[key] = struct{}{}
		result = append(result, contribution{
			key: key, ledgerEpoch: ledgerEpoch, providerID: providerID, userID: userID,
			model: item.RequestedModel, bucketStart: item.BucketStartUTC, commits: commits,
			input: item.InputTokens, output: item.OutputTokens, cacheCreate: item.CacheCreationTokens,
			cacheRead: item.CacheReadTokens, total: item.TotalTokens, count: item.RequestCount,
		})
	}
	return result, nil
}

// ValidateRewrite rejects ambiguous lineage before the event is persisted.
// Rewrite identity is user/repository scoped because a durable pool no longer
// retains workspace identity.
func ValidateRewrite(ctx context.Context, client *ent.Client, userID, repoConfigID int, oldCommitSHA, newCommitSHA string) error {
	_, err := canonicalRewriteTarget(ctx, client, userID, repoConfigID, oldCommitSHA, newCommitSHA)
	return err
}

func canonicalRewriteTarget(ctx context.Context, client *ent.Client, userID, repoConfigID int, oldCommitSHA, newCommitSHA string) (string, error) {
	oldCommitSHA = strings.TrimSpace(oldCommitSHA)
	newCommitSHA = strings.TrimSpace(newCommitSHA)
	if client == nil || userID <= 0 || repoConfigID <= 0 || oldCommitSHA == "" || newCommitSHA == "" || oldCommitSHA == newCommitSHA {
		return "", fmt.Errorf("validate attribution rewrite: distinct commits, user, and repository are required")
	}
	rows, err := client.CommitRewrite.Query().Where(
		commitrewrite.UserIDEQ(userID), commitrewrite.RepoConfigIDEQ(repoConfigID),
	).All(ctx)
	if err != nil {
		return "", fmt.Errorf("query attribution rewrite lineage: %w", err)
	}
	next := make(map[string]string, len(rows)+1)
	for _, row := range rows {
		if existing, ok := next[row.OldCommitSha]; ok && existing != row.NewCommitSha {
			return "", fmt.Errorf("attribution rewrite lineage is already conflicting")
		}
		next[row.OldCommitSha] = row.NewCommitSha
	}
	if existing, ok := next[oldCommitSHA]; ok && existing != newCommitSHA {
		return "", fmt.Errorf("attribution rewrite conflicts with existing mapping")
	}
	next[oldCommitSHA] = newCommitSHA
	seen := map[string]struct{}{oldCommitSHA: {}}
	terminal := newCommitSHA
	for commit := newCommitSHA; commit != ""; commit = next[commit] {
		if _, ok := seen[commit]; ok {
			return "", fmt.Errorf("attribution rewrite would create a cycle")
		}
		seen[commit] = struct{}{}
		terminal = commit
	}
	return terminal, nil
}

// ApplyRewrite migrates hot allocations and durable pools in the caller's
// transaction. Replays are safe: after the first pass no old relation remains.
func ApplyRewrite(ctx context.Context, client *ent.Client, userID, repoConfigID int, oldCommitSHA, newCommitSHA string, now time.Time) error {
	terminal, err := canonicalRewriteTarget(ctx, client, userID, repoConfigID, oldCommitSHA, newCommitSHA)
	if err != nil {
		return err
	}
	newCommitSHA = terminal
	groups, err := client.AttributionClaimGroup.Query().Where(
		attributionclaimgroup.UserIDEQ(userID), attributionclaimgroup.FinalizedAtIsNil(),
	).Order(ent.Asc(attributionclaimgroup.FieldID)).All(ctx)
	if err != nil {
		return fmt.Errorf("query hot attribution groups for rewrite: %w", err)
	}
	for _, group := range groups {
		allocations, changed := rewriteAllocations(group.CommitAllocations, repoConfigID, oldCommitSHA, newCommitSHA)
		if !changed {
			continue
		}
		updated, err := client.AttributionClaimGroup.Update().Where(
			attributionclaimgroup.IDEQ(group.ID), attributionclaimgroup.UpdatedAtEQ(group.UpdatedAt),
		).SetCommitAllocations(allocations).Save(ctx)
		if err != nil {
			return fmt.Errorf("rewrite hot attribution group: %w", err)
		}
		if updated != 1 {
			return fmt.Errorf("rewrite hot attribution group raced: updated=%d", updated)
		}
		if err := MaterializeGroup(ctx, client, group.ID, now); err != nil {
			return fmt.Errorf("rematerialize rewritten attribution group: %w", err)
		}
	}
	return migrateDurablePools(ctx, client, userID, repoConfigID, oldCommitSHA, newCommitSHA)
}

func rewriteAllocations(allocations []map[string]any, repoConfigID int, oldCommitSHA, newCommitSHA string) ([]map[string]any, bool) {
	result := make([]map[string]any, len(allocations))
	changed := false
	for index, allocation := range allocations {
		copy := make(map[string]any, len(allocation))
		for key, value := range allocation {
			copy[key] = value
		}
		repoID, ok := integerValue(copy["repo_config_id"])
		commitSHA, _ := copy["commit_sha"].(string)
		if ok && repoID == repoConfigID && strings.TrimSpace(commitSHA) == oldCommitSHA {
			copy["commit_sha"] = newCommitSHA
			changed = true
		}
		result[index] = copy
	}
	return result, changed
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
	desired, err := canonicalContribution(group.LedgerEpoch, group.RelayProviderID, group.UserID, group.CommitAllocations, claim)
	if err != nil {
		return fmt.Errorf("build request claim contribution: %w", err)
	}
	pool, err := ensurePool(ctx, client, desired)
	if err != nil {
		return err
	}
	if claim.MaterializedPoolID != nil && *claim.MaterializedPoolID == pool.ID {
		return ensureAllCommitRelations(ctx, client, pool.ID, desired.userID, desired.commits)
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
	if err := ensureAllCommitRelations(ctx, client, pool.ID, desired.userID, desired.commits); err != nil {
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
	parts := []string{group.LedgerEpoch, strconv.Itoa(group.RelayProviderID), strconv.Itoa(group.UserID), CoverageGapModel, bucket.Format(time.RFC3339)}
	for _, commit := range commits {
		parts = append(parts, fmt.Sprintf("%d:%s", commit.repoConfigID, commit.commitSHA))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	value := contribution{
		key: hex.EncodeToString(sum[:]), ledgerEpoch: group.LedgerEpoch, providerID: group.RelayProviderID, userID: group.UserID, model: CoverageGapModel,
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
	return ensureAllCommitRelations(ctx, client, pool.ID, group.UserID, commits)
}

func canonicalContribution(ledgerEpoch string, providerID, userID int, allocations []map[string]any, claim *ent.AttributionRequestClaim) (contribution, error) {
	ledgerEpoch = strings.TrimSpace(ledgerEpoch)
	if ledgerEpoch == "" || providerID <= 0 || userID <= 0 || claim == nil || strings.TrimSpace(claim.RequestedModel) == "" || claim.UsageAt == nil || claim.UsageAt.IsZero() {
		return contribution{}, fmt.Errorf("provider, user, requested model, and usage time are required")
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
	return contribution{
		key: canonicalPoolKey(ledgerEpoch, providerID, userID, model, bucket, commits), ledgerEpoch: ledgerEpoch, providerID: providerID, userID: userID, model: model, bucketStart: bucket, commits: commits,
		input: claim.InputTokens, output: claim.OutputTokens, cacheCreate: claim.CacheCreationTokens,
		cacheRead: claim.CacheReadTokens, total: claim.TotalTokens, count: 1,
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
			SetCanonicalPoolKey(value.key).SetLedgerEpoch(value.ledgerEpoch).SetRelayProviderID(value.providerID).SetUserID(value.userID).
			SetRequestedModel(value.model).SetBucketStartUtc(value.bucketStart).Save(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("ensure attribution usage pool: %w", err)
	}
	if pool.LedgerEpoch != value.ledgerEpoch || pool.RelayProviderID != value.providerID || pool.UserID != value.userID || pool.RequestedModel != value.model || !pool.BucketStartUtc.Equal(value.bucketStart) {
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
		attributionusagepool.RequestCountLTE(math.MaxInt-value.count),
	).AddInputTokens(value.input).AddOutputTokens(value.output).AddCacheCreationTokens(value.cacheCreate).
		AddCacheReadTokens(value.cacheRead).AddTotalTokens(value.total).AddRequestCount(value.count).Save(ctx)
	if err != nil {
		return fmt.Errorf("add attribution pool contribution: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("add attribution pool contribution: updated=%d", updated)
	}
	return nil
}

func desiredFromClaim(claim *ent.AttributionRequestClaim) contribution {
	return contribution{input: claim.InputTokens, output: claim.OutputTokens, cacheCreate: claim.CacheCreationTokens, cacheRead: claim.CacheReadTokens, total: claim.TotalTokens, count: 1}
}

func subtractContribution(ctx context.Context, client *ent.Client, poolID int, value contribution) error {
	updated, err := client.AttributionUsagePool.Update().Where(
		attributionusagepool.IDEQ(poolID), attributionusagepool.InputTokensGTE(value.input),
		attributionusagepool.OutputTokensGTE(value.output), attributionusagepool.CacheCreationTokensGTE(value.cacheCreate),
		attributionusagepool.CacheReadTokensGTE(value.cacheRead), attributionusagepool.TotalTokensGTE(value.total),
		attributionusagepool.RequestCountGTE(value.count),
	).AddInputTokens(-value.input).AddOutputTokens(-value.output).AddCacheCreationTokens(-value.cacheCreate).
		AddCacheReadTokens(-value.cacheRead).AddTotalTokens(-value.total).AddRequestCount(-value.count).Save(ctx)
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

func ensureAllCommitRelations(ctx context.Context, client *ent.Client, poolID, userID int, commits []commitRef) error {
	if err := ensureCommitRelations(ctx, client, poolID, commits); err != nil {
		return err
	}
	return ensureInheritedRelations(ctx, client, poolID, userID, commits)
}

// ApplyCherryPick projects an explicitly patch-matched target without moving
// or recounting the source pool.
func ApplyCherryPick(ctx context.Context, client *ent.Client, userID, repoConfigID int, sourceCommitSHA, targetCommitSHA, sourcePatchID, targetPatchID string) error {
	if client == nil || userID <= 0 || repoConfigID <= 0 || strings.TrimSpace(sourceCommitSHA) == "" || strings.TrimSpace(targetCommitSHA) == "" ||
		strings.TrimSpace(sourcePatchID) == "" || sourcePatchID != targetPatchID || sourceCommitSHA == targetCommitSHA {
		return fmt.Errorf("apply inherited attribution lineage: explicit distinct commits and matching stable patch evidence are required")
	}
	lineage, err := loadRewriteMap(ctx, client, userID, repoConfigID)
	if err != nil {
		return err
	}
	sourceCommitSHA, err = resolveRewrite(lineage, sourceCommitSHA)
	if err != nil {
		return err
	}
	targetCommitSHA, err = resolveRewrite(lineage, targetCommitSHA)
	if err != nil {
		return err
	}
	relations, err := client.AttributionUsagePoolCommit.Query().Where(
		attributionusagepoolcommit.RepoConfigIDEQ(repoConfigID),
		attributionusagepoolcommit.CommitShaEQ(sourceCommitSHA),
		attributionusagepoolcommit.RelationKindNEQ(attributionusagepoolcommit.RelationKindInheritedNonCounting),
	).All(ctx)
	if err != nil {
		return fmt.Errorf("query inherited attribution source pools: %w", err)
	}
	for _, relation := range relations {
		pool, err := client.AttributionUsagePool.Get(ctx, relation.PoolID)
		if err != nil {
			return fmt.Errorf("load inherited attribution source pool: %w", err)
		}
		if pool.UserID != userID {
			continue
		}
		if err := ensureInheritedRelation(ctx, client, pool.ID, repoConfigID, targetCommitSHA); err != nil {
			return err
		}
	}
	return nil
}

// MarkCommitOrphaned is intentionally an internal boundary: only a caller
// holding authoritative SCM reachability evidence may invoke it. Git reset,
// branch deletion, force-push, patch similarity, and time are not inputs.
func MarkCommitOrphaned(ctx context.Context, client *ent.Client, userID, repoConfigID int, commitSHA, evidenceSource string) error {
	if client == nil || userID <= 0 || repoConfigID <= 0 || strings.TrimSpace(commitSHA) == "" || evidenceSource != "authoritative_scm" {
		return fmt.Errorf("mark attribution commit orphaned: authoritative SCM evidence is required")
	}
	pools, err := client.AttributionUsagePool.Query().Where(attributionusagepool.UserIDEQ(userID)).IDs(ctx)
	if err != nil {
		return fmt.Errorf("query attribution pools for orphan marking: %w", err)
	}
	if len(pools) == 0 {
		return nil
	}
	if _, err := client.AttributionUsagePoolCommit.Update().Where(
		attributionusagepoolcommit.PoolIDIn(pools...), attributionusagepoolcommit.RepoConfigIDEQ(repoConfigID),
		attributionusagepoolcommit.CommitShaEQ(strings.TrimSpace(commitSHA)),
	).SetOrphaned(true).Save(ctx); err != nil {
		return fmt.Errorf("mark attribution commit orphaned: %w", err)
	}
	return nil
}

func ensureInheritedRelations(ctx context.Context, client *ent.Client, poolID, userID int, commits []commitRef) error {
	for _, source := range commits {
		lineage, err := loadRewriteMap(ctx, client, userID, source.repoConfigID)
		if err != nil {
			return err
		}
		checkpoints, err := client.CommitCheckpoint.Query().Where(
			commitcheckpoint.UserIDEQ(userID), commitcheckpoint.RepoConfigIDEQ(source.repoConfigID),
			commitcheckpoint.LineageKindEQ(commitcheckpoint.LineageKindCherryPick),
		).All(ctx)
		if err != nil {
			return fmt.Errorf("query inherited attribution checkpoints: %w", err)
		}
		for _, checkpoint := range checkpoints {
			if checkpoint.CommitPatchID == "" || checkpoint.CommitPatchID != checkpoint.SourcePatchID {
				continue
			}
			resolvedSource, err := resolveRewrite(lineage, checkpoint.SourceCommitSha)
			if err != nil {
				return err
			}
			if resolvedSource != source.commitSHA {
				continue
			}
			resolvedTarget, err := resolveRewrite(lineage, checkpoint.CommitSha)
			if err != nil {
				return err
			}
			if err := ensureInheritedRelation(ctx, client, poolID, source.repoConfigID, resolvedTarget); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadRewriteMap(ctx context.Context, client *ent.Client, userID, repoConfigID int) (map[string]string, error) {
	rows, err := client.CommitRewrite.Query().Where(
		commitrewrite.UserIDEQ(userID), commitrewrite.RepoConfigIDEQ(repoConfigID),
	).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query attribution rewrite map: %w", err)
	}
	result := make(map[string]string, len(rows))
	for _, row := range rows {
		if existing, ok := result[row.OldCommitSha]; ok && existing != row.NewCommitSha {
			return nil, fmt.Errorf("attribution rewrite map is conflicting")
		}
		result[row.OldCommitSha] = row.NewCommitSha
	}
	return result, nil
}

func resolveRewrite(lineage map[string]string, commitSHA string) (string, error) {
	commitSHA = strings.TrimSpace(commitSHA)
	seen := make(map[string]struct{}, len(lineage)+1)
	for {
		if commitSHA == "" {
			return "", fmt.Errorf("attribution rewrite commit is required")
		}
		if _, ok := seen[commitSHA]; ok {
			return "", fmt.Errorf("attribution rewrite map contains a cycle")
		}
		seen[commitSHA] = struct{}{}
		next := lineage[commitSHA]
		if next == "" {
			return commitSHA, nil
		}
		commitSHA = next
	}
}

func ensureInheritedRelation(ctx context.Context, client *ent.Client, poolID, repoConfigID int, commitSHA string) error {
	_, err := client.AttributionUsagePoolCommit.Query().Where(
		attributionusagepoolcommit.PoolIDEQ(poolID), attributionusagepoolcommit.RepoConfigIDEQ(repoConfigID),
		attributionusagepoolcommit.CommitShaEQ(commitSHA),
	).Only(ctx)
	if err == nil {
		return nil
	}
	if !ent.IsNotFound(err) {
		return fmt.Errorf("query inherited attribution relation: %w", err)
	}
	if _, err := client.AttributionUsagePoolCommit.Create().SetPoolID(poolID).SetRepoConfigID(repoConfigID).
		SetCommitSha(commitSHA).SetRelationKind(attributionusagepoolcommit.RelationKindInheritedNonCounting).Save(ctx); err != nil {
		return fmt.Errorf("create inherited attribution relation: %w", err)
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
	if pool.RequestCount != 0 || pool.CoverageGapCount != 0 {
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

func migrateDurablePools(ctx context.Context, client *ent.Client, userID, repoConfigID int, oldCommitSHA, newCommitSHA string) error {
	relations, err := client.AttributionUsagePoolCommit.Query().Where(
		attributionusagepoolcommit.RepoConfigIDEQ(repoConfigID),
		attributionusagepoolcommit.CommitShaEQ(oldCommitSHA),
	).Order(ent.Asc(attributionusagepoolcommit.FieldPoolID)).All(ctx)
	if err != nil {
		return fmt.Errorf("query durable attribution rewrite relations: %w", err)
	}
	for _, relation := range relations {
		pool, err := client.AttributionUsagePool.Get(ctx, relation.PoolID)
		if ent.IsNotFound(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("load durable attribution rewrite pool: %w", err)
		}
		if pool.UserID != userID {
			continue
		}
		allRelations, err := client.AttributionUsagePoolCommit.Query().Where(
			attributionusagepoolcommit.PoolIDEQ(pool.ID),
		).Order(ent.Asc(attributionusagepoolcommit.FieldID)).All(ctx)
		if err != nil {
			return fmt.Errorf("load durable attribution rewrite pool relations: %w", err)
		}
		counting := make([]commitRef, 0, len(allRelations))
		inherited := make([]*ent.AttributionUsagePoolCommit, 0, len(allRelations))
		for _, item := range allRelations {
			sha := item.CommitSha
			if item.RepoConfigID == repoConfigID && sha == oldCommitSHA {
				sha = newCommitSHA
			}
			if item.RelationKind == attributionusagepoolcommit.RelationKindInheritedNonCounting {
				copy := *item
				copy.CommitSha = sha
				inherited = append(inherited, &copy)
				continue
			}
			counting = append(counting, commitRef{repoConfigID: item.RepoConfigID, commitSHA: sha})
		}
		counting = uniqueCommitRefs(counting)
		desired := contribution{
			key:         canonicalPoolKey(pool.LedgerEpoch, pool.RelayProviderID, pool.UserID, pool.RequestedModel, pool.BucketStartUtc, counting),
			ledgerEpoch: pool.LedgerEpoch,
			providerID:  pool.RelayProviderID, userID: pool.UserID, model: pool.RequestedModel, bucketStart: pool.BucketStartUtc, commits: counting,
			input: pool.InputTokens, output: pool.OutputTokens, cacheCreate: pool.CacheCreationTokens,
			cacheRead: pool.CacheReadTokens, total: pool.TotalTokens,
		}
		target, err := ensurePool(ctx, client, desired)
		if err != nil {
			return err
		}
		if target.ID == pool.ID {
			if relation.RelationKind == attributionusagepoolcommit.RelationKindInheritedNonCounting {
				if err := ensureInheritedRelation(ctx, client, pool.ID, repoConfigID, newCommitSHA); err != nil {
					return err
				}
				if err := client.AttributionUsagePoolCommit.DeleteOneID(relation.ID).Exec(ctx); err != nil {
					return fmt.Errorf("delete prior inherited attribution rewrite relation: %w", err)
				}
			}
			continue
		}
		if err := mergePoolTotals(ctx, client, target.ID, pool); err != nil {
			return err
		}
		if err := ensureCommitRelations(ctx, client, target.ID, counting); err != nil {
			return err
		}
		for _, item := range inherited {
			if containsCommit(counting, item.RepoConfigID, item.CommitSha) {
				continue
			}
			existing, err := client.AttributionUsagePoolCommit.Query().Where(
				attributionusagepoolcommit.PoolIDEQ(target.ID), attributionusagepoolcommit.RepoConfigIDEQ(item.RepoConfigID),
				attributionusagepoolcommit.CommitShaEQ(item.CommitSha),
			).Only(ctx)
			if err == nil {
				if existing.RelationKind != attributionusagepoolcommit.RelationKindInheritedNonCounting {
					return fmt.Errorf("inherited attribution rewrite relation conflicts with counting relation")
				}
				continue
			}
			if !ent.IsNotFound(err) {
				return fmt.Errorf("query inherited attribution rewrite relation: %w", err)
			}
			if _, err := client.AttributionUsagePoolCommit.Create().SetPoolID(target.ID).SetRepoConfigID(item.RepoConfigID).
				SetCommitSha(item.CommitSha).SetRelationKind(attributionusagepoolcommit.RelationKindInheritedNonCounting).
				SetOrphaned(item.Orphaned).Save(ctx); err != nil {
				return fmt.Errorf("copy inherited attribution rewrite relation: %w", err)
			}
		}
		if _, err := client.AttributionRequestClaim.Update().Where(
			attributionrequestclaim.MaterializedPoolIDEQ(pool.ID),
		).SetMaterializedPoolID(target.ID).Save(ctx); err != nil {
			return fmt.Errorf("move attribution request pointers after rewrite: %w", err)
		}
		if _, err := client.AttributionUsagePoolCommit.Delete().Where(attributionusagepoolcommit.PoolIDEQ(pool.ID)).Exec(ctx); err != nil {
			return fmt.Errorf("delete rewritten attribution pool relations: %w", err)
		}
		if err := client.AttributionUsagePool.DeleteOneID(pool.ID).Exec(ctx); err != nil {
			return fmt.Errorf("delete rewritten attribution pool: %w", err)
		}
	}
	return nil
}

func canonicalPoolKey(ledgerEpoch string, providerID, userID int, model string, bucket time.Time, commits []commitRef) string {
	parts := []string{strings.TrimSpace(ledgerEpoch), strconv.Itoa(providerID), strconv.Itoa(userID), strings.TrimSpace(model), bucket.UTC().Format(time.RFC3339)}
	for _, commit := range commits {
		parts = append(parts, fmt.Sprintf("%d:%s", commit.repoConfigID, commit.commitSHA))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func uniqueCommitRefs(commits []commitRef) []commitRef {
	unique := make(map[string]commitRef, len(commits))
	for _, commit := range commits {
		unique[fmt.Sprintf("%d:%s", commit.repoConfigID, commit.commitSHA)] = commit
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]commitRef, 0, len(keys))
	for _, key := range keys {
		result = append(result, unique[key])
	}
	return result
}

func containsCommit(commits []commitRef, repoConfigID int, commitSHA string) bool {
	for _, commit := range commits {
		if commit.repoConfigID == repoConfigID && commit.commitSHA == commitSHA {
			return true
		}
	}
	return false
}

func mergePoolTotals(ctx context.Context, client *ent.Client, targetID int, source *ent.AttributionUsagePool) error {
	updated, err := client.AttributionUsagePool.Update().Where(
		attributionusagepool.IDEQ(targetID),
		attributionusagepool.InputTokensLTE(math.MaxInt64-source.InputTokens),
		attributionusagepool.OutputTokensLTE(math.MaxInt64-source.OutputTokens),
		attributionusagepool.CacheCreationTokensLTE(math.MaxInt64-source.CacheCreationTokens),
		attributionusagepool.CacheReadTokensLTE(math.MaxInt64-source.CacheReadTokens),
		attributionusagepool.TotalTokensLTE(math.MaxInt64-source.TotalTokens),
		attributionusagepool.RequestCountLTE(math.MaxInt-source.RequestCount),
		attributionusagepool.CoverageGapCountLTE(math.MaxInt-source.CoverageGapCount),
	).AddInputTokens(source.InputTokens).AddOutputTokens(source.OutputTokens).
		AddCacheCreationTokens(source.CacheCreationTokens).AddCacheReadTokens(source.CacheReadTokens).
		AddTotalTokens(source.TotalTokens).AddRequestCount(source.RequestCount).
		AddCoverageGapCount(source.CoverageGapCount).Save(ctx)
	if err != nil {
		return fmt.Errorf("merge rewritten attribution pool: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("merge rewritten attribution pool: updated=%d", updated)
	}
	return nil
}
