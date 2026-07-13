package quotareset

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/quotaresetapprovalchain"
	"github.com/ai-efficiency/backend/ent/quotaresetapprovalchainnode"
	"github.com/ai-efficiency/backend/ent/quotaresetapproverconfig"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/directorytree"
)

const departmentRepresentativeIDsMetadataKey = "representative_external_ids"

type WorkflowResolver struct{ client *ent.Client }

type workflowDirectoryFacts struct {
	sourceID               int
	requester              *ent.User
	requesterMember        *ent.DirectoryMember
	requesterDepartmentIDs []string
	departmentPaths        []string
	tree                   *directorytree.Tree
	configsByDepartment    map[string][]int
	representatives        map[string][]*ent.DirectoryMember
	usersByID              map[int]*ent.User
	usersByEmail           map[string]*ent.User
	membersByUserID        map[int][]*ent.DirectoryMember
	activeMembersByUserID  map[int]*ent.DirectoryMember
}

type workflowApproverCandidate struct {
	user   *ent.User
	member *ent.DirectoryMember
}

func NewWorkflowResolver(client *ent.Client) *WorkflowResolver {
	return &WorkflowResolver{client: client}
}

func (r *WorkflowResolver) Resolve(ctx context.Context, requesterUserID, providerID int, groupID string) (*WorkflowSnapshot, error) {
	sourceID, ok, err := directorysync.CurrentSourceID(ctx, r.client)
	if err != nil {
		return nil, fmt.Errorf("resolve current directory source: %w", err)
	}
	if !ok {
		return nil, ErrDirectoryUnavailable
	}
	facts, err := r.loadWorkflowDirectoryFacts(ctx, sourceID, requesterUserID)
	if err != nil {
		return nil, err
	}
	initial := r.resolveInitialNode(facts)
	configured, err := r.resolveConfiguredNodes(ctx, facts, providerID, strings.TrimSpace(groupID))
	if err != nil {
		return nil, err
	}
	nodes := make([]ResolvedWorkflowNode, 0, len(configured)+1)
	nodes = append(nodes, initial)
	for i := range configured {
		configured[i].Position = i + 1
		nodes = append(nodes, configured[i])
	}
	return &WorkflowSnapshot{
		Requester: RequesterIdentitySnapshot{
			DisplayName:     firstNonEmptyQuotaReset(requesterMemberDisplayName(facts.requesterMember), facts.requester.Username),
			Email:           facts.requester.Email,
			DepartmentPaths: append([]string(nil), facts.departmentPaths...),
			NotificationIDs: notificationIDsForMember(facts.requesterMember),
		},
		Nodes: nodes,
	}, nil
}

func (r *WorkflowResolver) loadWorkflowDirectoryFacts(ctx context.Context, sourceID, requesterUserID int) (*workflowDirectoryFacts, error) {
	requester, err := r.client.User.Get(ctx, requesterUserID)
	if err != nil {
		return nil, fmt.Errorf("get workflow requester: %w", err)
	}
	departments, err := r.client.DirectoryDepartment.Query().
		Where(directorydepartment.SourceIDEQ(sourceID)).
		Order(ent.Asc(directorydepartment.FieldPath), ent.Asc(directorydepartment.FieldName), ent.Asc(directorydepartment.FieldExternalID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workflow departments: %w", err)
	}
	members, err := r.client.DirectoryMember.Query().
		Where(directorymember.SourceIDEQ(sourceID)).
		Order(ent.Asc(directorymember.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workflow members: %w", err)
	}
	memberships, err := r.client.DirectoryMemberDepartment.Query().
		Where(directorymemberdepartment.SourceIDEQ(sourceID)).
		Order(ent.Asc(directorymemberdepartment.FieldDirectoryMemberID), ent.Asc(directorymemberdepartment.FieldDepartmentExternalID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workflow memberships: %w", err)
	}
	configs, err := r.client.QuotaResetApproverConfig.Query().
		Where(quotaresetapproverconfig.DirectorySourceIDEQ(sourceID), quotaresetapproverconfig.EnabledEQ(true)).
		Order(ent.Asc(quotaresetapproverconfig.FieldDepartmentExternalID), ent.Asc(quotaresetapproverconfig.FieldApproverUserID), ent.Asc(quotaresetapproverconfig.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workflow approver configs: %w", err)
	}
	users, err := r.client.User.Query().Order(ent.Asc("id")).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workflow users: %w", err)
	}

	usersByID := make(map[int]*ent.User, len(users))
	usersByEmail := make(map[string]*ent.User, len(users))
	for _, user := range users {
		if user == nil {
			continue
		}
		usersByID[user.ID] = user
		email := strings.ToLower(strings.TrimSpace(user.Email))
		if email != "" {
			usersByEmail[email] = user
		}
	}
	membersByUserID := make(map[int][]*ent.DirectoryMember)
	activeMembersByUserID := make(map[int]*ent.DirectoryMember)
	membersByExternalID := make(map[string]*ent.DirectoryMember)
	for _, member := range members {
		if member == nil {
			continue
		}
		externalID := strings.TrimSpace(member.ExternalID)
		if externalID != "" {
			membersByExternalID[externalID] = member
		}
		user := workflowMemberUser(member, usersByID, usersByEmail)
		if user == nil {
			continue
		}
		membersByUserID[user.ID] = append(membersByUserID[user.ID], member)
		if !workflowMemberIsActive(member) || user.RelayDisabledAt != nil || user.TokenValidAfter != nil {
			continue
		}
		if existing := activeMembersByUserID[user.ID]; existing == nil || member.ID < existing.ID {
			activeMembersByUserID[user.ID] = member
		}
	}

	configsByDepartment := make(map[string][]int)
	for _, config := range configs {
		departmentID := strings.TrimSpace(config.DepartmentExternalID)
		if departmentID == "" {
			continue
		}
		configsByDepartment[departmentID] = appendUniqueSortedInt(configsByDepartment[departmentID], config.ApproverUserID)
	}
	representatives := make(map[string][]*ent.DirectoryMember)
	for _, department := range departments {
		if department == nil {
			continue
		}
		for _, externalID := range departmentRepresentativeExternalIDs(department.Metadata) {
			if member := membersByExternalID[externalID]; member != nil {
				representatives[department.ExternalID] = append(representatives[department.ExternalID], member)
			}
		}
	}
	for departmentID := range representatives {
		sort.SliceStable(representatives[departmentID], func(i, j int) bool {
			return representatives[departmentID][i].ID < representatives[departmentID][j].ID
		})
	}

	requesterMember := findRequesterDirectoryMember(requester, members)
	requesterDepartmentIDs := workflowRequesterDepartmentIDs(requesterMember, memberships)
	tree := directorytree.New(departments)
	departmentPaths := make([]string, 0, len(requesterDepartmentIDs))
	for _, departmentID := range requesterDepartmentIDs {
		departmentPaths = appendUniqueSortedString(departmentPaths, tree.DisplayPath(departmentID))
	}
	return &workflowDirectoryFacts{
		sourceID:               sourceID,
		requester:              requester,
		requesterMember:        requesterMember,
		requesterDepartmentIDs: requesterDepartmentIDs,
		departmentPaths:        departmentPaths,
		tree:                   tree,
		configsByDepartment:    configsByDepartment,
		representatives:        representatives,
		usersByID:              usersByID,
		usersByEmail:           usersByEmail,
		membersByUserID:        membersByUserID,
		activeMembersByUserID:  activeMembersByUserID,
	}, nil
}

func (r *WorkflowResolver) resolveInitialNode(facts *workflowDirectoryFacts) ResolvedWorkflowNode {
	initial := ResolvedWorkflowNode{
		Position:      0,
		NodeType:      "requester_departments",
		Label:         "Requester departments",
		Departments:   []DepartmentSnapshot{},
		Approvers:     []ResolvedNodeApprover{},
		InitialStatus: "queued",
	}
	for _, departmentID := range facts.requesterDepartmentIDs {
		configured := usableConfiguredApprovers(facts.configsByDepartment[departmentID], facts, facts.requester.ID)
		resolution := "configured"
		candidates := configured
		if len(candidates) == 0 {
			resolution = "directory_representative"
			candidates = usableRepresentativeApprovers(facts.representatives[departmentID], facts, facts.requester.ID)
		}
		initial.Departments = append(initial.Departments, DepartmentSnapshot{
			ExternalID:  departmentID,
			DisplayPath: facts.tree.DisplayPath(departmentID),
			Resolution:  resolution,
		})
		mergeResolvedApprovers(&initial.Approvers, candidates, departmentID, resolution)
	}
	if len(initial.Approvers) == 0 {
		initial.InitialStatus = "skipped_no_approver"
	}
	return initial
}

func (r *WorkflowResolver) resolveConfiguredNodes(ctx context.Context, facts *workflowDirectoryFacts, providerID int, groupID string) ([]ResolvedWorkflowNode, error) {
	chain, err := r.client.QuotaResetApprovalChain.Query().
		Where(
			quotaresetapprovalchain.ProviderIDEQ(providerID),
			quotaresetapprovalchain.GroupIDEQ(groupID),
			quotaresetapprovalchain.EnabledEQ(true),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return []ResolvedWorkflowNode{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load workflow approval chain: %w", err)
	}
	chainRows, err := r.client.QuotaResetApprovalChainNode.Query().
		Where(quotaresetapprovalchainnode.ChainIDEQ(chain.ID)).
		Order(ent.Asc(quotaresetapprovalchainnode.FieldPosition), ent.Asc(quotaresetapprovalchainnode.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load workflow approval chain nodes: %w", err)
	}
	nodes := make([]ResolvedWorkflowNode, 0, len(chainRows))
	for _, row := range chainRows {
		departmentID := strings.TrimSpace(row.DepartmentExternalID)
		approvers := resolvedApproversForDepartment(usableConfiguredApprovers(facts.configsByDepartment[departmentID], facts, facts.requester.ID), departmentID, "configured")
		nodes = append(nodes, ResolvedWorkflowNode{
			NodeType: "configured_department",
			Label:    row.DepartmentDisplayPath,
			Departments: []DepartmentSnapshot{{
				ExternalID:  departmentID,
				DisplayPath: row.DepartmentDisplayPath,
				Resolution:  "configured",
			}},
			Approvers:             approvers,
			InitialStatus:         "queued",
			AdminFallbackRequired: len(approvers) == 0,
		})
	}
	return nodes, nil
}

func workflowRequesterDepartmentIDs(member *ent.DirectoryMember, memberships []*ent.DirectoryMemberDepartment) []string {
	if member == nil {
		return []string{}
	}
	ids := make([]string, 0)
	for _, membership := range memberships {
		if membership != nil && membership.DirectoryMemberID == member.ID {
			ids = appendUniqueSortedString(ids, membership.DepartmentExternalID)
		}
	}
	if len(ids) == 0 {
		ids = appendUniqueSortedString(ids, member.DepartmentExternalID)
	}
	return ids
}

func appendUniqueSortedString(values []string, candidate string) []string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return values
	}
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	values = append(values, candidate)
	sort.Strings(values)
	return values
}

func appendUniqueSortedInt(values []int, candidate int) []int {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	values = append(values, candidate)
	sort.Ints(values)
	return values
}

func usableConfiguredApprovers(userIDs []int, facts *workflowDirectoryFacts, requesterUserID int) []workflowApproverCandidate {
	candidates := make([]workflowApproverCandidate, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID == requesterUserID {
			continue
		}
		user := facts.usersByID[userID]
		member := facts.activeMembersByUserID[userID]
		if user == nil || user.RelayDisabledAt != nil || user.TokenValidAfter != nil {
			continue
		}
		if len(facts.membersByUserID[userID]) > 0 && member == nil {
			continue
		}
		candidates = append(candidates, workflowApproverCandidate{user: user, member: member})
	}
	return candidates
}

func usableRepresentativeApprovers(members []*ent.DirectoryMember, facts *workflowDirectoryFacts, requesterUserID int) []workflowApproverCandidate {
	candidates := make([]workflowApproverCandidate, 0, len(members))
	seen := make(map[int]struct{}, len(members))
	for _, member := range members {
		if member == nil || !workflowMemberIsActive(member) {
			continue
		}
		user := workflowMemberUser(member, facts.usersByID, facts.usersByEmail)
		if user == nil || user.ID == requesterUserID || user.RelayDisabledAt != nil || user.TokenValidAfter != nil {
			continue
		}
		if _, ok := seen[user.ID]; ok {
			continue
		}
		seen[user.ID] = struct{}{}
		candidates = append(candidates, workflowApproverCandidate{user: user, member: member})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].user.ID < candidates[j].user.ID })
	return candidates
}

func resolvedApproversForDepartment(candidates []workflowApproverCandidate, departmentID, resolution string) []ResolvedNodeApprover {
	approvers := make([]ResolvedNodeApprover, 0, len(candidates))
	mergeResolvedApprovers(&approvers, candidates, departmentID, resolution)
	return approvers
}

func mergeResolvedApprovers(approvers *[]ResolvedNodeApprover, candidates []workflowApproverCandidate, departmentID, resolution string) {
	for _, candidate := range candidates {
		if candidate.user == nil {
			continue
		}
		index := -1
		for i := range *approvers {
			if (*approvers)[i].UserID == candidate.user.ID {
				index = i
				break
			}
		}
		if index == -1 {
			*approvers = append(*approvers, ResolvedNodeApprover{
				UserID:                      candidate.user.ID,
				DisplayName:                 firstNonEmptyQuotaReset(requesterMemberDisplayName(candidate.member), candidate.user.Username),
				Email:                       candidate.user.Email,
				Source:                      resolution,
				SourceDepartmentExternalIDs: []string{},
				NotificationIDs:             notificationIDsForMember(candidate.member),
			})
			index = len(*approvers) - 1
		}
		(*approvers)[index].SourceDepartmentExternalIDs = appendUniqueSortedString((*approvers)[index].SourceDepartmentExternalIDs, departmentID)
		if resolution == "configured" {
			(*approvers)[index].Source = "configured"
		}
	}
	sort.SliceStable(*approvers, func(i, j int) bool { return (*approvers)[i].UserID < (*approvers)[j].UserID })
}

func workflowMemberIsActive(member *ent.DirectoryMember) bool {
	return member != nil && strings.EqualFold(strings.TrimSpace(member.Status), "active")
}

func workflowMemberUser(member *ent.DirectoryMember, usersByID map[int]*ent.User, usersByEmail map[string]*ent.User) *ent.User {
	if member == nil {
		return nil
	}
	if member.MatchedUserID != nil && *member.MatchedUserID > 0 {
		return usersByID[*member.MatchedUserID]
	}
	if usersByEmail == nil {
		return nil
	}
	return usersByEmail[strings.ToLower(strings.TrimSpace(member.EmailNormalized))]
}

func requesterMemberDisplayName(member *ent.DirectoryMember) string {
	if member == nil {
		return ""
	}
	return strings.TrimSpace(member.DisplayName)
}

func firstNonEmptyQuotaReset(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func notificationIDsForMember(member *ent.DirectoryMember) map[string]string {
	ids := make(map[string]string)
	if member == nil {
		return ids
	}
	if value, ok := member.Metadata["wecom_userid"].(string); ok && strings.TrimSpace(value) != "" {
		ids["wecom"] = strings.TrimSpace(value)
		return ids
	}
	if externalID := strings.TrimSpace(member.ExternalID); externalID != "" {
		ids["wecom"] = externalID
	}
	return ids
}

func departmentRepresentativeExternalIDs(metadata map[string]any) []string {
	if metadata == nil {
		return []string{}
	}
	var values []any
	switch raw := metadata[departmentRepresentativeIDsMetadataKey].(type) {
	case []any:
		values = raw
	case []string:
		values = make([]any, len(raw))
		for i := range raw {
			values[i] = raw[i]
		}
	case string:
		values = []any{raw}
	}
	ids := make([]string, 0, len(values))
	for _, value := range values {
		if id, ok := value.(string); ok {
			ids = appendUniqueSortedString(ids, id)
		}
	}
	return ids
}
