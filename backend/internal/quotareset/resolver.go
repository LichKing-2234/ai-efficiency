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
	"github.com/ai-efficiency/backend/ent/quotaresetapproverconfig"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/directorytree"
)

type ApproverResolver struct {
	client *ent.Client
}

func NewApproverResolver(client *ent.Client) *ApproverResolver {
	return &ApproverResolver{client: client}
}

func (r *ApproverResolver) Resolve(ctx context.Context, requesterUserID int) (*ApproverResolution, error) {
	sourceID, ok, err := directorysync.CurrentSourceID(ctx, r.client)
	if err != nil {
		return nil, fmt.Errorf("current directory source: %w", err)
	}
	if !ok {
		return &ApproverResolution{}, nil
	}
	requester, err := r.client.User.Get(ctx, requesterUserID)
	if err != nil {
		return nil, fmt.Errorf("get requester: %w", err)
	}
	departments, err := r.client.DirectoryDepartment.Query().Where(directorydepartment.SourceIDEQ(sourceID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query departments: %w", err)
	}
	members, err := r.client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(sourceID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query members: %w", err)
	}
	memberships, err := r.client.DirectoryMemberDepartment.Query().Where(directorymemberdepartment.SourceIDEQ(sourceID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query memberships: %w", err)
	}
	configs, err := r.client.QuotaResetApproverConfig.Query().
		Where(quotaresetapproverconfig.DirectorySourceIDEQ(sourceID), quotaresetapproverconfig.Enabled(true)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query approver configs: %w", err)
	}

	tree := directorytree.New(departments)
	departmentsByID := make(map[string]*ent.DirectoryDepartment, len(departments))
	for _, department := range departments {
		if department != nil {
			departmentsByID[department.ExternalID] = department
		}
	}
	member := findRequesterDirectoryMember(requester, members)
	if member == nil {
		return &ApproverResolution{}, nil
	}
	departmentIDs := requesterDepartmentIDs(member, memberships)
	configsByDepartment := approverConfigsByDepartment(configs)
	seenApprover := map[int]struct{}{}
	var approverIDs []int
	var paths []DepartmentPathEvidence
	for _, departmentID := range departmentIDs {
		evidence, ids := resolveDepartmentPath(tree, departmentsByID, departmentID, configsByDepartment, requesterUserID)
		paths = append(paths, evidence)
		for _, id := range ids {
			if _, ok := seenApprover[id]; ok {
				continue
			}
			seenApprover[id] = struct{}{}
			approverIDs = append(approverIDs, id)
		}
	}
	sort.Ints(approverIDs)
	return &ApproverResolution{ApproverUserIDs: approverIDs, Paths: paths}, nil
}

func findRequesterDirectoryMember(requester *ent.User, members []*ent.DirectoryMember) *ent.DirectoryMember {
	if requester == nil {
		return nil
	}
	for _, member := range members {
		if member != nil && member.MatchedUserID != nil && *member.MatchedUserID == requester.ID {
			return member
		}
	}
	requesterEmail := strings.ToLower(strings.TrimSpace(requester.Email))
	for _, member := range members {
		if member != nil && strings.ToLower(strings.TrimSpace(member.EmailNormalized)) == requesterEmail {
			return member
		}
	}
	return nil
}

func requesterDepartmentIDs(member *ent.DirectoryMember, memberships []*ent.DirectoryMemberDepartment) []string {
	if member == nil {
		return nil
	}
	seen := map[string]struct{}{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id != "" {
			seen[id] = struct{}{}
		}
	}
	add(member.DepartmentExternalID)
	for _, membership := range memberships {
		if membership != nil && membership.DirectoryMemberID == member.ID {
			add(membership.DepartmentExternalID)
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func approverConfigsByDepartment(configs []*ent.QuotaResetApproverConfig) map[string][]int {
	result := make(map[string][]int)
	seen := make(map[string]map[int]struct{})
	for _, config := range configs {
		if config == nil || !config.Enabled {
			continue
		}
		departmentID := strings.TrimSpace(config.DepartmentExternalID)
		if departmentID == "" {
			continue
		}
		if seen[departmentID] == nil {
			seen[departmentID] = map[int]struct{}{}
		}
		if _, ok := seen[departmentID][config.ApproverUserID]; ok {
			continue
		}
		seen[departmentID][config.ApproverUserID] = struct{}{}
		result[departmentID] = append(result[departmentID], config.ApproverUserID)
	}
	for departmentID := range result {
		sort.Ints(result[departmentID])
	}
	return result
}

func resolveDepartmentPath(tree *directorytree.Tree, departmentsByID map[string]*ent.DirectoryDepartment, startDepartmentID string, configsByDepartment map[string][]int, requesterUserID int) (DepartmentPathEvidence, []int) {
	evidence := DepartmentPathEvidence{
		StartDepartmentExternalID: strings.TrimSpace(startDepartmentID),
		Resolution:                "no_config_found",
	}
	currentID := evidence.StartDepartmentExternalID
	visited := map[string]struct{}{}
	for currentID != "" {
		if _, ok := visited[currentID]; ok {
			break
		}
		visited[currentID] = struct{}{}
		evidence.Path = append(evidence.Path, DepartmentPathNode{
			ExternalID:  currentID,
			DisplayPath: tree.DisplayPath(currentID),
		})
		if approverIDs, ok := configsByDepartment[currentID]; ok {
			filtered := make([]int, 0, len(approverIDs))
			for _, id := range approverIDs {
				if id != requesterUserID {
					filtered = append(filtered, id)
				}
			}
			evidence.MatchedDepartmentExternalID = currentID
			evidence.MatchedApproverUserIDs = filtered
			evidence.Resolution = "matched"
			return evidence, filtered
		}
		department := departmentsByID[currentID]
		if department == nil {
			break
		}
		currentID = directorytree.ParentExternalID(department)
	}
	return evidence, nil
}
