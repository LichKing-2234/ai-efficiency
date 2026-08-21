package activity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/prsyncjob"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

var (
	ErrForbidden       = errors.New("activity subject is outside the current authorized scope")
	ErrInvalidCursor   = errors.New("invalid activity cursor")
	ErrSnapshotExpired = errors.New("activity snapshot expired")
	ErrInvalidQuery    = errors.New("invalid activity query")
)

type ScopeResolver interface {
	Resolve(context.Context, int) (*representativescope.Scope, error)
}

type V2DB interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type ServiceOptions struct {
	ScopeResolver ScopeResolver
	CursorSecret  string
	V2LedgerEpoch string
	V2CutoverAt   time.Time
	V2Denominator V2DenominatorReader
	V2DB          V2DB
}

type Service struct {
	client        *ent.Client
	scope         ScopeResolver
	cursorSecret  []byte
	v2LedgerEpoch string
	v2CutoverAt   time.Time
	v2Denominator V2DenominatorReader
	v2DB          V2DB
	now           func() time.Time
}

func NewService(client *ent.Client, options ServiceOptions) *Service {
	scopeResolver := options.ScopeResolver
	if scopeResolver == nil && client != nil {
		scopeResolver = representativescope.New(client)
	}
	return &Service{
		client: client, scope: scopeResolver, cursorSecret: []byte(options.CursorSecret),
		v2LedgerEpoch: strings.TrimSpace(options.V2LedgerEpoch), v2CutoverAt: options.V2CutoverAt.UTC(),
		v2Denominator: options.V2Denominator, v2DB: options.V2DB, now: time.Now,
	}
}

type authorizationScope struct {
	Admin          bool
	Representative bool
	Version        string
	SourceID       int
	RunID          int
	AllowedUserIDs map[int]struct{}
	Teams          map[string]Team
}

func (s *Service) resolveAuthorization(ctx context.Context, actorUserID int) (*authorizationScope, error) {
	actor, err := s.client.User.Query().Where(user.IDEQ(actorUserID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("load activity actor: %w", err)
	}
	authorization := &authorizationScope{AllowedUserIDs: map[int]struct{}{actor.ID: {}}, Teams: map[string]Team{}}
	snapshot, hasSnapshot, err := directorysync.CurrentSnapshot(ctx, s.client)
	if err != nil {
		return nil, fmt.Errorf("resolve activity directory snapshot: %w", err)
	}
	if hasSnapshot {
		authorization.Version = fmt.Sprintf("directory:%d:%d", snapshot.SourceID, snapshot.RunID)
		authorization.SourceID = snapshot.SourceID
		authorization.RunID = snapshot.RunID
	}
	if actor.Role == user.RoleAdmin {
		authorization.Admin = true
		if !hasSnapshot {
			return authorization, nil
		}
		departments, err := s.client.DirectoryDepartment.Query().Where(
			directorydepartment.SourceIDEQ(snapshot.SourceID),
			directorydepartment.LastSeenRunIDEQ(snapshot.RunID),
		).All(ctx)
		if err != nil {
			return nil, fmt.Errorf("load current Admin activity teams: %w", err)
		}
		for _, department := range departments {
			authorization.Teams[department.ExternalID] = Team{
				ExternalID: department.ExternalID, ParentExternalID: department.EffectiveParentExternalID,
				Name: department.Name, DisplayPath: department.Path,
			}
		}
		members, memberships, err := s.currentDirectoryMembers(ctx, snapshot.SourceID, snapshot.RunID)
		if err != nil {
			return nil, err
		}
		memberCounts := adminActivityTeamMemberCounts(departments, members, memberships)
		for externalID, count := range memberCounts {
			team := authorization.Teams[externalID]
			team.MemberCount = count
			authorization.Teams[externalID] = team
		}
		usersByID, usersByEmail, err := s.usersByIdentity(ctx)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			if matched := matchedUser(member, usersByID, usersByEmail); matched != nil {
				authorization.AllowedUserIDs[matched.ID] = struct{}{}
			}
		}
		return authorization, nil
	}
	if s.scope == nil || !hasSnapshot {
		return authorization, nil
	}
	representativeScope, err := s.scope.Resolve(ctx, actor.ID)
	if err != nil {
		return nil, fmt.Errorf("resolve representative activity scope: %w", err)
	}
	if representativeScope == nil || !representativeScope.IsRepresentative {
		return authorization, nil
	}
	authorization.Representative = true
	authorization.Version = representativeScope.Version
	for _, department := range representativeScope.MemberTreeDepartments {
		authorization.Teams[department.ExternalID] = Team{
			ExternalID: department.ExternalID, ParentExternalID: department.ParentExternalID,
			Name: department.Name, DisplayPath: department.DisplayPath, MemberCount: department.SubtreeMemberCount,
		}
	}
	for _, subject := range representativeScope.OverviewSubjects {
		if subject.SubjectType == "member" && subject.UserID > 0 {
			authorization.AllowedUserIDs[subject.UserID] = struct{}{}
		}
	}
	return authorization, nil
}

func (s *Service) loadCurrentTeamMembers(ctx context.Context, authorization *authorizationScope, teamExternalID string) ([]MemberIdentity, error) {
	members, memberships, err := s.currentDirectoryMembers(ctx, authorization.SourceID, authorization.RunID)
	if err != nil {
		return nil, err
	}
	memberIDs := map[int]struct{}{}
	departmentsByMember := map[int][]string{}
	for _, membership := range memberships {
		departmentsByMember[membership.DirectoryMemberID] = appendUniqueString(departmentsByMember[membership.DirectoryMemberID], membership.DepartmentExternalID)
		if membership.DepartmentExternalID == teamExternalID {
			memberIDs[membership.DirectoryMemberID] = struct{}{}
		}
	}
	if len(memberIDs) == 0 {
		return []MemberIdentity{}, nil
	}
	usersByID, usersByEmail, err := s.usersByIdentity(ctx)
	if err != nil {
		return nil, err
	}
	identities := make([]MemberIdentity, 0, len(memberIDs))
	for _, member := range members {
		if _, selected := memberIDs[member.ID]; !selected {
			continue
		}
		identity := MemberIdentity{
			DirectoryMemberExternalID: member.ExternalID, DisplayName: member.DisplayName,
			Email: member.EmailNormalized, DepartmentExternalIDs: departmentsByMember[member.ID],
		}
		if matched := matchedUser(member, usersByID, usersByEmail); matched != nil {
			if _, allowed := authorization.AllowedUserIDs[matched.ID]; !allowed {
				continue
			}
			identity.UserID, identity.DisplayName, identity.Email = matched.ID, matched.Username, matched.Email
		}
		identities = append(identities, identity)
	}
	sort.Slice(identities, func(i, j int) bool {
		left, right := strings.ToLower(strings.TrimSpace(identities[i].DisplayName)), strings.ToLower(strings.TrimSpace(identities[j].DisplayName))
		if left != right {
			return left < right
		}
		return identities[i].DirectoryMemberExternalID < identities[j].DirectoryMemberExternalID
	})
	return identities, nil
}

func (s *Service) currentDirectoryMembers(ctx context.Context, sourceID, runID int) ([]*ent.DirectoryMember, []*ent.DirectoryMemberDepartment, error) {
	members, err := s.client.DirectoryMember.Query().Where(
		directorymember.SourceIDEQ(sourceID), directorymember.LastSeenRunIDEQ(runID), directorymember.StatusEQ("active"),
	).All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load current activity members: %w", err)
	}
	memberships, err := s.client.DirectoryMemberDepartment.Query().Where(
		directorymemberdepartment.SourceIDEQ(sourceID), directorymemberdepartment.LastSeenRunIDEQ(runID),
	).All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load current activity member departments: %w", err)
	}
	return members, memberships, nil
}

func (s *Service) usersByIdentity(ctx context.Context) (map[int]*ent.User, map[string]*ent.User, error) {
	users, err := s.client.User.Query().All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load activity users: %w", err)
	}
	byID, byEmail := make(map[int]*ent.User, len(users)), make(map[string]*ent.User, len(users))
	for _, item := range users {
		byID[item.ID] = item
		byEmail[strings.ToLower(strings.TrimSpace(item.Email))] = item
	}
	return byID, byEmail, nil
}

func matchedUser(member *ent.DirectoryMember, byID map[int]*ent.User, byEmail map[string]*ent.User) *ent.User {
	if member.MatchedUserID != nil {
		if matched := byID[*member.MatchedUserID]; matched != nil {
			return matched
		}
	}
	return byEmail[strings.ToLower(strings.TrimSpace(member.EmailNormalized))]
}

func adminActivityTeamMemberCounts(departments []*ent.DirectoryDepartment, members []*ent.DirectoryMember, memberships []*ent.DirectoryMemberDepartment) map[string]int {
	known, parents := map[string]struct{}{}, map[string]string{}
	for _, department := range departments {
		known[department.ExternalID] = struct{}{}
		if department.EffectiveParentExternalID != nil {
			parents[department.ExternalID] = strings.TrimSpace(*department.EffectiveParentExternalID)
		}
	}
	active, direct := map[int]struct{}{}, map[int]map[string]struct{}{}
	add := func(memberID int, departmentID string) {
		departmentID = strings.TrimSpace(departmentID)
		if memberID <= 0 {
			return
		}
		if _, ok := known[departmentID]; !ok {
			return
		}
		if direct[memberID] == nil {
			direct[memberID] = map[string]struct{}{}
		}
		direct[memberID][departmentID] = struct{}{}
	}
	for _, member := range members {
		active[member.ID] = struct{}{}
		add(member.ID, member.DepartmentExternalID)
	}
	for _, membership := range memberships {
		if _, ok := active[membership.DirectoryMemberID]; ok {
			add(membership.DirectoryMemberID, membership.DepartmentExternalID)
		}
	}
	byDepartment := map[string]map[int]struct{}{}
	for memberID, departmentIDs := range direct {
		seen := map[string]struct{}{}
		for departmentID := range departmentIDs {
			for current := departmentID; current != ""; current = parents[current] {
				if _, ok := known[current]; !ok {
					break
				}
				if _, duplicate := seen[current]; duplicate {
					break
				}
				seen[current] = struct{}{}
				if byDepartment[current] == nil {
					byDepartment[current] = map[int]struct{}{}
				}
				byDepartment[current][memberID] = struct{}{}
			}
		}
	}
	counts := make(map[string]int, len(known))
	for departmentID := range known {
		counts[departmentID] = len(byDepartment[departmentID])
	}
	return counts
}

func (s *Service) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func latestPRSyncJobPredicate() func(*entsql.Selector) {
	return func(jobs *entsql.Selector) {
		newer := entsql.Table(prsyncjob.Table).As("newer_activity_pr_sync_jobs")
		jobs.Where(entsql.NotExists(
			entsql.SelectExpr(entsql.Expr("1")).From(newer).Where(entsql.And(
				entsql.ColumnsEQ(newer.C(prsyncjob.FieldRepoConfigID), jobs.C(prsyncjob.FieldRepoConfigID)),
				entsql.ColumnsGT(newer.C(prsyncjob.FieldID), jobs.C(prsyncjob.FieldID)),
			)),
		))
	}
}

func coverageForRepositories(repoIDs map[int]struct{}, jobs map[int]*ent.PRSyncJob, now time.Time) SyncCoverage {
	coverage := SyncCoverage{Complete: true}
	for repoID := range repoIDs {
		job := jobs[repoID]
		switch {
		case job == nil:
			coverage.UnsyncedRepositories++
		case job.Status == prsyncjob.StatusFailed || job.Status == prsyncjob.StatusAbandoned || job.Status == prsyncjob.StatusCancelled:
			coverage.FailedRepositories++
		case job.Status != prsyncjob.StatusCompleted || job.CompletedAt == nil, job.UpsertFailedPrs > 0 || job.UsageFailedPrs > 0:
			coverage.PartiallySyncedRepositories++
		case now.Sub(job.CompletedAt.UTC()) > defaultPRSyncStaleAfter:
			coverage.StaleRepositories++
		}
	}
	coverage.AffectedRepositories = coverage.UnsyncedRepositories + coverage.StaleRepositories + coverage.PartiallySyncedRepositories + coverage.FailedRepositories
	coverage.Complete = coverage.AffectedRepositories == 0
	return coverage
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func sortedIntKeys[T any](values map[int]T) []int {
	keys := make([]int, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}
