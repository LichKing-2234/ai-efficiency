package relayplanning

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
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
)

const (
	maxPlanningUsers    = 5000
	defaultValidityDays = 30
)

type ProviderResolver interface {
	Resolve(context.Context, int) (relay.Provider, error)
}

type subscriptionAssigner interface {
	AssignSubscriptionForUser(context.Context, int64, int64, int) error
}

type subscriptionRemover interface {
	RemoveSubscriptionForUser(context.Context, int64, int64) error
}

type Service struct {
	client   *ent.Client
	resolver ProviderResolver
	users    *adminusers.Service
}

func NewService(client *ent.Client, resolver ProviderResolver) *Service {
	return &Service{client: client, resolver: resolver, users: adminusers.NewService(client)}
}

type PreviewRequest struct {
	ProviderID        int     `json:"provider_id"`
	DepartmentID      string  `json:"department_id"`
	Platform          string  `json:"platform"`
	SourceGroupID     int64   `json:"source_group_id"`
	WeeklyCostTarget  float64 `json:"weekly_cost_target"`
	GroupCount        int     `json:"group_count"`
	SelectedUserIDs   []int   `json:"selected_user_ids"`
	ExistingMappingID int     `json:"existing_mapping_id"`
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
	Selected           bool     `json:"selected"`
	Eligible           bool     `json:"eligible"`
	Warnings           []string `json:"warnings,omitempty"`
}

type Assignment struct {
	Index         int     `json:"index"`
	TotalCost     float64 `json:"total_cost"`
	UserIDs       []int   `json:"user_ids"`
	TargetGroupID int64   `json:"target_group_id,omitempty"`
}

type Plan struct {
	ProviderID       int          `json:"provider_id"`
	DepartmentID     string       `json:"department_id"`
	DepartmentName   string       `json:"department_name"`
	Platform         string       `json:"platform"`
	SourceGroupID    int64        `json:"source_group_id"`
	SourceGroupName  string       `json:"source_group_name"`
	WeeklyCostTarget float64      `json:"weekly_cost_target"`
	RecommendedCount int          `json:"recommended_group_count"`
	GroupCount       int          `json:"group_count"`
	Candidates       []Candidate  `json:"candidates"`
	Assignments      []Assignment `json:"assignments"`
	Warnings         []string     `json:"warnings,omitempty"`
	GeneratedAt      time.Time    `json:"generated_at"`
	MappingID        int          `json:"mapping_id,omitempty"`
}

type Mapping struct {
	ID               int       `json:"id"`
	ProviderID       int       `json:"provider_id"`
	DepartmentID     string    `json:"department_id"`
	DepartmentName   string    `json:"department_name"`
	Platform         string    `json:"platform"`
	SourceGroupID    int64     `json:"source_group_id"`
	SourceGroupName  string    `json:"source_group_name"`
	GroupIDs         []int64   `json:"group_ids"`
	Status           string    `json:"status"`
	WeeklyCostTarget float64   `json:"weekly_cost_target"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ExecuteRequest struct {
	PreviewRequest
	OperationKey string `json:"operation_key"`
}

type GroupResult struct {
	Index  int    `json:"index"`
	ID     int64  `json:"id,omitempty"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type MemberResult struct {
	UserID        int      `json:"user_id"`
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
	if err := validateRequest(req); err != nil {
		return nil, err
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
	source, err := findSourceGroup(groups, req.SourceGroupID, req.Platform)
	if err != nil {
		return nil, err
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
	candidates, err := s.buildCandidates(ctx, p, users, source, req.Platform, req.DepartmentID)
	if err != nil {
		return nil, err
	}
	eligible := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Eligible && (len(selected) == 0 || candidate.Selected) {
			eligible = append(eligible, candidate)
		}
	}
	count := req.GroupCount
	total := 0.0
	for _, candidate := range eligible {
		total += candidate.RangeCost
	}
	recommended := 1
	if req.WeeklyCostTarget > 0 && total > 0 {
		recommended = int(math.Ceil(total / req.WeeklyCostTarget))
		if recommended < 1 {
			recommended = 1
		}
	}
	if count <= 0 {
		count = recommended
	}
	if count < 1 {
		count = 1
	}
	assignments := allocate(eligible, count)
	warnings := make([]string, 0)
	if len(eligible) == 0 {
		warnings = append(warnings, "no eligible member has a valid relay mapping and source-group membership")
	}
	if req.WeeklyCostTarget > 0 {
		for _, assignment := range assignments {
			if assignment.TotalCost > req.WeeklyCostTarget {
				warnings = append(warnings, fmt.Sprintf("group %d exceeds the planning target", assignment.Index+1))
			}
		}
	}
	for _, candidate := range candidates {
		warnings = append(warnings, candidate.Warnings...)
	}
	plan := &Plan{
		ProviderID: req.ProviderID, DepartmentID: req.DepartmentID, Platform: req.Platform,
		SourceGroupID: source.ID, SourceGroupName: source.Name, WeeklyCostTarget: req.WeeklyCostTarget,
		RecommendedCount: recommended, GroupCount: count, Candidates: candidates, Assignments: assignments,
		Warnings: uniqueStrings(warnings), GeneratedAt: time.Now().UTC(),
	}
	if department, err := s.departmentName(ctx, req.DepartmentID); err == nil {
		plan.DepartmentName = department
	}
	if req.ExistingMappingID > 0 {
		plan.MappingID = req.ExistingMappingID
	}
	return plan, nil
}

func (s *Service) Execute(ctx context.Context, req ExecuteRequest) (*ExecutionResult, error) {
	if strings.TrimSpace(req.OperationKey) == "" {
		return nil, fmt.Errorf("operation_key is required")
	}
	plan, err := s.Preview(ctx, req.PreviewRequest)
	if err != nil {
		return nil, err
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
	targetIDs := make([]int64, 0, plan.GroupCount)
	for index := 0; index < plan.GroupCount; index++ {
		group, duplicateErr := duplicator.DuplicateGroup(ctx, plan.SourceGroupID, fmt.Sprintf("%s-%d", req.OperationKey, index))
		result := GroupResult{Index: index, Status: "failed"}
		if duplicateErr != nil {
			result.Error = duplicateErr.Error()
			groupResults = append(groupResults, result)
			continue
		}
		result.ID = group.ID
		if statusUpdater != nil {
			if activateErr := statusUpdater.UpdateGroupStatus(ctx, group.ID, "active"); activateErr != nil {
				result.Error = activateErr.Error()
				groupResults = append(groupResults, result)
				continue
			}
		}
		result.Status = "succeeded"
		targetIDs = append(targetIDs, group.ID)
		groupResults = append(groupResults, result)
	}
	assigner, _ := p.(subscriptionAssigner)
	remover, _ := p.(subscriptionRemover)
	binder, _ := p.(relay.APIKeyGroupBinder)
	memberResults := make([]MemberResult, 0, len(plan.Candidates))
	for _, assignment := range plan.Assignments {
		if assignment.Index >= len(targetIDs) {
			continue
		}
		for _, userID := range assignment.UserIDs {
			member := MemberResult{UserID: userID, TargetGroupID: targetIDs[assignment.Index], Subscription: "skipped", SourceRemoval: "skipped"}
			candidate := candidateByUserID(plan.Candidates, userID)
			if candidate == nil {
				member.Error = "candidate disappeared from plan"
				memberResults = append(memberResults, member)
				continue
			}
			if assigner == nil {
				member.Error = "relay provider does not support subscription assignment"
				memberResults = append(memberResults, member)
				continue
			}
			if assignErr := assigner.AssignSubscriptionForUser(ctx, candidate.RelayUserID, member.TargetGroupID, defaultValidityDays); assignErr != nil {
				member.Error = assignErr.Error()
				memberResults = append(memberResults, member)
				continue
			}
			member.Subscription = "succeeded"
			keys, keyErr := p.ListUserAPIKeys(ctx, candidate.RelayUserID)
			if keyErr != nil {
				member.Error = keyErr.Error()
				memberResults = append(memberResults, member)
				continue
			}
			for _, key := range keys {
				if apiKeyGroupID(key) != plan.SourceGroupID || binder == nil {
					continue
				}
				if bindErr := binder.BindAPIKeyToGroup(ctx, key.ID, member.TargetGroupID); bindErr != nil {
					member.APIKeys = append(member.APIKeys, fmt.Sprintf("%d:failed:%s", key.ID, bindErr))
					continue
				}
				member.APIKeys = append(member.APIKeys, fmt.Sprintf("%d:succeeded", key.ID))
			}
			if remover != nil {
				if removeErr := remover.RemoveSubscriptionForUser(ctx, candidate.RelayUserID, plan.SourceGroupID); removeErr != nil {
					member.SourceRemoval = "failed"
					member.Error = removeErr.Error()
				} else {
					member.SourceRemoval = "succeeded"
				}
			}
			memberResults = append(memberResults, member)
		}
	}
	var mapping *Mapping
	if len(targetIDs) > 0 {
		mapping, err = s.saveMapping(ctx, plan, targetIDs)
		if err != nil {
			return nil, fmt.Errorf("save group mapping: %w", err)
		}
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
	for _, row := range rows {
		out = append(out, mappingFromEnt(row))
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
	return &mapping, nil
}

func (s *Service) Rebind(ctx context.Context, id int, sourceGroupID int64, groupIDs []int64, status string) (*Mapping, error) {
	if id <= 0 || sourceGroupID <= 0 {
		return nil, fmt.Errorf("mapping id and source_group_id are required")
	}
	if status == "" {
		status = "active"
	}
	row, err := s.client.RelayGroupMapping.UpdateOneID(id).
		SetSourceGroupID(sourceGroupID).
		SetGroupIds(append([]int64(nil), groupIDs...)).
		SetStatus(status).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("rebind relay group mapping: %w", err)
	}
	mapping := mappingFromEnt(row)
	return &mapping, nil
}

func (s *Service) Replan(ctx context.Context, mappingID int, selected []int, groupCount int) (*Plan, error) {
	row, err := s.client.RelayGroupMapping.Get(ctx, mappingID)
	if err != nil {
		return nil, fmt.Errorf("load relay group mapping: %w", err)
	}
	return s.Preview(ctx, PreviewRequest{ProviderID: row.ProviderID, DepartmentID: row.DepartmentExternalID, Platform: row.Platform, SourceGroupID: row.SourceGroupID, WeeklyCostTarget: row.WeeklyCostTarget, GroupCount: groupCount, SelectedUserIDs: selected, ExistingMappingID: mappingID})
}

// ExecuteReplan applies a previously persisted mapping's explicit replan. New
// groups are created only when requested; shrinking first restores members to
// the source group and then marks surplus groups inactive.
func (s *Service) ExecuteReplan(ctx context.Context, mappingID int, req ExecuteRequest) (*ExecutionResult, error) {
	mapping, err := s.GetMapping(ctx, mappingID)
	if err != nil {
		return nil, err
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
	if req.WeeklyCostTarget == 0 {
		req.WeeklyCostTarget = mapping.WeeklyCostTarget
	}
	plan, err := s.Preview(ctx, req.PreviewRequest)
	if err != nil {
		return nil, err
	}
	p, err := s.resolver.Resolve(ctx, mapping.ProviderID)
	if err != nil {
		return nil, err
	}
	targetIDs := append([]int64(nil), mapping.GroupIDs...)
	groupResults := make([]GroupResult, 0, plan.GroupCount)
	duplicator, _ := p.(relay.GroupDuplicator)
	statusUpdater, _ := p.(relay.GroupStatusUpdater)
	for len(targetIDs) < plan.GroupCount {
		result := GroupResult{Index: len(targetIDs), Status: "failed"}
		if duplicator == nil {
			result.Error = "relay provider does not support group duplication"
			groupResults = append(groupResults, result)
			break
		}
		group, duplicateErr := duplicator.DuplicateGroup(ctx, mapping.SourceGroupID, fmt.Sprintf("%s-%d", req.OperationKey, len(targetIDs)))
		if duplicateErr != nil {
			result.Error = duplicateErr.Error()
			groupResults = append(groupResults, result)
			break
		}
		result.ID = group.ID
		if statusUpdater != nil {
			if activateErr := statusUpdater.UpdateGroupStatus(ctx, group.ID, "active"); activateErr != nil {
				result.Error = activateErr.Error()
				groupResults = append(groupResults, result)
				break
			}
		}
		result.Status = "succeeded"
		targetIDs = append(targetIDs, group.ID)
		groupResults = append(groupResults, result)
	}
	if len(targetIDs) > plan.GroupCount {
		for index := plan.GroupCount; index < len(targetIDs); index++ {
			result := GroupResult{Index: index, ID: targetIDs[index], Status: "inactive"}
			if statusUpdater != nil {
				if deactivateErr := statusUpdater.UpdateGroupStatus(ctx, targetIDs[index], "inactive"); deactivateErr != nil {
					result.Status = "failed"
					result.Error = deactivateErr.Error()
				}
			}
			groupResults = append(groupResults, result)
		}
		targetIDs = targetIDs[:plan.GroupCount]
	}
	if len(targetIDs) < plan.GroupCount {
		plan.GroupCount = len(targetIDs)
		plan.Assignments = allocate(eligibleCandidates(plan), plan.GroupCount)
	}
	assigner, _ := p.(subscriptionAssigner)
	remover, _ := p.(subscriptionRemover)
	binder, _ := p.(relay.APIKeyGroupBinder)
	subsLister, _ := p.(relay.UserSubscriptionLister)
	memberResults := make([]MemberResult, 0, len(plan.Candidates))
	selected := selectedSet(req.SelectedUserIDs)
	users, _ := s.users.Targets(ctx, adminusers.Filters{DepartmentID: mapping.DepartmentID}, maxPlanningUsers)
	for _, u := range users {
		if u.RelayUserID == nil || *u.RelayUserID <= 0 {
			continue
		}
		if subsLister == nil {
			continue
		}
		current, listErr := subsLister.ListUserSubscriptions(ctx, int64(*u.RelayUserID))
		if listErr != nil {
			continue
		}
		currentManaged := make(map[int64]struct{})
		for _, subscription := range current {
			if containsInt64(mapping.GroupIDs, subscription.GroupID) {
				currentManaged[subscription.GroupID] = struct{}{}
			}
		}
		if len(currentManaged) == 0 {
			continue
		}
		if len(selected) > 0 {
			if _, keep := selected[u.ID]; !keep {
				for groupID := range currentManaged {
					if remover != nil {
						_ = remover.RemoveSubscriptionForUser(ctx, int64(*u.RelayUserID), groupID)
					}
				}
				if assigner != nil {
					_ = assigner.AssignSubscriptionForUser(ctx, int64(*u.RelayUserID), mapping.SourceGroupID, defaultValidityDays)
				}
				memberResults = append(memberResults, MemberResult{UserID: u.ID, Subscription: "restored_source", SourceRemoval: "succeeded"})
			}
		}
	}
	for _, assignment := range plan.Assignments {
		if assignment.Index >= len(targetIDs) {
			continue
		}
		for _, userID := range assignment.UserIDs {
			candidate := candidateByUserID(plan.Candidates, userID)
			if candidate == nil || assigner == nil {
				continue
			}
			member := MemberResult{UserID: userID, TargetGroupID: targetIDs[assignment.Index], Subscription: "failed", SourceRemoval: "skipped"}
			if assignErr := assigner.AssignSubscriptionForUser(ctx, candidate.RelayUserID, member.TargetGroupID, defaultValidityDays); assignErr != nil && !strings.Contains(strings.ToLower(assignErr.Error()), "already") {
				member.Error = assignErr.Error()
				memberResults = append(memberResults, member)
				continue
			}
			member.Subscription = "succeeded"
			keys, keyErr := p.ListUserAPIKeys(ctx, candidate.RelayUserID)
			if keyErr == nil && binder != nil {
				for _, key := range keys {
					if apiKeyGroupID(key) == mapping.SourceGroupID {
						if bindErr := binder.BindAPIKeyToGroup(ctx, key.ID, member.TargetGroupID); bindErr != nil {
							member.APIKeys = append(member.APIKeys, fmt.Sprintf("%d:failed:%s", key.ID, bindErr))
						} else {
							member.APIKeys = append(member.APIKeys, fmt.Sprintf("%d:succeeded", key.ID))
						}
					}
				}
			}
			if remover != nil {
				if removeErr := remover.RemoveSubscriptionForUser(ctx, candidate.RelayUserID, mapping.SourceGroupID); removeErr != nil && !strings.Contains(strings.ToLower(removeErr.Error()), "not found") {
					member.Error = removeErr.Error()
				} else {
					member.SourceRemoval = "succeeded"
				}
			}
			memberResults = append(memberResults, member)
		}
	}
	plan.GroupCount = len(targetIDs)
	resultMapping, err := s.saveMapping(ctx, plan, targetIDs)
	if err != nil {
		return nil, err
	}
	return &ExecutionResult{Plan: plan, Groups: groupResults, Members: memberResults, Mapping: resultMapping}, nil
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
	if req.ProviderID <= 0 || strings.TrimSpace(req.DepartmentID) == "" || strings.TrimSpace(req.Platform) == "" || req.SourceGroupID <= 0 {
		return fmt.Errorf("provider_id, department_id, platform, and source_group_id are required")
	}
	if req.WeeklyCostTarget < 0 || math.IsNaN(req.WeeklyCostTarget) || math.IsInf(req.WeeklyCostTarget, 0) {
		return fmt.Errorf("weekly_cost_target must be a finite non-negative number")
	}
	return nil
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
	return relay.Group{}, fmt.Errorf("source group %d is unavailable", id)
}

func (s *Service) buildCandidates(ctx context.Context, p relay.Provider, users []*ent.User, source relay.Group, platform, departmentID string) ([]Candidate, error) {
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
	stats, err := usageStats(ctx, p, ids)
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
	out := make([]Candidate, 0, len(users))
	for _, u := range users {
		candidate := Candidate{UserID: u.ID, Username: u.Username, Email: u.Email, Eligible: false, Selected: true}
		if u.RelayUserID == nil || *u.RelayUserID <= 0 {
			candidate.Warnings = append(candidate.Warnings, fmt.Sprintf("user %d has no relay mapping", u.ID))
			out = append(out, candidate)
			continue
		}
		candidate.RelayUserID = int64(*u.RelayUserID)
		stat := globalStats[candidate.RelayUserID]
		candidate.RangeCost = usageCost(stat)
		candidate.RangeTokens = usageTokens(stat)
		candidate.GlobalTokenRank = ranks[candidate.RelayUserID]
		allowed, groupErr := p.ListAllowedGroupsForUser(ctx, candidate.RelayUserID)
		if groupErr != nil {
			candidate.Warnings = append(candidate.Warnings, fmt.Sprintf("relay groups unavailable: %v", groupErr))
			out = append(out, candidate)
			continue
		}
		for _, group := range allowed {
			if group.ID == source.ID {
				candidate.Eligible = strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(platform))
			}
			if group.ID != source.ID && group.ID > 0 {
				candidate.CurrentGroupIDs = append(candidate.CurrentGroupIDs, group.ID)
			}
		}
		keys, keyErr := p.ListUserAPIKeys(ctx, candidate.RelayUserID)
		if keyErr == nil {
			for _, key := range keys {
				if apiKeyGroupID(key) == source.ID {
					candidate.MigratableKeyCount++
				}
			}
		}
		if !candidate.Eligible {
			candidate.Warnings = append(candidate.Warnings, "user is not a member of the selected source group")
		} else if candidate.MigratableKeyCount == 0 {
			candidate.Warnings = append(candidate.Warnings, "no migratable AE-managed API key")
		}
		if conflict, conflictErr := s.hasDepartmentConflict(ctx, u, departmentID); conflictErr == nil && conflict {
			candidate.Warnings = append(candidate.Warnings, "user belongs to multiple departments")
		}
		candidate.Selected = candidate.Eligible
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RangeCost > out[j].RangeCost || (out[i].RangeCost == out[j].RangeCost && out[i].UserID < out[j].UserID)
	})
	return out, nil
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

func usageStats(ctx context.Context, p relay.Provider, ids []int64) (map[int64]relay.TeamUserUsageStats, error) {
	result := make(map[int64]relay.TeamUserUsageStats, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	now := time.Now().UTC()
	params := relay.TeamUsageSummaryParams{StartDate: now.AddDate(0, 0, -30).Format("2006-01-02"), EndDate: now.Format("2006-01-02"), Granularity: "day", Timezone: "UTC"}
	if batch, ok := p.(relay.TeamUsageSummaryProvider); ok {
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

func (s *Service) saveMapping(ctx context.Context, plan *Plan, groupIDs []int64) (*Mapping, error) {
	row, err := s.client.RelayGroupMapping.Query().Where(
		relaygroupmapping.ProviderIDEQ(plan.ProviderID),
		relaygroupmapping.DepartmentExternalIDEQ(plan.DepartmentID),
		relaygroupmapping.PlatformEQ(plan.Platform),
	).Only(ctx)
	if ent.IsNotFound(err) {
		row, err = s.client.RelayGroupMapping.Create().SetProviderID(plan.ProviderID).SetDepartmentExternalID(plan.DepartmentID).SetDepartmentName(plan.DepartmentName).SetPlatform(plan.Platform).SetSourceGroupID(plan.SourceGroupID).SetSourceGroupName(plan.SourceGroupName).SetGroupIds(groupIDs).SetWeeklyCostTarget(plan.WeeklyCostTarget).Save(ctx)
	} else if err == nil {
		row, err = row.Update().SetDepartmentName(plan.DepartmentName).SetSourceGroupID(plan.SourceGroupID).SetSourceGroupName(plan.SourceGroupName).SetGroupIds(groupIDs).SetWeeklyCostTarget(plan.WeeklyCostTarget).SetStatus("active").Save(ctx)
	}
	if err != nil {
		return nil, err
	}
	mapping := mappingFromEnt(row)
	return &mapping, nil
}

func mappingFromEnt(row *ent.RelayGroupMapping) Mapping {
	return Mapping{ID: row.ID, ProviderID: row.ProviderID, DepartmentID: row.DepartmentExternalID, DepartmentName: row.DepartmentName, Platform: row.Platform, SourceGroupID: row.SourceGroupID, SourceGroupName: row.SourceGroupName, GroupIDs: append([]int64(nil), row.GroupIds...), Status: row.Status, WeeklyCostTarget: row.WeeklyCostTarget, UpdatedAt: row.UpdatedAt}
}

func allocate(candidates []Candidate, count int) []Assignment {
	if count < 1 {
		count = 1
	}
	assignments := make([]Assignment, count)
	for i := range assignments {
		assignments[i].Index = i
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
