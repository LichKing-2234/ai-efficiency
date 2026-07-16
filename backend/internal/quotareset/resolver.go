package quotareset

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/quotaresetapproverconfig"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/directorytree"
)

type ApproverResolver struct {
	client *ent.Client
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

func NewApproverResolver(client *ent.Client) *ApproverResolver {
	return &ApproverResolver{client: client}
}

func (s *Service) resolveWorkflowSnapshot(ctx context.Context, requester *ent.User) (*Workflow, []DepartmentPathEvidence, error) {
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, nil, fmt.Errorf("start quota reset workflow snapshot: %w", err)
	}
	defer tx.Rollback()
	workflow, paths, err := NewApproverResolver(tx.Client()).ResolveWorkflow(ctx, requester)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit quota reset workflow snapshot: %w", err)
	}
	return workflow, paths, nil
}

func (r *ApproverResolver) ResolveWorkflow(ctx context.Context, requester *ent.User) (*Workflow, []DepartmentPathEvidence, error) {
	if requester == nil {
		return nil, nil, fmt.Errorf("%w: requester is missing", ErrInvalidWorkflow)
	}
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
	paths := []DepartmentPathEvidence{}

	sourceID, ok, err := directorysync.CurrentSourceID(ctx, r.client)
	if err != nil {
		return nil, nil, fmt.Errorf("current directory source: %w", err)
	}
	if ok {
		facts, err := r.loadWorkflowDirectoryFacts(ctx, sourceID)
		if err != nil {
			return nil, nil, err
		}
		if requesterMember := facts.memberForUser(requester); requesterMember != nil {
			workflow.Requester.DisplayName = firstWorkflowValue(requesterMember.DisplayName, requester.Username)
			workflow.Requester.NotificationIDs = notificationIDsForWorkflowMember(requesterMember)
			exactIDs := facts.memberDepartmentIDs(requesterMember)
			for _, departmentID := range exactIDs {
				if path := workflowDepartmentPath(facts.tree, facts.departmentsByID[departmentID]); path != "" {
					workflow.Requester.DepartmentPaths = append(workflow.Requester.DepartmentPaths, path)
				}
			}
			workflow.Requester.DepartmentPaths = uniqueSortedStrings(workflow.Requester.DepartmentPaths)

			exactStep, exactHadConfig, exactPaths := facts.resolveExactStep(exactIDs, requester.ID)
			paths = append(paths, exactPaths...)
			if len(exactStep.Approvers) > 0 || exactHadConfig {
				exactStep.AdminFallback = len(exactStep.Approvers) == 0
				workflow.Steps = append(workflow.Steps, exactStep)
			}

			visited := stringSet(exactIDs)
			for round := facts.parentRound(exactIDs, visited); len(round) > 0; round = facts.parentRound(round, visited) {
				step, roundHadConfig := facts.resolveConfiguredRound(round, requester.ID)
				if roundHadConfig {
					step.AdminFallback = len(step.Approvers) == 0
					workflow.Steps = append(workflow.Steps, step)
				}
				if len(workflow.Steps) > maxWorkflowSteps {
					return nil, nil, ErrInvalidWorkflow
				}
			}
		}
	}
	if len(workflow.Steps) == 0 {
		workflow.Steps = append(workflow.Steps, adminFallbackWorkflowStep())
	}
	workflow.CurrentStep = 0
	workflow.Steps[0].Status = WorkflowStepActive
	if _, err := EncodeWorkflow(workflow); err != nil {
		return nil, nil, err
	}
	return workflow, paths, nil
}

func (r *ApproverResolver) loadWorkflowDirectoryFacts(ctx context.Context, sourceID int) (*workflowDirectoryFacts, error) {
	departments, err := r.client.DirectoryDepartment.Query().Where(directorydepartment.SourceIDEQ(sourceID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load workflow departments: %w", err)
	}
	members, err := r.client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(sourceID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load workflow members: %w", err)
	}
	memberships, err := r.client.DirectoryMemberDepartment.Query().Where(directorymemberdepartment.SourceIDEQ(sourceID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load workflow memberships: %w", err)
	}
	users, err := r.client.User.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load workflow users: %w", err)
	}
	configs, err := r.client.QuotaResetApproverConfig.Query().Where(
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
		departmentID := strings.TrimSpace(config.DepartmentExternalID)
		if departmentID != "" {
			facts.configUserIDsByDept[departmentID] = append(facts.configUserIDsByDept[departmentID], config.ApproverUserID)
		}
	}
	for departmentID := range facts.configUserIDsByDept {
		facts.configUserIDsByDept[departmentID] = uniqueSortedWorkflowIDs(facts.configUserIDsByDept[departmentID])
	}
	return facts, nil
}

func (f *workflowDirectoryFacts) resolveExactStep(exactIDs []string, requesterID int) (WorkflowStep, bool, []DepartmentPathEvidence) {
	departmentIDs := uniqueSortedStrings(exactIDs)
	step := WorkflowStep{
		Kind:                  WorkflowStepRequesterDepartments,
		DepartmentExternalIDs: departmentIDs,
		Approvers:             []WorkflowApprover{},
		Status:                WorkflowStepQueued,
	}
	labels := make([]string, 0, len(departmentIDs))
	paths := make([]DepartmentPathEvidence, 0, len(departmentIDs))
	hadConfig := false
	for _, departmentID := range departmentIDs {
		path := workflowDepartmentPath(f.tree, f.departmentsByID[departmentID])
		if path != "" {
			labels = append(labels, path)
		}
		approvers, configured := f.configuredApprovers(departmentID, requesterID)
		resolution := "no_config_found"
		if configured {
			hadConfig = true
			resolution = "matched"
		} else {
			approvers = f.representativeApprovers(departmentID, requesterID)
		}
		paths = append(paths, DepartmentPathEvidence{
			StartDepartmentExternalID:   departmentID,
			Path:                        f.departmentPathEvidence(departmentID),
			MatchedDepartmentExternalID: departmentID,
			MatchedApproverUserIDs:      workflowApproverUserIDs(approvers),
			Resolution:                  resolution,
		})
		mergeWorkflowApprovers(&step.Approvers, approvers)
	}
	step.Label = strings.Join(uniqueSortedStrings(labels), ", ")
	return step, hadConfig, paths
}

func (f *workflowDirectoryFacts) resolveConfiguredRound(round []string, requesterID int) (WorkflowStep, bool) {
	step := WorkflowStep{
		Kind:                  WorkflowStepConfiguredDepartment,
		DepartmentExternalIDs: []string{},
		Approvers:             []WorkflowApprover{},
		Status:                WorkflowStepQueued,
	}
	labels := []string{}
	for _, departmentID := range uniqueSortedStrings(round) {
		approvers, configured := f.configuredApprovers(departmentID, requesterID)
		if !configured {
			continue
		}
		step.DepartmentExternalIDs = append(step.DepartmentExternalIDs, departmentID)
		if path := workflowDepartmentPath(f.tree, f.departmentsByID[departmentID]); path != "" {
			labels = append(labels, path)
		}
		mergeWorkflowApprovers(&step.Approvers, approvers)
	}
	step.Label = strings.Join(uniqueSortedStrings(labels), ", ")
	return step, len(step.DepartmentExternalIDs) > 0
}

func (f *workflowDirectoryFacts) parentRound(departmentIDs []string, visited map[string]struct{}) []string {
	next := []string{}
	for _, departmentID := range uniqueSortedStrings(departmentIDs) {
		department := f.departmentsByID[departmentID]
		parentID := directorytree.ParentExternalID(department)
		if parentID == "" {
			continue
		}
		if _, seen := visited[parentID]; seen {
			continue
		}
		visited[parentID] = struct{}{}
		next = append(next, parentID)
	}
	return uniqueSortedStrings(next)
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
	return uniqueSortedStrings(ids)
}

func (f *workflowDirectoryFacts) departmentPathEvidence(startDepartmentID string) []DepartmentPathNode {
	path := []DepartmentPathNode{}
	visited := map[string]struct{}{}
	for departmentID := strings.TrimSpace(startDepartmentID); departmentID != ""; {
		if _, seen := visited[departmentID]; seen {
			break
		}
		visited[departmentID] = struct{}{}
		department := f.departmentsByID[departmentID]
		if department == nil {
			break
		}
		path = append(path, DepartmentPathNode{
			ExternalID:  department.ExternalID,
			DisplayPath: workflowDepartmentPath(f.tree, department),
		})
		departmentID = directorytree.ParentExternalID(department)
	}
	return path
}

func (f *workflowDirectoryFacts) configuredApprovers(departmentID string, requesterID int) ([]WorkflowApprover, bool) {
	configuredUserIDs, configured := f.configUserIDsByDept[strings.TrimSpace(departmentID)]
	if !configured {
		return []WorkflowApprover{}, false
	}
	approvers := make([]WorkflowApprover, 0, len(configuredUserIDs))
	for _, userID := range configuredUserIDs {
		if userID == requesterID {
			continue
		}
		user := f.usersByID[userID]
		member := f.membersByUserID[userID]
		if !f.configuredMemberInDepartment(userID, member, departmentID) || !workflowCandidateUsable(user, member) {
			continue
		}
		approvers = append(approvers, workflowApprover(user, member, "configured"))
	}
	return approvers, true
}

func (f *workflowDirectoryFacts) configuredMemberInDepartment(userID int, member *ent.DirectoryMember, departmentID string) bool {
	if member == nil || member.MatchedUserID == nil || *member.MatchedUserID != userID {
		return false
	}
	_, ok := f.departmentIDsByMember[member.ID][strings.TrimSpace(departmentID)]
	return ok
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

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func uniqueSortedStrings(values []string) []string {
	set := stringSet(values)
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func adminFallbackWorkflowStep() WorkflowStep {
	return WorkflowStep{
		Kind:          WorkflowStepConfiguredDepartment,
		Label:         "Admin fallback",
		Approvers:     []WorkflowApprover{},
		AdminFallback: true,
		Status:        WorkflowStepQueued,
	}
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
