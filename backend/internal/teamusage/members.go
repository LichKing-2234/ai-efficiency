package teamusage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	defaultMembersLimit = 50
	maxMembersLimit     = 100
)

func (s *Service) Members(ctx context.Context, actorUserID int, params MembersParams) (*MembersResponse, error) {
	limit, err := normalizeMembersLimit(params.Limit)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeOverviewParams(params.OverviewParams)
	if err != nil {
		return nil, err
	}
	if s.memberCursorCodec == nil {
		return nil, fmt.Errorf("team member cursor codec is not configured")
	}

	var cursor *memberCursorPayload
	if strings.TrimSpace(params.Cursor) != "" {
		decoded, decodeErr := s.memberCursorCodec.Decode(params.Cursor)
		if decodeErr != nil || !memberCursorMatchesRequest(decoded, actorUserID, normalized) {
			return nil, ErrInvalidMemberCursor
		}
		cursor = &decoded
	}

	result, scopeVersion, err := s.readOverviewSnapshot(ctx, actorUserID, normalized)
	if err != nil {
		return nil, err
	}
	members := rankMembersForPagination(result.Snapshot.Members)
	snapshotID, err := memberSnapshotIdentity(members)
	if err != nil {
		return nil, err
	}
	offset := 0
	if cursor != nil {
		if cursor.ScopeVersion != scopeVersion || cursor.SnapshotID != snapshotID {
			return nil, ErrMemberSnapshotExpired
		}
		if cursor.Offset > len(members) {
			return nil, ErrInvalidMemberCursor
		}
		offset = cursor.Offset
	}

	end := offset + limit
	if end > len(members) {
		end = len(members)
	}
	response := &MembersResponse{
		SnapshotFreshness: result.Freshness,
		ScopeVersion:      scopeVersion,
		Window:            result.Snapshot.Window,
		Items:             append([]OverviewMember(nil), members[offset:end]...),
		TotalCount:        len(members),
	}
	if end < len(members) {
		response.NextCursor, err = s.memberCursorCodec.Encode(memberCursorPayload{
			Version: memberCursorVersion, ActorUserID: actorUserID, ScopeVersion: scopeVersion, SnapshotID: snapshotID,
			StartDate: normalized.StartDate, EndDate: normalized.EndDate, Granularity: normalized.Granularity, Timezone: normalized.Timezone,
			Offset: end,
		})
		if err != nil {
			return nil, err
		}
	}
	return response, nil
}

func normalizeMembersLimit(limit int) (int, error) {
	if limit == 0 {
		return defaultMembersLimit, nil
	}
	if limit < 1 || limit > maxMembersLimit {
		return 0, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidOverviewParams, maxMembersLimit)
	}
	return limit, nil
}

func memberCursorMatchesRequest(cursor memberCursorPayload, actorUserID int, params OverviewParams) bool {
	return cursor.ActorUserID == actorUserID && cursor.StartDate == params.StartDate && cursor.EndDate == params.EndDate &&
		cursor.Granularity == params.Granularity && cursor.Timezone == params.Timezone
}

func rankMembersForPagination(source []OverviewMember) []OverviewMember {
	members := append([]OverviewMember(nil), source...)
	sort.Slice(members, func(i, j int) bool {
		leftTokens := overviewMemberTokenTotal(members[i])
		rightTokens := overviewMemberTokenTotal(members[j])
		if leftTokens != rightTokens {
			return leftTokens > rightTokens
		}
		return pagedMemberIdentityLess(members[i], members[j])
	})
	for index := range members {
		members[index].Rank = index + 1
	}
	return members
}

func pagedMemberIdentityLess(left, right OverviewMember) bool {
	leftLocal := left.UserID > 0
	rightLocal := right.UserID > 0
	if leftLocal != rightLocal {
		return !leftLocal
	}
	if leftLocal {
		return left.UserID < right.UserID
	}
	leftExternalID := strings.TrimSpace(left.DirectoryMemberExternalID)
	rightExternalID := strings.TrimSpace(right.DirectoryMemberExternalID)
	if leftExternalID != rightExternalID {
		return leftExternalID < rightExternalID
	}
	return strings.ToLower(strings.TrimSpace(left.Email)) < strings.ToLower(strings.TrimSpace(right.Email))
}

func memberSnapshotIdentity(members []OverviewMember) (string, error) {
	encoded, err := json.Marshal(members)
	if err != nil {
		return "", fmt.Errorf("encode member snapshot identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "members-v1:" + hex.EncodeToString(digest[:]), nil
}
