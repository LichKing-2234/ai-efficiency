package quotareset

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetapproverconfig"
	"github.com/ai-efficiency/backend/internal/directoryfacts"
)

type ApproverResolver struct {
	client *ent.Client
	facts  directoryfacts.Reader
}

type workflowDirectoryFacts struct {
	directory           *directoryfacts.Facts
	configUserIDsByDept map[string][]int
}

func NewApproverResolver(client *ent.Client) *ApproverResolver {
	return &ApproverResolver{client: client, facts: directoryfacts.New(client)}
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

	view, ok, err := r.facts.Current(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("current directory source: %w", err)
	}
	if ok {
		facts, err := r.loadWorkflowDirectoryFactsForRequester(ctx, view, requester)
		if err != nil {
			return nil, nil, err
		}
		if requesterMember := facts.directory.MemberForUser(requester.ID); requesterMember != nil {
			workflow.Requester.DisplayName = firstWorkflowValue(requesterMember.DisplayName, requester.Username)
			exactIDs := facts.directory.DepartmentIDsForMember(*requesterMember)
			for _, departmentID := range exactIDs {
				if path := workflowDepartmentPath(facts.directory.Hierarchy(), facts.directory.Hierarchy().Department(departmentID)); path != "" {
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

func (r *ApproverResolver) loadWorkflowDirectoryFacts(ctx context.Context, view directoryfacts.View) (*workflowDirectoryFacts, error) {
	facts, err := view.Load(ctx, directoryfacts.Query{
		AllDepartments:     true,
		AllMembers:         true,
		IncludeMemberships: true,
		AllUsers:           true,
	})
	if err != nil {
		return nil, fmt.Errorf("load workflow directory facts: %w", err)
	}
	configs, err := r.client.QuotaResetApproverConfig.Query().Where(
		quotaresetapproverconfig.DirectorySourceIDEQ(view.Snapshot().SourceID),
		quotaresetapproverconfig.Enabled(true),
	).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load workflow approver configs: %w", err)
	}
	return buildWorkflowDirectoryFacts(facts, configs), nil
}

func (r *ApproverResolver) loadWorkflowDirectoryFactsForRequester(ctx context.Context, view directoryfacts.View, requester *ent.User) (*workflowDirectoryFacts, error) {
	requesterEmail := directoryfacts.NormalizeEmail(requester.Email)
	requesterFacts, err := view.Load(ctx, directoryfacts.Query{
		AllDepartments:     true,
		MemberUserIDs:      []int{requester.ID},
		MemberEmails:       []string{requesterEmail},
		IncludeMemberships: true,
		UserIDs:            []int{requester.ID},
		UserEmails:         []string{requesterEmail},
	})
	if err != nil {
		return nil, fmt.Errorf("load workflow requester facts: %w", err)
	}
	requesterMember := requesterFacts.MemberForUser(requester.ID)
	if requesterMember == nil {
		return buildWorkflowDirectoryFacts(requesterFacts, nil), nil
	}
	exactDepartmentIDs := requesterFacts.DepartmentIDsForMember(*requesterMember)

	departmentFacts := buildWorkflowDirectoryFacts(requesterFacts, nil)
	relevantDepartmentIDs := workflowRelevantDepartmentIDs(departmentFacts, exactDepartmentIDs)
	configs := []*ent.QuotaResetApproverConfig{}
	if len(relevantDepartmentIDs) > 0 {
		configs, err = r.client.QuotaResetApproverConfig.Query().Where(
			quotaresetapproverconfig.DirectorySourceIDEQ(view.Snapshot().SourceID),
			quotaresetapproverconfig.Enabled(true),
			quotaresetapproverconfig.DepartmentExternalIDIn(relevantDepartmentIDs...),
		).All(ctx)
		if err != nil {
			return nil, fmt.Errorf("load workflow approver configs: %w", err)
		}
	}

	configuredUserIDs := make([]int, 0, len(configs))
	for _, config := range configs {
		configuredUserIDs = append(configuredUserIDs, config.ApproverUserID)
	}
	configuredUserIDs = uniqueSortedWorkflowIDs(configuredUserIDs)
	configuredEmails := []string{}
	if len(configuredUserIDs) > 0 {
		configuredFacts, loadErr := view.Load(ctx, directoryfacts.Query{UserIDs: configuredUserIDs})
		err = loadErr
		if err != nil {
			return nil, fmt.Errorf("load workflow configured users: %w", err)
		}
		for _, userID := range configuredUserIDs {
			if user := configuredFacts.User(userID); user != nil {
				configuredEmails = append(configuredEmails, user.Email)
			}
		}
	}
	userIDs := append([]int{requester.ID}, configuredUserIDs...)
	userEmails := append([]string{requesterEmail}, configuredEmails...)
	facts, err := view.Load(ctx, directoryfacts.Query{
		AllDepartments:              true,
		MemberUserIDs:               userIDs,
		MemberEmails:                userEmails,
		RepresentativeDepartmentIDs: exactDepartmentIDs,
		IncludeMemberships:          true,
		UserIDs:                     userIDs,
		UserEmails:                  userEmails,
		MatchUsersForMembers:        true,
	})
	if err != nil {
		return nil, fmt.Errorf("load workflow candidate directory facts: %w", err)
	}
	return buildWorkflowDirectoryFacts(facts, configs), nil
}

func buildWorkflowDirectoryFacts(directory *directoryfacts.Facts, configs []*ent.QuotaResetApproverConfig) *workflowDirectoryFacts {
	facts := &workflowDirectoryFacts{
		directory:           directory,
		configUserIDsByDept: map[string][]int{},
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
	return facts
}

func workflowRelevantDepartmentIDs(facts *workflowDirectoryFacts, exactDepartmentIDs []string) []string {
	visited := make(map[string]struct{}, len(exactDepartmentIDs))
	relevant := append([]string(nil), exactDepartmentIDs...)
	for _, departmentID := range exactDepartmentIDs {
		visited[departmentID] = struct{}{}
	}
	for round := facts.parentRound(exactDepartmentIDs, visited); len(round) > 0; round = facts.parentRound(round, visited) {
		relevant = append(relevant, round...)
	}
	return uniqueSortedStrings(relevant)
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
		if path := workflowDepartmentPath(f.directory.Hierarchy(), f.directory.Hierarchy().Department(departmentID)); path != "" {
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
		parentID := f.directory.Hierarchy().ParentID(departmentID)
		if _, seen := visited[parentID]; parentID == "" || seen {
			continue
		}
		visited[parentID] = struct{}{}
		next = append(next, parentID)
	}
	return uniqueSortedStrings(next)
}

func (f *workflowDirectoryFacts) departmentPathEvidence(startDepartmentID string) []DepartmentPathNode {
	path := []DepartmentPathNode{}
	visited := map[string]struct{}{}
	for departmentID := strings.TrimSpace(startDepartmentID); departmentID != ""; {
		if _, seen := visited[departmentID]; seen {
			break
		}
		visited[departmentID] = struct{}{}
		department := f.directory.Hierarchy().Department(departmentID)
		if department == nil {
			break
		}
		path = append(path, DepartmentPathNode{
			ExternalID:  department.ExternalID,
			DisplayPath: workflowDepartmentPath(f.directory.Hierarchy(), department),
		})
		departmentID = f.directory.Hierarchy().ParentID(departmentID)
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
		user := f.directory.User(userID)
		member := f.directory.MemberForUser(userID)
		if !workflowCandidateUsable(user, member) {
			continue
		}
		if !slices.Contains(f.directory.DepartmentIDsForMember(*member), departmentID) {
			continue
		}
		approvers = append(approvers, workflowApprover(user, member))
	}
	return approvers, true
}

func (f *workflowDirectoryFacts) representativeApprovers(departmentID string, requesterID int) []WorkflowApprover {
	approvers := make([]WorkflowApprover, 0)
	for _, externalID := range f.directory.RepresentativesByDepartment()[departmentID] {
		member := f.directory.MemberByExternalID(externalID)
		if member == nil {
			continue
		}
		user := f.directory.UserForMember(*member)
		if user == nil || user.ID == requesterID || !workflowCandidateUsable(user, member) {
			continue
		}
		approvers = append(approvers, workflowApprover(user, member))
	}
	return approvers
}

func workflowCandidateUsable(user *directoryfacts.User, member *directoryfacts.Member) bool {
	return user != nil && member != nil && strings.EqualFold(strings.TrimSpace(member.Status), "active") && user.RelayDisabledAt == nil && user.TokenValidAfter == nil
}

func workflowApprover(user *directoryfacts.User, member *directoryfacts.Member) WorkflowApprover {
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

func workflowDepartmentPath(tree *directoryfacts.Hierarchy, department *directoryfacts.Department) string {
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
