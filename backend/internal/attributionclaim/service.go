package attributionclaim

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionclaimgroup"
	"github.com/ai-efficiency/backend/ent/attributionrequestclaim"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/attributionledger"
)

const (
	SchemaVersion   = 2
	LedgerEpoch     = "shadow_v2"
	MaxGroups       = 20
	MaxRequests     = 100
	MaxIdentitySize = 256
	HotRetention    = 90 * 24 * time.Hour
)

type Request struct {
	SchemaVersion     int                `json:"schema_version"`
	GroupID           string             `json:"group_id"`
	RelayProviderID   int                `json:"relay_provider_id"`
	ThreadID          string             `json:"thread_id"`
	TurnID            string             `json:"turn_id"`
	EvidenceDigest    string             `json:"evidence_digest"`
	Calibration       *Calibration       `json:"calibration,omitempty"`
	CommitAllocations []CommitAllocation `json:"commit_allocations"`
	RequestIDs        []string           `json:"request_ids"`
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
	Epoch   string   `json:"ledger_epoch"`
	Results []Result `json:"results"`
}

type Service struct {
	client *ent.Client
	now    func() time.Time
}

func NewService(client *ent.Client) *Service {
	return &Service{client: client, now: time.Now}
}

func (s *Service) Ingest(ctx context.Context, principal attributionledger.InstallationPrincipal, batch BatchRequest) (BatchResult, error) {
	result := BatchResult{Epoch: LedgerEpoch, Results: make([]Result, 0, len(batch.Groups))}
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

	group, groupCreated, groupChanged, calibrationStatus, err := upsertGroup(ctx, tx, principal, claim, s.now().UTC().Add(HotRetention))
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
			if result.Calibration.Status == "persisted" {
				result.Calibration.Status = "rolled_back"
			}
			for index := range result.Requests {
				if result.Requests[index].Status == "persisted" {
					result.Requests[index].Status = "rolled_back"
				}
			}
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
			return result, fmt.Errorf("update claim group request count: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit transaction: %w", err)
	}
	return result, nil
}

func upsertGroup(ctx context.Context, tx *ent.Tx, principal attributionledger.InstallationPrincipal, claim Request, expiresAt time.Time) (*ent.AttributionClaimGroup, bool, bool, string, error) {
	incomingAllocations := allocationMaps(claim.CommitAllocations)
	group, err := tx.AttributionClaimGroup.Query().Where(attributionclaimgroup.GroupIDEQ(claim.GroupID)).Only(ctx)
	if err == nil {
		if group.InstallationID != principal.DatabaseID || group.UserID != principal.UserID || group.RelayProviderID != claim.RelayProviderID ||
			group.ThreadID != claim.ThreadID || group.TurnID != claim.TurnID ||
			group.SchemaVersion != SchemaVersion || group.LedgerEpoch != LedgerEpoch {
			return nil, false, false, "not_present", fmt.Errorf("claim group conflict")
		}
		allocationChanged, compatible := compatibleAllocations(group.CommitAllocations, incomingAllocations)
		if !compatible {
			return nil, false, false, "not_present", fmt.Errorf("claim group allocation conflict")
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
		if allocationChanged || calibrationChanged {
			update := group.Update()
			if allocationChanged {
				update.SetCommitAllocations(incomingAllocations).SetEvidenceDigest(claim.EvidenceDigest)
				group.CommitAllocations = incomingAllocations
				group.EvidenceDigest = claim.EvidenceDigest
			}
			if calibrationChanged {
				setCalibrationUpdate(update, *claim.Calibration)
			}
			if err := update.Exec(ctx); err != nil {
				return nil, false, false, calibrationStatus, fmt.Errorf("update claim group: %w", err)
			}
		}
		return group, false, allocationChanged || calibrationChanged, calibrationStatus, nil
	}
	if !ent.IsNotFound(err) {
		return nil, false, false, "not_present", fmt.Errorf("query claim group: %w", err)
	}
	create := tx.AttributionClaimGroup.Create().
		SetGroupID(claim.GroupID).SetInstallationID(principal.DatabaseID).SetUserID(principal.UserID).
		SetRelayProviderID(claim.RelayProviderID).
		SetSchemaVersion(SchemaVersion).SetLedgerEpoch(LedgerEpoch).SetThreadID(claim.ThreadID).SetTurnID(claim.TurnID).
		SetEvidenceDigest(claim.EvidenceDigest).SetCommitAllocations(incomingAllocations).
		SetRequestCount(len(claim.RequestIDs)).SetExpiresAt(expiresAt)
	calibrationStatus := "not_present"
	if claim.Calibration != nil {
		setCalibrationCreate(create, *claim.Calibration)
		calibrationStatus = "persisted"
	}
	group, err = create.Save(ctx)
	if err != nil {
		return nil, false, false, calibrationStatus, fmt.Errorf("create claim group: %w", err)
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
	seen := map[string]struct{}{}
	requestIDs := make([]string, 0, len(claim.RequestIDs))
	for _, requestID := range claim.RequestIDs {
		requestID = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(requestID), "client:"))
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
	if len(claim.RequestIDs) == 0 || len(claim.RequestIDs) > MaxRequests {
		return fmt.Errorf("request_ids must contain 1-%d unique items", MaxRequests)
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
	shorter := existing
	longer := incoming
	if len(existing) > len(incoming) {
		shorter, longer = incoming, existing
	}
	for index := range shorter {
		left, _ := json.Marshal(shorter[index])
		right, _ := json.Marshal(longer[index])
		if string(left) != string(right) {
			return false, false
		}
	}
	return len(incoming) > len(existing), true
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
