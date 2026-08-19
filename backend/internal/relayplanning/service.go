package relayplanning

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/relaygroupmapping"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/adminusers"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/teamusage"
)

const (
	maxPlanningUsers    = 5000
	maxCandidateWorkers = 8
	defaultValidityDays = 30
	maxGroupNameRunes   = 100
)

type ProviderResolver interface {
	Resolve(context.Context, int) (relay.Provider, error)
}

type prewarmUsageReader interface {
	ReadAuthorizedStats(context.Context, teamusage.PrewarmReadRequest) (map[int64]relay.TeamUserUsageStats, teamusage.PrewarmReadOutcome, error)
}

type subscriptionAssigner interface {
	AssignSubscriptionForUser(context.Context, int64, int64, int) error
}

type subscriptionRemover interface {
	RemoveSubscriptionForUser(context.Context, int64, int64) error
}

type Service struct {
	client        *ent.Client
	resolver      ProviderResolver
	users         *adminusers.Service
	prewarmReader prewarmUsageReader
}

func NewService(client *ent.Client, resolver ProviderResolver, prewarmReader *teamusage.PrewarmReader) *Service {
	return &Service{client: client, resolver: resolver, users: adminusers.NewService(client), prewarmReader: prewarmReader}
}

type PreviewRequest struct {
	ProviderID        int          `json:"provider_id"`
	DepartmentID      string       `json:"department_id"`
	Platform          string       `json:"platform"`
	TemplateGroupID   int64        `json:"template_group_id"`
	SourceGroupID     int64        `json:"source_group_id"`
	WeeklyCostTarget  float64      `json:"weekly_cost_target"`
	GroupCount        int          `json:"group_count"`
	SelectedUserIDs   []int        `json:"selected_user_ids"`
	Assignments       []Assignment `json:"assignments,omitempty"`
	AdoptRelayUserIDs []int64      `json:"adopt_relay_user_ids,omitempty"`
	ExistingMappingID int          `json:"existing_mapping_id"`
}

type Candidate struct {
	UserID             int      `json:"user_id"`
	RelayUserID        int64    `json:"relay_user_id"`
	Username           string   `json:"username"`
	Email              string   `json:"email"`
	RangeCost          float64  `json:"range_cost"`
	RangeTokens        int64    `json:"range_tokens"`
	GlobalTokenRank    int      `json:"global_token_rank"`
	CurrentGroupIDs    []int64  `json:"current_group_ids,omitempty"`
	MigratableKeyCount int      `json:"migratable_key_count"`
	SourceMember       bool     `json:"source_member"`
	CanAdd             bool     `json:"can_add"`
	Selected           bool     `json:"selected"`
	Eligible           bool     `json:"eligible"`
	Warnings           []string `json:"warnings,omitempty"`
}

type Assignment struct {
	Index           int     `json:"index"`
	TotalCost       float64 `json:"total_cost"`
	UserIDs         []int   `json:"user_ids"`
	TargetGroupID   int64   `json:"target_group_id,omitempty"`
	TargetGroupName string  `json:"target_group_name,omitempty"`
}

type Plan struct {
	ProviderID        int               `json:"provider_id"`
	DepartmentID      string            `json:"department_id"`
	DepartmentName    string            `json:"department_name"`
	Platform          string            `json:"platform"`
	TemplateGroupID   int64             `json:"template_group_id"`
	TemplateGroupName string            `json:"template_group_name"`
	SourceGroupID     int64             `json:"source_group_id"`
	SourceGroupName   string            `json:"source_group_name"`
	WeeklyCostTarget  float64           `json:"weekly_cost_target"`
	RecommendedCount  int               `json:"recommended_group_count"`
	GroupCount        int               `json:"group_count"`
	Candidates        []Candidate       `json:"candidates"`
	Assignments       []Assignment      `json:"assignments"`
	UnmanagedMembers  []UnmanagedMember `json:"unmanaged_members,omitempty"`
	Warnings          []string          `json:"warnings,omitempty"`
	GeneratedAt       time.Time         `json:"generated_at"`
	MappingID         int               `json:"mapping_id,omitempty"`
}

type UnmanagedMember struct {
	RelayUserID    int64   `json:"relay_user_id"`
	Username       string  `json:"username"`
	Email          string  `json:"email"`
	TargetGroupIDs []int64 `json:"target_group_ids"`
	RangeCost      float64 `json:"range_cost"`
}

type Mapping struct {
	ID                    int                          `json:"id"`
	ProviderID            int                          `json:"provider_id"`
	DepartmentID          string                       `json:"department_id"`
	DepartmentName        string                       `json:"department_name"`
	Platform              string                       `json:"platform"`
	TemplateGroupID       int64                        `json:"template_group_id"`
	TemplateGroupName     string                       `json:"template_group_name"`
	SourceGroupID         int64                        `json:"source_group_id"`
	SourceGroupName       string                       `json:"source_group_name"`
	GroupIDs              []int64                      `json:"group_ids"`
	Status                string                       `json:"status"`
	WeeklyCostTarget      float64                      `json:"weekly_cost_target"`
	MemberAssignments     map[string]int64             `json:"member_assignments,omitempty"`
	MemberSources         map[string]int64             `json:"member_sources,omitempty"`
	OperationState        map[string]map[string]string `json:"operation_state,omitempty"`
	UnmanagedMembers      []UnmanagedMember            `json:"unmanaged_members,omitempty"`
	DepartmentSuggestions []DepartmentSuggestion       `json:"department_suggestions,omitempty"`
	Warnings              []string                     `json:"warnings,omitempty"`
	UpdatedAt             time.Time                    `json:"updated_at"`
}

type DepartmentSuggestion struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ExecuteRequest struct {
	PreviewRequest
	OperationKey string `json:"operation_key"`
}

type GroupResult struct {
	Index  int    `json:"index"`
	ID     int64  `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type MemberResult struct {
	UserID        int      `json:"user_id"`
	RelayUserID   int64    `json:"relay_user_id,omitempty"`
	TargetGroupID int64    `json:"target_group_id,omitempty"`
	Subscription  string   `json:"subscription"`
	SourceRemoval string   `json:"source_removal"`
	APIKeys       []string `json:"api_keys,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type ExecutionResult struct {
	Plan     *Plan          `json:"plan"`
	Groups   []GroupResult  `json:"groups"`
	Members  []MemberResult `json:"members"`
	Mapping  *Mapping       `json:"mapping,omitempty"`
	Warnings []string       `json:"warnings,omitempty"`
}

func (s *Service) Preview(ctx context.Context, req PreviewRequest) (*Plan, error) {
	req = normalizeRequest(req)
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	providerConfig, err := s.client.RelayProvider.Get(ctx, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("load relay provider configuration: %w", err)
	}
	p, err := s.resolver.Resolve(ctx, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	lister, ok := p.(relay.PlatformGroupLister)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support group listing")
	}
	groups, err := lister.ListPlatformGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list relay groups: %w", err)
	}
	template, err := findSourceGroup(groups, req.TemplateGroupID, req.Platform)
	if err != nil {
		return nil, fmt.Errorf("resolve template group: %w", err)
	}
	source, err := findSourceGroup(groups, req.SourceGroupID, req.Platform)
	if err != nil {
		return nil, fmt.Errorf("resolve migration source group: %w", err)
	}
	users, err := s.users.Targets(ctx, adminusers.Filters{DepartmentID: req.DepartmentID}, maxPlanningUsers)
	if err != nil {
		return nil, fmt.Errorf("load department users: %w", err)
	}
	selected := selectedSet(req.SelectedUserIDs)
	if len(selected) > 0 {
		filtered := users[:0]
		for _, u := range users {
			if _, ok := selected[u.ID]; ok {
				filtered = append(filtered, u)
			}
		}
		users = filtered
	}
	candidates, err := s.buildCandidates(ctx, p, req.ProviderID, providerConfig.ConfigurationVersion, users, source, req.Platform, req.DepartmentID)
	if err != nil {
		return nil, fmt.Errorf("build relay planning candidates: %w", err)
	}
	eligible := make([]Candidate, 0, len(candidates))
	selectedProvided := len(selected) > 0
	for index := range candidates {
		candidates[index].Selected = candidates[index].Eligible
		if selectedProvided {
			_, candidates[index].Selected = selected[candidates[index].UserID]
		}
		if candidates[index].Eligible && candidates[index].Selected {
			eligible = append(eligible, candidates[index])
		}
	}
	recommended, count := resolveGroupCount(req, eligible)
	if count == 0 && len(req.Assignments) > 0 {
		count = assignmentCount(req.Assignments)
	}
	assignments := allocate(eligible, count)
	var unmanagedMembers []UnmanagedMember
	if req.ExistingMappingID > 0 {
		mapping, mappingErr := s.client.RelayGroupMapping.Get(ctx, req.ExistingMappingID)
		if mappingErr != nil {
			return nil, fmt.Errorf("load relay group mapping assignments: %w", mappingErr)
		}
		unmanagedMembers, err = s.loadUnmanagedMembers(ctx, p, mapping)
		if err != nil {
			return nil, fmt.Errorf("load unmanaged relay members: %w", err)
		}
		if len(req.Assignments) == 0 {
			assignments = stableMappingAssignments(mapping, candidates, unmanagedMembers, selected, count, mapping.WeeklyCostTarget)
		}
	}
	if len(req.Assignments) > 0 {
		assignments, err = validateAssignments(req.Assignments, candidates, count)
		if err != nil {
			return nil, fmt.Errorf("validate relay planning assignments: %w", err)
		}
		addUnmanagedCapacity(assignments, unmanagedMembers)
	}
	if err := s.assignTargets(ctx, req, groups, template.Name, assignments); err != nil {
		return nil, fmt.Errorf("assign relay planning targets: %w", err)
	}
	warnings := make([]string, 0)
	if len(eligible) == 0 {
		warnings = append(warnings, "no eligible member has a valid relay mapping and source-group membership")
	}
	if req.WeeklyCostTarget > 0 {
		for _, assignment := range assignments {
			if assignment.TotalCost > req.WeeklyCostTarget {
				name := assignment.TargetGroupName
				if name == "" {
					name = fmt.Sprintf("group %d", assignment.Index+1)
				}
				warnings = append(warnings, fmt.Sprintf("%s exceeds the planning target", name))
			}
		}
	}
	for _, candidate := range candidates {
		warnings = append(warnings, candidate.Warnings...)
	}
	if req.ExistingMappingID > 0 {
		assignedUsers := make(map[int]struct{})
		for _, assignment := range assignments {
			for _, userID := range assignment.UserIDs {
				assignedUsers[userID] = struct{}{}
			}
		}
		for _, candidate := range eligible {
			if _, assigned := assignedUsers[candidate.UserID]; !assigned {
				warnings = append(warnings, fmt.Sprintf("user %d exceeds remaining planning capacity", candidate.UserID))
			}
		}
	}
	plan := &Plan{
		ProviderID: req.ProviderID, DepartmentID: req.DepartmentID, Platform: req.Platform,
		TemplateGroupID: template.ID, TemplateGroupName: template.Name,
		SourceGroupID: source.ID, SourceGroupName: source.Name, WeeklyCostTarget: req.WeeklyCostTarget,
		RecommendedCount: recommended, GroupCount: count, Candidates: candidates, Assignments: assignments,
		UnmanagedMembers: unmanagedMembers,
		Warnings:         uniqueStrings(warnings), GeneratedAt: time.Now().UTC(),
	}
	assigned := make(map[int]struct{})
	for _, assignment := range assignments {
		for _, userID := range assignment.UserIDs {
			assigned[userID] = struct{}{}
		}
	}
	for index := range plan.Candidates {
		if len(req.Assignments) > 0 {
			_, plan.Candidates[index].Selected = assigned[plan.Candidates[index].UserID]
		}
	}
	if department, err := s.departmentName(ctx, req.DepartmentID); err == nil {
		plan.DepartmentName = department
	}
	if req.ExistingMappingID > 0 {
		plan.MappingID = req.ExistingMappingID
	}
	return plan, nil
}

func stableMappingAssignments(mapping *ent.RelayGroupMapping, candidates []Candidate, unmanaged []UnmanagedMember, selected map[int]struct{}, count int, target float64) []Assignment {
	if count <= 0 {
		return nil
	}
	assignments := make([]Assignment, count)
	for index := range assignments {
		assignments[index] = Assignment{Index: index, UserIDs: make([]int, 0)}
		if index < len(mapping.GroupIds) {
			assignments[index].TargetGroupID = mapping.GroupIds[index]
		}
	}
	byUser := make(map[int]Candidate, len(candidates))
	assigned := make(map[int]struct{})
	for _, candidate := range candidates {
		byUser[candidate.UserID] = candidate
	}
	addUnmanagedCapacity(assignments, unmanaged)
	for rawUserID, groupID := range mapping.MemberAssignments {
		userID, err := strconv.Atoi(rawUserID)
		if err != nil || groupID <= 0 {
			continue
		}
		candidate, ok := byUser[userID]
		if !ok || !candidate.CanAdd {
			continue
		}
		for index := range assignments {
			if assignments[index].TargetGroupID == groupID {
				assignments[index].UserIDs = append(assignments[index].UserIDs, userID)
				assignments[index].TotalCost += candidate.RangeCost
				assigned[userID] = struct{}{}
				break
			}
		}
	}
	// Count active target subscriptions that Relay reports even when the local
	// mapping has not adopted that member yet. The warning layer keeps them
	// unmanaged; this read-only cost contribution only protects capacity.
	for _, candidate := range candidates {
		if _, ok := assigned[candidate.UserID]; ok || !candidate.CanAdd {
			continue
		}
		for index := range assignments {
			if containsInt64(candidate.CurrentGroupIDs, assignments[index].TargetGroupID) {
				assignments[index].TotalCost += candidate.RangeCost
				break
			}
		}
	}
	for _, candidate := range candidates {
		if !candidate.CanAdd || !candidate.SourceMember {
			continue
		}
		if len(selected) > 0 {
			if _, ok := selected[candidate.UserID]; !ok {
				continue
			}
		}
		if _, ok := assigned[candidate.UserID]; ok {
			continue
		}
		best := -1
		for index := 0; index < len(assignments); index++ {
			if target > 0 && assignments[index].TotalCost+candidate.RangeCost > target {
				continue
			}
			if best < 0 || assignments[index].TotalCost < assignments[best].TotalCost {
				best = index
			}
		}
		if best < 0 {
			for index := range assignments {
				if target <= 0 || assignments[index].TotalCost+candidate.RangeCost <= target {
					best = index
					break
				}
			}
		}
		if best < 0 {
			continue
		}
		assignments[best].UserIDs = append(assignments[best].UserIDs, candidate.UserID)
		assignments[best].TotalCost += candidate.RangeCost
	}
	return assignments
}

func addUnmanagedCapacity(assignments []Assignment, unmanaged []UnmanagedMember) {
	for _, member := range unmanaged {
		for _, targetGroupID := range member.TargetGroupIDs {
			for index := range assignments {
				if assignments[index].TargetGroupID == targetGroupID {
					assignments[index].TotalCost += member.RangeCost
					break
				}
			}
		}
	}
}

func (s *Service) Execute(ctx context.Context, req ExecuteRequest) (*ExecutionResult, error) {
	if strings.TrimSpace(req.OperationKey) == "" {
		return nil, fmt.Errorf("operation_key is required")
	}
	plan, err := s.Preview(ctx, req.PreviewRequest)
	if err != nil {
		return nil, fmt.Errorf("preview relay plan for execution: %w", err)
	}
	p, err := s.resolver.Resolve(ctx, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	duplicator, ok := p.(relay.GroupDuplicator)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support group duplication")
	}
	statusUpdater, _ := p.(relay.GroupStatusUpdater)
	groupResults := make([]GroupResult, 0, plan.GroupCount)
	targetIDs := make(map[int]int64, plan.GroupCount)
	for index := 0; index < plan.GroupCount; index++ {
		group, duplicateErr := duplicator.DuplicateGroup(ctx, plan.TemplateGroupID, fmt.Sprintf("%s-%d", req.OperationKey, index))
		result := GroupResult{Index: index, Status: "failed"}
		if duplicateErr != nil {
			result.Error = duplicateErr.Error()
			groupResults = append(groupResults, result)
			continue
		}
		result.ID = group.ID
		result.Name = group.Name
		if index < len(plan.Assignments) {
			plan.Assignments[index].TargetGroupID = group.ID
			plan.Assignments[index].TargetGroupName = group.Name
		}
		if statusUpdater != nil {
			if activateErr := statusUpdater.UpdateGroupStatus(ctx, group.ID, "active"); activateErr != nil {
				result.Error = activateErr.Error()
				groupResults = append(groupResults, result)
				continue
			}
		}
		result.Status = "succeeded"
		targetIDs[index] = group.ID
		groupResults = append(groupResults, result)
	}
	assigner, _ := p.(subscriptionAssigner)
	remover, _ := p.(subscriptionRemover)
	binder, _ := p.(relay.APIKeyGroupBinder)
	memberResults := make([]MemberResult, 0, len(plan.Candidates))
	for _, assignment := range plan.Assignments {
		targetID := targetIDs[assignment.Index]
		if targetID <= 0 {
			continue
		}
		for _, userID := range assignment.UserIDs {
			member := MemberResult{UserID: userID, TargetGroupID: targetID, Subscription: "skipped", SourceRemoval: "skipped"}
			candidate := candidateByUserID(plan.Candidates, userID)
			if candidate == nil {
				member.Error = "candidate disappeared from plan"
				memberResults = append(memberResults, member)
				continue
			}
			if !candidate.CanAdd {
				member.Error = "candidate cannot be added to a target group"
				memberResults = append(memberResults, member)
				continue
			}
			if candidate.SourceMember {
				member = executeMemberMigration(ctx, p, assigner, remover, binder, candidate, targetID, plan.SourceGroupID, member)
			} else {
				member = executeMemberMigration(ctx, p, assigner, nil, nil, candidate, targetID, 0, member)
			}
			memberResults = append(memberResults, member)
		}
	}
	if len(req.AdoptRelayUserIDs) > 0 {
		adopted := make(map[int64]struct{}, len(req.AdoptRelayUserIDs))
		for _, relayUserID := range req.AdoptRelayUserIDs {
			if relayUserID > 0 {
				adopted[relayUserID] = struct{}{}
			}
		}
		for _, unmanaged := range plan.UnmanagedMembers {
			if _, requested := adopted[unmanaged.RelayUserID]; !requested {
				continue
			}
			for _, targetID := range unmanaged.TargetGroupIDs {
				member := MemberResult{RelayUserID: unmanaged.RelayUserID, TargetGroupID: targetID, Subscription: "failed", SourceRemoval: "skipped"}
				if assigner == nil {
					member.Error = "relay provider does not support subscription assignment"
				} else if assignErr := assigner.AssignSubscriptionForUser(ctx, unmanaged.RelayUserID, targetID, defaultValidityDays); assignErr != nil && !isAlreadyAssignedError(assignErr) {
					member.Error = assignErr.Error()
				} else {
					member.Subscription = "succeeded"
				}
				memberResults = append(memberResults, member)
			}
		}
	}
	groupIDList := make([]int64, plan.GroupCount)
	for index := 0; index < plan.GroupCount; index++ {
		groupIDList[index] = targetIDs[index]
	}
	state := executionState(req.OperationKey, groupResults, memberResults)
	mapping, err := s.saveMapping(ctx, plan, groupIDList, state)
	if err != nil {
		return nil, fmt.Errorf("save group mapping: %w", err)
	}
	return &ExecutionResult{Plan: plan, Groups: groupResults, Members: memberResults, Mapping: mapping}, nil
}

func (s *Service) ListMappings(ctx context.Context, providerID int) ([]Mapping, error) {
	query := s.client.RelayGroupMapping.Query().Order(ent.Asc(relaygroupmapping.FieldID))
	if providerID > 0 {
		query = query.Where(relaygroupmapping.ProviderIDEQ(providerID))
	}
	rows, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list relay group mappings: %w", err)
	}
	out := make([]Mapping, 0, len(rows))
	groupCache := make(map[int][]relay.Group)
	providerCache := make(map[int]relay.Provider)
	for _, row := range rows {
		mapping := mappingFromEnt(row)
		if _, loaded := groupCache[mapping.ProviderID]; !loaded {
			if s.resolver != nil {
				if provider, resolveErr := s.resolver.Resolve(ctx, mapping.ProviderID); resolveErr == nil {
					providerCache[mapping.ProviderID] = provider
					if lister, ok := provider.(relay.PlatformGroupLister); ok {
						if groups, listErr := lister.ListPlatformGroups(ctx); listErr == nil {
							groupCache[mapping.ProviderID] = groups
						}
					}
				}
			}
			if _, loaded := groupCache[mapping.ProviderID]; !loaded {
				groupCache[mapping.ProviderID] = nil
			}
		}
		mapping.Warnings = append(mapping.Warnings, mappingAvailabilityWarnings(mapping, groupCache[mapping.ProviderID])...)
		mapping.Warnings = append(mapping.Warnings, mappingRelationshipWarnings(ctx, s.client, providerCache[mapping.ProviderID], mapping)...)
		if departmentErr := s.validateDepartment(ctx, mapping.DepartmentID); departmentErr != nil {
			mapping.Warnings = append(mapping.Warnings, fmt.Sprintf("department %s is unavailable", mapping.DepartmentID))
			mapping.DepartmentSuggestions = s.departmentSuggestions(ctx, mapping.ProviderID, mapping.Platform, mapping.DepartmentID)
		}
		if len(mapping.GroupIDs) == 0 {
			mapping.Warnings = append(mapping.Warnings, "mapping has no target groups")
		}
		for _, groupID := range mapping.GroupIDs {
			if groupID <= 0 {
				mapping.Warnings = append(mapping.Warnings, "mapping contains an invalid target group")
				break
			}
		}
		out = append(out, mapping)
	}
	for index := range out {
		for otherIndex := index + 1; otherIndex < len(out); otherIndex++ {
			if out[index].ProviderID != out[otherIndex].ProviderID || out[index].Platform != out[otherIndex].Platform {
				continue
			}
			for userID := range out[index].MemberAssignments {
				if _, exists := out[otherIndex].MemberAssignments[userID]; !exists {
					continue
				}
				warning := fmt.Sprintf("user %s is assigned in multiple mappings", userID)
				out[index].Warnings = append(out[index].Warnings, warning)
				out[otherIndex].Warnings = append(out[otherIndex].Warnings, warning)
			}
		}
	}
	for index := range out {
		out[index].Warnings = uniqueStrings(out[index].Warnings)
	}
	return out, nil
}

func (s *Service) GetMapping(ctx context.Context, id int) (*Mapping, error) {
	if id <= 0 {
		return nil, fmt.Errorf("mapping id is required")
	}
	row, err := s.client.RelayGroupMapping.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get relay group mapping: %w", err)
	}
	mapping := mappingFromEnt(row)
	if groups, groupsErr := s.listPlatformGroups(ctx, mapping.ProviderID); groupsErr == nil {
		mapping.Warnings = mappingAvailabilityWarnings(mapping, groups)
	}
	if departmentErr := s.validateDepartment(ctx, mapping.DepartmentID); departmentErr != nil {
		mapping.Warnings = append(mapping.Warnings, fmt.Sprintf("department %s is unavailable", mapping.DepartmentID))
		mapping.DepartmentSuggestions = s.departmentSuggestions(ctx, mapping.ProviderID, mapping.Platform, mapping.DepartmentID)
	}
	mapping.Warnings = uniqueStrings(mapping.Warnings)
	return &mapping, nil
}

func (s *Service) Rebind(ctx context.Context, id int, departmentID string, templateGroupID, sourceGroupID int64, groupIDs []int64, status string) (*Mapping, error) {
	if id <= 0 {
		return nil, fmt.Errorf("mapping id is required")
	}
	row, err := s.client.RelayGroupMapping.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load relay group mapping: %w", err)
	}
	if templateGroupID <= 0 {
		templateGroupID = row.TemplateGroupID
	}
	if sourceGroupID <= 0 {
		sourceGroupID = row.SourceGroupID
	}
	if strings.TrimSpace(departmentID) == "" {
		departmentID = row.DepartmentExternalID
	}
	departmentID = strings.TrimSpace(departmentID)
	departmentName := row.DepartmentName
	if departmentID != row.DepartmentExternalID {
		name, nameErr := s.departmentName(ctx, departmentID)
		if nameErr != nil {
			return nil, fmt.Errorf("department %s is unavailable: %w", departmentID, nameErr)
		}
		departmentName = name
	}
	if templateGroupID <= 0 || sourceGroupID <= 0 {
		return nil, fmt.Errorf("template_group_id and source_group_id are required")
	}
	if status == "" {
		status = "active"
	}
	groups, groupsErr := s.listPlatformGroups(ctx, row.ProviderID)
	if groupsErr != nil {
		return nil, fmt.Errorf("validate relay group mapping groups: %w", groupsErr)
	}
	template, groupErr := findGroupForPlatform(groups, templateGroupID, row.Platform, "template")
	if groupErr != nil {
		return nil, groupErr
	}
	source, groupErr := findGroupForPlatform(groups, sourceGroupID, row.Platform, "migration source")
	if groupErr != nil {
		return nil, groupErr
	}
	seenGroups := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			return nil, fmt.Errorf("target group IDs must be positive")
		}
		if _, exists := seenGroups[groupID]; exists {
			return nil, fmt.Errorf("target group %d is duplicated", groupID)
		}
		seenGroups[groupID] = struct{}{}
		if _, targetErr := findGroupForPlatform(groups, groupID, row.Platform, "target"); targetErr != nil {
			return nil, targetErr
		}
	}
	row, err = s.client.RelayGroupMapping.UpdateOneID(id).
		SetDepartmentExternalID(departmentID).
		SetDepartmentName(departmentName).
		SetTemplateGroupID(templateGroupID).
		SetTemplateGroupName(template.Name).
		SetSourceGroupID(sourceGroupID).
		SetSourceGroupName(source.Name).
		SetGroupIds(append([]int64(nil), groupIDs...)).
		SetStatus(status).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("rebind relay group mapping: %w", err)
	}
	mapping := mappingFromEnt(row)
	if nameErr := s.validateDepartment(ctx, mapping.DepartmentID); nameErr != nil {
		mapping.Warnings = append(mapping.Warnings, fmt.Sprintf("department %s is unavailable", mapping.DepartmentID))
		mapping.DepartmentSuggestions = s.departmentSuggestions(ctx, mapping.ProviderID, mapping.Platform, mapping.DepartmentID)
	}
	return &mapping, nil
}

func (s *Service) listPlatformGroups(ctx context.Context, providerID int) ([]relay.Group, error) {
	if s.resolver == nil {
		return nil, fmt.Errorf("relay provider resolver is unavailable")
	}
	p, err := s.resolver.Resolve(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	lister, ok := p.(relay.PlatformGroupLister)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support group listing")
	}
	groups, err := lister.ListPlatformGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list relay groups: %w", err)
	}
	return groups, nil
}

func findGroupForPlatform(groups []relay.Group, id int64, platform, role string) (relay.Group, error) {
	for _, group := range groups {
		if group.ID != id {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(platform)) {
			return relay.Group{}, fmt.Errorf("%s group %d does not belong to platform %s", role, id, platform)
		}
		return group, nil
	}
	return relay.Group{}, fmt.Errorf("%s group %d is unavailable", role, id)
}

func (s *Service) validateDepartment(ctx context.Context, externalID string) error {
	_, err := s.departmentName(ctx, externalID)
	return err
}

func (s *Service) departmentSuggestions(ctx context.Context, providerID int, platform, currentID string) []DepartmentSuggestion {
	snapshot, found, err := directorysync.CurrentSnapshot(ctx, s.client)
	if err != nil || !found {
		return nil
	}
	mappings, err := s.client.RelayGroupMapping.Query().Where(
		relaygroupmapping.ProviderIDEQ(providerID),
		relaygroupmapping.PlatformEQ(platform),
	).All(ctx)
	if err != nil {
		return nil
	}
	bound := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		bound[mapping.DepartmentExternalID] = struct{}{}
	}
	departments, err := s.client.DirectoryDepartment.Query().Where(directorydepartment.SourceIDEQ(snapshot.SourceID)).Order(ent.Asc(directorydepartment.FieldName)).Limit(50).All(ctx)
	if err != nil {
		return nil
	}
	suggestions := make([]DepartmentSuggestion, 0, len(departments))
	for _, department := range departments {
		if department.ExternalID == currentID {
			continue
		}
		if _, exists := bound[department.ExternalID]; exists {
			continue
		}
		suggestions = append(suggestions, DepartmentSuggestion{ID: department.ExternalID, Name: department.Name})
	}
	return suggestions
}

func (s *Service) Replan(ctx context.Context, mappingID int, selected []int, assignments []Assignment, adoptRelayUserIDs []int64) (*Plan, error) {
	row, err := s.client.RelayGroupMapping.Get(ctx, mappingID)
	if err != nil {
		return nil, fmt.Errorf("load relay group mapping: %w", err)
	}
	return s.Preview(ctx, PreviewRequest{ProviderID: row.ProviderID, DepartmentID: row.DepartmentExternalID, Platform: row.Platform, TemplateGroupID: row.TemplateGroupID, SourceGroupID: row.SourceGroupID, WeeklyCostTarget: row.WeeklyCostTarget, GroupCount: len(row.GroupIds), SelectedUserIDs: selected, Assignments: assignments, AdoptRelayUserIDs: adoptRelayUserIDs, ExistingMappingID: mappingID})
}

// ExecuteReplan applies only the final member-to-target assignment matrix. The
// mapping's target Group IDs remain stable; group creation and deactivation are
// deliberately outside replan.
func (s *Service) ExecuteReplan(ctx context.Context, mappingID int, req ExecuteRequest) (*ExecutionResult, error) {
	mapping, err := s.GetMapping(ctx, mappingID)
	if err != nil {
		return nil, fmt.Errorf("load mapping for replan execution: %w", err)
	}
	if strings.TrimSpace(req.OperationKey) == "" {
		return nil, fmt.Errorf("operation_key is required")
	}
	if req.ProviderID == 0 {
		req.ProviderID = mapping.ProviderID
	}
	if req.DepartmentID == "" {
		req.DepartmentID = mapping.DepartmentID
	}
	if req.Platform == "" {
		req.Platform = mapping.Platform
	}
	if req.SourceGroupID == 0 {
		req.SourceGroupID = mapping.SourceGroupID
	}
	if req.TemplateGroupID == 0 {
		req.TemplateGroupID = mapping.TemplateGroupID
	}
	if req.WeeklyCostTarget == 0 {
		req.WeeklyCostTarget = mapping.WeeklyCostTarget
	}
	req.GroupCount = len(mapping.GroupIDs)
	req.ExistingMappingID = mappingID
	plan, err := s.Preview(ctx, req.PreviewRequest)
	if err != nil {
		return nil, fmt.Errorf("preview relay replan for execution: %w", err)
	}
	p, err := s.resolver.Resolve(ctx, mapping.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider for replan: %w", err)
	}
	assigner, _ := p.(subscriptionAssigner)
	remover, _ := p.(subscriptionRemover)
	binder, _ := p.(relay.APIKeyGroupBinder)
	memberResults := make([]MemberResult, 0, len(plan.Candidates))
	groupResults := make([]GroupResult, 0, len(mapping.GroupIDs))
	for index, groupID := range mapping.GroupIDs {
		name := ""
		for _, assignment := range plan.Assignments {
			if assignment.Index == index {
				name = assignment.TargetGroupName
				break
			}
		}
		groupResults = append(groupResults, GroupResult{Index: index, ID: groupID, Name: name, Status: "unchanged"})
	}
	oldAssignments := mapping.MemberAssignments
	oldSources := mapping.MemberSources
	for _, assignment := range plan.Assignments {
		if assignment.Index >= len(mapping.GroupIDs) {
			continue
		}
		targetID := mapping.GroupIDs[assignment.Index]
		if targetID <= 0 {
			continue
		}
		for _, userID := range assignment.UserIDs {
			candidate := candidateByUserID(plan.Candidates, userID)
			if candidate == nil {
				continue
			}
			key := strconv.Itoa(userID)
			retry := operationStateNeedsRetry(mapping.OperationState, "member:"+key)
			fromGroupID := int64(0)
			if retry {
				fromGroupID = oldSources[key]
				if fromGroupID <= 0 && candidate.SourceMember {
					fromGroupID = mapping.SourceGroupID
				}
			} else {
				fromGroupID = oldAssignments[key]
				if fromGroupID <= 0 && candidate.SourceMember {
					fromGroupID = mapping.SourceGroupID
				}
			}
			member := MemberResult{UserID: userID, TargetGroupID: targetID, Subscription: "skipped", SourceRemoval: "skipped"}
			if !retry && fromGroupID == targetID {
				member.Subscription = "unchanged"
				member.SourceRemoval = "skipped"
			} else if candidate.SourceMember {
				member = executeMemberMigration(ctx, p, assigner, remover, binder, candidate, targetID, fromGroupID, member)
			} else {
				member = executeMemberMigration(ctx, p, assigner, nil, nil, candidate, targetID, 0, member)
			}
			memberResults = append(memberResults, member)
		}
	}
	if len(req.AdoptRelayUserIDs) > 0 {
		adopted := make(map[int64]struct{}, len(req.AdoptRelayUserIDs))
		for _, relayUserID := range req.AdoptRelayUserIDs {
			if relayUserID > 0 {
				adopted[relayUserID] = struct{}{}
			}
		}
		for _, unmanaged := range plan.UnmanagedMembers {
			if _, requested := adopted[unmanaged.RelayUserID]; !requested {
				continue
			}
			for _, targetID := range unmanaged.TargetGroupIDs {
				member := MemberResult{RelayUserID: unmanaged.RelayUserID, TargetGroupID: targetID, Subscription: "failed", SourceRemoval: "skipped"}
				if assigner == nil {
					member.Error = "relay provider does not support subscription assignment"
				} else if assignErr := assigner.AssignSubscriptionForUser(ctx, unmanaged.RelayUserID, targetID, defaultValidityDays); assignErr != nil && !isAlreadyAssignedError(assignErr) {
					member.Error = assignErr.Error()
				} else {
					member.Subscription = "succeeded"
				}
				memberResults = append(memberResults, member)
			}
		}
	}
	state := executionState(req.OperationKey, groupResults, memberResults)
	resultMapping, err := s.saveMapping(ctx, plan, append([]int64(nil), mapping.GroupIDs...), state)
	if err != nil {
		return nil, fmt.Errorf("save relay replan mapping: %w", err)
	}
	return &ExecutionResult{Plan: plan, Groups: groupResults, Members: memberResults, Mapping: resultMapping}, nil
}

func executionState(operationKey string, groups []GroupResult, members []MemberResult) map[string]map[string]string {
	state := map[string]map[string]string{
		"operation": {"key": operationKey, "status": "succeeded"},
	}
	for _, group := range groups {
		entry := map[string]string{"status": group.Status}
		if group.ID > 0 {
			entry["target_group_id"] = strconv.FormatInt(group.ID, 10)
		}
		if group.Name != "" {
			entry["target_group_name"] = group.Name
		}
		if group.Error != "" {
			entry["error"] = group.Error
			state["operation"]["status"] = "needs_retry"
		}
		state[fmt.Sprintf("group:%d", group.Index)] = entry
	}
	for _, member := range members {
		entry := map[string]string{
			"subscription":   member.Subscription,
			"source_removal": member.SourceRemoval,
		}
		if member.RelayUserID > 0 && member.Error == "" && member.Subscription == "succeeded" {
			entry["status"] = "adopted"
		}
		if member.TargetGroupID > 0 {
			entry["target_group_id"] = strconv.FormatInt(member.TargetGroupID, 10)
		}
		if len(member.APIKeys) > 0 {
			entry["api_keys"] = strings.Join(member.APIKeys, ",")
		}
		if member.Error != "" {
			entry["error"] = member.Error
			state["operation"]["status"] = "needs_retry"
		}
		if member.Subscription == "failed" || member.SourceRemoval == "failed" {
			state["operation"]["status"] = "needs_retry"
		}
		if member.RelayUserID > 0 {
			state[fmt.Sprintf("relay:%d:%d", member.RelayUserID, member.TargetGroupID)] = entry
		} else {
			state[fmt.Sprintf("member:%d", member.UserID)] = entry
		}
	}
	return state
}

func executeMemberMigration(ctx context.Context, p relay.Provider, assigner subscriptionAssigner, remover subscriptionRemover, binder relay.APIKeyGroupBinder, candidate *Candidate, targetGroupID, fromGroupID int64, member MemberResult) MemberResult {
	if assigner == nil {
		member.Error = "relay provider does not support subscription assignment"
		return member
	}
	if err := assigner.AssignSubscriptionForUser(ctx, candidate.RelayUserID, targetGroupID, defaultValidityDays); err != nil && !isAlreadyAssignedError(err) {
		member.Error = err.Error()
		return member
	}
	member.Subscription = "succeeded"
	if fromGroupID <= 0 || fromGroupID == targetGroupID {
		return member
	}
	keys, err := p.ListUserAPIKeys(ctx, candidate.RelayUserID)
	if err != nil {
		member.Error = err.Error()
		return member
	}
	if binder != nil {
		apiKeyError := false
		for _, key := range keys {
			if apiKeyGroupID(key) != fromGroupID {
				continue
			}
			if bindErr := binder.BindAPIKeyToGroup(ctx, key.ID, targetGroupID); bindErr != nil {
				member.APIKeys = append(member.APIKeys, fmt.Sprintf("%d:failed:%s", key.ID, bindErr))
				apiKeyError = true
			} else {
				member.APIKeys = append(member.APIKeys, fmt.Sprintf("%d:succeeded", key.ID))
			}
		}
		if apiKeyError {
			member.Error = "one or more API keys could not be moved"
			return member
		}
	}
	if remover == nil {
		member.SourceRemoval = "skipped"
		return member
	}
	if err := remover.RemoveSubscriptionForUser(ctx, candidate.RelayUserID, fromGroupID); err != nil && !isNotFoundError(err) {
		member.SourceRemoval = "failed"
		member.Error = err.Error()
		return member
	}
	member.SourceRemoval = "succeeded"
	return member
}

func isAlreadyAssignedError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "already")
}

func isNotFoundError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")
}

func eligibleCandidates(plan *Plan) []Candidate {
	out := make([]Candidate, 0, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		if candidate.Eligible {
			out = append(out, candidate)
		}
	}
	return out
}

func containsInt64(items []int64, target int64) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func apiKeyGroupID(key relay.APIKey) int64 {
	if key.GroupID > 0 {
		return key.GroupID
	}
	if key.Group != nil {
		return key.Group.ID
	}
	return 0
}

func validateRequest(req PreviewRequest) error {
	if req.ProviderID <= 0 || strings.TrimSpace(req.DepartmentID) == "" || strings.TrimSpace(req.Platform) == "" || req.TemplateGroupID <= 0 || req.SourceGroupID <= 0 {
		return fmt.Errorf("provider_id, department_id, platform, template_group_id, and source_group_id are required")
	}
	if req.WeeklyCostTarget < 0 || math.IsNaN(req.WeeklyCostTarget) || math.IsInf(req.WeeklyCostTarget, 0) {
		return fmt.Errorf("weekly_cost_target must be a finite non-negative number")
	}
	return nil
}

func normalizeRequest(req PreviewRequest) PreviewRequest {
	req.DepartmentID = strings.TrimSpace(req.DepartmentID)
	req.Platform = strings.TrimSpace(req.Platform)
	if req.TemplateGroupID <= 0 {
		req.TemplateGroupID = req.SourceGroupID
	}
	if req.SourceGroupID <= 0 {
		req.SourceGroupID = req.TemplateGroupID
	}
	if req.GroupCount < 0 {
		req.GroupCount = 0
	}
	return req
}

func assignmentCount(assignments []Assignment) int {
	count := 0
	for _, assignment := range assignments {
		if assignment.Index >= count {
			count = assignment.Index + 1
		}
	}
	return count
}

func validateAssignments(assignments []Assignment, candidates []Candidate, count int) ([]Assignment, error) {
	if count <= 0 || len(assignments) != count {
		return nil, fmt.Errorf("assignments must contain exactly %d target groups", count)
	}
	byUser := make(map[int]Candidate, len(candidates))
	for _, candidate := range candidates {
		byUser[candidate.UserID] = candidate
	}
	seenUsers := make(map[int]struct{})
	seenIndexes := make(map[int]struct{}, len(assignments))
	validated := make([]Assignment, count)
	for _, assignment := range assignments {
		if assignment.Index < 0 || assignment.Index >= count {
			return nil, fmt.Errorf("assignment index %d is out of range", assignment.Index)
		}
		if _, exists := seenIndexes[assignment.Index]; exists {
			return nil, fmt.Errorf("assignment index %d is duplicated", assignment.Index)
		}
		seenIndexes[assignment.Index] = struct{}{}
		validated[assignment.Index] = Assignment{Index: assignment.Index, TargetGroupID: assignment.TargetGroupID, TargetGroupName: strings.TrimSpace(assignment.TargetGroupName), UserIDs: make([]int, 0, len(assignment.UserIDs))}
		for _, userID := range assignment.UserIDs {
			candidate, ok := byUser[userID]
			if !ok || !candidate.CanAdd {
				return nil, fmt.Errorf("user %d cannot be added to a target group", userID)
			}
			if _, exists := seenUsers[userID]; exists {
				return nil, fmt.Errorf("user %d is assigned more than once", userID)
			}
			seenUsers[userID] = struct{}{}
			validated[assignment.Index].UserIDs = append(validated[assignment.Index].UserIDs, userID)
			validated[assignment.Index].TotalCost += candidate.RangeCost
		}
	}
	for index := range validated {
		if _, ok := seenIndexes[index]; !ok {
			return nil, fmt.Errorf("assignment index %d is missing", index)
		}
	}
	return validated, nil
}

func findSourceGroup(groups []relay.Group, id int64, platform string) (relay.Group, error) {
	for _, group := range groups {
		if group.ID == id {
			if !strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(platform)) {
				return relay.Group{}, fmt.Errorf("source group platform does not match requested platform")
			}
			return group, nil
		}
	}
	return relay.Group{}, fmt.Errorf("group %d is unavailable", id)
}

func (s *Service) buildCandidates(ctx context.Context, p relay.Provider, providerID int, providerVersion int64, users []*ent.User, source relay.Group, platform, departmentID string) ([]Candidate, error) {
	allUsers, err := s.client.User.Query().Where(user.RelayUserIDNotNil()).Order(ent.Asc(user.FieldID)).Limit(maxPlanningUsers).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load users for global token ranking: %w", err)
	}
	ids := make([]int64, 0, len(allUsers))
	for _, u := range allUsers {
		if u.RelayUserID != nil && *u.RelayUserID > 0 {
			ids = append(ids, int64(*u.RelayUserID))
		}
	}
	stats, err := s.loadUsageStats(ctx, p, providerID, providerVersion, ids)
	if err != nil {
		return nil, fmt.Errorf("load 30-day usage: %w", err)
	}
	globalStats := stats
	if len(globalStats) == 0 {
		globalStats = stats
	}
	rankIDs := append([]int64(nil), ids...)
	sort.Slice(rankIDs, func(i, j int) bool {
		left, right := stats[rankIDs[i]], stats[rankIDs[j]]
		return usageTokens(left) > usageTokens(right) || (usageTokens(left) == usageTokens(right) && rankIDs[i] < rankIDs[j])
	})
	ranks := make(map[int64]int, len(rankIDs))
	for i, id := range rankIDs {
		ranks[id] = i + 1
	}
	out := make([]Candidate, len(users))
	jobs := make(chan struct {
		index int
		user  *ent.User
	}, len(users))
	for index, u := range users {
		jobs <- struct {
			index int
			user  *ent.User
		}{index: index, user: u}
	}
	close(jobs)
	workerCount := maxCandidateWorkers
	if len(users) < workerCount {
		workerCount = len(users)
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for job := range jobs {
				out[job.index] = s.buildCandidate(ctx, p, job.user, source, platform, departmentID, globalStats, ranks)
			}
		}()
	}
	workers.Wait()
	sort.Slice(out, func(i, j int) bool {
		return out[i].RangeCost > out[j].RangeCost || (out[i].RangeCost == out[j].RangeCost && out[i].UserID < out[j].UserID)
	})
	return out, nil
}

func (s *Service) buildCandidate(ctx context.Context, p relay.Provider, u *ent.User, source relay.Group, platform, departmentID string, globalStats map[int64]relay.TeamUserUsageStats, ranks map[int64]int) Candidate {
	candidate := Candidate{UserID: u.ID, Username: u.Username, Email: u.Email, Eligible: false, Selected: true}
	if u.RelayUserID == nil || *u.RelayUserID <= 0 {
		candidate.Warnings = append(candidate.Warnings, fmt.Sprintf("user %d has no relay mapping", u.ID))
		return candidate
	}
	candidate.RelayUserID = int64(*u.RelayUserID)
	stat := globalStats[candidate.RelayUserID]
	candidate.RangeCost = usageCost(stat)
	candidate.RangeTokens = usageTokens(stat)
	candidate.GlobalTokenRank = ranks[candidate.RelayUserID]
	facts := loadCandidateRelayFacts(ctx, p, candidate.RelayUserID, source, platform)
	if facts.groupErr != nil {
		candidate.Warnings = append(candidate.Warnings, fmt.Sprintf("relay groups unavailable: %v", facts.groupErr))
		return candidate
	}
	candidate.SourceMember = facts.eligible
	candidate.Eligible = facts.eligible
	candidate.CanAdd = facts.canAdd
	candidate.CurrentGroupIDs = facts.currentGroupIDs
	candidate.MigratableKeyCount = facts.migratableKeyCount
	if !candidate.SourceMember {
		candidate.Warnings = append(candidate.Warnings, "user is not a member of the selected source group")
	} else if candidate.MigratableKeyCount == 0 {
		candidate.Warnings = append(candidate.Warnings, "no migratable AE-managed API key")
	}
	if conflict, conflictErr := s.hasDepartmentConflict(ctx, u, departmentID); conflictErr == nil && conflict {
		candidate.Warnings = append(candidate.Warnings, "user belongs to multiple departments")
	}
	candidate.Selected = candidate.Eligible
	return candidate
}

type candidateRelayFacts struct {
	eligible           bool
	canAdd             bool
	currentGroupIDs    []int64
	migratableKeyCount int
	groupErr, keyErr   error
}

func loadCandidateRelayFacts(ctx context.Context, p relay.Provider, userID int64, source relay.Group, platform string) candidateRelayFacts {
	var facts candidateRelayFacts
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		groupIDs := make(map[int64]struct{})
		usedSubscriptions := false
		if lister, ok := p.(relay.UserSubscriptionLister); ok {
			subscriptions, err := lister.ListUserSubscriptions(ctx, userID)
			if err == nil {
				usedSubscriptions = true
				facts.canAdd = true
				for _, subscription := range subscriptions {
					groupID := subscription.GroupID
					if groupID <= 0 && subscription.Group != nil {
						groupID = subscription.Group.ID
					}
					if !strings.EqualFold(strings.TrimSpace(subscription.Status), "active") || groupID <= 0 {
						continue
					}
					if groupID == source.ID {
						facts.eligible = strings.EqualFold(strings.TrimSpace(source.Platform), strings.TrimSpace(platform))
					} else {
						groupIDs[groupID] = struct{}{}
					}
				}
			}
		}
		if !usedSubscriptions {
			allowed, err := p.ListAllowedGroupsForUser(ctx, userID)
			facts.groupErr = err
			if err != nil {
				return
			}
			for _, group := range allowed {
				if group.ID == source.ID {
					facts.eligible = strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(platform))
				} else if group.ID > 0 {
					groupIDs[group.ID] = struct{}{}
				}
			}
			facts.canAdd = true
		}
		facts.currentGroupIDs = make([]int64, 0, len(groupIDs))
		for groupID := range groupIDs {
			facts.currentGroupIDs = append(facts.currentGroupIDs, groupID)
		}
		sort.Slice(facts.currentGroupIDs, func(i, j int) bool { return facts.currentGroupIDs[i] < facts.currentGroupIDs[j] })
	}()
	go func() {
		defer workers.Done()
		keys, err := p.ListUserAPIKeys(ctx, userID)
		facts.keyErr = err
		if err != nil {
			return
		}
		for _, key := range keys {
			if apiKeyGroupID(key) == source.ID {
				facts.migratableKeyCount++
			}
		}
	}()
	workers.Wait()
	return facts
}

func (s *Service) hasDepartmentConflict(ctx context.Context, u *ent.User, selectedDepartment string) (bool, error) {
	snapshot, found, err := directorysync.CurrentSnapshot(ctx, s.client)
	if err != nil || !found || u == nil {
		return false, err
	}
	memberQuery := s.client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(snapshot.SourceID))
	if u.RelayUserID != nil {
		memberQuery = memberQuery.Where(directorymember.Or(directorymember.MatchedUserIDEQ(u.ID), directorymember.EmailNormalizedEQ(strings.ToLower(strings.TrimSpace(u.Email)))))
	} else {
		memberQuery = memberQuery.Where(directorymember.EmailNormalizedEQ(strings.ToLower(strings.TrimSpace(u.Email))))
	}
	members, err := memberQuery.All(ctx)
	if err != nil {
		return false, err
	}
	departments := make(map[string]struct{})
	for _, member := range members {
		if strings.TrimSpace(member.DepartmentExternalID) != "" {
			departments[member.DepartmentExternalID] = struct{}{}
		}
		membershipRows, membershipErr := s.client.DirectoryMemberDepartment.Query().Where(directorymemberdepartment.DirectoryMemberIDEQ(member.ID), directorymemberdepartment.SourceIDEQ(snapshot.SourceID)).All(ctx)
		if membershipErr != nil {
			return false, membershipErr
		}
		for _, row := range membershipRows {
			departments[row.DepartmentExternalID] = struct{}{}
		}
	}
	if selectedDepartment != "" {
		delete(departments, selectedDepartment)
	}
	return len(departments) > 0, nil
}

func (s *Service) loadUsageStats(ctx context.Context, p relay.Provider, providerID int, providerVersion int64, ids []int64) (map[int64]relay.TeamUserUsageStats, error) {
	now := time.Now().UTC()
	params := thirtyDayUsageParams(now)
	if s.prewarmReader != nil && providerID > 0 && providerVersion > 0 {
		stats, outcome, err := s.prewarmReader.ReadAuthorizedStats(ctx, teamusage.PrewarmReadRequest{
			ProviderID: providerID, ProviderVersion: providerVersion,
			Params: teamusage.OverviewParams{
				StartDate: params.StartDate, EndDate: params.EndDate,
				Granularity: params.Granularity, Timezone: params.Timezone,
			},
			AuthorizedRelayUserIDs: ids,
		})
		if err == nil && outcome == teamusage.PrewarmReadFullHit && stats != nil {
			return stats, nil
		}
	}
	return usageStatsAt(ctx, p, ids, now)
}

func usageStats(ctx context.Context, p relay.Provider, ids []int64) (map[int64]relay.TeamUserUsageStats, error) {
	return usageStatsAt(ctx, p, ids, time.Now().UTC())
}

func usageStatsAt(ctx context.Context, p relay.Provider, ids []int64, now time.Time) (map[int64]relay.TeamUserUsageStats, error) {
	result := make(map[int64]relay.TeamUserUsageStats, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	params := thirtyDayUsageParams(now)
	if batch, ok := p.(relay.TeamUsageSummaryProvider); ok {
		var trend map[int64][]relay.UsageTrendPoint
		var trendErr error
		trendDone := make(chan struct{})
		if trendProvider, trendOK := p.(relay.TeamMemberTrendProvider); trendOK {
			go func() {
				trend, trendErr = trendProvider.GetUsageTrendForUsers(ctx, ids, relay.TeamMemberTrendParams{
					StartDate: params.StartDate, EndDate: params.EndDate, Granularity: params.Granularity, Timezone: params.Timezone,
				})
				close(trendDone)
			}()
		} else {
			close(trendDone)
		}
		for start := 0; start < len(ids); start += 500 {
			end := start + 500
			if end > len(ids) {
				end = len(ids)
			}
			stats, err := batch.GetBatchUserUsageStats(ctx, ids[start:end], params)
			if err != nil {
				return nil, err
			}
			for id, stat := range stats {
				result[id] = stat
			}
		}
		<-trendDone
		if trendErr == nil {
			mergeTrendUsage(result, trend)
		}
		return result, nil
	}
	for _, id := range ids {
		stat, err := p.GetUsageStats(ctx, id, now.Add(-30*24*time.Hour), now)
		if err != nil {
			return nil, err
		}
		cost := stat.TotalCost
		tokens := stat.TotalTokens
		result[id] = relay.TeamUserUsageStats{UserID: id, RangeActualCost: &cost, RangeTotalTokens: &tokens, TotalActualCost: cost, TotalTokens: &tokens}
	}
	return result, nil
}

func thirtyDayUsageParams(now time.Time) relay.TeamUsageSummaryParams {
	now = now.UTC()
	return relay.TeamUsageSummaryParams{
		StartDate: now.AddDate(0, 0, -29).Format(time.DateOnly),
		EndDate:   now.Format(time.DateOnly), Granularity: "day", Timezone: "UTC",
	}
}

func mergeTrendUsage(stats map[int64]relay.TeamUserUsageStats, trends map[int64][]relay.UsageTrendPoint) {
	for userID, points := range trends {
		var tokens int64
		var actualCost float64
		for _, point := range points {
			if point.TotalTokens != nil {
				tokens += *point.TotalTokens
			}
			actualCost += point.ActualCost
		}
		stat := stats[userID]
		if stat.UserID == 0 {
			stat.UserID = userID
		}
		if stat.RangeTotalTokens == nil {
			value := tokens
			stat.RangeTotalTokens = &value
		}
		if stat.RangeActualCost == nil {
			value := actualCost
			stat.RangeActualCost = &value
		}
		stats[userID] = stat
	}
}

func usageTokens(stat relay.TeamUserUsageStats) int64 {
	if stat.RangeTotalTokens != nil {
		return *stat.RangeTotalTokens
	}
	if stat.TotalTokens != nil {
		return *stat.TotalTokens
	}
	return 0
}

func usageCost(stat relay.TeamUserUsageStats) float64 {
	if stat.RangeActualCost != nil {
		return *stat.RangeActualCost
	}
	return stat.TotalActualCost
}

func (s *Service) departmentName(ctx context.Context, externalID string) (string, error) {
	snapshot, found, err := directorysync.CurrentSnapshot(ctx, s.client)
	if err != nil || !found {
		return "", err
	}
	row, err := s.client.DirectoryDepartment.Query().Where(
		directorydepartment.SourceIDEQ(snapshot.SourceID),
		directorydepartment.ExternalIDEQ(strings.TrimSpace(externalID)),
	).Only(ctx)
	if err != nil {
		return "", err
	}
	return row.Name, nil
}

func (s *Service) saveMapping(ctx context.Context, plan *Plan, groupIDs []int64, state map[string]map[string]string) (*Mapping, error) {
	memberAssignments := make(map[string]int64)
	memberSources := make(map[string]int64)
	for _, assignment := range plan.Assignments {
		if assignment.Index >= len(groupIDs) || groupIDs[assignment.Index] <= 0 {
			continue
		}
		for _, userID := range assignment.UserIDs {
			key := strconv.Itoa(userID)
			memberAssignments[key] = groupIDs[assignment.Index]
			if candidate := candidateByUserID(plan.Candidates, userID); candidate != nil && candidate.SourceMember {
				memberSources[key] = plan.SourceGroupID
			}
		}
	}
	row, err := s.client.RelayGroupMapping.Query().Where(
		relaygroupmapping.ProviderIDEQ(plan.ProviderID),
		relaygroupmapping.DepartmentExternalIDEQ(plan.DepartmentID),
		relaygroupmapping.PlatformEQ(plan.Platform),
	).Only(ctx)
	if ent.IsNotFound(err) {
		row, err = s.client.RelayGroupMapping.Create().SetProviderID(plan.ProviderID).SetDepartmentExternalID(plan.DepartmentID).SetDepartmentName(plan.DepartmentName).SetPlatform(plan.Platform).SetTemplateGroupID(plan.TemplateGroupID).SetTemplateGroupName(plan.TemplateGroupName).SetSourceGroupID(plan.SourceGroupID).SetSourceGroupName(plan.SourceGroupName).SetGroupIds(groupIDs).SetMemberAssignments(memberAssignments).SetMemberSources(memberSources).SetOperationState(cloneOperationState(state)).SetWeeklyCostTarget(plan.WeeklyCostTarget).SetStatus(operationStatus(state)).Save(ctx)
	} else if err == nil {
		mergedState := mergeOperationState(row.OperationState, state)
		update := row.Update().SetDepartmentName(plan.DepartmentName).SetTemplateGroupID(plan.TemplateGroupID).SetTemplateGroupName(plan.TemplateGroupName).SetSourceGroupID(plan.SourceGroupID).SetSourceGroupName(plan.SourceGroupName).SetGroupIds(groupIDs).SetMemberAssignments(memberAssignments).SetMemberSources(memberSources).SetOperationState(mergedState).SetWeeklyCostTarget(plan.WeeklyCostTarget).SetStatus(operationStatus(mergedState))
		row, err = update.Save(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("persist relay group mapping: %w", err)
	}
	mapping := mappingFromEnt(row)
	return &mapping, nil
}

func mappingFromEnt(row *ent.RelayGroupMapping) Mapping {
	return Mapping{ID: row.ID, ProviderID: row.ProviderID, DepartmentID: row.DepartmentExternalID, DepartmentName: row.DepartmentName, Platform: row.Platform, TemplateGroupID: row.TemplateGroupID, TemplateGroupName: row.TemplateGroupName, SourceGroupID: row.SourceGroupID, SourceGroupName: row.SourceGroupName, GroupIDs: append([]int64(nil), row.GroupIds...), Status: row.Status, WeeklyCostTarget: row.WeeklyCostTarget, MemberAssignments: cloneInt64Map(row.MemberAssignments), MemberSources: cloneInt64Map(row.MemberSources), OperationState: cloneOperationState(row.OperationState), UpdatedAt: row.UpdatedAt}
}

func mappingAvailabilityWarnings(mapping Mapping, groups []relay.Group) []string {
	if groups == nil {
		return nil
	}
	available := make(map[int64]struct{}, len(groups))
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(mapping.Platform)) {
			available[group.ID] = struct{}{}
		}
	}
	warnings := make([]string, 0)
	if mapping.TemplateGroupID > 0 {
		if _, ok := available[mapping.TemplateGroupID]; !ok {
			warnings = append(warnings, fmt.Sprintf("template group %d is unavailable", mapping.TemplateGroupID))
		}
	}
	if mapping.SourceGroupID > 0 {
		if _, ok := available[mapping.SourceGroupID]; !ok {
			warnings = append(warnings, fmt.Sprintf("migration source group %d is unavailable", mapping.SourceGroupID))
		}
	}
	for _, groupID := range mapping.GroupIDs {
		if groupID > 0 {
			if _, ok := available[groupID]; !ok {
				warnings = append(warnings, fmt.Sprintf("target group %d is unavailable", groupID))
			}
		}
	}
	return uniqueStrings(warnings)
}

func mappingRelationshipWarnings(ctx context.Context, client *ent.Client, provider relay.Provider, mapping Mapping) []string {
	if provider == nil || client == nil {
		return nil
	}
	subsLister, ok := provider.(relay.UserSubscriptionLister)
	directory, directoryOK := provider.(relay.UserDirectoryProvider)
	if !ok || !directoryOK {
		return nil
	}
	users, err := directory.ListUsers(ctx)
	if err != nil {
		return nil
	}
	if len(users) == 0 {
		return nil
	}
	localUsers, err := client.User.Query().Where(user.RelayUserIDNotNil()).All(ctx)
	if err != nil {
		return nil
	}
	localByRelay := make(map[int64]int, len(localUsers))
	for _, localUser := range localUsers {
		if localUser.RelayUserID != nil && *localUser.RelayUserID > 0 {
			localByRelay[int64(*localUser.RelayUserID)] = localUser.ID
		}
	}
	activeGroups := make(map[int64]struct{}, len(mapping.GroupIDs))
	for _, groupID := range mapping.GroupIDs {
		activeGroups[groupID] = struct{}{}
	}
	type membershipResult struct {
		relayUserID int64
		groups      []int64
	}
	results := make([]membershipResult, len(users))
	jobs := make(chan int)
	workerCount := maxCandidateWorkers
	if len(users) < workerCount {
		workerCount = len(users)
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for index := range jobs {
				relayUser := users[index]
				subscriptions, listErr := subsLister.ListUserSubscriptions(ctx, relayUser.ID)
				if listErr != nil {
					continue
				}
				groups := make([]int64, 0)
				for _, subscription := range subscriptions {
					if strings.EqualFold(strings.TrimSpace(subscription.Status), "active") {
						groupID := subscription.GroupID
						if groupID <= 0 && subscription.Group != nil {
							groupID = subscription.Group.ID
						}
						if _, managed := activeGroups[groupID]; managed {
							groups = append(groups, groupID)
						}
					}
				}
				results[index] = membershipResult{relayUserID: relayUser.ID, groups: groups}
			}
		}()
	}
	for index := range users {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	expected := make(map[int]int64, len(mapping.MemberAssignments))
	for rawUserID, groupID := range mapping.MemberAssignments {
		if userID, parseErr := strconv.Atoi(rawUserID); parseErr == nil {
			expected[userID] = groupID
		}
	}
	actual := make(map[int]map[int64]struct{})
	warnings := make([]string, 0)
	for _, result := range results {
		if len(result.groups) == 0 {
			continue
		}
		localID, known := localByRelay[result.relayUserID]
		if !known {
			for _, groupID := range result.groups {
				if relayGroupAdopted(mapping.OperationState, result.relayUserID, groupID) {
					continue
				}
				warnings = append(warnings, fmt.Sprintf("unmanaged relay member %d in target group %d", result.relayUserID, groupID))
			}
			continue
		}
		actual[localID] = make(map[int64]struct{})
		for _, groupID := range result.groups {
			actual[localID][groupID] = struct{}{}
			if expectedGroup, expectedOK := expected[localID]; !expectedOK {
				warnings = append(warnings, fmt.Sprintf("unmanaged member %d in target group %d", localID, groupID))
			} else if expectedGroup != groupID {
				warnings = append(warnings, fmt.Sprintf("member %d is subscribed to target group %d instead of %d", localID, groupID, expectedGroup))
			}
		}
	}
	for userID, groupID := range expected {
		if _, exists := actual[userID][groupID]; !exists {
			warnings = append(warnings, fmt.Sprintf("mapping member %d is missing from target group %d", userID, groupID))
		}
	}
	return uniqueStrings(warnings)
}

func (s *Service) loadUnmanagedMembers(ctx context.Context, provider relay.Provider, mapping *ent.RelayGroupMapping) ([]UnmanagedMember, error) {
	directory, directoryOK := provider.(relay.UserDirectoryProvider)
	subsLister, subsOK := provider.(relay.UserSubscriptionLister)
	if !directoryOK || !subsOK {
		return nil, nil
	}
	remoteUsers, err := directory.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list relay users: %w", err)
	}
	localUsers, err := s.client.User.Query().Where(user.RelayUserIDNotNil()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load local relay mappings: %w", err)
	}
	localByRelay := make(map[int64]struct{}, len(localUsers))
	for _, localUser := range localUsers {
		if localUser.RelayUserID != nil && *localUser.RelayUserID > 0 {
			localByRelay[int64(*localUser.RelayUserID)] = struct{}{}
		}
	}
	managedTargets := make(map[int64]struct{}, len(mapping.GroupIds))
	for _, groupID := range mapping.GroupIds {
		if groupID > 0 {
			managedTargets[groupID] = struct{}{}
		}
	}
	out := make([]UnmanagedMember, 0)
	relayIDs := make([]int64, 0)
	for _, remoteUser := range remoteUsers {
		if remoteUser.ID <= 0 {
			continue
		}
		if _, managed := localByRelay[remoteUser.ID]; managed {
			continue
		}
		subscriptions, listErr := subsLister.ListUserSubscriptions(ctx, remoteUser.ID)
		if listErr != nil {
			return nil, fmt.Errorf("list subscriptions for relay user %d: %w", remoteUser.ID, listErr)
		}
		targetIDs := make([]int64, 0)
		seen := make(map[int64]struct{})
		for _, subscription := range subscriptions {
			if !strings.EqualFold(strings.TrimSpace(subscription.Status), "active") {
				continue
			}
			groupID := subscription.GroupID
			if groupID <= 0 && subscription.Group != nil {
				groupID = subscription.Group.ID
			}
			if _, managed := managedTargets[groupID]; !managed {
				continue
			}
			if _, exists := seen[groupID]; exists {
				continue
			}
			seen[groupID] = struct{}{}
			targetIDs = append(targetIDs, groupID)
		}
		adoptedTargetIDs := targetIDs[:0]
		for _, groupID := range targetIDs {
			if !relayGroupAdopted(mapping.OperationState, remoteUser.ID, groupID) {
				adoptedTargetIDs = append(adoptedTargetIDs, groupID)
			}
		}
		targetIDs = adoptedTargetIDs
		if len(targetIDs) == 0 {
			continue
		}
		sort.Slice(targetIDs, func(i, j int) bool { return targetIDs[i] < targetIDs[j] })
		out = append(out, UnmanagedMember{RelayUserID: remoteUser.ID, Username: remoteUser.Username, Email: remoteUser.Email, TargetGroupIDs: targetIDs})
		relayIDs = append(relayIDs, remoteUser.ID)
	}
	stats, statsErr := usageStats(ctx, provider, relayIDs)
	if statsErr != nil {
		return nil, fmt.Errorf("load unmanaged member usage: %w", statsErr)
	}
	for index := range out {
		out[index].RangeCost = usageCost(stats[out[index].RelayUserID])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelayUserID < out[j].RelayUserID })
	return out, nil
}

func cloneInt64Map(input map[string]int64) map[string]int64 {
	if len(input) == 0 {
		return map[string]int64{}
	}
	out := make(map[string]int64, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneOperationState(input map[string]map[string]string) map[string]map[string]string {
	if len(input) == 0 {
		return map[string]map[string]string{}
	}
	out := make(map[string]map[string]string, len(input))
	for key, values := range input {
		out[key] = make(map[string]string, len(values))
		for field, value := range values {
			out[key][field] = value
		}
	}
	return out
}

func operationStatus(state map[string]map[string]string) string {
	if operation := state["operation"]; operation != nil && operation["status"] == "needs_retry" {
		return "needs_retry"
	}
	for key, entry := range state {
		if key == "operation" {
			continue
		}
		if entry["error"] != "" || entry["status"] == "failed" || entry["subscription"] == "failed" || entry["source_removal"] == "failed" {
			return "needs_retry"
		}
	}
	return "active"
}

func mergeOperationState(previous, current map[string]map[string]string) map[string]map[string]string {
	merged := cloneOperationState(previous)
	for key, values := range current {
		merged[key] = make(map[string]string, len(values))
		for field, value := range values {
			merged[key][field] = value
		}
	}
	if operation := merged["operation"]; operation != nil {
		operation["status"] = operationStatus(merged)
	}
	return merged
}

func operationStateNeedsRetry(state map[string]map[string]string, key string) bool {
	entry := state[key]
	if entry == nil {
		return false
	}
	return entry["error"] != "" || entry["status"] == "failed" || entry["subscription"] == "failed" || entry["source_removal"] == "failed" || strings.Contains(entry["api_keys"], ":failed:")
}

func relayGroupAdopted(state map[string]map[string]string, relayUserID, groupID int64) bool {
	entry := state[fmt.Sprintf("relay:%d:%d", relayUserID, groupID)]
	return entry != nil && entry["status"] == "adopted"
}

func allocate(candidates []Candidate, count int) []Assignment {
	if count < 1 {
		return make([]Assignment, 0)
	}
	assignments := make([]Assignment, count)
	for i := range assignments {
		assignments[i].Index = i
		assignments[i].UserIDs = make([]int, 0)
	}
	for _, candidate := range candidates {
		best := 0
		for i := 1; i < len(assignments); i++ {
			if assignments[i].TotalCost < assignments[best].TotalCost {
				best = i
			}
		}
		assignments[best].TotalCost += candidate.RangeCost
		assignments[best].UserIDs = append(assignments[best].UserIDs, candidate.UserID)
	}
	return assignments
}

func resolveGroupCount(req PreviewRequest, candidates []Candidate) (int, int) {
	if len(candidates) == 0 {
		if req.ExistingMappingID > 0 && req.GroupCount > 0 {
			return 0, req.GroupCount
		}
		return 0, 0
	}
	total := 0.0
	for _, candidate := range candidates {
		total += candidate.RangeCost
	}
	recommended := 1
	if req.WeeklyCostTarget > 0 && total > 0 {
		recommended = int(math.Ceil(total / req.WeeklyCostTarget))
	}
	if recommended > len(candidates) {
		recommended = len(candidates)
	}
	count := recommended
	if req.ExistingMappingID > 0 && req.GroupCount > 0 {
		count = req.GroupCount
	}
	return recommended, count
}

func (s *Service) assignTargets(ctx context.Context, req PreviewRequest, groups []relay.Group, sourceName string, assignments []Assignment) error {
	existingIDs := make([]int64, 0)
	if req.ExistingMappingID > 0 {
		mapping, err := s.client.RelayGroupMapping.Get(ctx, req.ExistingMappingID)
		if err != nil {
			return fmt.Errorf("load relay group mapping targets: %w", err)
		}
		existingIDs = mapping.GroupIds
	}
	groupByID := make(map[int64]relay.Group, len(groups))
	for _, group := range groups {
		groupByID[group.ID] = group
	}
	existingCount := len(existingIDs)
	if existingCount > len(assignments) {
		existingCount = len(assignments)
	}
	proposedNames := proposedGroupNames(sourceName, groups, len(assignments)-existingCount)
	for i := range assignments {
		if i < existingCount {
			assignments[i].TargetGroupID = existingIDs[i]
			assignments[i].TargetGroupName = groupByID[existingIDs[i]].Name
			continue
		}
		assignments[i].TargetGroupName = proposedNames[i-existingCount]
	}
	return nil
}

func proposedGroupNames(sourceName string, groups []relay.Group, count int) []string {
	used := make(map[string]struct{}, len(groups)+count)
	for _, group := range groups {
		used[group.Name] = struct{}{}
	}
	names := make([]string, 0, count)
	copyNumber := 1
	for len(names) < count {
		for {
			name := proposedGroupName(sourceName, copyNumber)
			copyNumber++
			if _, exists := used[name]; exists {
				continue
			}
			used[name] = struct{}{}
			names = append(names, name)
			break
		}
	}
	return names
}

func proposedGroupName(sourceName string, copyNumber int) string {
	suffix := " (Copy)"
	if copyNumber > 1 {
		suffix = fmt.Sprintf(" (Copy %d)", copyNumber)
	}
	base := []rune(strings.TrimSpace(sourceName))
	maxBase := maxGroupNameRunes - len([]rune(suffix))
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	return string(base) + suffix
}

func candidateByUserID(candidates []Candidate, id int) *Candidate {
	for i := range candidates {
		if candidates[i].UserID == id {
			return &candidates[i]
		}
	}
	return nil
}

func selectedSet(ids []int) map[int]struct{} {
	set := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			set[id] = struct{}{}
		}
	}
	return set
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
