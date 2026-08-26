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
	"github.com/ai-efficiency/backend/ent/prsyncjob"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/directoryfacts"
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
	facts         directoryfacts.Reader
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
		client: client, facts: directoryfacts.New(client), scope: scopeResolver, cursorSecret: []byte(options.CursorSecret),
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
	if actor.Role == user.RoleAdmin {
		authorization.Admin = true
		view, ok, err := s.facts.Current(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve activity directory snapshot: %w", err)
		}
		if !ok {
			return authorization, nil
		}
		snapshot := view.Snapshot()
		authorization.Version = fmt.Sprintf("directory:%d:%d", snapshot.SourceID, snapshot.RunID)
		authorization.SourceID = snapshot.SourceID
		authorization.RunID = snapshot.RunID
		facts, err := view.Load(ctx, directoryfacts.Query{
			AllDepartments:     true,
			AllMembers:         true,
			ActiveMembersOnly:  true,
			IncludeMemberships: true,
			AllUsers:           true,
		})
		if err != nil {
			return nil, fmt.Errorf("load current Admin activity directory facts: %w", err)
		}
		for _, department := range facts.Departments() {
			authorization.Teams[department.ExternalID] = Team{
				ExternalID: department.ExternalID, ParentExternalID: department.EffectiveParentExternalID,
				Name: department.Name, DisplayPath: department.Path, MemberCount: facts.DepartmentStats(department.ExternalID).MemberCount,
			}
		}
		for _, member := range facts.Members() {
			if matched := facts.UserForMember(member); matched != nil {
				authorization.AllowedUserIDs[matched.ID] = struct{}{}
			}
		}
		return authorization, nil
	}
	if s.scope == nil {
		return authorization, nil
	}
	representativeScope, err := s.scope.Resolve(ctx, actor.ID)
	if err != nil {
		return nil, fmt.Errorf("resolve representative activity scope: %w", err)
	}
	if representativeScope == nil {
		return authorization, nil
	}
	authorization.Version = representativeScope.Version
	authorization.SourceID = representativeScope.DirectorySourceID
	authorization.RunID = representativeScope.DirectoryRunID
	if !representativeScope.IsRepresentative {
		return authorization, nil
	}
	authorization.Representative = true
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
	view := s.facts.At(directoryfacts.Snapshot{SourceID: authorization.SourceID, RunID: authorization.RunID})
	facts, err := view.Load(ctx, directoryfacts.Query{
		AllMembers:         true,
		ActiveMembersOnly:  true,
		IncludeMemberships: true,
		AllUsers:           true,
	})
	if err != nil {
		return nil, fmt.Errorf("load current activity member facts: %w", err)
	}
	memberIDs := map[int]struct{}{}
	departmentsByMember := map[int][]string{}
	for _, member := range facts.Members() {
		departmentIDs := facts.ExplicitDepartmentIDsForMember(member)
		departmentsByMember[member.ID] = departmentIDs
		for _, departmentID := range departmentIDs {
			if departmentID == teamExternalID {
				memberIDs[member.ID] = struct{}{}
			}
		}
	}
	if len(memberIDs) == 0 {
		return []MemberIdentity{}, nil
	}
	identities := make([]MemberIdentity, 0, len(memberIDs))
	for _, member := range facts.Members() {
		if _, selected := memberIDs[member.ID]; !selected {
			continue
		}
		identity := MemberIdentity{
			DirectoryMemberExternalID: member.ExternalID, DisplayName: member.DisplayName,
			Email: member.EmailNormalized, DepartmentExternalIDs: departmentsByMember[member.ID],
		}
		if matched := facts.UserForMember(member); matched != nil {
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
