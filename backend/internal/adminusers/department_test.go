package adminusers

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type departmentReadFixture struct {
	client          *ent.Client
	users           map[string]*ent.User
	departments     map[string]string
	rootDepartments []string
}

type departmentSeed struct {
	id                string
	parentID          string
	effectiveParentID string
	name              string
	metadata          map[string]any
}

func TestDepartmentOptionsPagingSelectionAndBounds(t *testing.T) {
	fixture := seedDepartmentReadFixture(t, false)
	service := NewService(fixture.client)
	wantAll := fixture.sortedDepartmentIDs("")

	tests := []struct {
		name         string
		request      DepartmentOptionRequest
		wantPage     int
		wantPageSize int
		wantIDs      []string
	}{
		{name: "defaults", request: DepartmentOptionRequest{}, wantPage: 1, wantPageSize: 20, wantIDs: wantAll[:20]},
		{name: "zero page size defaults", request: DepartmentOptionRequest{PageSize: 0}, wantPage: 1, wantPageSize: 20, wantIDs: wantAll[:20]},
		{name: "page size caps at one hundred", request: DepartmentOptionRequest{PageSize: 101}, wantPage: 1, wantPageSize: 100, wantIDs: wantAll},
		{name: "second page", request: DepartmentOptionRequest{Page: 2, PageSize: 20}, wantPage: 2, wantPageSize: 20, wantIDs: wantAll[20:]},
		{name: "maximum integer page overflows safely", request: DepartmentOptionRequest{Page: int(^uint(0) >> 1), PageSize: 100}, wantPage: int(^uint(0) >> 1), wantPageSize: 100, wantIDs: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := service.DepartmentOptions(context.Background(), tt.request)
			if err != nil {
				t.Fatalf("DepartmentOptions: %v", err)
			}
			if page.Page != tt.wantPage || page.PageSize != tt.wantPageSize || page.Total != len(wantAll) {
				t.Fatalf("page metadata = (%d, %d, %d), want (%d, %d, %d)", page.Page, page.PageSize, page.Total, tt.wantPage, tt.wantPageSize, len(wantAll))
			}
			if got := departmentOptionIDs(page.Items); !reflect.DeepEqual(got, tt.wantIDs) {
				t.Fatalf("option ids = %v, want %v", got, tt.wantIDs)
			}
		})
	}

	filteredIDs := fixture.sortedDepartmentIDs(" representative ")
	page, err := service.DepartmentOptions(context.Background(), DepartmentOptionRequest{
		Query:      " representative ",
		SelectedID: " dept-cycle-b ",
		Page:       1,
		PageSize:   1,
	})
	if err != nil {
		t.Fatalf("DepartmentOptions filtered: %v", err)
	}
	if page.Total != len(filteredIDs) || !reflect.DeepEqual(departmentOptionIDs(page.Items), filteredIDs[:1]) {
		t.Fatalf("filtered page = total %d ids %v, want total %d ids %v", page.Total, departmentOptionIDs(page.Items), len(filteredIDs), filteredIDs[:1])
	}
	if page.Selected == nil || page.Selected.ExternalID != "dept-cycle-b" || page.Selected.DisplayPath != "Cycle Alpha / Cycle Beta" {
		t.Fatalf("selected = %+v, want independently resolved current-source cycle B", page.Selected)
	}

	page, err = service.DepartmentOptions(context.Background(), DepartmentOptionRequest{Query: "dept-filler-0", SelectedID: "dept-unknown"})
	if err != nil {
		t.Fatalf("DepartmentOptions unknown selected: %v", err)
	}
	if page.Selected != nil {
		t.Fatalf("unknown selected = %+v, want nil", page.Selected)
	}
	for _, option := range page.Items {
		if !strings.Contains(option.ExternalID, "dept-filler-0") {
			t.Fatalf("external-id query returned %+v", option)
		}
	}
	ordered, err := service.DepartmentOptions(context.Background(), DepartmentOptionRequest{Query: "same", PageSize: 100})
	if err != nil {
		t.Fatalf("DepartmentOptions normalized tie order: %v", err)
	}
	if got := departmentOptionIDs(ordered.Items); !reflect.DeepEqual(got, []string{"dept-order-a", "dept-order-z"}) {
		t.Fatalf("normalized-name tie ids = %v, want external-id ascending", got)
	}

	empty, err := NewService(testdb.Open(t)).DepartmentOptions(context.Background(), DepartmentOptionRequest{SelectedID: "dept-alpha"})
	if err != nil {
		t.Fatalf("DepartmentOptions without source: %v", err)
	}
	if empty.Total != 0 || len(empty.Items) != 0 || empty.Selected != nil || empty.Page != 1 || empty.PageSize != 20 {
		t.Fatalf("source-less page = %+v, want normalized empty page", empty)
	}
}

func TestDepartmentChildrenPagingBounds(t *testing.T) {
	fixture := seedDepartmentReadFixture(t, false)
	service := NewService(fixture.client)
	wantRoots := fixture.sortedRootIDs()

	tests := []struct {
		name         string
		request      DepartmentChildrenRequest
		wantPage     int
		wantPageSize int
		wantIDs      []string
	}{
		{name: "defaults", request: DepartmentChildrenRequest{}, wantPage: 1, wantPageSize: 25, wantIDs: wantRoots[:25]},
		{name: "zero page size defaults", request: DepartmentChildrenRequest{PageSize: 0}, wantPage: 1, wantPageSize: 25, wantIDs: wantRoots[:25]},
		{name: "page size caps at one hundred", request: DepartmentChildrenRequest{PageSize: 101}, wantPage: 1, wantPageSize: 100, wantIDs: wantRoots},
		{name: "second root page", request: DepartmentChildrenRequest{Page: 2, PageSize: 25}, wantPage: 2, wantPageSize: 25, wantIDs: wantRoots[25:]},
		{name: "maximum integer page overflows safely", request: DepartmentChildrenRequest{Page: int(^uint(0) >> 1), PageSize: 100}, wantPage: int(^uint(0) >> 1), wantPageSize: 100, wantIDs: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := service.DepartmentChildren(context.Background(), tt.request)
			if err != nil {
				t.Fatalf("DepartmentChildren: %v", err)
			}
			if page.ParentDepartmentID != "" || page.Page != tt.wantPage || page.PageSize != tt.wantPageSize || page.Total != len(wantRoots) {
				t.Fatalf("page metadata = %+v, want parent empty/page %d/size %d/total %d", page, tt.wantPage, tt.wantPageSize, len(wantRoots))
			}
			if got := departmentSummaryIDs(page.Items); !reflect.DeepEqual(got, tt.wantIDs) {
				t.Fatalf("root ids = %v, want %v", got, tt.wantIDs)
			}
		})
	}
}

func TestDepartmentChildrenRequiresCurrentSourceParent(t *testing.T) {
	fixture := seedDepartmentReadFixture(t, false)
	service := NewService(fixture.client)

	page, err := service.DepartmentChildren(context.Background(), DepartmentChildrenRequest{ParentDepartmentID: " dept-alpha ", PageSize: 100})
	if err != nil {
		t.Fatalf("DepartmentChildren alpha: %v", err)
	}
	if page.ParentDepartmentID != "dept-alpha" || !reflect.DeepEqual(departmentSummaryIDs(page.Items), []string{"dept-alpha-child"}) {
		t.Fatalf("alpha child page = parent %q ids %v, want immediate child only", page.ParentDepartmentID, departmentSummaryIDs(page.Items))
	}

	for _, parentID := range []string{"dept-missing", "dept-unknown"} {
		page, err = service.DepartmentChildren(context.Background(), DepartmentChildrenRequest{ParentDepartmentID: parentID})
		if err != nil {
			t.Fatalf("DepartmentChildren %s: %v", parentID, err)
		}
		if page.Total != 0 || len(page.Items) != 0 {
			t.Fatalf("missing parent %s page = %+v, want empty despite non-current collision", parentID, page)
		}
	}

	roots, err := service.DepartmentChildren(context.Background(), DepartmentChildrenRequest{PageSize: 100})
	if err != nil {
		t.Fatalf("DepartmentChildren roots: %v", err)
	}
	if countString(departmentSummaryIDs(roots.Items), "dept-orphan") != 1 {
		t.Fatalf("orphan root ids = %v, want dept-orphan exactly once", departmentSummaryIDs(roots.Items))
	}
	orphan := requireDepartmentSummary(t, roots.Items, "dept-orphan")
	if orphan.ParentExternalID == nil || *orphan.ParentExternalID != "dept-missing" || orphan.Depth != 0 || orphan.DisplayPath != "Current Orphan" {
		t.Fatalf("orphan = %+v, want stored missing parent but effective current-source root", orphan)
	}

	empty, err := NewService(testdb.Open(t)).DepartmentChildren(context.Background(), DepartmentChildrenRequest{ParentDepartmentID: "dept-alpha"})
	if err != nil {
		t.Fatalf("DepartmentChildren without source: %v", err)
	}
	if empty.Total != 0 || len(empty.Items) != 0 || empty.ParentDepartmentID != "dept-alpha" || empty.Page != 1 || empty.PageSize != 25 {
		t.Fatalf("source-less page = %+v, want normalized empty page", empty)
	}
}

func TestDepartmentChildrenClosedCycleNavigation(t *testing.T) {
	walks := make([][]string, 0, 2)
	for _, reverse := range []bool{false, true} {
		fixture := seedDepartmentReadFixture(t, reverse)
		service := NewService(fixture.client)
		roots, err := service.DepartmentChildren(context.Background(), DepartmentChildrenRequest{PageSize: 100})
		if err != nil {
			t.Fatalf("DepartmentChildren roots reverse=%v: %v", reverse, err)
		}
		rootIDs := departmentSummaryIDs(roots.Items)
		if countString(rootIDs, "dept-cycle-a") != 1 || countString(rootIDs, "dept-cycle-b") != 0 || countString(rootIDs, "dept-cycle-c") != 0 {
			t.Fatalf("cycle roots reverse=%v = %v, want only anchor A once", reverse, rootIDs)
		}
		a := requireDepartmentSummary(t, roots.Items, "dept-cycle-a")
		if a.Depth != 0 || a.DisplayPath != "Cycle Alpha" {
			t.Fatalf("cycle anchor reverse=%v = %+v", reverse, a)
		}

		walk := []string{"dept-cycle-a"}
		parent := "dept-cycle-a"
		for {
			children, err := service.DepartmentChildren(context.Background(), DepartmentChildrenRequest{ParentDepartmentID: parent, PageSize: 100})
			if err != nil {
				t.Fatalf("DepartmentChildren %s reverse=%v: %v", parent, reverse, err)
			}
			ids := departmentSummaryIDs(children.Items)
			if len(ids) == 0 {
				break
			}
			if len(ids) != 1 {
				t.Fatalf("cycle children of %s reverse=%v = %v, want one", parent, reverse, ids)
			}
			walk = append(walk, ids[0])
			parent = ids[0]
			if len(walk) > 3 {
				t.Fatalf("cycle navigation repeated anchor: %v", walk)
			}
		}
		if !reflect.DeepEqual(walk, []string{"dept-cycle-a", "dept-cycle-b", "dept-cycle-c"}) {
			t.Fatalf("cycle walk reverse=%v = %v", reverse, walk)
		}
		walks = append(walks, walk)
	}
	if !reflect.DeepEqual(walks[0], walks[1]) {
		t.Fatalf("cycle walk changed with insertion order: normal=%v reverse=%v", walks[0], walks[1])
	}
}

func TestDepartmentChildrenUsesPersistedCycleAnchorUnderICUCollation(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open raw postgres connection: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.ExecContext(ctx, `ALTER TABLE directory_departments ALTER COLUMN name TYPE varchar COLLATE "en-US-x-icu"`); err != nil {
		t.Fatalf("set synthetic ICU name collation: %v", err)
	}

	source, run := createTargetSourceSnapshot(t, client, "Locale Independent Directory", time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC))
	createDepartmentSeeds(t, client, source.ID, run.ID, []departmentSeed{
		{id: "dept-zulu", parentID: "dept-dotted-i", name: "  Zulu  "},
		{id: "dept-dotted-i", parentID: "dept-zulu", effectiveParentID: "dept-zulu", name: "İstanbul"},
	}, false)

	roots, err := NewService(client).DepartmentChildren(ctx, DepartmentChildrenRequest{PageSize: 100})
	if err != nil {
		t.Fatalf("DepartmentChildren roots: %v", err)
	}
	if got := departmentSummaryIDs(roots.Items); !reflect.DeepEqual(got, []string{"dept-zulu"}) {
		t.Fatalf("cycle root ids = %v, want persisted locale-independent anchor dept-zulu", got)
	}
}

func TestHierarchyCleanupCompleteDepartmentsUsesLocaleIndependentTieBreak(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%v", reverse), func(t *testing.T) {
			client, dsn := testdb.OpenWithDSN(t)
			ctx := context.Background()
			db, err := sql.Open("postgres", dsn)
			if err != nil {
				t.Fatalf("open raw postgres connection: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if _, err := db.ExecContext(ctx, `ALTER TABLE directory_departments ALTER COLUMN external_id TYPE varchar COLLATE "en-US-x-icu"`); err != nil {
				t.Fatalf("set synthetic ICU external-id collation: %v", err)
			}

			source, run := createTargetSourceSnapshot(t, client, "Complete Locale Directory", time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC))
			createDepartmentSeeds(t, client, source.ID, run.ID, []departmentSeed{
				{id: "dept-Zulu", name: "Same Department"},
				{id: "dept-İstanbul", name: "Same Department"},
			}, reverse)

			departments, err := NewService(client).Departments(ctx)
			if err != nil {
				t.Fatalf("Departments: %v", err)
			}
			if got := departmentSummaryIDs(departments); !reflect.DeepEqual(got, []string{"dept-Zulu", "dept-İstanbul"}) {
				t.Fatalf("complete department order = %v, want UTF-8/C tie-break independent of insertion order", got)
			}
		})
	}
}

func TestAdministratorReadsUsePersistedEffectiveParents(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source, run := createTargetSourceSnapshot(t, client, "Persisted Hierarchy Directory", time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC))
	createDepartmentSeeds(t, client, source.ID, run.ID, []departmentSeed{
		{id: "dept-persisted-root", parentID: "dept-persisted-child", name: "Persisted Root"},
		{id: "dept-persisted-child", parentID: "dept-missing", effectiveParentID: "dept-persisted-root", name: "Persisted Child"},
		{id: "dept-persisted-leaf", effectiveParentID: "dept-persisted-child", name: "Persisted Leaf"},
	}, false)

	users := make(map[string]*ent.User, 3)
	for _, departmentID := range []string{"dept-persisted-root", "dept-persisted-child", "dept-persisted-leaf"} {
		key := strings.TrimPrefix(departmentID, "dept-persisted-")
		user := createTargetUser(t, client, "persisted-"+key, "persisted-"+key+"@example.org", nil, "", nil)
		member := createTargetMember(t, client, source.ID, run.ID, "member-persisted-"+key, "directory-persisted-"+key+"@example.org", departmentID, &user.ID)
		createTargetMembership(t, client, source.ID, run.ID, member, departmentID)
		users[key] = user
	}

	service := NewService(client)
	roots, err := service.DepartmentChildren(ctx, DepartmentChildrenRequest{PageSize: 100})
	if err != nil {
		t.Fatalf("DepartmentChildren roots: %v", err)
	}
	if got := departmentSummaryIDs(roots.Items); !reflect.DeepEqual(got, []string{"dept-persisted-root"}) {
		t.Fatalf("persisted root ids = %v, want only dept-persisted-root", got)
	}
	children, err := service.DepartmentChildren(ctx, DepartmentChildrenRequest{ParentDepartmentID: "dept-persisted-root", PageSize: 100})
	if err != nil {
		t.Fatalf("DepartmentChildren persisted root: %v", err)
	}
	child := requireDepartmentSummary(t, children.Items, "dept-persisted-child")
	if child.DisplayPath != "Persisted Root / Persisted Child" || child.SubtreeMemberCount != 2 || child.SubtreeMatchedUserCount != 2 {
		t.Fatalf("persisted child summary = %+v, want persisted path and child/leaf totals", child)
	}

	options, err := service.DepartmentOptions(ctx, DepartmentOptionRequest{SelectedID: "dept-persisted-leaf", PageSize: 100})
	if err != nil {
		t.Fatalf("DepartmentOptions persisted leaf: %v", err)
	}
	if options.Selected == nil || options.Selected.DisplayPath != "Persisted Root / Persisted Child / Persisted Leaf" {
		t.Fatalf("persisted selected option = %+v", options.Selected)
	}

	wantIDs := targetIDs(users["child"], users["leaf"])
	page, err := service.List(ctx, ListRequest{Filters: Filters{DepartmentID: "dept-persisted-child"}, Page: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("List persisted child: %v", err)
	}
	if got := targetIDs(page.Users...); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("persisted child List ids = %v, want %v", got, wantIDs)
	}
	targets, err := service.Targets(ctx, Filters{DepartmentID: "dept-persisted-child"}, 100)
	if err != nil {
		t.Fatalf("Targets persisted child: %v", err)
	}
	if got := targetIDs(targets...); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("persisted child Targets ids = %v, want %v", got, wantIDs)
	}
}

func TestDepartmentChildrenEffectiveSubtreeParity(t *testing.T) {
	fixture := seedDepartmentReadFixture(t, false)
	service := NewService(fixture.client)

	aChildren, err := service.DepartmentChildren(context.Background(), DepartmentChildrenRequest{ParentDepartmentID: "dept-cycle-a", PageSize: 100})
	if err != nil {
		t.Fatalf("DepartmentChildren cycle A: %v", err)
	}
	b := requireDepartmentSummary(t, aChildren.Items, "dept-cycle-b")
	if b.ChildCount != 1 || !b.HasChildren || b.MemberCount != 1 || b.MatchedUserCount != 1 || b.SubtreeMemberCount != 2 || b.SubtreeMatchedUserCount != 2 {
		t.Fatalf("cycle B summary = %+v, want effective subtree {b,c} counts 1/1/2/2", b)
	}
	if b.Depth != 1 || b.DisplayPath != "Cycle Alpha / Cycle Beta" {
		t.Fatalf("cycle B presentation = %+v", b)
	}

	list, err := service.List(context.Background(), ListRequest{Filters: Filters{DepartmentID: "dept-cycle-b"}, Page: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("List cycle B: %v", err)
	}
	targets, err := service.Targets(context.Background(), Filters{DepartmentID: "dept-cycle-b"}, 100)
	if err != nil {
		t.Fatalf("Targets cycle B: %v", err)
	}
	wantIDs := targetIDs(fixture.users["cycle-b"], fixture.users["cycle-c"])
	if got := targetIDs(list.Users...); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("cycle B List ids = %v, want %v", got, wantIDs)
	}
	if got := targetIDs(targets...); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("cycle B Targets ids = %v, want %v", got, wantIDs)
	}
	if b.SubtreeMatchedUserCount != len(wantIDs) || b.SubtreeMemberCount != len(wantIDs) {
		t.Fatalf("cycle B summary totals = %d/%d, want List/Targets cardinality %d", b.SubtreeMemberCount, b.SubtreeMatchedUserCount, len(wantIDs))
	}

	roots, err := service.DepartmentChildren(context.Background(), DepartmentChildrenRequest{PageSize: 100})
	if err != nil {
		t.Fatalf("DepartmentChildren roots: %v", err)
	}
	alpha := requireDepartmentSummary(t, roots.Items, "dept-alpha")
	if alpha.ChildCount != 1 || alpha.MemberCount != 1 || alpha.MatchedUserCount != 1 || alpha.SubtreeMemberCount != 2 || alpha.SubtreeMatchedUserCount != 2 {
		t.Fatalf("alpha legacy-fixture parity = %+v, want child/direct/subtree 1/1/2", alpha)
	}
	alphaChildren, err := service.DepartmentChildren(context.Background(), DepartmentChildrenRequest{ParentDepartmentID: "dept-alpha", PageSize: 100})
	if err != nil {
		t.Fatalf("DepartmentChildren alpha: %v", err)
	}
	child := requireDepartmentSummary(t, alphaChildren.Items, "dept-alpha-child")
	if child.ChildCount != 1 || child.MemberCount != 1 || child.MatchedUserCount != 1 || child.SubtreeMemberCount != 1 || child.SubtreeMatchedUserCount != 1 {
		t.Fatalf("alpha child legacy-fixture parity = %+v", child)
	}
}

func TestDepartmentChildrenRepresentativeJSONShapes(t *testing.T) {
	fixture := seedDepartmentReadFixture(t, false)
	page, err := NewService(fixture.client).DepartmentChildren(context.Background(), DepartmentChildrenRequest{PageSize: 100})
	if err != nil {
		t.Fatalf("DepartmentChildren roots: %v", err)
	}
	main := requireDepartmentSummary(t, page.Items, "dept-representative-main")
	if main.RepresentativeCount != 5 || main.MatchedRepresentativeCount != 3 {
		t.Fatalf("main representative counts = %d/%d, want 5/3", main.RepresentativeCount, main.MatchedRepresentativeCount)
	}
	scalar := requireDepartmentSummary(t, page.Items, "dept-representative-scalar")
	if scalar.RepresentativeCount != 1 || scalar.MatchedRepresentativeCount != 0 {
		t.Fatalf("scalar representative counts = %d/%d, want 1/0", scalar.RepresentativeCount, scalar.MatchedRepresentativeCount)
	}
	if main.DisplayPath != "Current Representative Main" || main.Depth != 0 || scalar.DisplayPath != "Current Representative Scalar" {
		t.Fatalf("current-source representative presentation drifted: main=%+v scalar=%+v", main, scalar)
	}
}

func seedDepartmentReadFixture(t *testing.T, reverse bool) departmentReadFixture {
	t.Helper()
	client := testdb.Open(t)
	users := map[string]*ent.User{}
	for _, key := range []string{"alpha", "alpha-child", "cycle-a", "cycle-b", "cycle-c", "rep-department-matched", "rep-leader-matched", "rep-duplicate"} {
		users[key] = createTargetUser(t, client, key, key+"@example.com", nil, "", nil)
	}

	staleSource, staleRun := createTargetSourceSnapshot(t, client, "Stale Department Read Directory", time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC))
	staleDepartments := []departmentSeed{
		{id: "dept-missing", name: "Stale Missing Parent"},
		{id: "dept-alpha", name: "Stale Alpha"},
		{id: "dept-cycle-a", name: "Stale Cycle Alpha"},
		{id: "dept-cycle-b", parentID: "dept-cycle-a", name: "Stale Cycle Beta"},
		{id: "dept-representative-main", name: "Stale Representative Main", metadata: map[string]any{"representative_external_ids": []any{"rep-department-matched", "rep-department-unmatched", "rep-duplicate"}}},
		{id: "dept-representative-scalar", name: "Stale Representative Scalar", metadata: map[string]any{"representative_external_ids": "rep-scalar-unmatched"}},
	}
	createDepartmentSeeds(t, client, staleSource.ID, staleRun.ID, staleDepartments, false)
	for index, rep := range []struct {
		id      string
		matched bool
		leader  any
	}{
		{id: "rep-department-matched", matched: false},
		{id: "rep-department-unmatched", matched: true},
		{id: "rep-leader-matched", matched: false, leader: "dept-representative-main"},
		{id: "rep-leader-unmatched", matched: true, leader: []any{"dept-representative-main"}},
		{id: "rep-duplicate", matched: false, leader: []any{"dept-representative-main"}},
		{id: "rep-scalar-unmatched", matched: true},
	} {
		createDepartmentRepresentative(t, client, staleSource.ID, staleRun.ID, rep.id, fmt.Sprintf("stale-%d-%s@example.org", index, rep.id), rep.matched, rep.leader)
	}

	currentSource, currentRun := createTargetSourceSnapshot(t, client, "Current Department Read Directory", time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))
	departments := []departmentSeed{
		{id: "dept-alpha", name: "  Alpha Department  "},
		{id: "dept-alpha-child", parentID: "dept-alpha", effectiveParentID: "dept-alpha", name: "Alpha Child"},
		{id: "dept-alpha-grandchild", parentID: "dept-alpha-child", effectiveParentID: "dept-alpha-child", name: "Alpha Grandchild"},
		{id: "dept-orphan", parentID: "dept-missing", name: "Current Orphan"},
		{id: "dept-cycle-a", parentID: "dept-cycle-c", name: "Cycle Alpha"},
		{id: "dept-cycle-b", parentID: "dept-cycle-a", effectiveParentID: "dept-cycle-a", name: "Cycle Beta"},
		{id: "dept-cycle-c", parentID: "dept-cycle-b", effectiveParentID: "dept-cycle-b", name: "Cycle Gamma"},
		{id: "dept-representative-main", name: "Current Representative Main", metadata: map[string]any{"representative_external_ids": []any{"rep-department-matched", "rep-department-unmatched", "rep-duplicate", "rep-duplicate"}}},
		{id: "dept-representative-scalar", name: "Current Representative Scalar", metadata: map[string]any{"representative_external_ids": "rep-scalar-unmatched"}},
		{id: "dept-order-z", name: "same"},
		{id: "dept-order-a", name: "  SAME  "},
	}
	for i := 0; i < 25; i++ {
		departments = append(departments, departmentSeed{id: fmt.Sprintf("dept-filler-%02d", i), name: fmt.Sprintf("Team %02d", i)})
	}
	createDepartmentSeeds(t, client, currentSource.ID, currentRun.ID, departments, reverse)

	for _, assignment := range []struct {
		key          string
		departmentID string
	}{
		{key: "alpha", departmentID: "dept-alpha"},
		{key: "alpha-child", departmentID: "dept-alpha-child"},
		{key: "cycle-a", departmentID: "dept-cycle-a"},
		{key: "cycle-b", departmentID: "dept-cycle-b"},
		{key: "cycle-c", departmentID: "dept-cycle-c"},
	} {
		user := users[assignment.key]
		member := createTargetMember(t, client, currentSource.ID, currentRun.ID, "member-"+assignment.key, "directory-"+assignment.key+"@example.com", assignment.departmentID, &user.ID)
		createTargetMembership(t, client, currentSource.ID, currentRun.ID, member, assignment.departmentID)
	}
	createDepartmentRepresentative(t, client, currentSource.ID, currentRun.ID, "rep-department-matched", "rep-department-matched@example.org", true, nil)
	createDepartmentRepresentative(t, client, currentSource.ID, currentRun.ID, "rep-department-unmatched", "rep-department-unmatched@example.org", false, nil)
	createDepartmentRepresentative(t, client, currentSource.ID, currentRun.ID, "rep-leader-matched", "rep-leader-matched@example.org", true, "dept-representative-main")
	createDepartmentRepresentative(t, client, currentSource.ID, currentRun.ID, "rep-leader-unmatched", "rep-leader-unmatched@example.org", false, []any{"dept-representative-main", "dept-representative-main"})
	createDepartmentRepresentative(t, client, currentSource.ID, currentRun.ID, "rep-duplicate", "rep-duplicate@example.org", true, []any{"dept-representative-main"})
	createDepartmentRepresentative(t, client, currentSource.ID, currentRun.ID, "rep-scalar-unmatched", "rep-scalar-unmatched@example.org", false, nil)

	departmentNames := make(map[string]string, len(departments))
	rootIDs := make([]string, 0, len(departments))
	for _, department := range departments {
		departmentNames[department.id] = department.name
		if department.parentID == "" || department.id == "dept-cycle-a" || department.id == "dept-orphan" {
			rootIDs = append(rootIDs, department.id)
		}
	}
	return departmentReadFixture{client: client, users: users, departments: departmentNames, rootDepartments: rootIDs}
}

func createDepartmentSeeds(t *testing.T, client *ent.Client, sourceID, runID int, departments []departmentSeed, reverse bool) {
	t.Helper()
	ordered := append([]departmentSeed(nil), departments...)
	if reverse {
		for left, right := 0, len(ordered)-1; left < right; left, right = left+1, right-1 {
			ordered[left], ordered[right] = ordered[right], ordered[left]
		}
	}
	for _, department := range ordered {
		builder := client.DirectoryDepartment.Create().
			SetSourceID(sourceID).
			SetExternalID(department.id).
			SetName(department.name).
			SetPath("current/" + department.id).
			SetLastSeenRunID(runID)
		if department.parentID != "" {
			builder.SetParentExternalID(department.parentID)
		}
		if department.effectiveParentID != "" {
			builder.SetEffectiveParentExternalID(department.effectiveParentID)
		}
		if department.metadata != nil {
			builder.SetMetadata(department.metadata)
		}
		if _, err := builder.Save(context.Background()); err != nil {
			t.Fatalf("create department %s: %v", department.id, err)
		}
	}
}

func createDepartmentRepresentative(t *testing.T, client *ent.Client, sourceID, runID int, externalID, email string, matched bool, leaderDepartments any) {
	t.Helper()
	builder := client.DirectoryMember.Create().
		SetSourceID(sourceID).
		SetExternalID(externalID).
		SetEmailNormalized(email).
		SetDisplayName(externalID).
		SetLastSeenRunID(runID)
	if matched {
		builder.SetMatchedUserID(900000 + runID)
	}
	if leaderDepartments != nil {
		builder.SetMetadata(map[string]any{"leader_department_ids": leaderDepartments})
	}
	if _, err := builder.Save(context.Background()); err != nil {
		t.Fatalf("create representative %s: %v", externalID, err)
	}
}

func (f departmentReadFixture) sortedDepartmentIDs(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	ids := make([]string, 0, len(f.departments))
	for id, name := range f.departments {
		if query == "" || strings.Contains(strings.ToLower(strings.TrimSpace(name)), query) || strings.Contains(strings.ToLower(id), query) {
			ids = append(ids, id)
		}
	}
	f.sortDepartmentIDs(ids)
	return ids
}

func (f departmentReadFixture) sortedRootIDs() []string {
	ids := append([]string(nil), f.rootDepartments...)
	f.sortDepartmentIDs(ids)
	return ids
}

func (f departmentReadFixture) sortDepartmentIDs(ids []string) {
	sort.Slice(ids, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(f.departments[ids[i]]))
		right := strings.ToLower(strings.TrimSpace(f.departments[ids[j]]))
		if left == right {
			return ids[i] < ids[j]
		}
		return left < right
	})
}

func departmentOptionIDs(items []DepartmentOption) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ExternalID)
	}
	return ids
}

func departmentSummaryIDs(items []DepartmentSummary) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ExternalID)
	}
	return ids
}

func requireDepartmentSummary(t *testing.T, items []DepartmentSummary, externalID string) DepartmentSummary {
	t.Helper()
	for _, item := range items {
		if item.ExternalID == externalID {
			return item
		}
	}
	t.Fatalf("department %s not found in %v", externalID, departmentSummaryIDs(items))
	return DepartmentSummary{}
}

func countString(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}
