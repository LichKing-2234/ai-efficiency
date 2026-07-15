package quotareset

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/quotaresetapprovalchain"
	"github.com/ai-efficiency/backend/ent/quotaresetapprovalchainnode"
	"github.com/ai-efficiency/backend/ent/quotaresetapproverconfig"
	entrelayprovider "github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/ent/systemsetting"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/directorytree"
	"github.com/ai-efficiency/backend/internal/relay"
)

const (
	quotaResetApprovalConfigurationLockKey = "quota_reset_approval_configuration"
	approvalChainGroupDiscoveryTimeout     = 10 * time.Second
	approverCandidateSnapshotReadAttempts  = 2
)

type approvalConfigurationLockHooksContextKey struct{}

type approvalConfigurationLockHooks struct {
	beforeLock func()
	afterLock  func()
}

type platformGroupLister interface {
	ListPlatformGroups(context.Context) ([]relay.Group, error)
}

func (s *Service) beginApprovalConfigurationTx(ctx context.Context) (*ent.Tx, error) {
	if err := s.ensureApprovalConfigurationLockRow(ctx); err != nil {
		return nil, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin quota reset approval configuration tx: %w", err)
	}
	hooks, _ := ctx.Value(approvalConfigurationLockHooksContextKey{}).(approvalConfigurationLockHooks)
	if hooks.beforeLock != nil {
		hooks.beforeLock()
	}
	if err := lockApprovalConfiguration(ctx, tx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if hooks.afterLock != nil {
		hooks.afterLock()
	}
	return tx, nil
}

func (s *Service) ensureApprovalConfigurationLockRow(ctx context.Context) error {
	exists, err := s.client.SystemSetting.Query().
		Where(systemsetting.KeyEQ(quotaResetApprovalConfigurationLockKey)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check quota reset approval configuration lock: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := s.client.SystemSetting.Create().
		SetKey(quotaResetApprovalConfigurationLockKey).
		SetValue(quotaResetApprovalConfigurationLockKey).
		Save(ctx); err != nil {
		if ent.IsConstraintError(err) {
			return nil
		}
		return fmt.Errorf("ensure quota reset approval configuration lock: %w", err)
	}
	return nil
}

func lockApprovalConfiguration(ctx context.Context, tx *ent.Tx) error {
	affected, err := tx.SystemSetting.Update().
		Where(systemsetting.KeyEQ(quotaResetApprovalConfigurationLockKey)).
		SetValue(quotaResetApprovalConfigurationLockKey).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("lock quota reset approval configuration: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("lock quota reset approval configuration: affected %d rows", affected)
	}
	return nil
}

func (s *Service) ListApproverCandidates(ctx context.Context, params ApproverCandidateParams) (*ApproverCandidateListResponse, error) {
	page, pageSize := normalizePage(params.Page, params.PageSize)
	for attempt := 0; attempt < approverCandidateSnapshotReadAttempts; attempt++ {
		snapshot, err := requireCandidateSnapshot(ctx, s.client, params.SourceID)
		if err != nil {
			return nil, err
		}
		members, err := s.client.DirectoryMember.Query().
			Where(
				directorymember.SourceIDEQ(snapshot.SourceID),
				directorymember.LastSeenRunIDEQ(snapshot.RunID),
			).
			Order(ent.Asc(directorymember.FieldDisplayName), ent.Asc(directorymember.FieldEmailNormalized)).
			All(ctx)
		if err != nil {
			return nil, fmt.Errorf("list approver candidate members: %w", err)
		}
		users, err := s.client.User.Query().All(ctx)
		if err != nil {
			return nil, fmt.Errorf("list approver candidate users: %w", err)
		}
		memberships, departments, err := loadCandidateOrganizationFacts(ctx, s.client, snapshot)
		if err != nil {
			return nil, err
		}
		latest, err := requireCandidateSnapshot(ctx, s.client, params.SourceID)
		if err != nil {
			return nil, err
		}
		if latest != snapshot {
			continue
		}

		items := matchApproverCandidates(params.Query, members, memberships, departments, users)
		total := len(items)
		if total == 0 || (page > 1 && page-1 > (total-1)/pageSize) {
			return &ApproverCandidateListResponse{Items: []ApproverCandidate{}, Page: page, PageSize: pageSize, Total: total}, nil
		}
		start := (page - 1) * pageSize
		end := total
		if pageSize < total-start {
			end = start + pageSize
		}
		return &ApproverCandidateListResponse{Items: items[start:end], Page: page, PageSize: pageSize, Total: total}, nil
	}
	return nil, fmt.Errorf("%w: current directory snapshot changed during candidate lookup", ErrDirectoryUnavailable)
}

type approverCandidateSnapshot struct {
	SourceID int
	RunID    int
}

func requireCandidateSnapshot(ctx context.Context, client *ent.Client, requested int) (approverCandidateSnapshot, error) {
	sourceID, err := requireCandidateSourceID(ctx, client, requested)
	if err != nil {
		return approverCandidateSnapshot{}, err
	}
	source, err := client.DirectorySource.Get(ctx, sourceID)
	if ent.IsNotFound(err) {
		return approverCandidateSnapshot{}, fmt.Errorf("%w: current directory source %d is unavailable", ErrDirectoryUnavailable, sourceID)
	}
	if err != nil {
		return approverCandidateSnapshot{}, fmt.Errorf("load current approver candidate source: %w", err)
	}
	if source.LastSuccessfulRunID == nil || *source.LastSuccessfulRunID <= 0 {
		return approverCandidateSnapshot{}, fmt.Errorf("%w: current directory source %d has no successful snapshot", ErrDirectoryUnavailable, sourceID)
	}
	return approverCandidateSnapshot{SourceID: sourceID, RunID: *source.LastSuccessfulRunID}, nil
}

func requireCandidateSourceID(ctx context.Context, client *ent.Client, requested int) (int, error) {
	if requested < 0 {
		return 0, fmt.Errorf("%w: source_id must not be negative", ErrInvalidApproverConfig)
	}
	if requested == 0 {
		return 0, fmt.Errorf("%w: source_id is required", ErrInvalidApproverConfig)
	}
	currentSourceID, ok, err := directorysync.CurrentSourceID(ctx, client)
	if err != nil {
		return 0, fmt.Errorf("resolve current approver candidate source: %w", err)
	}
	if !ok || currentSourceID != requested {
		return 0, fmt.Errorf("%w: source_id %d is not the current synchronized source", ErrDirectoryUnavailable, requested)
	}
	return currentSourceID, nil
}

func loadCandidateOrganizationFacts(ctx context.Context, client *ent.Client, snapshot approverCandidateSnapshot) ([]*ent.DirectoryMemberDepartment, map[string]*ent.DirectoryDepartment, error) {
	memberships, err := client.DirectoryMemberDepartment.Query().
		Where(
			directorymemberdepartment.SourceIDEQ(snapshot.SourceID),
			directorymemberdepartment.LastSeenRunIDEQ(snapshot.RunID),
		).
		Order(ent.Asc(directorymemberdepartment.FieldDirectoryMemberID), ent.Asc(directorymemberdepartment.FieldDepartmentExternalID)).
		All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list approver candidate memberships: %w", err)
	}
	departmentRows, err := client.DirectoryDepartment.Query().
		Where(
			directorydepartment.SourceIDEQ(snapshot.SourceID),
			directorydepartment.LastSeenRunIDEQ(snapshot.RunID),
		).
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
	user        *ent.User
	member      *ent.DirectoryMember
	paths       map[string]struct{}
	searchTerms map[string]struct{}
}

func matchApproverCandidates(query string, members []*ent.DirectoryMember, memberships []*ent.DirectoryMemberDepartment, departments map[string]*ent.DirectoryDepartment, users []*ent.User) []ApproverCandidate {
	usersByID := make(map[int]*ent.User, len(users))
	usersByEmail := make(map[string]*ent.User, len(users))
	for _, user := range users {
		if user == nil {
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
		user := directoryMemberUser(member, usersByID, usersByEmail)
		if !directoryApproverIsCurrentlyUsable(user, member) {
			continue
		}

		match := matches[user.ID]
		if match == nil {
			match = &approverCandidateMatch{
				user:        user,
				paths:       map[string]struct{}{},
				searchTerms: map[string]struct{}{},
			}
			matches[user.ID] = match
		}
		match.member = canonicalDirectoryMember(match.member, member)

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
		if match.user == nil || match.member == nil {
			continue
		}
		candidate := ApproverCandidate{
			UserID:                    match.user.ID,
			Username:                  strings.TrimSpace(match.user.Username),
			Email:                     strings.TrimSpace(match.user.Email),
			DisplayName:               strings.TrimSpace(match.member.DisplayName),
			DirectoryMemberExternalID: strings.TrimSpace(match.member.ExternalID),
			DepartmentPaths:           make([]string, 0, len(match.paths)),
			WeComMentionAvailable:     candidateHasWeComMention(match.member),
		}
		for path := range match.paths {
			candidate.DepartmentPaths = append(candidate.DepartmentPaths, path)
		}
		sort.Strings(candidate.DepartmentPaths)
		items = append(items, candidate)
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
	snapshot, err := requireCandidateSnapshot(ctx, s.client, sourceID)
	if err != nil {
		return err
	}
	members, err := s.client.DirectoryMember.Query().Where(
		directorymember.SourceIDEQ(snapshot.SourceID),
		directorymember.LastSeenRunIDEQ(snapshot.RunID),
	).All(ctx)
	if err != nil {
		return fmt.Errorf("list approver config candidate members: %w", err)
	}
	users, err := s.client.User.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("list approver config candidate users: %w", err)
	}
	memberships, departments, err := loadCandidateOrganizationFacts(ctx, s.client, snapshot)
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
	return validWeComMentionUserID(notificationIDsForMember(member)["wecom"])
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
	groups, err := s.discoverApprovalChainGroups(ctx)
	if err != nil {
		return nil, err
	}
	departments, err := listApprovalChainDepartmentOptions(ctx, s.client)
	if err != nil {
		return nil, err
	}
	return &ApprovalChainOptionsResponse{Groups: groups, Departments: departments}, nil
}

func (s *Service) discoverApprovalChainGroups(ctx context.Context) ([]ApprovalChainGroupOption, error) {
	discoveryCtx, cancel := context.WithTimeout(ctx, approvalChainGroupDiscoveryTimeout)
	defer cancel()
	providers, err := s.client.RelayProvider.Query().
		Where(entrelayprovider.EnabledEQ(true)).
		Order(ent.Asc(entrelayprovider.FieldID)).
		All(discoveryCtx)
	if err != nil {
		return nil, fmt.Errorf("list enabled relay providers for approval chains: %w", err)
	}
	if len(providers) > 0 && s.providerResolver == nil {
		return nil, ErrProviderUnsupported
	}
	groupsResponse := make([]ApprovalChainGroupOption, 0)
	seenGroups := map[string]struct{}{}
	for _, providerRow := range providers {
		provider, err := s.providerResolver.Resolve(discoveryCtx, providerRow.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve relay provider %d for approval chains: %w", providerRow.ID, err)
		}
		lister, ok := provider.(platformGroupLister)
		if !ok {
			return nil, fmt.Errorf("%w: relay provider %d does not list platform groups", ErrProviderUnsupported, providerRow.ID)
		}
		groups, err := lister.ListPlatformGroups(discoveryCtx)
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
			groupsResponse = append(groupsResponse, ApprovalChainGroupOption{
				ProviderID: providerRow.ID,
				GroupID:    groupID,
				GroupName:  strings.TrimSpace(group.Name),
				Platform:   strings.TrimSpace(group.Platform),
			})
		}
	}
	sort.SliceStable(groupsResponse, func(i, j int) bool {
		if groupsResponse[i].ProviderID != groupsResponse[j].ProviderID {
			return groupsResponse[i].ProviderID < groupsResponse[j].ProviderID
		}
		leftName := normalizeCandidateValue(groupsResponse[i].GroupName)
		rightName := normalizeCandidateValue(groupsResponse[j].GroupName)
		if leftName != rightName {
			return leftName < rightName
		}
		return groupsResponse[i].GroupID < groupsResponse[j].GroupID
	})
	return groupsResponse, nil
}

func listApprovalChainDepartmentOptions(ctx context.Context, client *ent.Client) ([]ApprovalChainDepartmentOption, error) {
	response := make([]ApprovalChainDepartmentOption, 0)
	sourceID, ok, err := directorysync.CurrentSourceID(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("resolve current directory source for approval chains: %w", err)
	}
	if !ok {
		return response, nil
	}
	configs, err := client.QuotaResetApproverConfig.Query().
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
	departments, err := client.DirectoryDepartment.Query().
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
		response = append(response, ApprovalChainDepartmentOption{
			DirectorySourceID:     sourceID,
			DepartmentExternalID:  departmentID,
			DepartmentDisplayPath: candidateDepartmentPath(departmentTree, department),
			ApproverCount:         approverCount,
		})
	}
	sort.SliceStable(response, func(i, j int) bool {
		leftPath := normalizeCandidateValue(response[i].DepartmentDisplayPath)
		rightPath := normalizeCandidateValue(response[j].DepartmentDisplayPath)
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return response[i].DepartmentExternalID < response[j].DepartmentExternalID
	})
	return response, nil
}

func (s *Service) ListApprovalChains(ctx context.Context) (*ApprovalChainListResponse, error) {
	tx, err := s.beginApprovalConfigurationTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	response, err := listApprovalChains(ctx, tx.Client())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit quota reset approval chain list: %w", err)
	}
	return response, nil
}

func listApprovalChains(ctx context.Context, client *ent.Client) (*ApprovalChainListResponse, error) {
	chainRows, err := client.QuotaResetApprovalChain.Query().
		Order(
			ent.Asc(quotaresetapprovalchain.FieldProviderID),
			ent.Asc(quotaresetapprovalchain.FieldGroupID),
			ent.Asc(quotaresetapprovalchain.FieldID),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list quota reset approval chains: %w", err)
	}
	nodeRows, err := client.QuotaResetApprovalChainNode.Query().
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
	if len(items) > 0 {
		groups, err := s.discoverApprovalChainGroups(ctx)
		if err != nil {
			return nil, err
		}
		if err := validateApprovalChainGroupSnapshot(items, groups); err != nil {
			return nil, err
		}
	}
	tx, err := s.beginApprovalConfigurationTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := validateApprovalChainDatabaseFacts(ctx, tx.Client(), items); err != nil {
		return nil, err
	}

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
	response, err := listApprovalChains(ctx, tx.Client())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit quota reset approval chains: %w", err)
	}
	return response, nil
}

func validateApprovalChainGroupSnapshot(items []ApprovalChainInput, groups []ApprovalChainGroupOption) error {
	allowedGroups := make(map[string]ApprovalChainGroupOption, len(groups))
	for _, group := range groups {
		allowedGroups[approvalChainGroupKey(group.ProviderID, group.GroupID)] = group
	}
	seenGroups := map[string]struct{}{}
	for itemIndex := range items {
		item := &items[itemIndex]
		groupKey := approvalChainGroupKey(item.ProviderID, item.GroupID)
		group, ok := allowedGroups[groupKey]
		if !ok {
			return fmt.Errorf("%w: unknown subscription group %s", ErrInvalidApproverConfig, groupKey)
		}
		if _, duplicate := seenGroups[groupKey]; duplicate {
			return fmt.Errorf("%w: duplicate subscription group %s", ErrInvalidApproverConfig, groupKey)
		}
		seenGroups[groupKey] = struct{}{}
		if item.GroupName == "" {
			item.GroupName = group.GroupName
		}
		seenDepartments := map[string]struct{}{}
		for nodeIndex := range item.Nodes {
			node := &item.Nodes[nodeIndex]
			nodeKey := approvalChainDepartmentKey(node.DirectorySourceID, node.DepartmentExternalID)
			if _, duplicate := seenDepartments[nodeKey]; duplicate {
				return fmt.Errorf("%w: duplicate department %s", ErrInvalidApproverConfig, nodeKey)
			}
			seenDepartments[nodeKey] = struct{}{}
		}
	}
	return nil
}

func validateApprovalChainDatabaseFacts(ctx context.Context, client *ent.Client, items []ApprovalChainInput) error {
	if len(items) == 0 {
		return nil
	}
	providerIDs := make([]int, 0, len(items))
	seenProviderIDs := make(map[int]struct{}, len(items))
	for _, item := range items {
		if _, seen := seenProviderIDs[item.ProviderID]; seen {
			continue
		}
		seenProviderIDs[item.ProviderID] = struct{}{}
		providerIDs = append(providerIDs, item.ProviderID)
	}
	enabledProviderIDs, err := client.RelayProvider.Query().
		Where(
			entrelayprovider.IDIn(providerIDs...),
			entrelayprovider.EnabledEQ(true),
		).
		IDs(ctx)
	if err != nil {
		return fmt.Errorf("revalidate enabled relay providers for approval chains: %w", err)
	}
	enabledProviders := make(map[int]struct{}, len(enabledProviderIDs))
	for _, providerID := range enabledProviderIDs {
		enabledProviders[providerID] = struct{}{}
	}
	sort.Ints(providerIDs)
	for _, providerID := range providerIDs {
		if _, ok := enabledProviders[providerID]; !ok {
			return fmt.Errorf("%w: relay provider %d is not enabled", ErrInvalidApproverConfig, providerID)
		}
	}
	departments, err := listApprovalChainDepartmentOptions(ctx, client)
	if err != nil {
		return err
	}
	allowedDepartments := make(map[string]ApprovalChainDepartmentOption, len(departments))
	for _, department := range departments {
		allowedDepartments[approvalChainDepartmentKey(department.DirectorySourceID, department.DepartmentExternalID)] = department
	}
	for itemIndex := range items {
		for nodeIndex := range items[itemIndex].Nodes {
			node := &items[itemIndex].Nodes[nodeIndex]
			nodeKey := approvalChainDepartmentKey(node.DirectorySourceID, node.DepartmentExternalID)
			department, ok := allowedDepartments[nodeKey]
			if !ok {
				return fmt.Errorf("%w: department %s has no enabled approver config", ErrInvalidApproverConfig, nodeKey)
			}
			if node.DepartmentDisplayPath == "" {
				node.DepartmentDisplayPath = department.DepartmentDisplayPath
			}
		}
	}
	return nil
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
	enabledChains, err := s.client.QuotaResetApprovalChain.Query().
		Where(quotaresetapprovalchain.EnabledEQ(true)).
		All(ctx)
	if err != nil {
		return fmt.Errorf("list enabled approval chains: %w", err)
	}
	if len(enabledChains) == 0 {
		return nil
	}
	enabledChainIDs := make([]int, 0, len(enabledChains))
	chainsByID := make(map[int]*ent.QuotaResetApprovalChain, len(enabledChains))
	for _, chain := range enabledChains {
		enabledChainIDs = append(enabledChainIDs, chain.ID)
		chainsByID[chain.ID] = chain
	}
	nodes, err := s.client.QuotaResetApprovalChainNode.Query().
		Where(
			quotaresetapprovalchainnode.DirectorySourceIDEQ(sourceID),
			quotaresetapprovalchainnode.ChainIDIn(enabledChainIDs...),
		).
		All(ctx)
	if err != nil {
		return fmt.Errorf("list approval chain references: %w", err)
	}
	referencingChains := make(map[int]*ent.QuotaResetApprovalChain)
	for _, node := range nodes {
		if _, ok := remaining[strings.TrimSpace(node.DepartmentExternalID)]; !ok {
			referencingChains[node.ChainID] = chainsByID[node.ChainID]
		}
	}
	if len(referencingChains) == 0 {
		return nil
	}
	chains := make([]*ent.QuotaResetApprovalChain, 0, len(referencingChains))
	for _, chain := range referencingChains {
		chains = append(chains, chain)
	}
	sort.Slice(chains, func(i, j int) bool {
		if chains[i].ProviderID != chains[j].ProviderID {
			return chains[i].ProviderID < chains[j].ProviderID
		}
		if chains[i].GroupID != chains[j].GroupID {
			return chains[i].GroupID < chains[j].GroupID
		}
		if chains[i].GroupName != chains[j].GroupName {
			return chains[i].GroupName < chains[j].GroupName
		}
		return chains[i].ID < chains[j].ID
	})
	identities := make([]string, 0, len(chains))
	for _, chain := range chains {
		identities = append(identities, fmt.Sprintf("provider_id=%d group_id=%s group_name=%q", chain.ProviderID, chain.GroupID, chain.GroupName))
	}
	return fmt.Errorf("%w: enabled approval chains reference departments without approver configs: %s", ErrApproverConfigReferenced, strings.Join(identities, ", "))
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
