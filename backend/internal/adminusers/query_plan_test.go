package adminusers

import (
	"context"
	stdsql "database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	entdialect "entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/testdb"
	_ "github.com/lib/pq"
)

type targetPlanScale struct {
	name        string
	users       int
	members     int
	departments int
	memberships int
}

func TestTargetPlanRecursiveRelationsRunOnceAcrossScales(t *testing.T) {
	scales := []targetPlanScale{
		{name: "small", users: 24, members: 22, departments: 12, memberships: 36},
		{name: "large", users: 2400, members: 2200, departments: 120, memberships: 3600},
	}

	for _, scale := range scales {
		t.Run(scale.name, func(t *testing.T) {
			client, dsn := testdb.OpenWithDSN(t)
			ctx := context.Background()
			sourceID := seedTargetPlanFixture(t, client, scale)
			assertTargetPlanFixtureCounts(t, client, scale)
			analyzeTargetPlanTables(t, dsn)

			captureClient, recorder := newTargetQueryCaptureClient(t, dsn)
			users, err := NewService(captureClient).Targets(ctx, Filters{DepartmentID: "dept-cycle-b"}, scale.users+1)
			if err != nil {
				t.Fatalf("Targets: %v", err)
			}
			if len(users) == 0 {
				t.Fatal("Targets returned no cycle-b users; fixture does not exercise the filtered statement")
			}
			query, args := recorder.targetQuery(t)

			sourcePlaceholder := placeholderForBoundValue(t, args, int64(sourceID))
			departmentPlaceholder := placeholderForBoundValue(t, args, "dept-cycle-b")
			assertExactEffectivePrefix(t, query, sourcePlaceholder, departmentPlaceholder)
			if !strings.Contains(query, `"users"."id" IN (WITH RECURSIVE`) {
				t.Fatalf("target predicate is not selector-qualified and uncorrelated:\n%s", query)
			}

			plan := explainTargetPlan(t, dsn, query, args)
			assertNamedRecursiveUnionLoopsOnce(t, plan, "cycle_walk")
			assertNamedRecursiveUnionLoopsOnce(t, plan, "subtree")
		})
	}
}

func assertExactEffectivePrefix(t *testing.T, query, sourcePlaceholder, departmentPlaceholder string) {
	t.Helper()
	start := strings.Index(query, "WITH RECURSIVE")
	if start < 0 {
		t.Fatalf("target SQL has no WITH RECURSIVE prefix:\n%s", query)
	}
	want := effectiveDepartmentCTEs(sourcePlaceholder) + effectiveSubtreeCTE(departmentPlaceholder)
	if len(query) < start+len(want) {
		t.Fatalf("target SQL shorter than shared effective prefix:\n%s", query)
	}
	if got := query[start : start+len(want)]; got != want {
		t.Fatalf("effective prefix drifted\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func assertNamedRecursiveUnionLoopsOnce(t *testing.T, plan any, cteName string) {
	t.Helper()
	wantName := "CTE " + cteName
	matches := make([]map[string]any, 0, 1)
	walkPlanJSON(plan, func(node map[string]any) {
		if node["Subplan Name"] == wantName {
			matches = append(matches, node)
		}
	})
	if len(matches) != 1 {
		t.Fatalf("plan nodes named %q = %d, want exactly 1; plan=%s", wantName, len(matches), compactPlanJSON(plan))
	}
	node := matches[0]
	if got := node["Node Type"]; got != "Recursive Union" {
		t.Fatalf("%s node type = %v, want Recursive Union", wantName, got)
	}
	if got := node["Actual Loops"]; got != float64(1) {
		t.Fatalf("%s actual loops = %v, want 1", wantName, got)
	}
}

func walkPlanJSON(value any, visit func(map[string]any)) {
	switch value := value.(type) {
	case map[string]any:
		visit(value)
		for _, child := range value {
			walkPlanJSON(child, visit)
		}
	case []any:
		for _, child := range value {
			walkPlanJSON(child, visit)
		}
	}
}

func compactPlanJSON(plan any) string {
	data, err := json.Marshal(plan)
	if err != nil {
		return fmt.Sprintf("<marshal plan: %v>", err)
	}
	return string(data)
}

func explainTargetPlan(t *testing.T, dsn, query string, args []any) any {
	t.Helper()
	db, err := stdsql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open explain database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var raw []byte
	if err := db.QueryRowContext(
		context.Background(),
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query,
		args...,
	).Scan(&raw); err != nil {
		t.Fatalf("explain target query: %v\nSQL:\n%s\nargs=%#v", err, query, args)
	}
	var plan any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("decode target plan: %v; raw=%s", err, raw)
	}
	return plan
}

type targetQueryRecorder struct {
	entdialect.Driver
	mu    sync.Mutex
	query string
	args  []any
}

func (r *targetQueryRecorder) Query(ctx context.Context, query string, args, rows any) error {
	if strings.Contains(query, "eligible_user_ids") {
		r.mu.Lock()
		r.query = query
		if values, ok := args.([]any); ok {
			r.args = append([]any(nil), values...)
		} else {
			r.args = nil
		}
		r.mu.Unlock()
	}
	return r.Driver.Query(ctx, query, args, rows)
}

func (r *targetQueryRecorder) targetQuery(t *testing.T) (string, []any) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.query == "" {
		t.Fatal("filtered target SQL was not captured")
	}
	if r.args == nil {
		t.Fatal("filtered target SQL arguments were not captured as []any")
	}
	return r.query, append([]any(nil), r.args...)
}

func newTargetQueryCaptureClient(t *testing.T, dsn string) (*ent.Client, *targetQueryRecorder) {
	t.Helper()
	db, err := stdsql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open capture database: %v", err)
	}
	recorder := &targetQueryRecorder{Driver: entsql.OpenDB(entdialect.Postgres, db)}
	client := ent.NewClient(ent.Driver(recorder))
	t.Cleanup(func() { _ = client.Close() })
	return client, recorder
}

func placeholderForBoundValue(t *testing.T, args []any, want any) string {
	t.Helper()
	for i, arg := range args {
		value := arg
		if valuer, ok := arg.(driver.Valuer); ok {
			var err error
			value, err = valuer.Value()
			if err != nil {
				t.Fatalf("resolve bound argument %d: %v", i+1, err)
			}
		}
		if reflect.DeepEqual(value, want) || fmt.Sprint(value) == fmt.Sprint(want) {
			return fmt.Sprintf("$%d", i+1)
		}
	}
	t.Fatalf("bound value %#v not found in args %#v", want, args)
	return ""
}

func seedTargetPlanFixture(t *testing.T, client *ent.Client, scale targetPlanScale) int {
	t.Helper()
	ctx := context.Background()
	source, run := createTargetSourceSnapshot(t, client, "Plan Directory "+scale.name, time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))
	if _, err := client.DirectorySyncRun.UpdateOneID(run.ID).
		SetDepartmentCount(scale.departments).
		SetMemberCount(scale.members).
		Save(ctx); err != nil {
		t.Fatalf("set plan run counts: %v", err)
	}

	userBuilders := make([]*ent.UserCreate, 0, scale.users)
	for i := 0; i < scale.users; i++ {
		userBuilders = append(userBuilders, client.User.Create().
			SetUsername(fmt.Sprintf("plan-user-%04d", i)).
			SetEmail(fmt.Sprintf("plan-user-%04d@example.com", i)).
			SetAuthSource(entuser.AuthSourceLdap).
			SetRole(entuser.RoleUser).
			SetRelayUserID(100000+i))
	}
	users, err := client.User.CreateBulk(userBuilders...).Save(ctx)
	if err != nil {
		t.Fatalf("create plan users: %v", err)
	}

	departmentIDs := make([]string, 0, scale.departments)
	departmentBuilders := make([]*ent.DirectoryDepartmentCreate, 0, scale.departments)
	for i := 0; i < scale.departments; i++ {
		externalID := fmt.Sprintf("dept-%03d", i)
		name := fmt.Sprintf("Department %03d", i)
		parentID := ""
		switch i {
		case 0:
			externalID, parentID, name = "dept-cycle-a", "dept-cycle-c", "Cycle Alpha"
		case 1:
			externalID, parentID, name = "dept-cycle-b", "dept-cycle-a", "Cycle Beta"
		case 2:
			externalID, parentID, name = "dept-cycle-c", "dept-cycle-b", "Cycle Gamma"
		default:
			if i > 3 && i%3 != 0 {
				parentID = fmt.Sprintf("dept-%03d", i-1)
			}
		}
		departmentIDs = append(departmentIDs, externalID)
		builder := client.DirectoryDepartment.Create().
			SetSourceID(source.ID).
			SetExternalID(externalID).
			SetName(name).
			SetPath("synthetic/" + externalID).
			SetLastSeenRunID(run.ID)
		if parentID != "" {
			builder.SetParentExternalID(parentID)
		}
		departmentBuilders = append(departmentBuilders, builder)
	}
	if _, err := client.DirectoryDepartment.CreateBulk(departmentBuilders...).Save(ctx); err != nil {
		t.Fatalf("create plan departments: %v", err)
	}

	legacyOnlyCount := scale.members / 10
	if legacyOnlyCount < 1 {
		legacyOnlyCount = 1
	}
	memberBuilders := make([]*ent.DirectoryMemberCreate, 0, scale.members)
	memberEmails := make([]string, scale.members)
	memberDepartments := make([]string, scale.members)
	for i := 0; i < scale.members; i++ {
		legacyDepartmentID := departmentIDs[i%len(departmentIDs)]
		email := users[i].Email
		builder := client.DirectoryMember.Create().
			SetSourceID(source.ID).
			SetExternalID(fmt.Sprintf("plan-member-%04d", i)).
			SetDisplayName(fmt.Sprintf("Plan Member %04d", i)).
			SetDepartmentExternalID(legacyDepartmentID).
			SetLastSeenRunID(run.ID)
		if i%2 == 0 {
			email = fmt.Sprintf("directory-only-%04d@example.org", i)
			builder.SetMatchedUserID(users[i].ID)
		}
		memberEmails[i] = strings.ToLower(strings.TrimSpace(email))
		memberDepartments[i] = legacyDepartmentID
		memberBuilders = append(memberBuilders, builder.SetEmailNormalized(memberEmails[i]))
	}
	members, err := client.DirectoryMember.CreateBulk(memberBuilders...).Save(ctx)
	if err != nil {
		t.Fatalf("create plan members: %v", err)
	}

	currentMemberCount := scale.members - legacyOnlyCount
	membershipPairs := make([][2]int, 0, scale.memberships)
	seenPairs := make(map[[2]int]struct{}, scale.memberships)
	addPair := func(memberIndex, departmentIndex int) {
		pair := [2]int{memberIndex, departmentIndex}
		if _, exists := seenPairs[pair]; exists || len(membershipPairs) >= scale.memberships {
			return
		}
		seenPairs[pair] = struct{}{}
		membershipPairs = append(membershipPairs, pair)
	}
	for i := 0; i < currentMemberCount; i++ {
		addPair(i, i%len(departmentIDs))
	}
	for round := 1; len(membershipPairs) < scale.memberships; round++ {
		for memberIndex := 0; memberIndex < currentMemberCount && len(membershipPairs) < scale.memberships; memberIndex++ {
			addPair(memberIndex, (memberIndex+round)%len(departmentIDs))
		}
	}
	membershipBuilders := make([]*ent.DirectoryMemberDepartmentCreate, 0, len(membershipPairs))
	for _, pair := range membershipPairs {
		memberIndex, departmentIndex := pair[0], pair[1]
		member := members[memberIndex]
		membershipBuilders = append(membershipBuilders, client.DirectoryMemberDepartment.Create().
			SetSourceID(source.ID).
			SetDirectoryMemberID(member.ID).
			SetMemberExternalID(member.ExternalID).
			SetMemberEmailNormalized(memberEmails[memberIndex]).
			SetDepartmentExternalID(departmentIDs[departmentIndex]).
			SetLastSeenRunID(run.ID))
	}
	if _, err := client.DirectoryMemberDepartment.CreateBulk(membershipBuilders...).Save(ctx); err != nil {
		t.Fatalf("create plan memberships: %v", err)
	}
	return source.ID
}

func assertTargetPlanFixtureCounts(t *testing.T, client *ent.Client, scale targetPlanScale) {
	t.Helper()
	ctx := context.Background()
	got := targetPlanScale{
		name:        scale.name,
		users:       client.User.Query().CountX(ctx),
		members:     client.DirectoryMember.Query().CountX(ctx),
		departments: client.DirectoryDepartment.Query().CountX(ctx),
		memberships: client.DirectoryMemberDepartment.Query().CountX(ctx),
	}
	if got != scale {
		t.Fatalf("fixture counts = %s, want %s", describeTargetCounts(got.users, got.members, got.departments, got.memberships), describeTargetCounts(scale.users, scale.members, scale.departments, scale.memberships))
	}
}

func analyzeTargetPlanTables(t *testing.T, dsn string) {
	t.Helper()
	db, err := stdsql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open analyze database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, table := range []string{
		entuser.Table,
		directorydepartment.Table,
		directorymember.Table,
		directorymemberdepartment.Table,
		directorysyncrun.Table,
	} {
		if _, err := db.ExecContext(context.Background(), "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
}
