package attributionclaim

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionclaimgroup"
	"github.com/ai-efficiency/backend/ent/attributionrequestclaim"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/attributionledger"
	"github.com/ai-efficiency/backend/internal/attributionpool"
)

const (
	SchemaVersion            = 2
	MaxGroups                = 20
	MaxRequests              = 100
	MaxIdentitySize          = 256
	HotRetention             = 90 * 24 * time.Hour
	TokenSourceRelayOfficial = "relay_official"
	TokenSourceCodexLocal    = "codex_local"
)

type Request struct {
	SchemaVersion     int                `json:"schema_version"`
	GroupID           string             `json:"group_id"`
	RelayProviderID   int                `json:"relay_provider_id"`
	TokenSource       string             `json:"token_source,omitempty"`
	ThreadID          string             `json:"thread_id"`
	TurnID            string             `json:"turn_id"`
	EvidenceDigest    string             `json:"evidence_digest"`
	Calibration       *Calibration       `json:"calibration,omitempty"`
	LocalUsage        []LocalUsageBucket `json:"local_usage,omitempty"`
	CommitAllocations []CommitAllocation `json:"commit_allocations"`
	RequestIDs        []string           `json:"request_ids"`
}

type LocalUsageBucket struct {
	RequestedModel      string    `json:"requested_model"`
	BucketStartUTC      time.Time `json:"bucket_start_utc"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	CacheCreationTokens int64     `json:"cache_creation_tokens"`
	CacheReadTokens     int64     `json:"cache_read_tokens"`
	TotalTokens         int64     `json:"total_tokens"`
	CreditUsage         float64   `json:"credit_usage,omitempty"`
	RequestCount        int       `json:"request_count"`
}

type Calibration struct {
	Digest              string `json:"digest"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	TotalTokens         int64  `json:"total_tokens"`
}

type CommitAllocation struct {
	Sequence          int    `json:"sequence"`
	RepoConfigID      int    `json:"repo_config_id"`
	RepoKey           string `json:"repo_key,omitempty"`
	WorkspaceID       string `json:"workspace_id"`
	CheckpointEventID string `json:"checkpoint_event_id"`
	CommitSHA         string `json:"commit_sha"`
	EvidenceDigest    string `json:"evidence_digest"`
}

type BatchRequest struct {
	Groups []Request `json:"groups"`
}

type ItemStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type Result struct {
	Group       ItemStatus   `json:"group"`
	Calibration ItemStatus   `json:"calibration"`
	Requests    []ItemStatus `json:"requests"`
}

type BatchResult struct {
	Epoch             string   `json:"ledger_epoch"`
	V1WritePolicy     string   `json:"v1_write_policy"`
	MinimumCLIVersion string   `json:"minimum_cli_version,omitempty"`
	Results           []Result `json:"results"`
}

type Service struct {
	client   *ent.Client
	now      func() time.Time
	protocol attributionledger.ProtocolContract
}

func NewService(client *ent.Client, protocol attributionledger.ProtocolContract) *Service {
	return &Service{client: client, now: time.Now, protocol: protocol}
}

func (s *Service) Ingest(ctx context.Context, principal attributionledger.InstallationPrincipal, batch BatchRequest) (BatchResult, error) {
	result := BatchResult{Epoch: s.protocol.LedgerEpoch, V1WritePolicy: s.protocol.V1WritePolicy, MinimumCLIVersion: s.protocol.MinimumCLIVersion, Results: make([]Result, 0, len(batch.Groups))}
	if s == nil || s.client == nil || principal.DatabaseID <= 0 || principal.UserID <= 0 {
		return result, fmt.Errorf("ingest v2 claims: client and installation principal are required")
	}
	if len(batch.Groups) == 0 || len(batch.Groups) > MaxGroups {
		return result, fmt.Errorf("ingest v2 claims: groups must contain 1-%d items", MaxGroups)
	}
	for _, claim := range batch.Groups {
		item, err := s.ingestOne(ctx, principal, normalize(claim))
		if err != nil {
			item.Group = ItemStatus{ID: strings.TrimSpace(claim.GroupID), Status: "rejected", Error: err.Error()}
		}
		result.Results = append(result.Results, item)
	}
	return result, nil
}

func (s *Service) ingestOne(ctx context.Context, principal attributionledger.InstallationPrincipal, claim Request) (Result, error) {
	result := Result{
		Group:       ItemStatus{ID: claim.GroupID, Status: "persisted"},
		Calibration: ItemStatus{Status: "not_present"},
		Requests:    make([]ItemStatus, 0, len(claim.RequestIDs)),
	}
	if claim.Calibration != nil {
		result.Calibration.ID = claim.Calibration.Digest
	}
	if err := validate(claim); err != nil {
		return result, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return result, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	provider, err := tx.RelayProvider.Query().Where(relayprovider.IDEQ(claim.RelayProviderID)).Only(ctx)
	if err != nil || !provider.Enabled {
		return result, fmt.Errorf("relay provider is missing or disabled")
	}
	for _, allocation := range claim.CommitAllocations {
		checkpoint, err := tx.CommitCheckpoint.Query().Where(commitcheckpoint.EventIDEQ(allocation.CheckpointEventID)).Only(ctx)
		if err != nil || checkpoint.RepoConfigID != allocation.RepoConfigID || checkpoint.CommitSha != allocation.CommitSHA || checkpoint.WorkspaceID != allocation.WorkspaceID || checkpoint.UserID == nil || *checkpoint.UserID != principal.UserID {
			return result, fmt.Errorf("checkpoint allocation %d does not belong to the installation owner, repository, workspace, and commit", allocation.Sequence)
		}
		if _, err := tx.RepoConfig.Get(ctx, allocation.RepoConfigID); err != nil {
			return result, fmt.Errorf("repository for allocation %d does not exist", allocation.Sequence)
		}
	}

	group, groupCreated, groupChanged, calibrationStatus, err := upsertGroup(ctx, tx, principal, claim, s.now().UTC().Add(HotRetention), s.protocol.LedgerEpoch)
	if err != nil {
		return result, err
	}
	if !groupCreated && !groupChanged {
		result.Group.Status = "duplicate_identical"
	}
	if claim.Calibration != nil {
		result.Calibration.Status = calibrationStatus
	}

	created := 0
	for _, requestID := range claim.RequestIDs {
		status, wasCreated, err := upsertRequest(ctx, tx, group.ID, claim, requestID, group.ExpiresAt)
		if err != nil {
			invalidatePersistedACKs(&result, "rolled_back")
			result.Requests = append(result.Requests, ItemStatus{ID: requestID, Status: "conflict", Error: err.Error()})
			return result, err
		}
		result.Requests = append(result.Requests, status)
		if wasCreated {
			created++
		}
	}
	if created > 0 && !groupCreated {
		result.Group.Status = "persisted"
		if err := group.Update().SetRequestCount(group.RequestCount + created).Exec(ctx); err != nil {
			invalidatePersistedACKs(&result, "rolled_back")
			return result, fmt.Errorf("update claim group request count: %w", err)
		}
	}
	if groupChanged {
		if err := attributionpool.MaterializeGroup(ctx, tx.Client(), group.ID, s.now().UTC()); err != nil {
			invalidatePersistedACKs(&result, "rolled_back")
			return result, fmt.Errorf("rematerialize claim group: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		invalidatePersistedACKs(&result, "unknown")
		return result, fmt.Errorf("commit transaction: %w", err)
	}
	return result, nil
}

func upsertGroup(ctx context.Context, tx *ent.Tx, principal attributionledger.InstallationPrincipal, claim Request, expiresAt time.Time, ledgerEpoch string) (*ent.AttributionClaimGroup, bool, bool, string, error) {
	incomingAllocations := allocationMaps(claim.CommitAllocations)
	group, err := tx.AttributionClaimGroup.Query().Where(attributionclaimgroup.GroupIDEQ(claim.GroupID)).Only(ctx)
	if err == nil {
		locked, lockErr := tx.AttributionClaimGroup.Update().Where(
			attributionclaimgroup.IDEQ(group.ID), attributionclaimgroup.FinalizedAtIsNil(),
		).AddRequestCount(0).Save(ctx)
		if lockErr != nil {
			return nil, false, false, "not_present", fmt.Errorf("lock claim group: %w", lockErr)
		}
		if locked != 1 {
			return nil, false, false, "not_present", fmt.Errorf("claim group is finalized")
		}
		group, err = tx.AttributionClaimGroup.Get(ctx, group.ID)
		if err != nil {
			return nil, false, false, "not_present", fmt.Errorf("reload claim group: %w", err)
		}
		if group.InstallationID != principal.DatabaseID || group.UserID != principal.UserID || group.RelayProviderID != claim.RelayProviderID ||
			tokenSourceForGroup(group) != claim.TokenSource ||
			group.ThreadID != claim.ThreadID || group.TurnID != claim.TurnID ||
			group.SchemaVersion != SchemaVersion || group.LedgerEpoch != ledgerEpoch {
			return nil, false, false, "not_present", fmt.Errorf("claim group conflict")
		}
		allocationChanged, compatible := compatibleAllocations(group.CommitAllocations, incomingAllocations)
		if !compatible {
			return nil, false, false, "not_present", fmt.Errorf("claim group allocation conflict")
		}
		if !allocationChanged && group.EvidenceDigest != claim.EvidenceDigest {
			return nil, false, false, "not_present", fmt.Errorf("claim group evidence conflict")
		}
		calibrationStatus := "not_present"
		calibrationChanged := false
		if claim.Calibration != nil {
			switch {
			case group.CalibrationDigest == "":
				calibrationStatus = "persisted"
				calibrationChanged = true
			case calibrationMatches(group, *claim.Calibration):
				calibrationStatus = "duplicate_identical"
			default:
				calibrationStatus = "conflict"
			}
		}
		incomingLocalUsage := localUsageMaps(claim.LocalUsage)
		localUsageChanged, localUsageCompatible := compatibleLocalUsage(group.LocalUsage, incomingLocalUsage)
		if !localUsageCompatible {
			return nil, false, false, "not_present", fmt.Errorf("claim group local usage conflict")
		}
		if claim.TokenSource == TokenSourceCodexLocal && (allocationChanged || localUsageChanged) {
			if err := attributionpool.ApplyLocalGroupChange(ctx, tx.Client(), group.LedgerEpoch, group.RelayProviderID, group.UserID, group.CommitAllocations, group.LocalUsage, incomingAllocations, incomingLocalUsage); err != nil {
				return nil, false, false, "not_present", fmt.Errorf("rematerialize local claim group: %w", err)
			}
		}
		if allocationChanged || calibrationChanged || localUsageChanged {
			update := group.Update()
			if allocationChanged {
				update.SetCommitAllocations(incomingAllocations).SetEvidenceDigest(claim.EvidenceDigest)
				group.CommitAllocations = incomingAllocations
				group.EvidenceDigest = claim.EvidenceDigest
			}
			if calibrationChanged {
				setCalibrationUpdate(update, *claim.Calibration)
			}
			if localUsageChanged {
				update.SetLocalUsage(incomingLocalUsage).SetRequestCount(localUsageRequestCount(claim.LocalUsage))
				group.LocalUsage = incomingLocalUsage
			}
			if err := update.Exec(ctx); err != nil {
				return nil, false, false, calibrationStatus, fmt.Errorf("update claim group: %w", err)
			}
		}
		return group, false, allocationChanged || calibrationChanged || localUsageChanged, calibrationStatus, nil
	}
	if !ent.IsNotFound(err) {
		return nil, false, false, "not_present", fmt.Errorf("query claim group: %w", err)
	}
	if claim.TokenSource == TokenSourceRelayOfficial && len(claim.RequestIDs) == 0 {
		return nil, false, false, "not_present", fmt.Errorf("new relay_official claim group requires at least one request_id")
	}
	incomingLocalUsage := localUsageMaps(claim.LocalUsage)
	create := tx.AttributionClaimGroup.Create().
		SetGroupID(claim.GroupID).SetInstallationID(principal.DatabaseID).SetUserID(principal.UserID).
		SetRelayProviderID(claim.RelayProviderID).
		SetSchemaVersion(SchemaVersion).SetLedgerEpoch(ledgerEpoch).SetThreadID(claim.ThreadID).SetTurnID(claim.TurnID).
		SetEvidenceDigest(claim.EvidenceDigest).SetLocalUsage(incomingLocalUsage).SetCommitAllocations(incomingAllocations).
		SetRequestCount(maxInt(len(claim.RequestIDs), localUsageRequestCount(claim.LocalUsage))).SetExpiresAt(expiresAt)
	calibrationStatus := "not_present"
	if claim.Calibration != nil {
		setCalibrationCreate(create, *claim.Calibration)
		calibrationStatus = "persisted"
	}
	group, err = create.Save(ctx)
	if err != nil {
		return nil, false, false, calibrationStatus, fmt.Errorf("create claim group: %w", err)
	}
	if claim.TokenSource == TokenSourceCodexLocal {
		if err := attributionpool.ApplyLocalGroupChange(ctx, tx.Client(), ledgerEpoch, claim.RelayProviderID, principal.UserID, nil, nil, incomingAllocations, incomingLocalUsage); err != nil {
			return nil, false, false, calibrationStatus, fmt.Errorf("materialize local claim group: %w", err)
		}
	}
	return group, true, true, calibrationStatus, nil
}

func upsertRequest(ctx context.Context, tx *ent.Tx, groupID int, claim Request, requestID string, expiresAt time.Time) (ItemStatus, bool, error) {
	digest := digestStrings(claim.GroupID, requestID)
	existing, err := tx.AttributionRequestClaim.Query().Where(
		attributionrequestclaim.RelayProviderIDEQ(claim.RelayProviderID),
		attributionrequestclaim.RequestIDEQ(requestID),
	).Only(ctx)
	if err == nil {
		if existing.ClaimGroupID != groupID || existing.CanonicalDigest != digest {
			return ItemStatus{}, false, fmt.Errorf("request %q conflicts with an existing claim", requestID)
		}
		return ItemStatus{ID: requestID, Status: "duplicate_identical"}, false, nil
	}
	if !ent.IsNotFound(err) {
		return ItemStatus{}, false, fmt.Errorf("query request claim: %w", err)
	}
	if _, err := tx.AttributionRequestClaim.Create().SetClaimGroupID(groupID).SetRelayProviderID(claim.RelayProviderID).
		SetRequestID(requestID).SetCanonicalDigest(digest).SetExpiresAt(expiresAt).Save(ctx); err != nil {
		return ItemStatus{}, false, fmt.Errorf("create request claim: %w", err)
	}
	return ItemStatus{ID: requestID, Status: "persisted"}, true, nil
}

func normalize(claim Request) Request {
	claim.GroupID = strings.TrimSpace(claim.GroupID)
	claim.ThreadID = strings.TrimSpace(claim.ThreadID)
	claim.TurnID = strings.TrimSpace(claim.TurnID)
	claim.EvidenceDigest = strings.TrimSpace(claim.EvidenceDigest)
	claim.TokenSource = strings.TrimSpace(claim.TokenSource)
	if claim.TokenSource == "" {
		claim.TokenSource = TokenSourceRelayOfficial
	}
	if claim.Calibration != nil {
		claim.Calibration.Digest = strings.TrimSpace(claim.Calibration.Digest)
	}
	for index := range claim.CommitAllocations {
		allocation := &claim.CommitAllocations[index]
		allocation.RepoKey = strings.TrimSpace(allocation.RepoKey)
		allocation.WorkspaceID = strings.TrimSpace(allocation.WorkspaceID)
		allocation.CheckpointEventID = strings.TrimSpace(allocation.CheckpointEventID)
		allocation.CommitSHA = strings.TrimSpace(allocation.CommitSHA)
		allocation.EvidenceDigest = strings.TrimSpace(allocation.EvidenceDigest)
	}
	for index := range claim.LocalUsage {
		usage := &claim.LocalUsage[index]
		usage.RequestedModel = strings.TrimSpace(usage.RequestedModel)
		usage.BucketStartUTC = usage.BucketStartUTC.UTC()
	}
	sort.Slice(claim.LocalUsage, func(i, j int) bool {
		if claim.LocalUsage[i].BucketStartUTC.Equal(claim.LocalUsage[j].BucketStartUTC) {
			return claim.LocalUsage[i].RequestedModel < claim.LocalUsage[j].RequestedModel
		}
		return claim.LocalUsage[i].BucketStartUTC.Before(claim.LocalUsage[j].BucketStartUTC)
	})
	seen := map[string]struct{}{}
	requestIDs := make([]string, 0, len(claim.RequestIDs))
	for _, requestID := range claim.RequestIDs {
		requestID = strings.TrimSpace(requestID)
		if _, ok := seen[requestID]; requestID == "" || ok {
			continue
		}
		seen[requestID] = struct{}{}
		requestIDs = append(requestIDs, requestID)
	}
	claim.RequestIDs = requestIDs
	return claim
}

func validate(claim Request) error {
	if claim.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %d", SchemaVersion)
	}
	if claim.RelayProviderID <= 0 {
		return fmt.Errorf("relay_provider_id is required")
	}
	if claim.TokenSource != TokenSourceRelayOfficial && claim.TokenSource != TokenSourceCodexLocal {
		return fmt.Errorf("token_source must be %q or %q", TokenSourceRelayOfficial, TokenSourceCodexLocal)
	}
	if claim.TokenSource == TokenSourceRelayOfficial && len(claim.LocalUsage) > 0 {
		return fmt.Errorf("relay_official claims forbid local_usage")
	}
	if claim.TokenSource == TokenSourceCodexLocal && (len(claim.RequestIDs) > 0 || claim.Calibration != nil || len(claim.LocalUsage) == 0) {
		return fmt.Errorf("codex_local claims require local_usage and forbid request_ids and calibration")
	}
	for name, value := range map[string]string{"group_id": claim.GroupID, "thread_id": claim.ThreadID, "turn_id": claim.TurnID, "evidence_digest": claim.EvidenceDigest} {
		if value == "" || len(value) > MaxIdentitySize {
			return fmt.Errorf("%s is required and must be at most %d bytes", name, MaxIdentitySize)
		}
	}
	if claim.Calibration != nil {
		if claim.Calibration.Digest == "" || len(claim.Calibration.Digest) > MaxIdentitySize || claim.Calibration.InputTokens < 0 || claim.Calibration.OutputTokens < 0 || claim.Calibration.CacheCreationTokens < 0 || claim.Calibration.CacheReadTokens < 0 || claim.Calibration.TotalTokens < 0 {
			return fmt.Errorf("calibration is invalid")
		}
	}
	if len(claim.CommitAllocations) == 0 || len(claim.CommitAllocations) > MaxRequests {
		return fmt.Errorf("commit_allocations must contain 1-%d items", MaxRequests)
	}
	for index, allocation := range claim.CommitAllocations {
		if allocation.Sequence != index+1 || allocation.RepoConfigID <= 0 || allocation.WorkspaceID == "" || allocation.CheckpointEventID == "" || allocation.CommitSHA == "" || allocation.EvidenceDigest == "" {
			return fmt.Errorf("commit allocation sequence %d is invalid", index+1)
		}
	}
	if len(claim.RequestIDs) > MaxRequests {
		return fmt.Errorf("request_ids must contain at most %d unique items", MaxRequests)
	}
	if len(claim.LocalUsage) > MaxRequests {
		return fmt.Errorf("local_usage must contain at most %d buckets", MaxRequests)
	}
	seenUsage := map[string]struct{}{}
	localRequestCount := 0
	for _, usage := range claim.LocalUsage {
		if usage.RequestedModel == "" || len(usage.RequestedModel) > MaxIdentitySize || usage.BucketStartUTC.IsZero() || !usage.BucketStartUTC.Equal(usage.BucketStartUTC.UTC().Truncate(15*time.Minute)) || usage.RequestCount <= 0 {
			return fmt.Errorf("local_usage model, aligned UTC bucket, and positive request_count are required")
		}
		// A bucket must carry an amount in at least one unit. Credit is the unit
		// some agents bill in instead of tokens — Kiro CLI reports only credit —
		// so a zero-token bucket is valid when it carries credit. This mirrors
		// attributionpool.localGroupContributions, which already accepted it;
		// requiring positive tokens here rejected the whole claim before the
		// pool ever saw it.
		if usage.CreditUsage < 0 || (usage.TotalTokens <= 0 && usage.CreditUsage <= 0) {
			return fmt.Errorf("local_usage must carry tokens or credit")
		}
		if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CacheCreationTokens < 0 || usage.CacheReadTokens < 0 || usage.TotalTokens < 0 ||
			usage.InputTokens > math.MaxInt64-usage.OutputTokens || usage.InputTokens+usage.OutputTokens > math.MaxInt64-usage.CacheCreationTokens ||
			usage.InputTokens+usage.OutputTokens+usage.CacheCreationTokens > math.MaxInt64-usage.CacheReadTokens ||
			usage.InputTokens+usage.OutputTokens+usage.CacheCreationTokens+usage.CacheReadTokens != usage.TotalTokens {
			return fmt.Errorf("local_usage Token total is inconsistent")
		}
		key := usage.RequestedModel + "\x00" + usage.BucketStartUTC.Format(time.RFC3339)
		if _, duplicate := seenUsage[key]; duplicate {
			return fmt.Errorf("local_usage bucket is duplicated")
		}
		seenUsage[key] = struct{}{}
		if usage.RequestCount > math.MaxInt-localRequestCount {
			return fmt.Errorf("local_usage request_count overflows")
		}
		localRequestCount += usage.RequestCount
	}
	for _, requestID := range claim.RequestIDs {
		if len(requestID) > MaxIdentitySize {
			return fmt.Errorf("request_id must be at most %d bytes", MaxIdentitySize)
		}
	}
	return nil
}

func allocationMaps(allocations []CommitAllocation) []map[string]any {
	payload, _ := json.Marshal(allocations)
	var result []map[string]any
	_ = json.Unmarshal(payload, &result)
	return result
}

func compatibleAllocations(existing, incoming []map[string]any) (bool, bool) {
	if len(incoming) < len(existing) {
		return false, false
	}
	for index := range existing {
		left, _ := json.Marshal(existing[index])
		right, _ := json.Marshal(incoming[index])
		if string(left) != string(right) {
			return false, false
		}
	}
	return len(incoming) > len(existing), true
}

func tokenSourceForGroup(group *ent.AttributionClaimGroup) string {
	if group != nil && len(group.LocalUsage) > 0 {
		return TokenSourceCodexLocal
	}
	return TokenSourceRelayOfficial
}

func localUsageMaps(usage []LocalUsageBucket) []map[string]any {
	payload, _ := json.Marshal(usage)
	var result []map[string]any
	_ = json.Unmarshal(payload, &result)
	return result
}

func compatibleLocalUsage(existing, incoming []map[string]any) (bool, bool) {
	decode := func(values []map[string]any) ([]LocalUsageBucket, bool) {
		payload, err := json.Marshal(values)
		if err != nil {
			return nil, false
		}
		var result []LocalUsageBucket
		if err := json.Unmarshal(payload, &result); err != nil {
			return nil, false
		}
		return result, true
	}
	oldValues, ok := decode(existing)
	if !ok {
		return false, false
	}
	newValues, ok := decode(incoming)
	if !ok {
		return false, false
	}
	byKey := make(map[string]LocalUsageBucket, len(newValues))
	for _, usage := range newValues {
		byKey[strings.TrimSpace(usage.RequestedModel)+"\x00"+usage.BucketStartUTC.UTC().Format(time.RFC3339)] = usage
	}
	changed := len(oldValues) != len(newValues)
	for _, old := range oldValues {
		incoming, found := byKey[strings.TrimSpace(old.RequestedModel)+"\x00"+old.BucketStartUTC.UTC().Format(time.RFC3339)]
		if !found || incoming.InputTokens < old.InputTokens || incoming.OutputTokens < old.OutputTokens ||
			incoming.CacheCreationTokens < old.CacheCreationTokens || incoming.CacheReadTokens < old.CacheReadTokens ||
			incoming.TotalTokens < old.TotalTokens || incoming.CreditUsage < old.CreditUsage ||
			incoming.RequestCount < old.RequestCount {
			return false, false
		}
		changed = changed || incoming != old
	}
	return changed, true
}

func localUsageRequestCount(usage []LocalUsageBucket) int {
	count := 0
	for _, item := range usage {
		count += item.RequestCount
	}
	return count
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func invalidatePersistedACKs(result *Result, status string) {
	if result.Calibration.Status == "persisted" {
		result.Calibration.Status = status
	}
	for index := range result.Requests {
		if result.Requests[index].Status == "persisted" {
			result.Requests[index].Status = status
		}
	}
}

func calibrationMatches(group *ent.AttributionClaimGroup, calibration Calibration) bool {
	return group.CalibrationDigest == calibration.Digest && group.CalibrationInputTokens == calibration.InputTokens &&
		group.CalibrationOutputTokens == calibration.OutputTokens && group.CalibrationCacheCreationTokens == calibration.CacheCreationTokens &&
		group.CalibrationCacheReadTokens == calibration.CacheReadTokens && group.CalibrationTotalTokens == calibration.TotalTokens
}

func setCalibrationCreate(create *ent.AttributionClaimGroupCreate, calibration Calibration) {
	create.SetCalibrationDigest(calibration.Digest).SetCalibrationInputTokens(calibration.InputTokens).
		SetCalibrationOutputTokens(calibration.OutputTokens).SetCalibrationCacheCreationTokens(calibration.CacheCreationTokens).
		SetCalibrationCacheReadTokens(calibration.CacheReadTokens).SetCalibrationTotalTokens(calibration.TotalTokens)
}

func setCalibrationUpdate(update *ent.AttributionClaimGroupUpdateOne, calibration Calibration) {
	update.SetCalibrationDigest(calibration.Digest).SetCalibrationInputTokens(calibration.InputTokens).
		SetCalibrationOutputTokens(calibration.OutputTokens).SetCalibrationCacheCreationTokens(calibration.CacheCreationTokens).
		SetCalibrationCacheReadTokens(calibration.CacheReadTokens).SetCalibrationTotalTokens(calibration.TotalTokens)
}

func digestStrings(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:])
}
