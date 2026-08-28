package attributionlocal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

const (
	V2DeliveryPending         = "pending"
	V2DeliveryConflict        = "conflict"
	V2DeliveryUpgradeRequired = "upgrade_required"
)

type V2DeliverySummary struct {
	Pending                  int
	Conflict                 int
	UpgradeRequired          int
	Accepted                 int
	MissingRequestID         int
	AmbiguousRequestEvidence int
	RequestEvidenceExpired   int
	UnrecognizedPatchWrapper int
}

func UpdateV2ClaimState(ctx context.Context, fn func(*V2ClaimState) error) error {
	return withStateFileLock(ctx, V2ClaimStatePath()+".lock", "v2 claim state is busy", func() error {
		state, err := LoadV2ClaimState()
		if err != nil {
			return err
		}
		if err := fn(state); err != nil {
			return err
		}
		return SaveV2ClaimState(state)
	})
}

func SummarizeV2ClaimDelivery(state *V2ClaimState) V2DeliverySummary {
	var summary V2DeliverySummary
	if state == nil {
		return summary
	}
	for _, claim := range state.Claims {
		switch claim.DeliveryStatus {
		case V2DeliveryConflict:
			summary.Conflict++
		case V2DeliveryUpgradeRequired:
			summary.UpgradeRequired++
		default:
			if v2ClaimUploadable(claim) {
				summary.Pending++
			} else if claim.GroupAcknowledged && claim.GapReason == "" {
				summary.Accepted++
			}
		}
		switch claim.GapReason {
		case v2GapMissingRequestID:
			summary.MissingRequestID++
		case v2GapAmbiguousRequestEvidence:
			summary.AmbiguousRequestEvidence++
		case v2GapRequestEvidenceExpired:
			summary.RequestEvidenceExpired++
		case v2GapUnrecognizedPatchWrapper:
			summary.UnrecognizedPatchWrapper++
		}
	}
	return summary
}

func v2ClaimUploadable(claim V2ClaimCandidate) bool {
	if claim.DeliveryStatus == V2DeliveryConflict {
		return false
	}
	return claim.GapReason == "" && (len(claim.Group.RequestIDs) > 0 || claim.Group.Calibration != nil || !claim.GroupAcknowledged)
}

// ApplyV2ClaimAcknowledgements removes only independently acknowledged local
// items. It returns an error while retaining conflicts and unknown responses.
func ApplyV2ClaimAcknowledgements(state *V2ClaimState, sent []client.AttributionV2ClaimGroup, result *client.AttributionV2ClaimBatchResult, expected client.AttributionProtocol, now time.Time) error {
	if state == nil {
		return fmt.Errorf("v2 claim state is nil")
	}
	sentByID := make(map[string]client.AttributionV2ClaimGroup, len(sent))
	for _, group := range sent {
		sentByID[group.GroupID] = group
	}
	if result == nil || expected.Validate() != nil || result.Protocol().Validate() != nil || result.Protocol() != expected {
		markV2SentClaims(state, sentByID, V2DeliveryUpgradeRequired, "invalid v2 claim acknowledgement", now)
		return fmt.Errorf("invalid v2 claim acknowledgement")
	}
	results := make(map[string]client.AttributionV2ClaimResult, len(result.Results))
	for _, item := range result.Results {
		id := strings.TrimSpace(item.Group.ID)
		if _, ok := sentByID[id]; !ok || id == "" {
			markV2SentClaims(state, sentByID, V2DeliveryUpgradeRequired, "unknown v2 group acknowledgement", now)
			return fmt.Errorf("unknown v2 group acknowledgement")
		}
		if _, duplicate := results[id]; duplicate {
			markV2SentClaims(state, sentByID, V2DeliveryUpgradeRequired, "duplicate v2 group acknowledgement", now)
			return fmt.Errorf("duplicate v2 group acknowledgement")
		}
		results[id] = item
	}

	var firstErr error
	kept := state.Claims[:0]
	for _, claim := range state.Claims {
		sentGroup, wasSent := sentByID[claim.Group.GroupID]
		if !wasSent {
			kept = append(kept, claim)
			continue
		}
		ack, ok := results[claim.Group.GroupID]
		if !ok {
			claim.DeliveryStatus = V2DeliveryUpgradeRequired
			claim.LastDeliveryError = "missing v2 group acknowledgement"
			claim.UpdatedAt = now.UTC()
			if firstErr == nil {
				firstErr = fmt.Errorf("claim %s: %s", claim.Group.GroupID, claim.LastDeliveryError)
			}
			kept = append(kept, claim)
			continue
		}
		if err := validateV2ItemAcknowledgements(sentGroup, ack); err != nil {
			claim.DeliveryStatus = V2DeliveryUpgradeRequired
			claim.LastDeliveryError = err.Error()
			claim.UpdatedAt = now.UTC()
			if firstErr == nil {
				firstErr = fmt.Errorf("claim %s: %w", claim.Group.GroupID, err)
			}
			kept = append(kept, claim)
			continue
		}
		claim.GroupAcknowledged = claim.GroupAcknowledged || (v2SentGroupCovers(sentGroup, claim.Group) && v2ItemAcknowledged(ack.Group.Status))
		requestACKs := make(map[string]client.AttributionV2ItemStatus, len(ack.Requests))
		for _, item := range ack.Requests {
			requestACKs[strings.TrimSpace(item.ID)] = item
		}
		remainingRequests := claim.Group.RequestIDs[:0]
		for _, requestID := range claim.Group.RequestIDs {
			item, found := requestACKs[requestID]
			if found && v2ItemAcknowledged(item.Status) {
				claim.AcknowledgedRequestDigests = uniqueSorted(append(claim.AcknowledgedRequestDigests, claimDigest(requestID)))
				continue
			}
			remainingRequests = append(remainingRequests, requestID)
		}
		claim.Group.RequestIDs = remainingRequests
		calibrationAcknowledged := claim.Group.Calibration == nil
		if claim.Group.Calibration != nil && strings.TrimSpace(ack.Calibration.ID) == strings.TrimSpace(claim.Group.Calibration.Digest) && v2ItemAcknowledged(ack.Calibration.Status) {
			claim.AcknowledgedCalibrationDigest = claim.Group.Calibration.Digest
			claim.Group.Calibration = nil
			calibrationAcknowledged = true
		}
		if claim.GroupAcknowledged && len(claim.Group.RequestIDs) == 0 && calibrationAcknowledged {
			claim.DeliveryStatus = ""
			claim.LastDeliveryError = ""
			claim.UpdatedAt = now.UTC()
			kept = append(kept, claim)
			continue
		}
		claim.DeliveryStatus, claim.LastDeliveryError = v2UnacknowledgedStatus(sentGroup, ack, claim)
		claim.UpdatedAt = now.UTC()
		if firstErr == nil {
			firstErr = fmt.Errorf("claim %s: %s", claim.Group.GroupID, claim.LastDeliveryError)
		}
		kept = append(kept, claim)
	}
	state.Claims = kept
	return firstErr
}

// v2SentGroupCovers reports whether everything this claim wants delivered was
// inside the sent group.
//
// The sent group is the collapse of every local claim sharing its id — an
// original turn and the replays a resumed session produced — so no single claim
// equals it field for field. The previous rule demanded exactly that equality,
// and a batch the backend had accepted was therefore recorded on every covered
// claim as a failure and re-sent forever. Coverage is judged on content: each
// allocation the claim holds is in the sent group, and each usage bucket it
// holds fits inside the sent group's matching bucket, whose amounts are the sum
// over the collapsed claims. Thread and turn are not compared — the collapse
// keeps one representative's — and neither is the envelope evidence digest,
// which each claim derives from its own allocation set; the allocations
// themselves carry the digests that matter.
//
// The equality this replaces also guarded a race: content merged into the claim
// between building the batch and applying its acknowledgement must not be
// marked delivered. Coverage keeps that guard — anything newly merged is not in
// the sent group, so the claim stays unacknowledged and re-sends.
func v2SentGroupCovers(sent, current client.AttributionV2ClaimGroup) bool {
	if current.SchemaVersion != sent.SchemaVersion ||
		current.GroupID != sent.GroupID ||
		current.RelayProviderID != sent.RelayProviderID ||
		current.TokenSource != sent.TokenSource {
		return false
	}
	for _, allocation := range current.CommitAllocations {
		found := false
		for _, sentAllocation := range sent.CommitAllocations {
			if allocation.CheckpointEventID == sentAllocation.CheckpointEventID &&
				allocation.CommitSHA == sentAllocation.CommitSHA &&
				allocation.EvidenceDigest == sentAllocation.EvidenceDigest &&
				allocation.RepoConfigID == sentAllocation.RepoConfigID &&
				allocation.WorkspaceID == sentAllocation.WorkspaceID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, bucket := range current.LocalUsage {
		found := false
		for _, sentBucket := range sent.LocalUsage {
			if bucket.RequestedModel != sentBucket.RequestedModel || !bucket.BucketStartUTC.Equal(sentBucket.BucketStartUTC) {
				continue
			}
			if bucket.InputTokens <= sentBucket.InputTokens &&
				bucket.OutputTokens <= sentBucket.OutputTokens &&
				bucket.CacheCreationTokens <= sentBucket.CacheCreationTokens &&
				bucket.CacheReadTokens <= sentBucket.CacheReadTokens &&
				bucket.TotalTokens <= sentBucket.TotalTokens &&
				bucket.CreditUsage <= sentBucket.CreditUsage &&
				bucket.RequestCount <= sentBucket.RequestCount {
				found = true
			}
			break
		}
		if !found {
			return false
		}
	}
	return true
}

func validateV2ItemAcknowledgements(sent client.AttributionV2ClaimGroup, ack client.AttributionV2ClaimResult) error {
	wanted := make(map[string]struct{}, len(sent.RequestIDs))
	for _, id := range sent.RequestIDs {
		wanted[strings.TrimSpace(id)] = struct{}{}
	}
	seen := make(map[string]struct{}, len(ack.Requests))
	for _, item := range ack.Requests {
		id := strings.TrimSpace(item.ID)
		if _, ok := wanted[id]; !ok || id == "" {
			return fmt.Errorf("unknown v2 request acknowledgement")
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("duplicate v2 request acknowledgement")
		}
		seen[id] = struct{}{}
	}
	wantCalibration := ""
	if sent.Calibration != nil {
		wantCalibration = strings.TrimSpace(sent.Calibration.Digest)
	}
	gotCalibration := strings.TrimSpace(ack.Calibration.ID)
	if gotCalibration != "" && gotCalibration != wantCalibration {
		return fmt.Errorf("unknown v2 calibration acknowledgement")
	}
	return nil
}

func markV2SentClaims(state *V2ClaimState, sent map[string]client.AttributionV2ClaimGroup, status, message string, now time.Time) {
	for index := range state.Claims {
		claim := &state.Claims[index]
		if _, ok := sent[claim.Group.GroupID]; ok {
			claim.DeliveryStatus = status
			claim.LastDeliveryError = message
			claim.UpdatedAt = now.UTC()
		}
	}
}

func v2ItemAcknowledged(status string) bool {
	status = strings.TrimSpace(status)
	return status == "persisted" || status == "duplicate_identical"
}

func v2UnacknowledgedStatus(sent client.AttributionV2ClaimGroup, ack client.AttributionV2ClaimResult, claim V2ClaimCandidate) (string, string) {
	statuses := []client.AttributionV2ItemStatus{ack.Group, ack.Calibration}
	statuses = append(statuses, ack.Requests...)
	for _, item := range statuses {
		switch strings.TrimSpace(item.Status) {
		case "conflict", "rejected", "rolled_back":
			message := strings.TrimSpace(item.Error)
			if message == "" {
				message = strings.TrimSpace(item.Status)
			}
			return V2DeliveryConflict, message
		}
	}
	if len(claim.Group.RequestIDs) < len(sent.RequestIDs) || claim.Group.Calibration == nil && sent.Calibration != nil {
		return V2DeliveryUpgradeRequired, "partial v2 acknowledgement omitted required item status"
	}
	return V2DeliveryUpgradeRequired, "unrecognized v2 acknowledgement status"
}
