package quotareset

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/quotaresetapprovalchain"
	"github.com/ai-efficiency/backend/ent/quotaresetapprovalchainnode"
	"github.com/ai-efficiency/backend/ent/quotaresetapproverconfig"
	entrelayprovider "github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/directorytree"
	"github.com/ai-efficiency/backend/internal/relay"
)

type platformGroupLister interface {
	ListPlatformGroups(context.Context) ([]relay.Group, error)
}

func (s *Service) ListApproverCandidates(ctx context.Context, params ApproverCandidateParams) (*ApproverCandidateListResponse, error) {
	page, pageSize := normalizePage(params.Page, params.PageSize)
	sourceID, err := resolveCandidateSourceID(ctx, s.client, params.SourceID)
	if err != nil {
		return nil, err
	}
	members, err := s.client.DirectoryMember.Query().
		Where(directorymember.SourceIDEQ(sourceID)).
		Order(ent.Asc(directorymember.FieldDisplayName), ent.Asc(directorymember.FieldEmailNormalized)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list approver candidate members: %w", err)
	}
	users, err := s.client.User.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list approver candidate users: %w", err)
	}
	memberships, departments, err := loadCandidateOrganizationFacts(ctx, s.client, sourceID)
	if err != nil {
		return nil, err
	}
	items := matchApproverCandidates(params.Query, members, memberships, departments, users)
	total := len(items)
	start := min((page-1)*pageSize, total)
	end := min(start+pageSize, total)
	return &ApproverCandidateListResponse{Items: items[start:end], Page: page, PageSize: pageSize, Total: total}, nil
}

func resolveCandidateSourceID(ctx context.Context, client *ent.Client, requested int) (int, error) {
	if requested > 0 {
		return requested, nil
	}
	if requested < 0 {
		return 0, fmt.Errorf("%w: source_id must not be negative", ErrInvalidApproverConfig)
	}
	sourceID, ok, err := directorysync.CurrentSourceID(ctx, client)
	if err != nil {
		return 0, fmt.Errorf("resolve current approver candidate source: %w", err)
	}
	if !ok {
		return 0, ErrDirectoryUnavailable
	}
	return sourceID, nil
}

func loadCandidateOrganizationFacts(ctx context.Context, client *ent.Client, sourceID int) ([]*ent.DirectoryMemberDepartment, map[string]*ent.DirectoryDepartment, error) {
	memberships, err := client.DirectoryMemberDepartment.Query().
		Where(directorymemberdepartment.SourceIDEQ(sourceID)).
		Order(ent.Asc(directorymemberdepartment.FieldDirectoryMemberID), ent.Asc(directorymemberdepartment.FieldDepartmentExternalID)).
		All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list approver candidate memberships: %w", err)
	}
	departmentRows, err := client.DirectoryDepartment.Query().
		Where(directorydepartment.SourceIDEQ(sourceID)).
		Order(ent.Asc(directorydepartment.FieldPath), ent.Asc(directorydepartment.FieldName), ent.Asc(directorydepartment.FieldExternalID)).
		All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list approver candidate departments: %w", err)
	}
	departments := make(map[string]*ent.DirectoryDepartment, len(departmentRows))
	for _, department := range departmentRows {
		if department == nil {
			continue
		}
		externalID := strings.TrimSpace(department.ExternalID)
		if externalID != "" {
			departments[externalID] = department
		}
	}
	return memberships, departments, nil
}

type approverCandidateMatch struct {
	candidate   ApproverCandidate
	paths       map[string]struct{}
	searchTerms map[string]struct{}
}

func matchApproverCandidates(query string, members []*ent.DirectoryMember, memberships []*ent.DirectoryMemberDepartment, departments map[string]*ent.DirectoryDepartment, users []*ent.User) []ApproverCandidate {
	usersByID := make(map[int]*ent.User, len(users))
	usersByEmail := make(map[string]*ent.User, len(users))
	for _, user := range users {
		if user == nil || user.RelayDisabledAt != nil {
			continue
		}
		usersByID[user.ID] = user
		email := normalizeCandidateValue(user.Email)
		if email == "" {
			continue
		}
		if existing := usersByEmail[email]; existing == nil || user.ID < existing.ID {
			usersByEmail[email] = user
		}
	}

	departmentIDsByMember := make(map[int][]string)
	for _, membership := range memberships {
		if membership == nil {
			continue
		}
		departmentID := strings.TrimSpace(membership.DepartmentExternalID)
		if departmentID != "" {
			departmentIDsByMember[membership.DirectoryMemberID] = append(departmentIDsByMember[membership.DirectoryMemberID], departmentID)
		}
	}
	departmentRows := make([]*ent.DirectoryDepartment, 0, len(departments))
	for _, department := range departments {
		departmentRows = append(departmentRows, department)
	}
	departmentTree := directorytree.New(departmentRows)

	matches := make(map[int]*approverCandidateMatch)
	for _, member := range members {
		if member == nil || !strings.EqualFold(strings.TrimSpace(member.Status), "active") {
			continue
		}
		var user *ent.User
		if member.MatchedUserID != nil && *member.MatchedUserID > 0 {
			user = usersByID[*member.MatchedUserID]
		} else {
			user = usersByEmail[normalizeCandidateValue(member.EmailNormalized)]
		}
		if user == nil {
			continue
		}

		candidate := ApproverCandidate{
			UserID:                    user.ID,
			Username:                  strings.TrimSpace(user.Username),
			Email:                     strings.TrimSpace(user.Email),
			DisplayName:               strings.TrimSpace(member.DisplayName),
			DirectoryMemberExternalID: strings.TrimSpace(member.ExternalID),
			WeComMentionAvailable:     candidateHasWeComMention(member),
		}
		match := matches[user.ID]
		if match == nil {
			match = &approverCandidateMatch{
				candidate:   candidate,
				paths:       map[string]struct{}{},
				searchTerms: map[string]struct{}{},
			}
			matches[user.ID] = match
		} else if candidateIdentityLess(candidate, match.candidate) {
			candidate.WeComMentionAvailable = candidate.WeComMentionAvailable || match.candidate.WeComMentionAvailable
			match.candidate = candidate
		} else if candidate.WeComMentionAvailable {
			match.candidate.WeComMentionAvailable = true
		}

		for _, value := range []string{member.DisplayName, member.EmailNormalized, user.Username, user.Email} {
			if normalized := normalizeCandidateValue(value); normalized != "" {
				match.searchTerms[normalized] = struct{}{}
			}
		}
		departmentIDs := append([]string(nil), departmentIDsByMember[member.ID]...)
		departmentIDs = append(departmentIDs, member.DepartmentExternalID)
		for _, departmentID := range departmentIDs {
			department := departments[strings.TrimSpace(departmentID)]
			if path := candidateDepartmentPath(departmentTree, department); path != "" {
				match.paths[path] = struct{}{}
			}
		}
	}

	normalizedQuery := normalizeCandidateValue(query)
	items := make([]ApproverCandidate, 0, len(matches))
	for _, match := range matches {
		if normalizedQuery != "" && !candidateSearchMatches(match.searchTerms, normalizedQuery) {
			continue
		}
		match.candidate.DepartmentPaths = make([]string, 0, len(match.paths))
		for path := range match.paths {
			match.candidate.DepartmentPaths = append(match.candidate.DepartmentPaths, path)
		}
		sort.Strings(match.candidate.DepartmentPaths)
		items = append(items, match.candidate)
	}
	sort.SliceStable(items, func(i, j int) bool {
		left := candidateSortKey(items[i])
		right := candidateSortKey(items[j])
		if left != right {
			return left < right
		}
		return items[i].UserID < items[j].UserID
	})
	return items
}

func (s *Service) validateApproverConfigs(ctx context.Context, sourceID int, items []ApproverConfigInput) error {
	if len(items) == 0 {
		return nil
	}
	members, err := s.client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(sourceID)).All(ctx)
	if err != nil {
		return fmt.Errorf("list approver config candidate members: %w", err)
	}
	users, err := s.client.User.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("list approver config candidate users: %w", err)
	}
	memberships, departments, err := loadCandidateOrganizationFacts(ctx, s.client, sourceID)
	if err != nil {
		return err
	}
	candidates := matchApproverCandidates("", members, memberships, departments, users)
	candidateUserIDs := make(map[int]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidateUserIDs[candidate.UserID] = struct{}{}
	}
	for _, item := range items {
		departmentID := strings.TrimSpace(item.DepartmentExternalID)
		if _, ok := departments[departmentID]; !ok {
			return fmt.Errorf("%w: department %s does not exist in the current directory source", ErrInvalidApproverConfig, departmentID)
		}
		if _, ok := candidateUserIDs[item.ApproverUserID]; !ok {
			return fmt.Errorf("%w: approver_user_id %d is not an active directory-matched user", ErrInvalidApproverConfig, item.ApproverUserID)
		}
	}
	return nil
}

func candidateDepartmentPath(tree *directorytree.Tree, department *ent.DirectoryDepartment) string {
	if department == nil {
		return ""
	}
	if path := strings.TrimSpace(tree.DisplayPath(department.ExternalID)); path != "" {
		return path
	}
	return strings.TrimSpace(department.Name)
}

func candidateHasWeComMention(member *ent.DirectoryMember) bool {
	if member == nil {
		return false
	}
	if value, ok := member.Metadata["wecom_userid"].(string); ok && strings.TrimSpace(value) != "" {
		return true
	}
	return strings.TrimSpace(member.ExternalID) != ""
}

func candidateIdentityLess(left, right ApproverCandidate) bool {
	return candidateIdentityKey(left) < candidateIdentityKey(right)
}

func candidateIdentityKey(candidate ApproverCandidate) string {
	return strings.Join([]string{
		normalizeCandidateValue(candidate.DisplayName),
		normalizeCandidateValue(candidate.DirectoryMemberExternalID),
		normalizeCandidateValue(candidate.Username),
		normalizeCandidateValue(candidate.Email),
	}, "\x00")
}

func candidateSortKey(candidate ApproverCandidate) string {
	displayName := normalizeCandidateValue(candidate.DisplayName)
	if displayName == "" {
		displayName = normalizeCandidateValue(candidate.Username)
	}
	return strings.Join([]string{
		displayName,
		normalizeCandidateValue(candidate.Username),
		normalizeCandidateValue(candidate.Email),
	}, "\x00")
}

func candidateSearchMatches(terms map[string]struct{}, query string) bool {
	for term := range terms {
		if strings.Contains(term, query) {
			return true
		}
	}
	return false
}

func normalizeCandidateValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func (s *Service) ListApprovalChainOptions(ctx context.Context) (*ApprovalChainOptionsResponse, error) {
	response := &ApprovalChainOptionsResponse{
		Groups:      []ApprovalChainGroupOption{},
		Departments: []ApprovalChainDepartmentOption{},
	}
	providers, err := s.client.RelayProvider.Query().
		Where(entrelayprovider.EnabledEQ(true)).
		Order(ent.Asc(entrelayprovider.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled relay providers for approval chains: %w", err)
	}
	if len(providers) > 0 && s.providerResolver == nil {
		return nil, ErrProviderUnsupported
	}
	seenGroups := map[string]struct{}{}
	for _, providerRow := range providers {
		provider, err := s.providerResolver.Resolve(ctx, providerRow.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve relay provider %d for approval chains: %w", providerRow.ID, err)
		}
		lister, ok := provider.(platformGroupLister)
		if !ok {
			return nil, fmt.Errorf("%w: relay provider %d does not list platform groups", ErrProviderUnsupported, providerRow.ID)
		}
		groups, err := lister.ListPlatformGroups(ctx)
		if err != nil {
			return nil, fmt.Errorf("list platform groups for relay provider %d: %w", providerRow.ID, err)
		}
		for _, group := range groups {
			if !strings.EqualFold(strings.TrimSpace(group.SubscriptionType), "subscription") {
				continue
			}
			groupID := strconv.FormatInt(group.ID, 10)
			key := approvalChainGroupKey(providerRow.ID, groupID)
			if _, duplicate := seenGroups[key]; duplicate {
				continue
			}
			seenGroups[key] = struct{}{}
			response.Groups = append(response.Groups, ApprovalChainGroupOption{
				ProviderID: providerRow.ID,
				GroupID:    groupID,
				GroupName:  strings.TrimSpace(group.Name),
				Platform:   strings.TrimSpace(group.Platform),
			})
		}
	}
	sort.SliceStable(response.Groups, func(i, j int) bool {
		if response.Groups[i].ProviderID != response.Groups[j].ProviderID {
			return response.Groups[i].ProviderID < response.Groups[j].ProviderID
		}
		leftName := normalizeCandidateValue(response.Groups[i].GroupName)
		rightName := normalizeCandidateValue(response.Groups[j].GroupName)
		if leftName != rightName {
			return leftName < rightName
		}
		return response.Groups[i].GroupID < response.Groups[j].GroupID
	})

	sourceID, ok, err := directorysync.CurrentSourceID(ctx, s.client)
	if err != nil {
		return nil, fmt.Errorf("resolve current directory source for approval chains: %w", err)
	}
	if !ok {
		return response, nil
	}
	configs, err := s.client.QuotaResetApproverConfig.Query().
		Where(
			quotaresetapproverconfig.DirectorySourceIDEQ(sourceID),
			quotaresetapproverconfig.EnabledEQ(true),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled approver configs for approval chains: %w", err)
	}
	approverCounts := make(map[string]int)
	for _, config := range configs {
		departmentID := strings.TrimSpace(config.DepartmentExternalID)
		if departmentID != "" {
			approverCounts[departmentID]++
		}
	}
	if len(approverCounts) == 0 {
		return response, nil
	}
	departments, err := s.client.DirectoryDepartment.Query().
		Where(directorydepartment.SourceIDEQ(sourceID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list configured departments for approval chains: %w", err)
	}
	departmentTree := directorytree.New(departments)
	for _, department := range departments {
		departmentID := strings.TrimSpace(department.ExternalID)
		approverCount := approverCounts[departmentID]
		if approverCount == 0 {
			continue
		}
		response.Departments = append(response.Departments, ApprovalChainDepartmentOption{
			DirectorySourceID:     sourceID,
			DepartmentExternalID:  departmentID,
			DepartmentDisplayPath: candidateDepartmentPath(departmentTree, department),
			ApproverCount:         approverCount,
		})
	}
	sort.SliceStable(response.Departments, func(i, j int) bool {
		leftPath := normalizeCandidateValue(response.Departments[i].DepartmentDisplayPath)
		rightPath := normalizeCandidateValue(response.Departments[j].DepartmentDisplayPath)
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return response.Departments[i].DepartmentExternalID < response.Departments[j].DepartmentExternalID
	})
	return response, nil
}

func (s *Service) ListApprovalChains(ctx context.Context) (*ApprovalChainListResponse, error) {
	chainRows, err := s.client.QuotaResetApprovalChain.Query().
		Order(
			ent.Asc(quotaresetapprovalchain.FieldProviderID),
			ent.Asc(quotaresetapprovalchain.FieldGroupID),
			ent.Asc(quotaresetapprovalchain.FieldID),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list quota reset approval chains: %w", err)
	}
	nodeRows, err := s.client.QuotaResetApprovalChainNode.Query().
		Order(
			ent.Asc(quotaresetapprovalchainnode.FieldChainID),
			ent.Asc(quotaresetapprovalchainnode.FieldPosition),
			ent.Asc(quotaresetapprovalchainnode.FieldID),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list quota reset approval chain nodes: %w", err)
	}
	nodesByChainID := make(map[int][]ApprovalChainNodeInput)
	for _, node := range nodeRows {
		nodesByChainID[node.ChainID] = append(nodesByChainID[node.ChainID], ApprovalChainNodeInput{
			DirectorySourceID:     node.DirectorySourceID,
			DepartmentExternalID:  node.DepartmentExternalID,
			DepartmentDisplayPath: node.DepartmentDisplayPath,
		})
	}
	items := make([]ApprovalChain, 0, len(chainRows))
	for _, chain := range chainRows {
		nodes := nodesByChainID[chain.ID]
		if nodes == nil {
			nodes = []ApprovalChainNodeInput{}
		}
		items = append(items, ApprovalChain{
			ID:         chain.ID,
			ProviderID: chain.ProviderID,
			GroupID:    chain.GroupID,
			GroupName:  chain.GroupName,
			Enabled:    chain.Enabled,
			Nodes:      nodes,
		})
	}
	return &ApprovalChainListResponse{Items: items}, nil
}

func (s *Service) SaveApprovalChains(ctx context.Context, input SaveApprovalChainsInput) (*ApprovalChainListResponse, error) {
	items := normalizeApprovalChainInputs(input.Items)
	allowedGroups := map[string]ApprovalChainGroupOption{}
	allowedDepartments := map[string]ApprovalChainDepartmentOption{}
	if len(items) > 0 {
		options, err := s.ListApprovalChainOptions(ctx)
		if err != nil {
			return nil, err
		}
		for _, group := range options.Groups {
			allowedGroups[approvalChainGroupKey(group.ProviderID, group.GroupID)] = group
		}
		for _, department := range options.Departments {
			allowedDepartments[approvalChainDepartmentKey(department.DirectorySourceID, department.DepartmentExternalID)] = department
		}
	}
	seenGroups := map[string]struct{}{}
	for itemIndex := range items {
		item := &items[itemIndex]
		groupKey := approvalChainGroupKey(item.ProviderID, item.GroupID)
		group, ok := allowedGroups[groupKey]
		if !ok {
			return nil, fmt.Errorf("%w: unknown subscription group %s", ErrInvalidApproverConfig, groupKey)
		}
		if _, duplicate := seenGroups[groupKey]; duplicate {
			return nil, fmt.Errorf("%w: duplicate subscription group %s", ErrInvalidApproverConfig, groupKey)
		}
		seenGroups[groupKey] = struct{}{}
		if item.GroupName == "" {
			item.GroupName = group.GroupName
		}
		seenDepartments := map[string]struct{}{}
		for nodeIndex := range item.Nodes {
			node := &item.Nodes[nodeIndex]
			nodeKey := approvalChainDepartmentKey(node.DirectorySourceID, node.DepartmentExternalID)
			department, ok := allowedDepartments[nodeKey]
			if !ok {
				return nil, fmt.Errorf("%w: department %s has no enabled approver config", ErrInvalidApproverConfig, nodeKey)
			}
			if _, duplicate := seenDepartments[nodeKey]; duplicate {
				return nil, fmt.Errorf("%w: duplicate department %s", ErrInvalidApproverConfig, nodeKey)
			}
			seenDepartments[nodeKey] = struct{}{}
			if node.DepartmentDisplayPath == "" {
				node.DepartmentDisplayPath = department.DepartmentDisplayPath
			}
		}
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin quota reset approval chain tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.QuotaResetApprovalChainNode.Delete().Exec(ctx); err != nil {
		return nil, fmt.Errorf("delete quota reset approval chain nodes: %w", err)
	}
	if _, err := tx.QuotaResetApprovalChain.Delete().Exec(ctx); err != nil {
		return nil, fmt.Errorf("delete quota reset approval chains: %w", err)
	}
	for _, item := range items {
		chain, err := tx.QuotaResetApprovalChain.Create().
			SetProviderID(item.ProviderID).
			SetGroupID(item.GroupID).
			SetGroupName(item.GroupName).
			SetEnabled(item.Enabled).
			SetCreatedByUserID(input.ActorUserID).
			SetUpdatedByUserID(input.ActorUserID).
			Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("create quota reset approval chain: %w", err)
		}
		for position, node := range item.Nodes {
			if _, err := tx.QuotaResetApprovalChainNode.Create().
				SetChainID(chain.ID).
				SetPosition(position).
				SetDirectorySourceID(node.DirectorySourceID).
				SetDepartmentExternalID(node.DepartmentExternalID).
				SetDepartmentDisplayPath(node.DepartmentDisplayPath).
				Save(ctx); err != nil {
				return nil, fmt.Errorf("create quota reset approval chain node: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit quota reset approval chains: %w", err)
	}
	return s.ListApprovalChains(ctx)
}

func (s *Service) projectApproverConfigsAfterSave(ctx context.Context, sourceID int, items []ApproverConfigInput, replaceAll bool) ([]ApproverConfigInput, error) {
	if replaceAll {
		return append([]ApproverConfigInput(nil), items...), nil
	}
	replacedDepartments := make(map[string]struct{})
	for _, departmentID := range approverConfigDepartmentIDs(items) {
		replacedDepartments[departmentID] = struct{}{}
	}
	existingRows, err := s.client.QuotaResetApproverConfig.Query().
		Where(quotaresetapproverconfig.DirectorySourceIDEQ(sourceID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list existing approver configs for replacement projection: %w", err)
	}
	finalItems := make([]ApproverConfigInput, 0, len(existingRows)+len(items))
	for _, row := range existingRows {
		if _, replaced := replacedDepartments[strings.TrimSpace(row.DepartmentExternalID)]; replaced {
			continue
		}
		finalItems = append(finalItems, ApproverConfigInput{
			DepartmentExternalID:  row.DepartmentExternalID,
			DepartmentDisplayPath: row.DepartmentDisplayPath,
			ApproverUserID:        row.ApproverUserID,
			Enabled:               row.Enabled,
		})
	}
	return append(finalItems, items...), nil
}

func (s *Service) validateChainReferencesAfterApproverSave(ctx context.Context, sourceID int, finalItems []ApproverConfigInput) error {
	remaining := map[string]struct{}{}
	for _, item := range finalItems {
		if item.Enabled {
			remaining[strings.TrimSpace(item.DepartmentExternalID)] = struct{}{}
		}
	}
	nodes, err := s.client.QuotaResetApprovalChainNode.Query().
		Where(quotaresetapprovalchainnode.DirectorySourceIDEQ(sourceID)).
		All(ctx)
	if err != nil {
		return fmt.Errorf("list approval chain references: %w", err)
	}
	for _, node := range nodes {
		if _, ok := remaining[strings.TrimSpace(node.DepartmentExternalID)]; !ok {
			return fmt.Errorf("%w: department %s is used by an approval chain", ErrApproverConfigReferenced, node.DepartmentDisplayPath)
		}
	}
	return nil
}

func normalizeApprovalChainInputs(items []ApprovalChainInput) []ApprovalChainInput {
	out := make([]ApprovalChainInput, len(items))
	for i, item := range items {
		item.GroupID = strings.TrimSpace(item.GroupID)
		item.GroupName = strings.TrimSpace(item.GroupName)
		item.Nodes = append([]ApprovalChainNodeInput(nil), item.Nodes...)
		for j := range item.Nodes {
			item.Nodes[j].DepartmentExternalID = strings.TrimSpace(item.Nodes[j].DepartmentExternalID)
			item.Nodes[j].DepartmentDisplayPath = strings.TrimSpace(item.Nodes[j].DepartmentDisplayPath)
		}
		out[i] = item
	}
	return out
}

func approvalChainGroupKey(providerID int, groupID string) string {
	return fmt.Sprintf("%d/%s", providerID, strings.TrimSpace(groupID))
}

func approvalChainDepartmentKey(sourceID int, departmentID string) string {
	return fmt.Sprintf("%d/%s", sourceID, strings.TrimSpace(departmentID))
}
