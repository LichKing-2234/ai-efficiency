package adminusers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorysource"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/adminuseraccess"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type targetSemanticFixture struct {
	client *ent.Client
	users  map[string]*ent.User
}

func TestTargetsDepartmentMappingMatrix(t *testing.T) {
	fixture := seedTargetSemanticFixture(t)
	service := NewService(fixture.client)
	all := targetIDs(
		fixture.users["alice"],
		fixture.users["bob"],
		fixture.users["carol"],
		fixture.users["dave"],
		fixture.users["erin"],
		fixture.users["frank"],
		fixture.users["grace"],
		fixture.users["heidi"],
		fixture.users["cycle-a"],
		fixture.users["cycle-b"],
		fixture.users["cycle-c"],
	)

	tests := []struct {
		name        string
		filters     Filters
		limit       int
		want        []int
		wantErr     error
		wantMessage string
		cancel      bool
	}{
		{
			name:    "direct node includes current memberships and legacy-only member",
			filters: Filters{DepartmentID: "dept-alpha-one"},
			limit:   100,
			want: targetIDs(
				fixture.users["alice"],
				fixture.users["bob"],
				fixture.users["carol"],
				fixture.users["erin"],
				fixture.users["grace"],
				fixture.users["heidi"],
			),
		},
		{
			name:    "ancestor includes effective descendants",
			filters: Filters{DepartmentID: "dept-alpha"},
			limit:   100,
			want: targetIDs(
				fixture.users["alice"],
				fixture.users["bob"],
				fixture.users["carol"],
				fixture.users["erin"],
				fixture.users["grace"],
				fixture.users["heidi"],
			),
		},
		{
			name:    "sibling excludes alpha-only members",
			filters: Filters{DepartmentID: "dept-beta"},
			limit:   100,
			want:    targetIDs(fixture.users["carol"], fixture.users["dave"]),
		},
		{
			name:    "multi-membership maps one local user once",
			filters: Filters{DepartmentID: "dept-beta"},
			limit:   100,
			want:    targetIDs(fixture.users["carol"], fixture.users["dave"]),
		},
		{
			name:    "current membership authority blocks legacy primary leakage",
			filters: Filters{DepartmentID: "dept-alpha-one", Query: "dave"},
			limit:   100,
			want:    []int{},
		},
		{
			name:    "zero current memberships permits legacy primary fallback",
			filters: Filters{DepartmentID: "dept-alpha-one", Query: "erin"},
			limit:   100,
			want:    targetIDs(fixture.users["erin"]),
		},
		{
			name:    "positive matched user id qualifies independently of email",
			filters: Filters{DepartmentID: "dept-alpha-one", Query: "alice"},
			limit:   100,
			want:    targetIDs(fixture.users["alice"]),
		},
		{
			name:    "normalized member email matches mixed-case trimmed local email",
			filters: Filters{DepartmentID: "dept-alpha-one", Query: "bob"},
			limit:   100,
			want:    targetIDs(fixture.users["bob"]),
		},
		{
			name:    "one member maps matched id and normalized email then deduplicates ids",
			filters: Filters{DepartmentID: "dept-alpha-one", Query: "grace"},
			limit:   100,
			want:    targetIDs(fixture.users["grace"]),
		},
		{
			name:    "dual mapping includes the email-matched second local user",
			filters: Filters{DepartmentID: "dept-alpha-one", Query: "heidi"},
			limit:   100,
			want:    targetIDs(fixture.users["heidi"]),
		},
		{
			name:    "no department filter preserves unmatched local user",
			filters: Filters{},
			limit:   100,
			want:    all,
		},
		{
			name:    "department filter excludes unmatched and non-current-source evidence",
			filters: Filters{DepartmentID: "dept-alpha", Query: "frank"},
			limit:   100,
			want:    []int{},
		},
		{
			name:    "unknown department returns no targets",
			filters: Filters{DepartmentID: "dept-unknown"},
			limit:   100,
			want:    []int{},
		},
		{
			name:    "search intersects department scope",
			filters: Filters{Query: "bob@example.org", DepartmentID: "dept-alpha"},
			limit:   100,
			want:    targetIDs(fixture.users["bob"]),
		},
		{
			name:    "numeric local user id search intersects department scope",
			filters: Filters{Query: strconv.Itoa(fixture.users["alice"].ID), DepartmentID: "dept-alpha-one"},
			limit:   100,
			want:    targetIDs(fixture.users["alice"]),
		},
		{
			name:    "numeric relay user id search intersects department scope",
			filters: Filters{Query: strconv.Itoa(*fixture.users["bob"].RelayUserID), DepartmentID: "dept-alpha-one"},
			limit:   100,
			want:    targetIDs(fixture.users["bob"]),
		},
		{
			name:    "configured access status intersects department scope",
			filters: Filters{DepartmentID: "dept-alpha-one", AccessStatus: adminuseraccess.StatusConfigured},
			limit:   100,
			want:    targetIDs(fixture.users["alice"]),
		},
		{
			name:    "disabled access status intersects department scope",
			filters: Filters{DepartmentID: "dept-alpha-one", AccessStatus: adminuseraccess.StatusDisabled},
			limit:   100,
			want:    targetIDs(fixture.users["bob"]),
		},
		{
			name:    "missing credential access status intersects department scope",
			filters: Filters{DepartmentID: "dept-alpha-one", AccessStatus: adminuseraccess.StatusMissingCredential},
			limit:   100,
			want: targetIDs(
				fixture.users["carol"],
				fixture.users["erin"],
				fixture.users["grace"],
				fixture.users["heidi"],
			),
		},
		{
			name:        "invalid access status wraps package sentinel and existing message",
			filters:     Filters{AccessStatus: "unknown"},
			limit:       100,
			wantErr:     ErrInvalidAccessStatus,
			wantMessage: "access_status must be configured, disabled, or missing_credential",
		},
		{
			name:    "positive limit truncates stable id order",
			filters: Filters{},
			limit:   3,
			want:    all[:3],
		},
		{
			name:        "nonpositive limit is rejected",
			filters:     Filters{},
			limit:       0,
			wantMessage: "limit must be positive",
		},
		{
			name:        "canceled context stops filtered target resolution",
			filters:     Filters{DepartmentID: "dept-alpha"},
			limit:       100,
			wantErr:     context.Canceled,
			wantMessage: context.Canceled.Error(),
			cancel:      true,
		},
		{
			name:    "cycle anchor includes the exact effective component",
			filters: Filters{DepartmentID: "dept-cycle-a"},
			limit:   100,
			want: targetIDs(
				fixture.users["cycle-a"],
				fixture.users["cycle-b"],
				fixture.users["cycle-c"],
			),
		},
		{
			name:    "cycle non-anchor excludes the anchor-only user",
			filters: Filters{DepartmentID: "dept-cycle-b"},
			limit:   100,
			want: targetIDs(
				fixture.users["cycle-b"],
				fixture.users["cycle-c"],
			),
		},
		{
			name:    "cycle leaf includes only itself",
			filters: Filters{DepartmentID: "dept-cycle-c"},
			limit:   100,
			want:    targetIDs(fixture.users["cycle-c"]),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			users, err := service.Targets(ctx, tt.filters, tt.limit)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("Targets error = %v, want errors.Is(%v)", err, tt.wantErr)
			}
			if tt.wantErr == nil && tt.wantMessage == "" && err != nil {
				t.Fatalf("Targets: %v", err)
			}
			if tt.wantMessage != "" && (err == nil || !strings.Contains(err.Error(), tt.wantMessage)) {
				t.Fatalf("Targets error = %v, want message containing %q", err, tt.wantMessage)
			}
			if err != nil {
				return
			}
			if got := targetIDs(users...); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("target ids = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("department filter with no current source returns no targets", func(t *testing.T) {
		client := testdb.Open(t)
		user := createTargetUser(t, client, "no-source", "no-source@example.com", nil, "", nil)
		users, err := NewService(client).Targets(context.Background(), Filters{DepartmentID: "dept-alpha"}, 100)
		if err != nil {
			t.Fatalf("Targets: %v", err)
		}
		if got := targetIDs(users...); len(got) != 0 {
			t.Fatalf("target ids = %v, want empty without current source (seeded local user %d)", got, user.ID)
		}
	})
}

func seedTargetSemanticFixture(t *testing.T) targetSemanticFixture {
	t.Helper()
	client := testdb.Open(t)
	disabledAt := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	relayAlice := 2001
	relayBob := 2002
	users := map[string]*ent.User{
		"alice":   createTargetUser(t, client, "alice", "alice@example.com", &relayAlice, "encrypted-password", nil),
		"bob":     createTargetUser(t, client, "bob", " Bob@Example.org ", &relayBob, "", &disabledAt),
		"carol":   createTargetUser(t, client, "carol", "carol@example.net", nil, "", nil),
		"dave":    createTargetUser(t, client, "dave", "dave@example.com", nil, "", nil),
		"erin":    createTargetUser(t, client, "erin", "erin@example.org", nil, "", nil),
		"frank":   createTargetUser(t, client, "frank", "frank@example.net", nil, "", nil),
		"grace":   createTargetUser(t, client, "grace", "grace@example.com", nil, "", nil),
		"heidi":   createTargetUser(t, client, "heidi", "heidi@example.org", nil, "", nil),
		"cycle-a": createTargetUser(t, client, "cycle-a", "cycle-a-user@example.com", nil, "", nil),
		"cycle-b": createTargetUser(t, client, "cycle-b", "cycle-b-user@example.com", nil, "", nil),
		"cycle-c": createTargetUser(t, client, "cycle-c", "cycle-c-user@example.com", nil, "", nil),
	}

	staleSource, staleRun := createTargetSourceSnapshot(t, client, "Stale Directory", time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC))
	createTargetDepartment(t, client, staleSource.ID, staleRun.ID, "dept-alpha", "", "Department Alpha")
	createTargetDepartment(t, client, staleSource.ID, staleRun.ID, "dept-alpha-one", "dept-alpha", "Team Alpha One")
	staleFrank := createTargetMember(t, client, staleSource.ID, staleRun.ID, "stale-frank", "stale-frank@example.com", "dept-alpha-one", &users["frank"].ID)
	createTargetMembership(t, client, staleSource.ID, staleRun.ID, staleFrank, "dept-alpha-one")

	currentSource, currentRun := createTargetSourceSnapshot(t, client, "Current Directory", time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))
	createTargetDepartment(t, client, currentSource.ID, currentRun.ID, "dept-alpha", "", "Department Alpha")
	createTargetDepartment(t, client, currentSource.ID, currentRun.ID, "dept-alpha-one", "dept-alpha", "Team Alpha One")
	createTargetDepartment(t, client, currentSource.ID, currentRun.ID, "dept-beta", "", "Department Beta")
	createTargetDepartment(t, client, currentSource.ID, currentRun.ID, "dept-cycle-a", "dept-cycle-c", "Cycle Alpha")
	createTargetDepartment(t, client, currentSource.ID, currentRun.ID, "dept-cycle-b", "dept-cycle-a", "Cycle Beta")
	createTargetDepartment(t, client, currentSource.ID, currentRun.ID, "dept-cycle-c", "dept-cycle-b", "Cycle Gamma")

	alice := createTargetMember(t, client, currentSource.ID, currentRun.ID, "member-alice", "alice-directory@example.com", "dept-alpha-one", &users["alice"].ID)
	createTargetMembership(t, client, currentSource.ID, currentRun.ID, alice, "dept-alpha-one")
	bob := createTargetMember(t, client, currentSource.ID, currentRun.ID, "member-bob", "bob@example.org", "dept-alpha-one", nil)
	createTargetMembership(t, client, currentSource.ID, currentRun.ID, bob, "dept-alpha-one")
	carol := createTargetMember(t, client, currentSource.ID, currentRun.ID, "member-carol", "carol-directory@example.com", "dept-alpha", &users["carol"].ID)
	createTargetMembership(t, client, currentSource.ID, currentRun.ID, carol, "dept-alpha-one")
	createTargetMembership(t, client, currentSource.ID, currentRun.ID, carol, "dept-beta")
	dave := createTargetMember(t, client, currentSource.ID, currentRun.ID, "member-dave", "dave-directory@example.com", "dept-alpha-one", &users["dave"].ID)
	createTargetMembership(t, client, currentSource.ID, currentRun.ID, dave, "dept-beta")
	createTargetMember(t, client, currentSource.ID, currentRun.ID, "member-erin", "erin-directory@example.com", "dept-alpha-one", &users["erin"].ID)
	grace := createTargetMember(t, client, currentSource.ID, currentRun.ID, "member-grace", "heidi@example.org", "dept-alpha-one", &users["grace"].ID)
	createTargetMembership(t, client, currentSource.ID, currentRun.ID, grace, "dept-alpha-one")
	cycleA := createTargetMember(t, client, currentSource.ID, currentRun.ID, "member-cycle-a", "cycle-a-directory@example.com", "dept-cycle-a", &users["cycle-a"].ID)
	createTargetMembership(t, client, currentSource.ID, currentRun.ID, cycleA, "dept-cycle-a")
	cycleB := createTargetMember(t, client, currentSource.ID, currentRun.ID, "member-cycle-b", "cycle-b-directory@example.com", "dept-cycle-b", &users["cycle-b"].ID)
	createTargetMembership(t, client, currentSource.ID, currentRun.ID, cycleB, "dept-cycle-b")
	cycleC := createTargetMember(t, client, currentSource.ID, currentRun.ID, "member-cycle-c", "cycle-c-directory@example.com", "dept-cycle-c", &users["cycle-c"].ID)
	createTargetMembership(t, client, currentSource.ID, currentRun.ID, cycleC, "dept-cycle-c")

	return targetSemanticFixture{client: client, users: users}
}

func createTargetUser(t *testing.T, client *ent.Client, username, email string, relayUserID *int, relayPassword string, disabledAt *time.Time) *ent.User {
	t.Helper()
	builder := client.User.Create().
		SetUsername(username).
		SetEmail(email).
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser)
	if relayUserID != nil {
		builder.SetRelayUserID(*relayUserID)
	}
	if relayPassword != "" {
		builder.SetRelayAuthPassword(relayPassword)
	}
	if disabledAt != nil {
		builder.SetTokenValidAfter(*disabledAt)
	}
	user, err := builder.Save(context.Background())
	if err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return user
}

func createTargetSourceSnapshot(t *testing.T, client *ent.Client, name string, completedAt time.Time) (*ent.DirectorySource, *ent.DirectorySyncRun) {
	t.Helper()
	ctx := context.Background()
	source, err := client.DirectorySource.Create().
		SetName(name).
		SetDescription("Synthetic organization directory").
		SetScope(directorysource.ScopeFullCompany).
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		Save(ctx)
	if err != nil {
		t.Fatalf("create source %s: %v", name, err)
	}
	run, err := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode(directorysyncrun.ModeApply).
		SetStatus(directorysyncrun.StatusCompleted).
		SetPhase(directorysyncrun.PhaseCompleted).
		SetCompletedAt(completedAt).
		Save(ctx)
	if err != nil {
		t.Fatalf("create run for %s: %v", name, err)
	}
	if _, err := client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("update source %s run pointers: %v", name, err)
	}
	return source, run
}

func createTargetDepartment(t *testing.T, client *ent.Client, sourceID, runID int, externalID, parentID, name string) {
	t.Helper()
	builder := client.DirectoryDepartment.Create().
		SetSourceID(sourceID).
		SetExternalID(externalID).
		SetName(name).
		SetPath("synthetic/" + externalID).
		SetLastSeenRunID(runID)
	if parentID != "" {
		builder.SetParentExternalID(parentID)
	}
	if _, err := builder.Save(context.Background()); err != nil {
		t.Fatalf("create department %s: %v", externalID, err)
	}
}

func createTargetMember(t *testing.T, client *ent.Client, sourceID, runID int, externalID, email, legacyDepartmentID string, matchedUserID *int) *ent.DirectoryMember {
	t.Helper()
	builder := client.DirectoryMember.Create().
		SetSourceID(sourceID).
		SetExternalID(externalID).
		SetEmailNormalized(strings.ToLower(strings.TrimSpace(email))).
		SetDisplayName(externalID).
		SetDepartmentExternalID(legacyDepartmentID).
		SetLastSeenRunID(runID)
	if matchedUserID != nil {
		builder.SetMatchedUserID(*matchedUserID)
	}
	member, err := builder.Save(context.Background())
	if err != nil {
		t.Fatalf("create member %s: %v", externalID, err)
	}
	return member
}

func createTargetMembership(t *testing.T, client *ent.Client, sourceID, runID int, member *ent.DirectoryMember, departmentID string) {
	t.Helper()
	if _, err := client.DirectoryMemberDepartment.Create().
		SetSourceID(sourceID).
		SetDirectoryMemberID(member.ID).
		SetMemberExternalID(member.ExternalID).
		SetMemberEmailNormalized(member.EmailNormalized).
		SetDepartmentExternalID(departmentID).
		SetLastSeenRunID(runID).
		Save(context.Background()); err != nil {
		t.Fatalf("create membership %s/%s: %v", member.ExternalID, departmentID, err)
	}
}

func targetIDs(users ...*ent.User) []int {
	ids := make([]int, 0, len(users))
	for _, user := range users {
		if user != nil {
			ids = append(ids, user.ID)
		}
	}
	return ids
}

func describeTargetCounts(users, members, departments, memberships int) string {
	return fmt.Sprintf("users=%d/members=%d/departments=%d/memberships=%d", users, members, departments, memberships)
}
