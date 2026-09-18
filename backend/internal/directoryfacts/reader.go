package directoryfacts

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/directorysource"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	"github.com/ai-efficiency/backend/ent/predicate"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/lib/pq"
)

type Store struct {
	client *ent.Client
}

type currentView struct {
	store    *Store
	snapshot Snapshot
}

func New(client *ent.Client) *Store {
	return &Store{client: client}
}

func (s *Store) Current(ctx context.Context) (View, bool, error) {
	if s == nil || s.client == nil {
		return nil, false, fmt.Errorf("directory facts reader is not configured")
	}
	sources, err := s.client.DirectorySource.Query().
		Where(
			directorysource.DeletedEQ(false),
			directorysource.ScopeEQ(directorysource.ScopeFullCompany),
			directorysource.LastSuccessfulRunIDNotNil(),
		).
		All(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("list directory sources with successful sync: %w", err)
	}
	if len(sources) == 0 {
		return nil, false, nil
	}

	sourceByRunID := make(map[int]int, len(sources))
	runIDs := make([]int, 0, len(sources))
	for _, source := range sources {
		if source.LastSuccessfulRunID == nil {
			continue
		}
		runID := *source.LastSuccessfulRunID
		sourceByRunID[runID] = source.ID
		runIDs = append(runIDs, runID)
	}
	if len(runIDs) == 0 {
		return nil, false, nil
	}

	run, err := s.client.DirectorySyncRun.Query().
		Where(
			directorysyncrun.IDIn(runIDs...),
			directorysyncrun.ModeEQ(directorysyncrun.ModeApply),
			directorysyncrun.StatusIn(directorysyncrun.StatusCompleted, directorysyncrun.StatusCompletedWithWarnings),
			directorysyncrun.CompletedAtNotNil(),
		).
		Order(ent.Desc(directorysyncrun.FieldCompletedAt), ent.Desc(directorysyncrun.FieldID)).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("resolve latest successful directory sync run: %w", err)
	}
	sourceID, ok := sourceByRunID[run.ID]
	if !ok {
		return nil, false, nil
	}
	return s.At(Snapshot{SourceID: sourceID, RunID: run.ID}), true, nil
}

func (s *Store) At(snapshot Snapshot) View {
	return &currentView{store: s, snapshot: snapshot}
}

func (v *currentView) Snapshot() Snapshot {
	if v == nil {
		return Snapshot{}
	}
	return v.snapshot
}

func (v *currentView) Load(ctx context.Context, query Query) (*Facts, error) {
	if v == nil || v.store == nil || v.store.client == nil {
		return nil, fmt.Errorf("directory facts view is not configured")
	}
	if v.snapshot.SourceID <= 0 || v.snapshot.RunID <= 0 {
		return nil, fmt.Errorf("directory facts snapshot must be positive")
	}

	initialDepartmentIDs := append([]string(nil), query.RepresentativeDepartmentIDs...)
	if !query.IncludeDepartmentAncestors {
		initialDepartmentIDs = append(initialDepartmentIDs, query.DepartmentIDs...)
	}
	departments, err := v.loadDepartments(ctx, query.AllDepartments, initialDepartmentIDs, false)
	if err != nil {
		return nil, err
	}
	members, err := v.loadMembers(ctx, query, departments)
	if err != nil {
		return nil, err
	}
	memberships, err := v.loadMemberships(ctx, members, query.IncludeMemberships)
	if err != nil {
		return nil, err
	}
	if query.IncludeDepartmentAncestors {
		candidateIDs := append([]string(nil), query.DepartmentIDs...)
		for _, member := range members {
			candidateIDs = append(candidateIDs, member.DepartmentExternalID)
		}
		for _, membership := range memberships {
			candidateIDs = append(candidateIDs, membership.DepartmentExternalID)
		}
		departments, err = v.loadDepartments(ctx, false, candidateIDs, true)
		if err != nil {
			return nil, err
		}
	}
	users, err := v.loadUsers(ctx, query, members)
	if err != nil {
		return nil, err
	}
	return NewFacts(v.snapshot, departments, members, memberships, users), nil
}

func (v *currentView) loadDepartments(ctx context.Context, all bool, ids []string, ancestors bool) ([]Department, error) {
	ids = compactStrings(ids)
	if !all && len(ids) == 0 {
		return nil, nil
	}
	predicates := []predicate.DirectoryDepartment{
		directorydepartment.SourceIDEQ(v.snapshot.SourceID),
		directorydepartment.LastSeenRunIDEQ(v.snapshot.RunID),
	}
	if !all {
		if ancestors {
			predicates = append(predicates, departmentAncestorPredicate(v.snapshot, ids))
		} else {
			predicates = append(predicates, directorydepartment.ExternalIDIn(ids...))
		}
	}
	rows, err := v.store.client.DirectoryDepartment.Query().
		Where(predicates...).
		Order(ent.Asc(directorydepartment.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load current directory departments: %w", err)
	}
	out := make([]Department, 0, len(rows))
	for _, row := range rows {
		out = append(out, Department{
			ID:                        row.ID,
			ExternalID:                row.ExternalID,
			ParentExternalID:          row.ParentExternalID,
			EffectiveParentExternalID: row.EffectiveParentExternalID,
			Name:                      row.Name,
			Path:                      row.Path,
			Metadata:                  row.Metadata,
		})
	}
	return out, nil
}

func (v *currentView) loadMembers(ctx context.Context, query Query, departments []Department) ([]Member, error) {
	memberUserIDs := positiveInts(query.MemberUserIDs)
	memberEmails := normalizedEmails(query.MemberEmails)
	memberExternalIDs := compactStrings(query.MemberExternalIDs)
	representativeDepartmentIDs := compactStrings(query.RepresentativeDepartmentIDs)
	if len(representativeDepartmentIDs) > 0 {
		requested := stringSet(representativeDepartmentIDs)
		for _, department := range departments {
			if _, ok := requested[department.ExternalID]; !ok {
				continue
			}
			memberExternalIDs = append(memberExternalIDs, MetadataStringValues(department.Metadata[DepartmentRepresentativeIDsKey])...)
		}
		memberExternalIDs = compactStrings(memberExternalIDs)
	}

	selectors := make([]predicate.DirectoryMember, 0, 5)
	if len(memberUserIDs) > 0 {
		selectors = append(selectors, directorymember.MatchedUserIDIn(memberUserIDs...))
	}
	if len(memberEmails) > 0 {
		selectors = append(selectors, directorymember.EmailNormalizedIn(memberEmails...))
	}
	if len(memberExternalIDs) > 0 {
		selectors = append(selectors, directorymember.ExternalIDIn(memberExternalIDs...))
	}
	if len(representativeDepartmentIDs) > 0 {
		selectors = append(selectors, leaderDepartmentPredicate(representativeDepartmentIDs))
	}
	if !query.AllMembers && len(selectors) == 0 {
		return nil, nil
	}

	predicates := []predicate.DirectoryMember{
		directorymember.SourceIDEQ(v.snapshot.SourceID),
		directorymember.LastSeenRunIDEQ(v.snapshot.RunID),
	}
	if query.ActiveMembersOnly {
		predicates = append(predicates, directorymember.StatusEQ("active"))
	}
	if !query.AllMembers {
		predicates = append(predicates, directorymember.Or(selectors...))
	}
	rows, err := v.store.client.DirectoryMember.Query().
		Where(predicates...).
		Order(ent.Asc(directorymember.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load current directory members: %w", err)
	}
	out := make([]Member, 0, len(rows))
	for _, row := range rows {
		out = append(out, Member{
			ID:                   row.ID,
			ExternalID:           row.ExternalID,
			EmailNormalized:      row.EmailNormalized,
			DisplayName:          row.DisplayName,
			DepartmentExternalID: row.DepartmentExternalID,
			Status:               row.Status,
			MatchedUserID:        row.MatchedUserID,
			Metadata:             row.Metadata,
		})
	}
	return out, nil
}

func (v *currentView) loadMemberships(ctx context.Context, members []Member, include bool) ([]Membership, error) {
	if !include || len(members) == 0 {
		return nil, nil
	}
	memberIDs := make([]int, 0, len(members))
	for _, member := range members {
		memberIDs = append(memberIDs, member.ID)
	}
	rows, err := v.store.client.DirectoryMemberDepartment.Query().
		Where(
			directorymemberdepartment.SourceIDEQ(v.snapshot.SourceID),
			directorymemberdepartment.LastSeenRunIDEQ(v.snapshot.RunID),
			directorymemberdepartment.DirectoryMemberIDIn(memberIDs...),
		).
		Order(
			ent.Asc(directorymemberdepartment.FieldDirectoryMemberID),
			ent.Asc(directorymemberdepartment.FieldDepartmentExternalID),
			ent.Asc(directorymemberdepartment.FieldID),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load current directory memberships: %w", err)
	}
	out := make([]Membership, 0, len(rows))
	for _, row := range rows {
		out = append(out, Membership{
			ID:                   row.ID,
			DirectoryMemberID:    row.DirectoryMemberID,
			DepartmentExternalID: row.DepartmentExternalID,
		})
	}
	return out, nil
}

func (v *currentView) loadUsers(ctx context.Context, query Query, members []Member) ([]User, error) {
	knownByID := make(map[int]User, len(query.KnownUsers))
	for _, user := range query.KnownUsers {
		if user.ID > 0 {
			knownByID[user.ID] = user
		}
	}
	userIDs := append([]int(nil), query.UserIDs...)
	userEmails := append([]string(nil), query.UserEmails...)
	if query.MatchUsersForMembers {
		for _, member := range members {
			if member.MatchedUserID != nil {
				userIDs = append(userIDs, *member.MatchedUserID)
			}
			userEmails = append(userEmails, member.EmailNormalized)
		}
	}
	userIDs = positiveInts(userIDs)
	userEmails = normalizedEmails(userEmails)
	selectors := make([]predicate.User, 0, len(userEmails)+1)
	if len(userIDs) > 0 {
		selectors = append(selectors, entuser.IDIn(userIDs...))
	}
	for _, email := range userEmails {
		selectors = append(selectors, entuser.EmailEqualFold(email))
	}
	if !query.AllUsers && len(selectors) == 0 {
		return sortedUsers(knownByID), nil
	}
	userQuery := v.store.client.User.Query().Order(ent.Asc(entuser.FieldID))
	if !query.AllUsers {
		userQuery = userQuery.Where(entuser.Or(selectors...))
	}
	rows, err := userQuery.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load directory-matched local users: %w", err)
	}
	for _, row := range rows {
		knownByID[row.ID] = User{
			ID:              row.ID,
			Username:        row.Username,
			Email:           row.Email,
			Role:            string(row.Role),
			RelayUserID:     row.RelayUserID,
			TokenValidAfter: row.TokenValidAfter,
			RelayDisabledAt: row.RelayDisabledAt,
		}
	}
	return sortedUsers(knownByID), nil
}

func sortedUsers(byID map[int]User) []User {
	out := make([]User, 0, len(byID))
	for _, user := range byID {
		out = append(out, user)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func departmentAncestorPredicate(snapshot Snapshot, candidateIDs []string) predicate.DirectoryDepartment {
	return func(selector *entsql.Selector) {
		ancestors := entsql.Table("directory_fact_ancestors")
		selector.Prefix(entsql.ExprFunc(func(builder *entsql.Builder) {
			builder.WriteString(`WITH RECURSIVE
directory_fact_parameters(source_id, run_id, candidate_ids) AS MATERIALIZED (
  SELECT `)
			builder.Arg(snapshot.SourceID)
			builder.WriteString(`::bigint, `)
			builder.Arg(snapshot.RunID)
			builder.WriteString(`::bigint, `)
			builder.Arg(pq.Array(candidateIDs))
			builder.WriteString(`::text[]
),
directory_fact_candidates(external_id) AS MATERIALIZED (
  SELECT UNNEST(directory_fact_parameters.candidate_ids)
  FROM directory_fact_parameters
),
directory_fact_ancestors(external_id, effective_parent_external_id) AS MATERIALIZED (
  SELECT seed.external_id, seed.effective_parent_external_id
  FROM directory_departments AS seed
  JOIN directory_fact_parameters AS parameters
    ON parameters.source_id = seed.source_id
   AND parameters.run_id = seed.last_seen_run_id
  JOIN directory_fact_candidates AS candidate
    ON candidate.external_id = seed.external_id
  UNION
  SELECT parent.external_id, parent.effective_parent_external_id
  FROM directory_departments AS parent
  JOIN directory_fact_parameters AS parameters
    ON parameters.source_id = parent.source_id
   AND parameters.run_id = parent.last_seen_run_id
  JOIN directory_fact_ancestors AS child
    ON child.effective_parent_external_id = parent.external_id
)`)
		}))
		selector.Where(entsql.In(selector.C(directorydepartment.FieldExternalID), entsql.Select(ancestors.C(directorydepartment.FieldExternalID)).From(ancestors)))
	}
}

func leaderDepartmentPredicate(departmentIDs []string) predicate.DirectoryMember {
	values := make([]any, 0, len(departmentIDs)*2)
	for _, departmentID := range departmentIDs {
		values = append(values, departmentID)
		var numeric any
		if err := json.Unmarshal([]byte(departmentID), &numeric); err == nil {
			if _, ok := numeric.(float64); ok {
				values = append(values, numeric)
			}
		}
	}
	return func(selector *entsql.Selector) {
		predicates := make([]*entsql.Predicate, 0, len(values)*2)
		for _, value := range values {
			for _, encodedValue := range []any{value, []any{value}} {
				encoded, _ := json.Marshal(map[string]any{MemberLeaderDepartmentIDsKey: encodedValue})
				needle := string(encoded)
				predicates = append(predicates, entsql.P(func(builder *entsql.Builder) {
					builder.Ident(selector.C(directorymember.FieldMetadata)).WriteString(" @> ").Arg(needle)
				}))
			}
		}
		selector.Where(entsql.Or(predicates...))
	}
}

func positiveInts(values []int) []int {
	seen := map[int]struct{}{}
	out := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

func normalizedEmails(values []string) []string {
	for index := range values {
		values[index] = NormalizeEmail(values[index])
	}
	return compactStrings(values)
}

var _ Reader = (*Store)(nil)
var _ View = (*currentView)(nil)
