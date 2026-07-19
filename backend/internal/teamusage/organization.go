package teamusage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

const (
	defaultOrganizationDepartmentLimit = 25
	maxOrganizationDepartmentLimit     = 100
	defaultOrganizationMemberLimit     = 50
	maxOrganizationMemberLimit         = 100
)

func projectOverviewCompatibilityMemberTree(scope *representativescope.Scope, members []OverviewMember) []OverviewMemberNode {
	if scope == nil {
		return []OverviewMemberNode{}
	}
	return BuildOverviewMemberTree(scope.MemberTreeDepartments, scope.MemberTreeRootIDs, members)
}

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

	result, scopeVersion, err := s.readOrganizationSnapshot(ctx, actorUserID, normalized, parentID)
	if err != nil {
		return nil, err
	}
	snapshotID, err := organizationSnapshotIdentity(result.Snapshot.Departments, result.Snapshot.Members)
	if err != nil {
		return nil, err
	}
	if organizationCursorExpired(departmentCursor, scopeVersion, snapshotID) || organizationCursorExpired(memberCursor, scopeVersion, snapshotID) {
		return nil, ErrOrganizationSnapshotExpired
	}

	departments := result.Snapshot.Departments
	members := result.Snapshot.Members
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

func (s *Service) readOrganizationSnapshot(ctx context.Context, actorUserID int, params OverviewParams, parentID string) (*OrganizationCacheResult, string, error) {
	scope, err := s.requireRepresentativeScope(ctx, actorUserID)
	if err != nil {
		return nil, "", fmt.Errorf("resolve team organization representative scope: %w", err)
	}
	branch, found := selectOrganizationBranch(scope, parentID)
	if !found {
		return nil, "", ErrOutOfScope
	}
	providerConfig, err := s.resolvePrimaryProviderConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("resolve primary relay provider configuration: %w", err)
	}

	loader := func(loadCtx context.Context) (OrganizationOriginLoadResult, error) {
		var provider relay.Provider
		if len(branch.subjects) > 0 {
			resolvedProvider, resolveErr := s.providerResolver.Resolve(loadCtx, providerConfig.ID)
			if resolveErr != nil {
				return OrganizationOriginLoadResult{}, fmt.Errorf("resolve primary relay provider origin: %w", resolveErr)
			}
			provider = resolvedProvider
		}
		snapshot, loadErr := s.generateOrganizationSnapshot(loadCtx, branch, provider, params)
		if loadErr == nil {
			return OrganizationOriginLoadResult{Snapshot: snapshot}, nil
		}
		if isHardSnapshotOriginError(loadErr) {
			return OrganizationOriginLoadResult{}, loadErr
		}
		return OrganizationOriginLoadResult{SnapshotErr: loadErr}, nil
	}

	if s.snapshotCache == nil {
		loaded, loadErr := loader(ctx)
		if loadErr != nil {
			return nil, "", fmt.Errorf("load team organization snapshot origin: %w", loadErr)
		}
		if loaded.SnapshotErr != nil {
			return nil, "", fmt.Errorf("load team organization snapshot origin: %w", loaded.SnapshotErr)
		}
		now := time.Now().UTC()
		return &OrganizationCacheResult{
			Snapshot:  loaded.Snapshot,
			Freshness: SnapshotFreshness{AsOf: now, FreshUntil: now, StaleUntil: now, CacheStatus: "miss", SourceStatus: "ok"},
		}, scope.Version, nil
	}

	scopeHash, err := effectiveScopeHash(scope)
	if err != nil {
		return nil, "", fmt.Errorf("hash team organization representative scope: %w", err)
	}
	result, err := s.snapshotCache.GetOrganizationOrLoad(ctx, OrganizationCacheKey{
		SnapshotCacheKey: SnapshotCacheKey{
			ProviderID: providerConfig.ID, ProviderVersion: providerConfig.ConfigurationVersion,
			ActorID: actorUserID, ScopeVersion: scope.Version, ScopeHash: scopeHash, Params: params,
		},
		ParentDepartmentExternalID: parentID,
	}, loader)
	if err != nil {
		return nil, "", fmt.Errorf("read team organization snapshot: %w", err)
	}
	return result, scope.Version, nil
}

type organizationBranchSelection struct {
	parentID         string
	rootDepthByID    map[string]int
	departmentsByID  map[string]representativescope.DepartmentScope
	childrenByParent map[string][]string
	childIDs         []string
	subjects         []representativescope.Subject
}

func selectOrganizationBranch(scope *representativescope.Scope, parentID string) (organizationBranchSelection, bool) {
	selection := organizationBranchSelection{
		parentID: parentID, rootDepthByID: map[string]int{}, departmentsByID: map[string]representativescope.DepartmentScope{},
		childrenByParent: map[string][]string{}, childIDs: []string{}, subjects: []representativescope.Subject{},
	}
	if scope == nil {
		return selection, false
	}
	for _, department := range scope.MemberTreeDepartments {
		id := strings.TrimSpace(department.ExternalID)
		if id == "" {
			continue
		}
		selection.departmentsByID[id] = department
		if department.ParentExternalID != nil {
			parent := strings.TrimSpace(*department.ParentExternalID)
			if parent != "" {
				selection.childrenByParent[parent] = append(selection.childrenByParent[parent], id)
			}
		}
	}
	for _, rootID := range scope.MemberTreeRootIDs {
		rootID = strings.TrimSpace(rootID)
		if root, ok := selection.departmentsByID[rootID]; ok {
			selection.rootDepthByID[rootID] = root.Depth
			if parentID == "" {
				selection.childIDs = append(selection.childIDs, rootID)
			}
		}
	}
	if parentID != "" {
		if _, ok := selection.departmentsByID[parentID]; !ok {
			return selection, false
		}
		selection.childIDs = append(selection.childIDs, selection.childrenByParent[parentID]...)
	}

	relevantDepartments := map[string]struct{}{}
	if parentID != "" {
		relevantDepartments[parentID] = struct{}{}
	}
	for _, childID := range selection.childIDs {
		for departmentID := range organizationDepartmentSubtree(childID, selection.childrenByParent) {
			relevantDepartments[departmentID] = struct{}{}
		}
	}
	source := scope.OverviewSubjects
	if len(source) == 0 {
		source = scope.Subjects
	}
	for _, subject := range source {
		if subject.SubjectType != "member" || !subjectIntersectsDepartments(subject, relevantDepartments) {
			continue
		}
		selection.subjects = append(selection.subjects, subject)
	}
	return selection, true
}

func (s *Service) generateOrganizationSnapshot(ctx context.Context, branch organizationBranchSelection, provider relay.Provider, params OverviewParams) (*OrganizationSnapshot, error) {
	resolvedSubjects := append([]representativescope.Subject(nil), branch.subjects...)
	statsByRelayUserID := map[int64]relay.TeamUserUsageStats{}
	if len(branch.subjects) > 0 {
		summaryProvider, ok := provider.(relay.TeamUsageSummaryProvider)
		if !ok {
			return nil, fmt.Errorf("team organization summary capability: %w", ErrProviderUnsupported)
		}
		var relayUserIDs []int64
		var err error
		resolvedSubjects, relayUserIDs, err = s.resolveSubjects(ctx, branch.subjects, provider)
		if err != nil {
			return nil, fmt.Errorf("resolve team organization Relay subjects: %w", err)
		}
		statsByRelayUserID, err = s.loadTeamUsageStats(ctx, summaryProvider, relayUserIDs, relay.TeamUsageSummaryParams{
			StartDate: strings.TrimSpace(params.StartDate), EndDate: strings.TrimSpace(params.EndDate),
			Granularity: strings.TrimSpace(params.Granularity), Timezone: strings.TrimSpace(params.Timezone), RequireCompleteRange: true,
		})
		if err != nil {
			return nil, fmt.Errorf("load team organization usage stats: %w", err)
		}
	}
	windowTotals := make(map[int64]overviewWindowTotal, len(statsByRelayUserID))
	for relayUserID, stat := range statsByRelayUserID {
		total := overviewWindowTotal{TotalTokens: stat.RangeTotalTokens}
		if stat.RangeActualCost != nil {
			total.ActualCost = *stat.RangeActualCost
		}
		windowTotals[relayUserID] = total
	}
	allMembers := BuildOverviewMemberDetails(resolvedSubjects, statsByRelayUserID, windowTotals)
	directMembers := []OverviewMember{}
	if branch.parentID != "" {
		for _, member := range allMembers {
			if overviewMemberInDepartment(member, branch.parentID) {
				directMembers = append(directMembers, member)
			}
		}
	}
	directMembers = rankMembersForPagination(directMembers)
	if directMembers == nil {
		directMembers = []OverviewMember{}
	}

	departments := make([]OrganizationDepartment, 0, len(branch.childIDs))
	for _, childID := range branch.childIDs {
		department, ok := branch.departmentsByID[childID]
		if !ok {
			continue
		}
		subtree := organizationDepartmentSubtree(childID, branch.childrenByParent)
		aggregateMembers := map[string]OverviewMember{}
		directCount := 0
		for _, member := range allMembers {
			if overviewMemberInDepartment(member, childID) {
				directCount++
			}
			if memberIntersectsDepartments(member, subtree) {
				if identity := overviewMemberIdentityKey(member); identity != "" {
					aggregateMembers[identity] = member
				}
			}
		}
		aggregateCount, connectedCount, rangeCost, rangeTokens := organizationMemberTotals(aggregateMembers)
		childCount := len(branch.childrenByParent[childID])
		departments = append(departments, OrganizationDepartment{
			DepartmentExternalID: childID, ParentExternalID: cloneStringPointer(department.ParentExternalID),
			Name: department.Name, DisplayPath: department.DisplayPath, Depth: organizationDisplayDepth(childID, branch),
			ChildCount: childCount, HasChildren: childCount > 0, DirectMemberCount: directCount,
			AggregateMemberCount: aggregateCount, ConnectedMemberCount: connectedCount,
			RangeActualCost: rangeCost, RangeTotalTokens: cloneInt64Pointer(rangeTokens),
		})
	}
	sort.Slice(departments, func(i, j int) bool {
		leftName := strings.ToLower(strings.TrimSpace(departments[i].Name))
		rightName := strings.ToLower(strings.TrimSpace(departments[j].Name))
		if leftName != rightName {
			return leftName < rightName
		}
		return departments[i].DepartmentExternalID < departments[j].DepartmentExternalID
	})
	snapshot := &OrganizationSnapshot{
		Window: buildOverviewWindow(params), Departments: departments, Members: directMembers,
	}
	if branch.parentID != "" {
		snapshot.ParentDepartmentExternalID = &branch.parentID
	}
	return snapshot, nil
}

func organizationMemberTotals(members map[string]OverviewMember) (count, connected int, rangeCost float64, rangeTokens *int64) {
	identities := make([]string, 0, len(members))
	for identity := range members {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	for _, identity := range identities {
		member := members[identity]
		if member.RelayUserID != nil {
			connected++
		}
		rangeCost += member.RangeActualCost
		rangeTokens = addOptionalInt64(rangeTokens, member.TotalTokens)
	}
	return len(members), connected, rangeCost, rangeTokens
}

func organizationDepartmentSubtree(rootID string, childrenByParent map[string][]string) map[string]struct{} {
	result := map[string]struct{}{}
	pending := []string{rootID}
	for len(pending) > 0 {
		last := len(pending) - 1
		departmentID := pending[last]
		pending = pending[:last]
		if _, seen := result[departmentID]; seen {
			continue
		}
		result[departmentID] = struct{}{}
		pending = append(pending, childrenByParent[departmentID]...)
	}
	return result
}

func subjectIntersectsDepartments(subject representativescope.Subject, departments map[string]struct{}) bool {
	for _, departmentID := range overviewSubjectDepartmentIDs(subject) {
		if _, ok := departments[departmentID]; ok {
			return true
		}
	}
	return false
}

func memberIntersectsDepartments(member OverviewMember, departments map[string]struct{}) bool {
	for _, departmentID := range overviewMemberDepartmentIDs(member) {
		if _, ok := departments[departmentID]; ok {
			return true
		}
	}
	return false
}

func organizationDisplayDepth(departmentID string, branch organizationBranchSelection) int {
	department, ok := branch.departmentsByID[departmentID]
	if !ok {
		return 0
	}
	currentID := departmentID
	for {
		if rootDepth, root := branch.rootDepthByID[currentID]; root {
			depth := department.Depth - rootDepth
			if depth < 0 {
				return 0
			}
			return depth
		}
		current, found := branch.departmentsByID[currentID]
		if !found || current.ParentExternalID == nil {
			break
		}
		currentID = strings.TrimSpace(*current.ParentExternalID)
		if currentID == "" {
			break
		}
	}
	if department.Depth < 0 {
		return 0
	}
	return department.Depth
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

func overviewMemberInDepartment(member OverviewMember, departmentID string) bool {
	for _, candidate := range overviewMemberDepartmentIDs(member) {
		if candidate == departmentID {
			return true
		}
	}
	return false
}

func organizationSnapshotIdentity(source []OrganizationDepartment, rankedMembers []OverviewMember) (string, error) {
	departments := append([]OrganizationDepartment(nil), source...)
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
