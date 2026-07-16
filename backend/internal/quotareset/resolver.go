package quotareset

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
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
	tree                  *directorytree.Tree
	departmentsByID       map[string]*ent.DirectoryDepartment
	membersByExternalID   map[string]*ent.DirectoryMember
	membersByUserID       map[int]*ent.DirectoryMember
	userIDByMemberID      map[int]int
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
		},
	}
	var paths []DepartmentPathEvidence

	sourceID, ok, err := directorysync.CurrentSourceID(ctx, r.client)
	if err != nil {
		return nil, nil, fmt.Errorf("current directory source: %w", err)
	}
	if ok {
		facts, err := r.loadWorkflowDirectoryFacts(ctx, sourceID)
		if err != nil {
			return nil, nil, err
		}
		if requesterMember := facts.membersByUserID[requester.ID]; requesterMember != nil {
			workflow.Requester.DisplayName = firstWorkflowValue(requesterMember.DisplayName, requester.Username)
			exactIDs := slices.Sorted(maps.Keys(facts.departmentIDsByMember[requesterMember.ID]))
			for _, departmentID := range exactIDs {
				if path := workflowDepartmentPath(facts.tree, facts.departmentsByID[departmentID]); path != "" {
					workflow.Requester.DepartmentPaths = append(workflow.Requester.DepartmentPaths, path)
				}
			}
			workflow.Requester.DepartmentPaths = uniqueSortedStrings(workflow.Requester.DepartmentPaths)

			exactStep, exactHadConfig, exactPaths := facts.resolveExactStep(exactIDs, requester.ID)
			exactStep.Label = strings.Join(workflow.Requester.DepartmentPaths, ", ")
			paths = append(paths, exactPaths...)
			if len(exactStep.Approvers) > 0 || exactHadConfig {
				exactStep.AdminFallback = len(exactStep.Approvers) == 0
				workflow.Steps = append(workflow.Steps, exactStep)
			}

			visited := make(map[string]struct{}, len(exactIDs))
			for _, departmentID := range exactIDs {
				visited[departmentID] = struct{}{}
			}
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
		fallback := newWorkflowStep(WorkflowStepConfiguredDepartment, nil)
		fallback.Label = "Admin fallback"
		fallback.AdminFallback = true
		workflow.Steps = append(workflow.Steps, fallback)
	}
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
		tree:                  directorytree.New(departments),
		departmentsByID:       make(map[string]*ent.DirectoryDepartment, len(departments)),
		membersByExternalID:   make(map[string]*ent.DirectoryMember, len(members)),
		membersByUserID:       map[int]*ent.DirectoryMember{},
		userIDByMemberID:      map[int]int{},
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
			facts.userIDByMemberID[member.ID] = user.ID
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
	step := newWorkflowStep(WorkflowStepRequesterDepartments, departmentIDs)
	paths := make([]DepartmentPathEvidence, 0, len(departmentIDs))
	hadConfig := false
	for _, departmentID := range departmentIDs {
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
	return step, hadConfig, paths
}

func (f *workflowDirectoryFacts) resolveConfiguredRound(round []string, requesterID int) (WorkflowStep, bool) {
	step := newWorkflowStep(WorkflowStepConfiguredDepartment, nil)
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
		if _, seen := visited[parentID]; parentID == "" || seen {
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
	departmentID = strings.TrimSpace(departmentID)
	configuredUserIDs, configured := f.configUserIDsByDept[departmentID]
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
		if member == nil || member.MatchedUserID == nil || *member.MatchedUserID != userID || !workflowCandidateUsable(user, member) {
			continue
		}
		if _, belongs := f.departmentIDsByMember[member.ID][departmentID]; !belongs {
			continue
		}
		approvers = append(approvers, workflowApprover(user, member))
	}
	return approvers, true
}

func (f *workflowDirectoryFacts) representativeApprovers(departmentID string, requesterID int) []WorkflowApprover {
	approvers := make([]WorkflowApprover, 0)
	for externalID := range f.representativesByDept[departmentID] {
		member := f.membersByExternalID[externalID]
		if member == nil {
			continue
		}
		user := f.usersByID[f.userIDByMemberID[member.ID]]
		if user == nil || user.ID == requesterID || !workflowCandidateUsable(user, member) {
			continue
		}
		approvers = append(approvers, workflowApprover(user, member))
	}
	return approvers
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

func workflowApprover(user *ent.User, member *ent.DirectoryMember) WorkflowApprover {
	notificationIDs := map[string]string(nil)
	if value, _ := member.Metadata["wecom_userid"].(string); strings.TrimSpace(value) != "" {
		notificationIDs = map[string]string{"wecom": strings.TrimSpace(value)}
	}
	return WorkflowApprover{
		UserID:          user.ID,
		DisplayName:     firstWorkflowValue(member.DisplayName, user.Username),
		Email:           strings.TrimSpace(user.Email),
		NotificationIDs: notificationIDs,
	}
}

func mergeWorkflowApprovers(target *[]WorkflowApprover, candidates []WorkflowApprover) {
	*target = append(*target, candidates...)
	sort.SliceStable(*target, func(i, j int) bool { return (*target)[i].UserID < (*target)[j].UserID })
	*target = slices.CompactFunc(*target, func(left, right WorkflowApprover) bool {
		return left.UserID == right.UserID
	})
}

func uniqueSortedStrings(values []string) []string {
	values = compactQuotaResetStrings(values)
	sort.Strings(values)
	return values
}

func newWorkflowStep(kind string, departmentIDs []string) WorkflowStep {
	return WorkflowStep{
		Kind:                  kind,
		DepartmentExternalIDs: departmentIDs,
		Approvers:             []WorkflowApprover{},
		Status:                WorkflowStepQueued,
	}
}

func workflowDepartmentPath(tree *directorytree.Tree, department *ent.DirectoryDepartment) string {
	if department == nil {
		return ""
	}
	if path := strings.TrimSpace(tree.DisplayPath(department.ExternalID)); path != "" {
		return path
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
