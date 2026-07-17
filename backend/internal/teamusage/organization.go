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
	defaultOrganizationDepartmentLimit = 25
	maxOrganizationDepartmentLimit     = 100
	defaultOrganizationMemberLimit     = 50
	maxOrganizationMemberLimit         = 100
)

func (s *Service) Organization(ctx context.Context, actorUserID int, params OrganizationParams) (*OrganizationResponse, error) {
	departmentLimit, err := normalizeOrganizationLimit(params.DepartmentLimit, defaultOrganizationDepartmentLimit, maxOrganizationDepartmentLimit, "department_limit")
	if err != nil {
		return nil, err
	}
	memberLimit, err := normalizeOrganizationLimit(params.MemberLimit, defaultOrganizationMemberLimit, maxOrganizationMemberLimit, "member_limit")
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeOverviewParams(params.OverviewParams)
	if err != nil {
		return nil, err
	}
	if s.organizationCursorCodec == nil {
		return nil, fmt.Errorf("team organization cursor codec is not configured")
	}
	parentID := strings.TrimSpace(params.ParentDepartmentExternalID)
	departmentCursor, err := s.decodeOrganizationCursor(params.DepartmentCursor, organizationCursorDepartments, actorUserID, normalized, parentID)
	if err != nil {
		return nil, err
	}
	memberCursor, err := s.decodeOrganizationCursor(params.MemberCursor, organizationCursorMembers, actorUserID, normalized, parentID)
	if err != nil {
		return nil, err
	}

	result, scopeVersion, err := s.readOverviewSnapshot(ctx, actorUserID, normalized)
	if err != nil {
		return nil, err
	}
	rankedMembers := rankMembersForPagination(result.Snapshot.Members)
	snapshotID, err := organizationSnapshotIdentity(result.Snapshot.MemberTree, rankedMembers)
	if err != nil {
		return nil, err
	}
	if organizationCursorExpired(departmentCursor, scopeVersion, snapshotID) || organizationCursorExpired(memberCursor, scopeVersion, snapshotID) {
		return nil, ErrOrganizationSnapshotExpired
	}

	departments, members, found := organizationBranch(result.Snapshot.MemberTree, rankedMembers, parentID)
	if !found {
		return nil, ErrOutOfScope
	}
	departmentOffset, err := organizationCursorOffset(departmentCursor, len(departments))
	if err != nil {
		return nil, err
	}
	memberOffset, err := organizationCursorOffset(memberCursor, len(members))
	if err != nil {
		return nil, err
	}
	departmentEnd := boundedPageEnd(departmentOffset, departmentLimit, len(departments))
	memberEnd := boundedPageEnd(memberOffset, memberLimit, len(members))

	response := &OrganizationResponse{
		SnapshotFreshness: result.Freshness,
		ScopeVersion:      scopeVersion,
		Window:            result.Snapshot.Window,
		Departments:       append([]OrganizationDepartment(nil), departments[departmentOffset:departmentEnd]...),
		Members:           append([]OverviewMember(nil), members[memberOffset:memberEnd]...),
	}
	if parentID != "" {
		response.ParentDepartmentExternalID = &parentID
	}
	if departmentEnd < len(departments) {
		response.NextDepartmentCursor, err = s.organizationCursorCodec.Encode(newOrganizationCursorPayload(organizationCursorDepartments, actorUserID, scopeVersion, snapshotID, normalized, parentID, departmentEnd))
		if err != nil {
			return nil, err
		}
	}
	if memberEnd < len(members) {
		response.NextMemberCursor, err = s.organizationCursorCodec.Encode(newOrganizationCursorPayload(organizationCursorMembers, actorUserID, scopeVersion, snapshotID, normalized, parentID, memberEnd))
		if err != nil {
			return nil, err
		}
	}
	return response, nil
}

func (s *Service) decodeOrganizationCursor(cursor, collection string, actorUserID int, params OverviewParams, parentID string) (*organizationCursorPayload, error) {
	if strings.TrimSpace(cursor) == "" {
		return nil, nil
	}
	decoded, err := s.organizationCursorCodec.Decode(cursor)
	if err != nil || decoded.Collection != collection || decoded.ActorUserID != actorUserID ||
		decoded.StartDate != params.StartDate || decoded.EndDate != params.EndDate ||
		decoded.Granularity != params.Granularity || decoded.Timezone != params.Timezone ||
		decoded.ParentDepartmentExternalID != parentID {
		return nil, ErrInvalidOrganizationCursor
	}
	return &decoded, nil
}

func normalizeOrganizationLimit(value, defaultValue, maxValue int, field string) (int, error) {
	if value == 0 {
		return defaultValue, nil
	}
	if value < 1 || value > maxValue {
		return 0, fmt.Errorf("%w: %s must be between 1 and %d", ErrInvalidOverviewParams, field, maxValue)
	}
	return value, nil
}

func newOrganizationCursorPayload(collection string, actorUserID int, scopeVersion, snapshotID string, params OverviewParams, parentID string, offset int) organizationCursorPayload {
	return organizationCursorPayload{
		Version: organizationCursorVersion, Collection: collection, ActorUserID: actorUserID,
		ScopeVersion: scopeVersion, SnapshotID: snapshotID,
		StartDate: params.StartDate, EndDate: params.EndDate, Granularity: params.Granularity, Timezone: params.Timezone,
		ParentDepartmentExternalID: parentID, Offset: offset,
	}
}

func organizationCursorExpired(cursor *organizationCursorPayload, scopeVersion, snapshotID string) bool {
	return cursor != nil && (cursor.ScopeVersion != scopeVersion || cursor.SnapshotID != snapshotID)
}

func organizationCursorOffset(cursor *organizationCursorPayload, total int) (int, error) {
	if cursor == nil {
		return 0, nil
	}
	if cursor.Offset > total {
		return 0, ErrInvalidOrganizationCursor
	}
	return cursor.Offset, nil
}

func boundedPageEnd(offset, limit, total int) int {
	end := offset + limit
	if end > total {
		return total
	}
	return end
}

func organizationBranch(roots []OverviewMemberNode, rankedMembers []OverviewMember, parentID string) ([]OrganizationDepartment, []OverviewMember, bool) {
	index := indexOrganizationNodes(roots)
	source := roots
	if parentID != "" {
		parent, ok := index[parentID]
		if !ok {
			return nil, nil, false
		}
		source = parent.Children
	}
	departments := make([]OrganizationDepartment, 0, len(source))
	for _, node := range source {
		departments = append(departments, organizationDepartmentFromNode(node))
	}
	sort.Slice(departments, func(i, j int) bool {
		leftName := strings.ToLower(strings.TrimSpace(departments[i].Name))
		rightName := strings.ToLower(strings.TrimSpace(departments[j].Name))
		if leftName != rightName {
			return leftName < rightName
		}
		return departments[i].DepartmentExternalID < departments[j].DepartmentExternalID
	})
	members := []OverviewMember{}
	if parentID != "" {
		for _, member := range rankedMembers {
			if overviewMemberInDepartment(member, parentID) {
				members = append(members, member)
			}
		}
	}
	return departments, members, true
}

func indexOrganizationNodes(roots []OverviewMemberNode) map[string]OverviewMemberNode {
	index := make(map[string]OverviewMemberNode)
	var visit func([]OverviewMemberNode)
	visit = func(nodes []OverviewMemberNode) {
		for _, node := range nodes {
			index[node.DepartmentExternalID] = node
			visit(node.Children)
		}
	}
	visit(roots)
	return index
}

func organizationDepartmentFromNode(node OverviewMemberNode) OrganizationDepartment {
	return OrganizationDepartment{
		DepartmentExternalID: node.DepartmentExternalID,
		ParentExternalID:     cloneStringPointer(node.ParentExternalID),
		Name:                 node.Name,
		DisplayPath:          node.DisplayPath,
		Depth:                node.Depth,
		ChildCount:           len(node.Children),
		HasChildren:          len(node.Children) > 0,
		DirectMemberCount:    len(node.Members),
		AggregateMemberCount: node.MemberCount,
		ConnectedMemberCount: node.ConnectedMemberCount,
		RangeActualCost:      node.RangeActualCost,
		RangeTotalTokens:     cloneInt64Pointer(node.RangeTotalTokens),
	}
}

func overviewMemberInDepartment(member OverviewMember, departmentID string) bool {
	for _, candidate := range overviewMemberDepartmentIDs(member) {
		if candidate == departmentID {
			return true
		}
	}
	return false
}

func organizationSnapshotIdentity(roots []OverviewMemberNode, rankedMembers []OverviewMember) (string, error) {
	index := indexOrganizationNodes(roots)
	departments := make([]OrganizationDepartment, 0, len(index))
	for _, node := range index {
		departments = append(departments, organizationDepartmentFromNode(node))
	}
	sort.Slice(departments, func(i, j int) bool {
		return departments[i].DepartmentExternalID < departments[j].DepartmentExternalID
	})
	memberID, err := memberSnapshotIdentity(rankedMembers)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		Departments []OrganizationDepartment `json:"departments"`
		MemberID    string                   `json:"member_id"`
	}{Departments: departments, MemberID: memberID})
	if err != nil {
		return "", fmt.Errorf("encode organization snapshot identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "organization-v1:" + hex.EncodeToString(digest[:]), nil
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
