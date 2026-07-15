package quotareset

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/quotaresetapprovalchain"
	"github.com/ai-efficiency/backend/ent/quotaresetapproverconfig"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/directorytree"
	"github.com/ai-efficiency/backend/internal/relay"
)

const (
	maxApprovalChains           = 100
	maxApprovalChainDepartments = 20
)

type platformGroupLister interface {
	ListPlatformGroups(context.Context) ([]relay.Group, error)
}

type workflowDirectoryFacts struct {
	sourceID              int
	tree                  *directorytree.Tree
	departmentsByID       map[string]*ent.DirectoryDepartment
	membersByExternalID   map[string]*ent.DirectoryMember
	membersByUserID       map[int]*ent.DirectoryMember
	usersByID             map[int]*ent.User
	departmentIDsByMember map[int]map[string]struct{}
	configUserIDsByDept   map[string][]int
	representativesByDept map[string]map[string]struct{}
}

func (s *Service) ListApprovalChains(ctx context.Context) (*ApprovalChainListResponse, error) {
	rows, err := s.client.QuotaResetApprovalChain.Query().
		Order(ent.Asc(quotaresetapprovalchain.FieldProviderID), ent.Asc(quotaresetapprovalchain.FieldGroupName)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list quota reset approval chains: %w", err)
	}
	items := make([]ApprovalChain, 0, len(rows))
	for _, row := range rows {
		departments, err := decodeChainDepartments(row.DepartmentChain)
		if err != nil {
			return nil, fmt.Errorf("decode quota reset approval chain %d: %w", row.ID, err)
		}
		items = append(items, ApprovalChain{
			ID:          row.ID,
			ProviderID:  row.ProviderID,
			GroupID:     row.GroupID,
			GroupName:   row.GroupName,
			Enabled:     row.Enabled,
			Departments: departments,
			UpdatedAt:   row.UpdatedAt,
		})
	}
	groups, err := s.approvalChainGroupOptions(ctx)
	if err != nil {
		return nil, err
	}
	return &ApprovalChainListResponse{Items: items, Groups: groups}, nil
}

func (s *Service) SaveApprovalChains(ctx context.Context, actorID int, inputs []ApprovalChainInput) (*ApprovalChainListResponse, error) {
	if len(inputs) > maxApprovalChains {
		return nil, fmt.Errorf("%w: at most %d approval chains are allowed", ErrInvalidApproverConfig, maxApprovalChains)
	}
	groups, err := s.approvalChainGroupOptions(ctx)
	if err != nil {
		return nil, err
	}
	groupByKey := make(map[string]ApprovalChainGroupOption, len(groups))
	for _, group := range groups {
		groupByKey[approvalChainKey(group.ProviderID, group.GroupID)] = group
	}
	sourceID, ok, err := directorysync.CurrentSourceID(ctx, s.client)
	if err != nil {
		return nil, fmt.Errorf("current directory source: %w", err)
	}
	if !ok && len(inputs) > 0 {
		return nil, ErrDirectoryUnavailable
	}
	departments, err := s.client.DirectoryDepartment.Query().Where(directorydepartment.SourceIDEQ(sourceID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list approval chain departments: %w", err)
	}
	departmentsByID := make(map[string]*ent.DirectoryDepartment, len(departments))
	for _, department := range departments {
		departmentsByID[department.ExternalID] = department
	}
	tree := directorytree.New(departments)
	normalized := make([]ApprovalChainInput, 0, len(inputs))
	seenChains := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		input.GroupID = strings.TrimSpace(input.GroupID)
		key := approvalChainKey(input.ProviderID, input.GroupID)
		group, exists := groupByKey[key]
		if !exists {
			return nil, fmt.Errorf("%w: subscription group %s is not available", ErrInvalidApproverConfig, key)
		}
		if _, duplicate := seenChains[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate subscription group %s", ErrInvalidApproverConfig, key)
		}
		seenChains[key] = struct{}{}
		if len(input.Departments) > maxApprovalChainDepartments {
			return nil, fmt.Errorf("%w: group %s exceeds %d departments", ErrInvalidApproverConfig, input.GroupID, maxApprovalChainDepartments)
		}
		seenDepartments := make(map[string]struct{}, len(input.Departments))
		cleanDepartments := make([]ChainDepartmentInput, 0, len(input.Departments))
		for _, item := range input.Departments {
			departmentID := strings.TrimSpace(item.DepartmentExternalID)
			department := departmentsByID[departmentID]
			if item.DirectorySourceID != sourceID || department == nil {
				return nil, fmt.Errorf("%w: department %s is not in the current directory", ErrInvalidApproverConfig, departmentID)
			}
			if _, duplicate := seenDepartments[departmentID]; duplicate {
				return nil, fmt.Errorf("%w: duplicate department %s", ErrInvalidApproverConfig, departmentID)
			}
			seenDepartments[departmentID] = struct{}{}
			cleanDepartments = append(cleanDepartments, ChainDepartmentInput{
				DirectorySourceID:     sourceID,
				DepartmentExternalID:  departmentID,
				DepartmentDisplayPath: workflowDepartmentPath(tree, department),
			})
		}
		input.GroupName = group.GroupName
		input.Departments = cleanDepartments
		normalized = append(normalized, input)
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("start approval chain replacement: %w", err)
	}
	if _, err := tx.QuotaResetApprovalChain.Delete().Exec(ctx); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("replace quota reset approval chains: %w", err)
	}
	for _, input := range normalized {
		raw, err := encodeChainDepartments(input.Departments)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if _, err := tx.QuotaResetApprovalChain.Create().
			SetProviderID(input.ProviderID).
			SetGroupID(input.GroupID).
			SetGroupName(input.GroupName).
			SetDepartmentChain(raw).
			SetEnabled(input.Enabled).
			SetCreatedByUserID(actorID).
			SetUpdatedByUserID(actorID).
			Save(ctx); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("create quota reset approval chain: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit quota reset approval chains: %w", err)
	}
	return s.ListApprovalChains(ctx)
}

func (s *Service) approvalChainGroupOptions(ctx context.Context) ([]ApprovalChainGroupOption, error) {
	rows, err := s.client.RelayProvider.Query().
		Where(relayprovider.Enabled(true)).
		Order(ent.Desc(relayprovider.FieldIsPrimary), ent.Asc(relayprovider.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list relay providers for approval chains: %w", err)
	}
	options := make([]ApprovalChainGroupOption, 0)
	for _, row := range rows {
		if s.providerResolver == nil {
			return nil, fmt.Errorf("resolve relay provider %d: provider resolver is not configured", row.ID)
		}
		provider, err := s.providerResolver.Resolve(ctx, row.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve relay provider %d: %w", row.ID, err)
		}
		lister, ok := provider.(platformGroupLister)
		if !ok {
			return nil, fmt.Errorf("relay provider %d does not support group listing", row.ID)
		}
		groups, err := lister.ListPlatformGroups(ctx)
		if err != nil {
			return nil, fmt.Errorf("list relay provider %d groups: %w", row.ID, err)
		}
		for _, group := range groups {
			if group.ID <= 0 || strings.TrimSpace(group.Platform) == "" {
				continue
			}
			if kind := strings.TrimSpace(group.SubscriptionType); kind != "" && !strings.EqualFold(kind, "subscription") {
				continue
			}
			groupID := strconv.FormatInt(group.ID, 10)
			groupName := strings.TrimSpace(group.Name)
			if groupName == "" {
				groupName = groupID
			}
			options = append(options, ApprovalChainGroupOption{
				ProviderID:   row.ID,
				ProviderName: row.DisplayName,
				GroupID:      groupID,
				GroupName:    groupName,
				Platform:     strings.TrimSpace(group.Platform),
			})
		}
	}
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].ProviderName != options[j].ProviderName {
			return options[i].ProviderName < options[j].ProviderName
		}
		if options[i].GroupName != options[j].GroupName {
			return options[i].GroupName < options[j].GroupName
		}
		return options[i].GroupID < options[j].GroupID
	})
	return options, nil
}

func (s *Service) resolveWorkflow(ctx context.Context, requester *ent.User, providerID int, groupID string) (*Workflow, []DepartmentPathEvidence, error) {
	workflow := &Workflow{
		Version: workflowVersionV2,
		Requester: WorkflowPerson{
			UserID:          requester.ID,
			DisplayName:     strings.TrimSpace(requester.Username),
			Email:           strings.TrimSpace(requester.Email),
			DepartmentPaths: []string{},
			NotificationIDs: map[string]string{},
		},
		Steps: []WorkflowStep{},
	}
	sourceID, ok, err := directorysync.CurrentSourceID(ctx, s.client)
	if err != nil {
		return nil, nil, fmt.Errorf("current directory source: %w", err)
	}
	var facts *workflowDirectoryFacts
	if ok {
		facts, err = s.loadWorkflowDirectoryFacts(ctx, sourceID)
		if err != nil {
			return nil, nil, err
		}
	}
	paths := []DepartmentPathEvidence{}
	if facts != nil {
		member := facts.memberForUser(requester)
		if member != nil {
			workflow.Requester.DisplayName = firstWorkflowValue(member.DisplayName, requester.Username)
			workflow.Requester.NotificationIDs = notificationIDsForWorkflowMember(member)
			departmentIDs := facts.memberDepartmentIDs(member)
			initial := WorkflowStep{
				Kind:                  WorkflowStepRequesterDepartments,
				DepartmentExternalIDs: []string{},
				Approvers:             []WorkflowApprover{},
				Status:                WorkflowStepQueued,
			}
			for _, departmentID := range departmentIDs {
				department := facts.departmentsByID[departmentID]
				path := workflowDepartmentPath(facts.tree, department)
				if path != "" {
					workflow.Requester.DepartmentPaths = append(workflow.Requester.DepartmentPaths, path)
				}
				approvers := facts.configuredApprovers(departmentID, requester.ID)
				resolution := "configured"
				if len(approvers) == 0 {
					approvers = facts.representativeApprovers(departmentID, requester.ID)
					resolution = "representative"
				}
				if len(approvers) == 0 {
					resolution = "no_approver"
				}
				paths = append(paths, DepartmentPathEvidence{
					StartDepartmentExternalID:   departmentID,
					Path:                        []DepartmentPathNode{{ExternalID: departmentID, DisplayPath: path}},
					MatchedDepartmentExternalID: departmentID,
					MatchedApproverUserIDs:      workflowApproverUserIDs(approvers),
					Resolution:                  resolution,
				})
				if len(approvers) > 0 {
					initial.DepartmentExternalIDs = append(initial.DepartmentExternalIDs, departmentID)
					mergeWorkflowApprovers(&initial.Approvers, approvers)
				}
			}
			if len(initial.Approvers) > 0 {
				initial.Label = strings.Join(workflow.Requester.DepartmentPaths, ", ")
				workflow.Steps = append(workflow.Steps, initial)
			}
		}
	}

	chain, err := s.client.QuotaResetApprovalChain.Query().
		Where(
			quotaresetapprovalchain.ProviderIDEQ(providerID),
			quotaresetapprovalchain.GroupIDEQ(strings.TrimSpace(groupID)),
			quotaresetapprovalchain.Enabled(true),
		).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, nil, fmt.Errorf("load quota reset approval chain: %w", err)
	}
	if err == nil {
		departments, err := decodeChainDepartments(chain.DepartmentChain)
		if err != nil {
			return nil, nil, fmt.Errorf("decode quota reset approval chain %d: %w", chain.ID, err)
		}
		for _, department := range departments {
			approvers := []WorkflowApprover{}
			if facts != nil && department.DirectorySourceID == facts.sourceID {
				approvers = facts.configuredApprovers(department.DepartmentExternalID, requester.ID)
			}
			workflow.Steps = append(workflow.Steps, WorkflowStep{
				Kind:                  WorkflowStepConfiguredDepartment,
				Label:                 firstWorkflowValue(department.DepartmentDisplayPath, department.DepartmentExternalID),
				DepartmentExternalIDs: []string{department.DepartmentExternalID},
				Approvers:             approvers,
				AdminFallback:         len(approvers) == 0,
				Status:                WorkflowStepQueued,
			})
		}
	}
	if len(workflow.Steps) == 0 {
		workflow.Steps = append(workflow.Steps, WorkflowStep{
			Kind:          WorkflowStepConfiguredDepartment,
			Label:         "Admin fallback",
			Approvers:     []WorkflowApprover{},
			AdminFallback: true,
			Status:        WorkflowStepQueued,
		})
	}
	workflow.CurrentStep = 0
	workflow.Steps[0].Status = WorkflowStepActive
	if _, err := EncodeWorkflow(workflow); err != nil {
		return nil, nil, err
	}
	return workflow, paths, nil
}

func (s *Service) loadWorkflowDirectoryFacts(ctx context.Context, sourceID int) (*workflowDirectoryFacts, error) {
	departments, err := s.client.DirectoryDepartment.Query().Where(directorydepartment.SourceIDEQ(sourceID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load workflow departments: %w", err)
	}
	members, err := s.client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(sourceID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load workflow members: %w", err)
	}
	memberships, err := s.client.DirectoryMemberDepartment.Query().Where(directorymemberdepartment.SourceIDEQ(sourceID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load workflow memberships: %w", err)
	}
	users, err := s.client.User.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load workflow users: %w", err)
	}
	configs, err := s.client.QuotaResetApproverConfig.Query().Where(
		quotaresetapproverconfig.DirectorySourceIDEQ(sourceID),
		quotaresetapproverconfig.Enabled(true),
	).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load workflow approver configs: %w", err)
	}
	facts := &workflowDirectoryFacts{
		sourceID:              sourceID,
		tree:                  directorytree.New(departments),
		departmentsByID:       make(map[string]*ent.DirectoryDepartment, len(departments)),
		membersByExternalID:   make(map[string]*ent.DirectoryMember, len(members)),
		membersByUserID:       map[int]*ent.DirectoryMember{},
		usersByID:             make(map[int]*ent.User, len(users)),
		departmentIDsByMember: map[int]map[string]struct{}{},
		configUserIDsByDept:   map[string][]int{},
		representativesByDept: representativeExternalIDsByDepartment(departments, members),
	}
	usersByEmail := make(map[string]*ent.User, len(users))
	for _, user := range users {
		facts.usersByID[user.ID] = user
		usersByEmail[strings.ToLower(strings.TrimSpace(user.Email))] = user
	}
	for _, department := range departments {
		facts.departmentsByID[department.ExternalID] = department
	}
	for _, member := range members {
		facts.membersByExternalID[member.ExternalID] = member
		user := workflowMemberUser(member, facts.usersByID, usersByEmail)
		if user != nil {
			if current := facts.membersByUserID[user.ID]; current == nil || member.ID < current.ID {
				facts.membersByUserID[user.ID] = member
			}
		}
		facts.addMemberDepartment(member.ID, member.DepartmentExternalID)
	}
	for _, membership := range memberships {
		facts.addMemberDepartment(membership.DirectoryMemberID, membership.DepartmentExternalID)
	}
	for _, config := range configs {
		facts.configUserIDsByDept[config.DepartmentExternalID] = append(facts.configUserIDsByDept[config.DepartmentExternalID], config.ApproverUserID)
	}
	for departmentID := range facts.configUserIDsByDept {
		facts.configUserIDsByDept[departmentID] = uniqueSortedWorkflowIDs(facts.configUserIDsByDept[departmentID])
	}
	return facts, nil
}

func (f *workflowDirectoryFacts) addMemberDepartment(memberID int, departmentID string) {
	departmentID = strings.TrimSpace(departmentID)
	if memberID <= 0 || departmentID == "" {
		return
	}
	if f.departmentIDsByMember[memberID] == nil {
		f.departmentIDsByMember[memberID] = map[string]struct{}{}
	}
	f.departmentIDsByMember[memberID][departmentID] = struct{}{}
}

func (f *workflowDirectoryFacts) memberForUser(user *ent.User) *ent.DirectoryMember {
	if f == nil || user == nil {
		return nil
	}
	return f.membersByUserID[user.ID]
}

func (f *workflowDirectoryFacts) memberDepartmentIDs(member *ent.DirectoryMember) []string {
	if f == nil || member == nil {
		return []string{}
	}
	ids := make([]string, 0, len(f.departmentIDsByMember[member.ID]))
	for id := range f.departmentIDsByMember[member.ID] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (f *workflowDirectoryFacts) configuredApprovers(departmentID string, requesterID int) []WorkflowApprover {
	approvers := make([]WorkflowApprover, 0)
	for _, userID := range f.configUserIDsByDept[departmentID] {
		if userID == requesterID {
			continue
		}
		user := f.usersByID[userID]
		member := f.membersByUserID[userID]
		if !workflowCandidateUsable(user, member) {
			continue
		}
		approvers = append(approvers, workflowApprover(user, member, "configured"))
	}
	return approvers
}

func (f *workflowDirectoryFacts) representativeApprovers(departmentID string, requesterID int) []WorkflowApprover {
	approvers := make([]WorkflowApprover, 0)
	for externalID := range f.representativesByDept[departmentID] {
		member := f.membersByExternalID[externalID]
		if member == nil {
			continue
		}
		user := f.userForMember(member)
		if user == nil || user.ID == requesterID || !workflowCandidateUsable(user, member) {
			continue
		}
		approvers = append(approvers, workflowApprover(user, member, "directory_representative"))
	}
	sort.SliceStable(approvers, func(i, j int) bool { return approvers[i].UserID < approvers[j].UserID })
	return approvers
}

func (f *workflowDirectoryFacts) userForMember(member *ent.DirectoryMember) *ent.User {
	if f == nil || member == nil {
		return nil
	}
	if member.MatchedUserID != nil {
		return f.usersByID[*member.MatchedUserID]
	}
	for userID, candidate := range f.membersByUserID {
		if candidate != nil && candidate.ID == member.ID {
			return f.usersByID[userID]
		}
	}
	return nil
}

func workflowMemberUser(member *ent.DirectoryMember, usersByID map[int]*ent.User, usersByEmail map[string]*ent.User) *ent.User {
	if member == nil {
		return nil
	}
	if member.MatchedUserID != nil {
		return usersByID[*member.MatchedUserID]
	}
	return usersByEmail[strings.ToLower(strings.TrimSpace(member.EmailNormalized))]
}

func workflowCandidateUsable(user *ent.User, member *ent.DirectoryMember) bool {
	return user != nil && member != nil && strings.EqualFold(strings.TrimSpace(member.Status), "active") && user.RelayDisabledAt == nil && user.TokenValidAfter == nil
}

func workflowApprover(user *ent.User, member *ent.DirectoryMember, source string) WorkflowApprover {
	return WorkflowApprover{
		UserID:          user.ID,
		DisplayName:     firstWorkflowValue(member.DisplayName, user.Username),
		Email:           strings.TrimSpace(user.Email),
		Source:          source,
		NotificationIDs: notificationIDsForWorkflowMember(member),
	}
}

func notificationIDsForWorkflowMember(member *ent.DirectoryMember) map[string]string {
	result := map[string]string{}
	if member == nil {
		return result
	}
	if value, ok := member.Metadata["wecom_userid"].(string); ok {
		if value = strings.TrimSpace(value); value != "" {
			result["wecom"] = value
		}
	}
	return result
}

func mergeWorkflowApprovers(target *[]WorkflowApprover, candidates []WorkflowApprover) {
	indexByUserID := make(map[int]int, len(*target))
	for index, approver := range *target {
		indexByUserID[approver.UserID] = index
	}
	for _, candidate := range candidates {
		if index, exists := indexByUserID[candidate.UserID]; exists {
			if candidate.Source == "configured" {
				(*target)[index] = candidate
			}
			continue
		}
		indexByUserID[candidate.UserID] = len(*target)
		*target = append(*target, candidate)
	}
	sort.SliceStable(*target, func(i, j int) bool { return (*target)[i].UserID < (*target)[j].UserID })
}

func workflowApproverUserIDs(approvers []WorkflowApprover) []int {
	ids := make([]int, 0, len(approvers))
	for _, approver := range approvers {
		ids = append(ids, approver.UserID)
	}
	return uniqueSortedWorkflowIDs(ids)
}

func uniqueSortedWorkflowIDs(ids []int) []int {
	seen := map[int]struct{}{}
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}

func encodeChainDepartments(items []ChainDepartmentInput) ([]map[string]any, error) {
	raw, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("encode approval chain departments: %w", err)
	}
	var result []map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("normalize approval chain departments: %w", err)
	}
	return result, nil
}

func decodeChainDepartments(raw []map[string]any) ([]ChainDepartmentInput, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var result []ChainDepartmentInput
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = []ChainDepartmentInput{}
	}
	return result, nil
}

func workflowDepartmentPath(tree *directorytree.Tree, department *ent.DirectoryDepartment) string {
	if department == nil {
		return ""
	}
	if tree != nil {
		if path := strings.TrimSpace(tree.DisplayPath(department.ExternalID)); path != "" {
			return path
		}
	}
	return firstWorkflowValue(department.Name, department.ExternalID)
}

func firstWorkflowValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func approvalChainKey(providerID int, groupID string) string {
	return fmt.Sprintf("%d/%s", providerID, strings.TrimSpace(groupID))
}
