package attributionclaim

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	SchemaVersion     int      `json:"schema_version"`
	GroupID           string   `json:"group_id"`
	RelayProviderID   int      `json:"relay_provider_id"`
	RepoConfigID      int      `json:"repo_config_id"`
	CheckpointEventID string   `json:"checkpoint_event_id"`
	ThreadID          string   `json:"thread_id"`
	TurnID            string   `json:"turn_id"`
	EvidenceDigest    string   `json:"evidence_digest"`
	CalibrationDigest string   `json:"calibration_digest,omitempty"`
	RequestIDs        []string `json:"request_ids"`
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
		Calibration: ItemStatus{ID: claim.CalibrationDigest, Status: "not_present"},
		Requests:    make([]ItemStatus, 0, len(claim.RequestIDs)),
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
	checkpoint, err := tx.CommitCheckpoint.Query().Where(commitcheckpoint.EventIDEQ(claim.CheckpointEventID)).Only(ctx)
	if err != nil || checkpoint.RepoConfigID != claim.RepoConfigID || checkpoint.UserID == nil || *checkpoint.UserID != principal.UserID {
		return result, fmt.Errorf("checkpoint does not belong to the installation owner and repository")
	}
	if _, err := tx.RepoConfig.Get(ctx, claim.RepoConfigID); err != nil {
		return result, fmt.Errorf("repository does not exist")
	}

	group, groupCreated, err := upsertGroup(ctx, tx, principal, claim, checkpoint.ID, s.now().UTC().Add(HotRetention))
	if err != nil {
		return result, err
	}
	if !groupCreated {
		result.Group.Status = "duplicate_identical"
	}
	if claim.CalibrationDigest != "" {
		result.Calibration.Status = "persisted"
		if !groupCreated {
			result.Calibration.Status = "duplicate_identical"
		}
	}

	created := 0
	for _, requestID := range claim.RequestIDs {
		status, wasCreated, err := upsertRequest(ctx, tx, group.ID, claim, requestID, group.ExpiresAt)
		if err != nil {
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

func upsertGroup(ctx context.Context, tx *ent.Tx, principal attributionledger.InstallationPrincipal, claim Request, checkpointID int, expiresAt time.Time) (*ent.AttributionClaimGroup, bool, error) {
	group, err := tx.AttributionClaimGroup.Query().Where(attributionclaimgroup.GroupIDEQ(claim.GroupID)).Only(ctx)
	if err == nil {
		if group.InstallationID != principal.DatabaseID || group.UserID != principal.UserID || group.RelayProviderID != claim.RelayProviderID ||
			group.RepoConfigID != claim.RepoConfigID || group.CheckpointID != checkpointID || group.ThreadID != claim.ThreadID ||
			group.TurnID != claim.TurnID || group.EvidenceDigest != claim.EvidenceDigest || group.CalibrationDigest != claim.CalibrationDigest ||
			group.SchemaVersion != SchemaVersion || group.LedgerEpoch != LedgerEpoch {
			return nil, false, fmt.Errorf("claim group conflict")
		}
		return group, false, nil
	}
	if !ent.IsNotFound(err) {
		return nil, false, fmt.Errorf("query claim group: %w", err)
	}
	group, err = tx.AttributionClaimGroup.Create().
		SetGroupID(claim.GroupID).SetInstallationID(principal.DatabaseID).SetUserID(principal.UserID).
		SetRelayProviderID(claim.RelayProviderID).SetRepoConfigID(claim.RepoConfigID).SetCheckpointID(checkpointID).
		SetSchemaVersion(SchemaVersion).SetLedgerEpoch(LedgerEpoch).SetThreadID(claim.ThreadID).SetTurnID(claim.TurnID).
		SetEvidenceDigest(claim.EvidenceDigest).SetCalibrationDigest(claim.CalibrationDigest).
		SetRequestCount(len(claim.RequestIDs)).SetExpiresAt(expiresAt).Save(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("create claim group: %w", err)
	}
	return group, true, nil
}

func upsertRequest(ctx context.Context, tx *ent.Tx, groupID int, claim Request, requestID string, expiresAt time.Time) (ItemStatus, bool, error) {
	digest := digestStrings(claim.GroupID, claim.EvidenceDigest, requestID)
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
	claim.CalibrationDigest = strings.TrimSpace(claim.CalibrationDigest)
	claim.CheckpointEventID = strings.TrimSpace(claim.CheckpointEventID)
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
	if claim.RelayProviderID <= 0 || claim.RepoConfigID <= 0 || claim.CheckpointEventID == "" {
		return fmt.Errorf("relay_provider_id, repo_config_id, and checkpoint_event_id are required")
	}
	for name, value := range map[string]string{"group_id": claim.GroupID, "thread_id": claim.ThreadID, "turn_id": claim.TurnID, "evidence_digest": claim.EvidenceDigest} {
		if value == "" || len(value) > MaxIdentitySize {
			return fmt.Errorf("%s is required and must be at most %d bytes", name, MaxIdentitySize)
		}
	}
	if len(claim.CalibrationDigest) > MaxIdentitySize {
		return fmt.Errorf("calibration_digest must be at most %d bytes", MaxIdentitySize)
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

func digestStrings(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:])
}
