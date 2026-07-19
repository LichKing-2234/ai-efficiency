package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/directoryoffboardingaction"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
)

const adminUsersTestEncryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"

type adminUsersFixture struct {
	aliceID    int
	bobID      int
	carolID    int
	ciphertext string
}

func seedAdminUsersFixture(t *testing.T, env *fullTestEnv) adminUsersFixture {
	t.Helper()
	ctx := context.Background()

	ciphertext, err := pkg.Encrypt("test-password", adminUsersTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt relay password: %v", err)
	}

	alice, err := env.client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SetRelayAuthPassword(ciphertext).
		Save(ctx)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	bob, err := env.client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource("relay_sso").
		SetRole("user").
		Save(ctx)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	carol, err := env.client.User.Create().
		SetUsername("carol").
		SetEmail("carol@example.net").
		SetAuthSource("ldap").
		SetRole("admin").
		SetRelayUserID(99).
		Save(ctx)
	if err != nil {
		t.Fatalf("create carol: %v", err)
	}

	return adminUsersFixture{aliceID: alice.ID, bobID: bob.ID, carolID: carol.ID, ciphertext: ciphertext}
}

func seedAdminUsersDirectorySnapshot(t *testing.T, env *fullTestEnv, fixture adminUsersFixture) {
	t.Helper()
	ctx := context.Background()

	source, err := env.client.DirectorySource.Create().
		SetName("Example Directory").
		SetDescription("Synthetic organization directory").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		Save(ctx)
	if err != nil {
		t.Fatalf("create directory source: %v", err)
	}
	run, err := env.client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetDepartmentCount(2).
		SetMemberCount(2).
		SetCompletedAt(time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)).
		Save(ctx)
	if err != nil {
		t.Fatalf("create directory run: %v", err)
	}
	if _, err := env.client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("update directory source run pointers: %v", err)
	}
	if _, err := env.client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("dept-alpha").
		SetName("Department Alpha").
		SetPath("Department Alpha").
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create alpha department: %v", err)
	}
	if _, err := env.client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("dept-beta").
		SetName("Department Beta").
		SetPath("Department Beta").
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create beta department: %v", err)
	}
	if _, err := env.client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-alpha").
		SetEmailNormalized("alice@example.com").
		SetDisplayName("Alice Example").
		SetDepartmentExternalID("dept-alpha").
		SetMatchedUserID(fixture.aliceID).
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create alpha member: %v", err)
	}
	if _, err := env.client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-beta").
		SetEmailNormalized("carol@example.net").
		SetDisplayName("Carol Example").
		SetDepartmentExternalID("dept-beta").
		SetMatchedUserID(fixture.carolID).
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create beta member: %v", err)
	}
}

func seedAdminUsersMultiDepartmentMembershipSnapshot(t *testing.T, env *fullTestEnv, fixture adminUsersFixture) {
	t.Helper()
	ctx := context.Background()

	source, err := env.client.DirectorySource.Create().
		SetName("Example Directory").
		SetDescription("Synthetic organization directory").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		Save(ctx)
	if err != nil {
		t.Fatalf("create directory source: %v", err)
	}
	run, err := env.client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetDepartmentCount(2).
		SetMemberCount(1).
		SetCompletedAt(time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)).
		Save(ctx)
	if err != nil {
		t.Fatalf("create directory run: %v", err)
	}
	if _, err := env.client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("update directory source run pointers: %v", err)
	}
	if _, err := env.client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("dept-alpha").
		SetName("Department Alpha").
		SetPath("Department Alpha").
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create alpha department: %v", err)
	}
	if _, err := env.client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("dept-beta").
		SetName("Department Beta").
		SetPath("Department Beta").
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create beta department: %v", err)
	}
	member, err := env.client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-alice").
		SetEmailNormalized("alice@example.com").
		SetDisplayName("Alice Example").
		SetDepartmentExternalID("dept-alpha").
		SetMatchedUserID(fixture.aliceID).
		SetLastSeenRunID(run.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create alice member: %v", err)
	}
	for _, departmentID := range []string{"dept-alpha", "dept-beta"} {
		if _, err := env.client.DirectoryMemberDepartment.Create().
			SetSourceID(source.ID).
			SetDirectoryMemberID(member.ID).
			SetMemberExternalID(member.ExternalID).
			SetMemberEmailNormalized(member.EmailNormalized).
			SetDepartmentExternalID(departmentID).
			SetLastSeenRunID(run.ID).
			Save(ctx); err != nil {
			t.Fatalf("create alice %s membership: %v", departmentID, err)
		}
	}
}

func seedAdminUsersHierarchicalDirectorySnapshot(t *testing.T, env *fullTestEnv, fixture adminUsersFixture) {
	t.Helper()
	ctx := context.Background()

	source, err := env.client.DirectorySource.Create().
		SetName("Example Directory").
		SetDescription("Synthetic organization directory").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		Save(ctx)
	if err != nil {
		t.Fatalf("create directory source: %v", err)
	}
	run, err := env.client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetDepartmentCount(3).
		SetMemberCount(3).
		SetCompletedAt(time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)).
		Save(ctx)
	if err != nil {
		t.Fatalf("create directory run: %v", err)
	}
	if _, err := env.client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("update directory source run pointers: %v", err)
	}
	if _, err := env.client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("dept-alpha").
		SetName("Department Alpha").
		SetPath("1.781448").
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create alpha department: %v", err)
	}
	if _, err := env.client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("dept-alpha-team-one").
		SetParentExternalID("dept-alpha").
		SetEffectiveParentExternalID("dept-alpha").
		SetName("Team One").
		SetPath("1.781448.1683962").
		SetMetadata(map[string]any{"representative_external_ids": []any{"member-alpha-child", "member-missing"}}).
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create alpha child department: %v", err)
	}
	if _, err := env.client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("dept-beta").
		SetName("Department Beta").
		SetPath("1.1178135").
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create beta department: %v", err)
	}
	if _, err := env.client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-alpha").
		SetEmailNormalized("alice@example.com").
		SetDisplayName("Alice Example").
		SetDepartmentExternalID("dept-alpha").
		SetMatchedUserID(fixture.aliceID).
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create alpha member: %v", err)
	}
	if _, err := env.client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-alpha-child").
		SetEmailNormalized("bob@example.org").
		SetDisplayName("Bob Example").
		SetDepartmentExternalID("dept-alpha-team-one").
		SetMetadata(map[string]any{"leader_department_ids": []any{"dept-alpha-team-one"}}).
		SetMatchedUserID(fixture.bobID).
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create alpha child member: %v", err)
	}
	if _, err := env.client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-beta").
		SetEmailNormalized("carol@example.net").
		SetDisplayName("Carol Example").
		SetDepartmentExternalID("dept-beta").
		SetMatchedUserID(fixture.carolID).
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create beta member: %v", err)
	}
}

func seedAdminUsersSingleMemberDirectorySnapshot(t *testing.T, env *fullTestEnv, sourceName, departmentID, departmentName string, userID int, email string, completedAt time.Time) int {
	t.Helper()
	ctx := context.Background()
	source, err := env.client.DirectorySource.Create().
		SetName(sourceName).
		SetDescription("Synthetic organization directory").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		Save(ctx)
	if err != nil {
		t.Fatalf("create directory source: %v", err)
	}
	run, err := env.client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetDepartmentCount(1).
		SetMemberCount(1).
		SetCompletedAt(completedAt).
		Save(ctx)
	if err != nil {
		t.Fatalf("create directory run: %v", err)
	}
	if _, err := env.client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("update directory source run pointers: %v", err)
	}
	if _, err := env.client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID(departmentID).
		SetName(departmentName).
		SetPath(departmentName).
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create department: %v", err)
	}
	if _, err := env.client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-" + departmentID).
		SetEmailNormalized(email).
		SetDisplayName(email).
		SetDepartmentExternalID(departmentID).
		SetMatchedUserID(userID).
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("create member: %v", err)
	}
	return source.ID
}

func seedAdminUsersEffectiveCycleSnapshot(t *testing.T, env *fullTestEnv) (map[string]int, string) {
	t.Helper()
	ctx := context.Background()
	ciphertext := "encrypted-cycle-password"
	userIDs := make(map[string]int, 3)
	for _, key := range []string{"a", "b", "c"} {
		builder := env.client.User.Create().
			SetUsername("cycle-" + key).
			SetEmail("cycle-" + key + "@example.com").
			SetAuthSource("ldap").
			SetRole("user")
		if key == "b" {
			builder.SetRelayUserID(4202).SetRelayAuthPassword(ciphertext)
		}
		user, err := builder.Save(ctx)
		if err != nil {
			t.Fatalf("create cycle %s user: %v", key, err)
		}
		userIDs[key] = user.ID
	}

	source, err := env.client.DirectorySource.Create().
		SetName("Cycle Directory").
		SetDescription("Synthetic cycle directory").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		Save(ctx)
	if err != nil {
		t.Fatalf("create cycle directory source: %v", err)
	}
	run, err := env.client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetDepartmentCount(3).
		SetMemberCount(3).
		SetCompletedAt(time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)).
		Save(ctx)
	if err != nil {
		t.Fatalf("create cycle directory run: %v", err)
	}
	if _, err := env.client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		Save(ctx); err != nil {
		t.Fatalf("update cycle directory run pointers: %v", err)
	}

	departments := []struct {
		id              string
		parent          string
		effectiveParent string
		name            string
	}{
		{id: "dept-cycle-a", parent: "dept-cycle-c", name: "Cycle Alpha"},
		{id: "dept-cycle-b", parent: "dept-cycle-a", effectiveParent: "dept-cycle-a", name: "Cycle Beta"},
		{id: "dept-cycle-c", parent: "dept-cycle-b", effectiveParent: "dept-cycle-b", name: "Cycle Gamma"},
	}
	for _, department := range departments {
		builder := env.client.DirectoryDepartment.Create().
			SetSourceID(source.ID).
			SetExternalID(department.id).
			SetParentExternalID(department.parent).
			SetName(department.name).
			SetPath("synthetic/" + department.id).
			SetLastSeenRunID(run.ID)
		if department.effectiveParent != "" {
			builder.SetEffectiveParentExternalID(department.effectiveParent)
		}
		if _, err := builder.Save(ctx); err != nil {
			t.Fatalf("create cycle department %s: %v", department.id, err)
		}
	}
	for _, key := range []string{"a", "b", "c"} {
		departmentID := "dept-cycle-" + key
		member, err := env.client.DirectoryMember.Create().
			SetSourceID(source.ID).
			SetExternalID("member-cycle-" + key).
			SetEmailNormalized("directory-cycle-" + key + "@example.com").
			SetDisplayName("Cycle " + strings.ToUpper(key)).
			SetDepartmentExternalID(departmentID).
			SetMatchedUserID(userIDs[key]).
			SetLastSeenRunID(run.ID).
			Save(ctx)
		if err != nil {
			t.Fatalf("create cycle member %s: %v", key, err)
		}
		if _, err := env.client.DirectoryMemberDepartment.Create().
			SetSourceID(source.ID).
			SetDirectoryMemberID(member.ID).
			SetMemberExternalID(member.ExternalID).
			SetMemberEmailNormalized(member.EmailNormalized).
			SetDepartmentExternalID(departmentID).
			SetLastSeenRunID(run.ID).
			Save(ctx); err != nil {
			t.Fatalf("create cycle membership %s: %v", key, err)
		}
	}
	return userIDs, ciphertext
}

func TestAdminUsersListSearchPaginationAndCiphertext(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?q=alice&page=1&page_size=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "test-password") {
		t.Fatalf("list response leaked plaintext: %s", w.Body.String())
	}

	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 1 {
		t.Fatalf("total = %d, want 1", got)
	}
	if got := int(data["page"].(float64)); got != 1 {
		t.Fatalf("page = %d, want 1", got)
	}
	if got := int(data["page_size"].(float64)); got != 2 {
		t.Fatalf("page_size = %d, want 2", got)
	}

	items := data["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	row := items[0].(map[string]interface{})
	if int(row["id"].(float64)) != fixture.aliceID {
		t.Fatalf("id = %v, want %d", row["id"], fixture.aliceID)
	}
	if row["username"] != "alice" || row["email"] != "alice@example.com" {
		t.Fatalf("unexpected identity row: %+v", row)
	}
	if row["relay_auth_password"] != fixture.ciphertext {
		t.Fatalf("relay_auth_password = %v, want ciphertext", row["relay_auth_password"])
	}
	if int(row["relay_user_id"].(float64)) != 42 {
		t.Fatalf("relay_user_id = %v, want 42", row["relay_user_id"])
	}
}

func TestAdminUsersListHundredRowWireBound(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	ctx := context.Background()
	fixedTime := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 100; i++ {
		username := fmt.Sprintf("wire-user-%03d", i)
		if _, err := env.client.User.Create().
			SetUsername(username).
			SetEmail(username + "@example.com").
			SetAuthSource("ldap").
			SetRole("user").
			SetRelayUserID(1000 + i).
			SetRelayAuthPassword("synthetic-ciphertext").
			SetCreatedAt(fixedTime).
			SetUpdatedAt(fixedTime).
			Save(ctx); err != nil {
			t.Fatalf("create synthetic wire user %d: %v", i, err)
		}
	}

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?q=wire-user&page=1&page_size=100", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 100 {
		t.Fatalf("total = %d, want 100", got)
	}
	if got := int(data["page"].(float64)); got != 1 {
		t.Fatalf("page = %d, want 1", got)
	}
	if got := int(data["page_size"].(float64)); got != 100 {
		t.Fatalf("page_size = %d, want 100", got)
	}
	if got := len(data["items"].([]interface{})); got != 100 {
		t.Fatalf("items = %d, want 100", got)
	}

	const syntheticFixtureWireRegressionBudgetBytes = 256 * 1024
	wireBytes := w.Body.Len()
	t.Logf("admin users synthetic fixture 100-row response bytes=%d regression_budget_bytes=%d", wireBytes, syntheticFixtureWireRegressionBudgetBytes)
	if wireBytes >= syntheticFixtureWireRegressionBudgetBytes {
		t.Fatalf("100-row response bytes = %d, want below test-owned synthetic-fixture regression budget %d", wireBytes, syntheticFixtureWireRegressionBudgetBytes)
	}
}

func TestAdminUsersListPageBounds(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	seedAdminUsersFixture(t, env)
	wantTotal := env.client.User.Query().CountX(context.Background())

	for _, path := range []string{
		"/api/v1/admin/users?page=0&page_size=0",
		"/api/v1/admin/users?page=-7&page_size=-3",
	} {
		w := doFullRequest(env, http.MethodGet, path, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200, body=%s", path, w.Code, w.Body.String())
		}
		data := parseFullResponse(t, w)["data"].(map[string]interface{})
		if data["page"] != float64(1) || data["page_size"] != float64(20) {
			t.Fatalf("%s page metadata = (%v, %v), want (1, 20)", path, data["page"], data["page_size"])
		}
	}

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?page=1&page_size=101", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("max size status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if data["page_size"] != float64(100) {
		t.Fatalf("page_size = %v, want capped 100", data["page_size"])
	}

	maxInt := int(^uint(0) >> 1)
	w = doFullRequest(env, http.MethodGet, fmt.Sprintf("/api/v1/admin/users?page=%d&page_size=100", maxInt), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("maximum page status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != wantTotal {
		t.Fatalf("maximum page total = %d, want %d", got, wantTotal)
	}
	if got := data["items"].([]interface{}); len(got) != 0 {
		t.Fatalf("maximum page items = %d, want empty", len(got))
	}
}

func TestAdminUsersListEffectiveCycleFilterParity(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	userIDs, ciphertext := seedAdminUsersEffectiveCycleSnapshot(t, env)
	wantPaths := map[int]string{
		userIDs["a"]: "Cycle Alpha",
		userIDs["b"]: "Cycle Alpha / Cycle Beta",
		userIDs["c"]: "Cycle Alpha / Cycle Beta / Cycle Gamma",
	}
	tests := []struct {
		departmentID string
		wantIDs      []int
	}{
		{departmentID: "dept-cycle-a", wantIDs: []int{userIDs["a"], userIDs["b"], userIDs["c"]}},
		{departmentID: "dept-cycle-b", wantIDs: []int{userIDs["b"], userIDs["c"]}},
		{departmentID: "dept-cycle-c", wantIDs: []int{userIDs["c"]}},
	}

	for _, tt := range tests {
		t.Run(tt.departmentID, func(t *testing.T) {
			gotIDs := make([]int, 0, len(tt.wantIDs))
			for page := 1; page <= len(tt.wantIDs)+1; page++ {
				path := fmt.Sprintf("/api/v1/admin/users?department_id=%s&page=%d&page_size=1", tt.departmentID, page)
				w := doFullRequest(env, http.MethodGet, path, nil)
				if w.Code != http.StatusOK {
					t.Fatalf("page %d status = %d, want 200, body=%s", page, w.Code, w.Body.String())
				}
				data := parseFullResponse(t, w)["data"].(map[string]interface{})
				if got := int(data["total"].(float64)); got != len(tt.wantIDs) {
					t.Fatalf("page %d total = %d, want %d", page, got, len(tt.wantIDs))
				}
				if data["page"] != float64(page) || data["page_size"] != float64(1) {
					t.Fatalf("page metadata = (%v, %v), want (%d, 1)", data["page"], data["page_size"], page)
				}
				for _, item := range data["items"].([]interface{}) {
					row := item.(map[string]interface{})
					userID := int(row["id"].(float64))
					gotIDs = append(gotIDs, userID)
					department := row["department"].(map[string]interface{})
					if department["display_path"] != wantPaths[userID] {
						t.Fatalf("user %d display path = %v, want %q", userID, department["display_path"], wantPaths[userID])
					}
					if userID == userIDs["b"] {
						if row["relay_auth_password"] != ciphertext || row["access_status"] != "configured" {
							t.Fatalf("cycle B credential/status fields changed: %+v", row)
						}
					}
				}
			}
			if fmt.Sprint(gotIDs) != fmt.Sprint(tt.wantIDs) {
				t.Fatalf("concatenated ids = %v, want %v", gotIDs, tt.wantIDs)
			}
			if tt.departmentID == "dept-cycle-b" {
				for _, id := range gotIDs {
					if id == userIDs["a"] {
						t.Fatalf("cycle B included anchor-only user %d", id)
					}
				}
			}
		})
	}
}

func TestAdminUsersListNumericSearchMatchesIDs(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?q=42&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("relay id search status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 1 {
		t.Fatalf("relay id search total = %d, want 1", got)
	}

	w = doFullRequest(env, http.MethodGet, fmt.Sprintf("/api/v1/admin/users?q=%d&page=1&page_size=20", fixture.aliceID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("local id search status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 1 {
		t.Fatalf("local id search total = %d, want 1", got)
	}
}

func TestAdminUsersListAccessStatusAndFilter(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	ctx := context.Background()

	if _, err := env.client.User.UpdateOneID(fixture.aliceID).
		SetTokenValidAfter(time.Date(2026, 6, 26, 9, 0, 0, 0, time.UTC)).
		Save(ctx); err != nil {
		t.Fatalf("revoke alice tokens: %v", err)
	}
	if _, err := env.client.User.Create().
		SetUsername("dana").
		SetEmail("dana@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(77).
		SetRelayAuthPassword(fixture.ciphertext).
		Save(ctx); err != nil {
		t.Fatalf("create dana: %v", err)
	}
	if _, err := env.client.User.Create().
		SetUsername("erin").
		SetEmail("erin@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(88).
		SetRelayAuthPassword("   ").
		Save(ctx); err != nil {
		t.Fatalf("create erin: %v", err)
	}

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?access_status=disabled&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("disabled status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if got := int(data["total"].(float64)); got != 1 {
		t.Fatalf("disabled total = %d, want 1", got)
	}
	row := items[0].(map[string]interface{})
	if row["username"] != "alice" || row["access_status"] != "disabled" {
		t.Fatalf("disabled row = %+v, want alice disabled", row)
	}
	if row["token_valid_after"] == nil {
		t.Fatalf("disabled row missing token_valid_after: %+v", row)
	}

	w = doFullRequest(env, http.MethodGet, "/api/v1/admin/users?access_status=configured&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("configured status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	items = data["items"].([]interface{})
	if got := int(data["total"].(float64)); got != 1 {
		t.Fatalf("configured total = %d, want 1", got)
	}
	row = items[0].(map[string]interface{})
	if row["username"] != "dana" || row["access_status"] != "configured" {
		t.Fatalf("configured row = %+v, want dana configured", row)
	}

	w = doFullRequest(env, http.MethodGet, "/api/v1/admin/users?access_status=missing_credential&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("missing credential status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 4 {
		t.Fatalf("missing credential total = %d, want 4", got)
	}

	w = doFullRequest(env, http.MethodGet, "/api/v1/admin/users?access_status=unknown&page=1&page_size=20", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestAdminUsersListMarksSucceededOffboardingActionDisabled(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	ctx := context.Background()
	sourceID := seedAdminUsersSingleMemberDirectorySnapshot(
		t,
		env,
		"Example Directory",
		"dept-alpha",
		"Department Alpha",
		fixture.aliceID,
		"alice@example.com",
		time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC),
	)
	source, err := env.client.DirectorySource.Get(ctx, sourceID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if source.LastSuccessfulRunID == nil {
		t.Fatal("source missing successful run")
	}
	if _, err := env.client.DirectoryOffboardingAction.Create().
		SetSourceID(sourceID).
		SetUserID(fixture.aliceID).
		SetRelayUserID(42).
		SetDirectoryRunID(*source.LastSuccessfulRunID).
		SetAction(directoryoffboardingaction.ActionDisableRelayUser).
		SetStatus(directoryoffboardingaction.StatusSucceeded).
		SetReason("missing_from_latest_full_company_directory").
		SetPerformedByUserID(fixture.carolID).
		Save(ctx); err != nil {
		t.Fatalf("create offboarding action: %v", err)
	}
	newSourceID := seedAdminUsersSingleMemberDirectorySnapshot(
		t,
		env,
		"New Example Directory",
		"dept-beta",
		"Department Beta",
		fixture.aliceID,
		"alice@example.com",
		time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC),
	)
	newSource, err := env.client.DirectorySource.Get(ctx, newSourceID)
	if err != nil {
		t.Fatalf("get new source: %v", err)
	}
	if newSource.LastSuccessfulRunID == nil {
		t.Fatal("new source missing successful run")
	}
	if _, err := env.client.DirectoryOffboardingAction.Create().
		SetSourceID(newSourceID).
		SetUserID(fixture.aliceID).
		SetRelayUserID(42).
		SetDirectoryRunID(*newSource.LastSuccessfulRunID).
		SetAction(directoryoffboardingaction.ActionDisableRelayUser).
		SetStatus(directoryoffboardingaction.StatusFailed).
		SetReason("missing_from_latest_full_company_directory").
		SetPerformedByUserID(fixture.carolID).
		Save(ctx); err != nil {
		t.Fatalf("create later failed offboarding action: %v", err)
	}

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?q=alice&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	row := items[0].(map[string]interface{})
	if row["access_status"] != "disabled" || row["offboarding_status"] != "failed" {
		t.Fatalf("row = %+v, want disabled access with latest failed offboarding audit status", row)
	}
}

func TestAdminUsersListIncludesDirectoryDepartment(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	seedAdminUsersDirectorySnapshot(t, env, fixture)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?q=alice&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	row := items[0].(map[string]interface{})
	department, ok := row["department"].(map[string]interface{})
	if !ok {
		t.Fatalf("department missing from row: %+v", row)
	}
	if department["external_id"] != "dept-alpha" || department["name"] != "Department Alpha" || department["path"] != "Department Alpha" {
		t.Fatalf("department = %+v, want alpha department", department)
	}
}

func TestAdminUsersListUsesLatestSuccessfulApplyRunAfterOlderSourceEdit(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	oldSourceID := seedAdminUsersSingleMemberDirectorySnapshot(t, env, "Old Directory", "dept-old", "Department Old", fixture.aliceID, "alice@example.com", time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC))
	seedAdminUsersSingleMemberDirectorySnapshot(t, env, "New Directory", "dept-new", "Department New", fixture.aliceID, "alice@example.com", time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC))
	if _, err := env.client.DirectorySource.UpdateOneID(oldSourceID).SetDescription("Edited after latest sync").Save(context.Background()); err != nil {
		t.Fatalf("update old source: %v", err)
	}

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?q=alice&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	department := items[0].(map[string]interface{})["department"].(map[string]interface{})
	if department["external_id"] != "dept-new" || department["name"] != "Department New" {
		t.Fatalf("department = %+v, want latest successful apply source", department)
	}
}

func TestAdminUsersListFiltersByDirectoryDepartment(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	seedAdminUsersDirectorySnapshot(t, env, fixture)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?department_id=dept-alpha&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 1 {
		t.Fatalf("total = %d, want 1", got)
	}
	items := data["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	row := items[0].(map[string]interface{})
	if row["username"] != "alice" {
		t.Fatalf("username = %v, want alice", row["username"])
	}

	w = doFullRequest(env, http.MethodGet, "/api/v1/admin/users?department_id=dept-alpha&q=carol&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 0 {
		t.Fatalf("total with mismatched q = %d, want 0", got)
	}
}

func TestAdminUsersListFiltersByDirectoryMemberDepartments(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	seedAdminUsersMultiDepartmentMembershipSnapshot(t, env, fixture)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?department_id=dept-beta&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 1 {
		t.Fatalf("total = %d, want 1", got)
	}
	items := data["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	row := items[0].(map[string]interface{})
	if row["username"] != "alice" {
		t.Fatalf("username = %v, want alice", row["username"])
	}
}

func TestAdminUsersListFiltersByDepartmentSubtree(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	seedAdminUsersHierarchicalDirectorySnapshot(t, env, fixture)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?department_id=dept-alpha&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 2 {
		t.Fatalf("total = %d, want 2", got)
	}
	items := data["items"].([]interface{})
	gotNames := make([]string, 0, len(items))
	for _, item := range items {
		row := item.(map[string]interface{})
		gotNames = append(gotNames, row["username"].(string))
	}
	if strings.Join(gotNames, ",") != "alice,bob" {
		t.Fatalf("usernames = %v, want alice,bob", gotNames)
	}

	w = doFullRequest(env, http.MethodGet, "/api/v1/admin/users?department_id=dept-alpha&q=carol&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 0 {
		t.Fatalf("total with unrelated subtree search = %d, want 0", got)
	}
}

func TestAdminUsersListUsesDepartmentDisplayPath(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	seedAdminUsersHierarchicalDirectorySnapshot(t, env, fixture)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?q=bob&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	row := items[0].(map[string]interface{})
	department, ok := row["department"].(map[string]interface{})
	if !ok {
		t.Fatalf("department missing from row: %+v", row)
	}
	if department["path"] != "1.781448.1683962" {
		t.Fatalf("raw path = %v, want source path retained", department["path"])
	}
	if department["display_path"] != "Department Alpha / Team One" {
		t.Fatalf("display_path = %v, want name-based path", department["display_path"])
	}
}

func TestAdminUsersDepartmentSummaries(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	seedAdminUsersDirectorySnapshot(t, env, fixture)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users/departments", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	alpha := items[0].(map[string]interface{})
	if alpha["external_id"] != "dept-alpha" || alpha["name"] != "Department Alpha" {
		t.Fatalf("first department = %+v, want alpha", alpha)
	}
	if int(alpha["member_count"].(float64)) != 1 || int(alpha["matched_user_count"].(float64)) != 1 {
		t.Fatalf("alpha counts = %+v, want member_count=1 matched_user_count=1", alpha)
	}
}

func TestHierarchyCleanupCompleteDepartmentsUsesPersistedEffectiveParents(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	sourceID, runID := seedAdminUsersBareDirectorySource(t, env, "Persisted Hierarchy Directory", time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()
	for _, department := range []struct {
		externalID      string
		parentID        string
		effectiveParent string
		name            string
	}{
		{externalID: "dept-persisted-root", parentID: "dept-persisted-leaf", name: "Persisted Root"},
		{externalID: "dept-persisted-child", effectiveParent: "dept-persisted-root", name: "Persisted Child"},
		{externalID: "dept-persisted-leaf", effectiveParent: "dept-persisted-child", name: "Persisted Leaf"},
	} {
		builder := env.client.DirectoryDepartment.Create().
			SetSourceID(sourceID).
			SetExternalID(department.externalID).
			SetName(department.name).
			SetPath("synthetic/" + department.externalID).
			SetLastSeenRunID(runID)
		if department.parentID != "" {
			builder.SetParentExternalID(department.parentID)
		}
		if department.effectiveParent != "" {
			builder.SetEffectiveParentExternalID(department.effectiveParent)
		}
		if _, err := builder.Save(ctx); err != nil {
			t.Fatalf("create %s: %v", department.externalID, err)
		}
	}
	for index, member := range []struct {
		externalID   string
		departmentID string
		matchedUser  int
	}{
		{externalID: "member-persisted-root", departmentID: "dept-persisted-root", matchedUser: fixture.aliceID},
		{externalID: "member-persisted-child", departmentID: "dept-persisted-child", matchedUser: fixture.bobID},
		{externalID: "member-persisted-leaf", departmentID: "dept-persisted-leaf", matchedUser: fixture.carolID},
	} {
		if _, err := env.client.DirectoryMember.Create().
			SetSourceID(sourceID).
			SetExternalID(member.externalID).
			SetEmailNormalized(fmt.Sprintf("persisted-member-%d@example.org", index)).
			SetDisplayName(member.externalID).
			SetDepartmentExternalID(member.departmentID).
			SetMatchedUserID(member.matchedUser).
			SetLastSeenRunID(runID).
			Save(ctx); err != nil {
			t.Fatalf("create %s: %v", member.externalID, err)
		}
	}

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users/departments", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	items := parseFullResponse(t, w)["data"].(map[string]interface{})["items"].([]interface{})
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	wantIDs := []string{"dept-persisted-root", "dept-persisted-child", "dept-persisted-leaf"}
	wantPaths := []string{"Persisted Root", "Persisted Root / Persisted Child", "Persisted Root / Persisted Child / Persisted Leaf"}
	wantSubtreeCounts := []int{3, 2, 1}
	for index, item := range items {
		row := item.(map[string]interface{})
		if row["external_id"] != wantIDs[index] || row["display_path"] != wantPaths[index] || int(row["depth"].(float64)) != index {
			t.Fatalf("row %d = %+v, want id/path/depth %s/%s/%d", index, row, wantIDs[index], wantPaths[index], index)
		}
		if int(row["member_count"].(float64)) != 1 || int(row["matched_user_count"].(float64)) != 1 || int(row["subtree_member_count"].(float64)) != wantSubtreeCounts[index] || int(row["subtree_matched_user_count"].(float64)) != wantSubtreeCounts[index] {
			t.Fatalf("row %d counts = %+v, want direct 1/1 and subtree %d/%d", index, row, wantSubtreeCounts[index], wantSubtreeCounts[index])
		}
	}
}

func TestAdminUsersDepartmentSummariesUseDirectoryMemberDepartments(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	seedAdminUsersMultiDepartmentMembershipSnapshot(t, env, fixture)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users/departments", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	beta := items[1].(map[string]interface{})
	if beta["external_id"] != "dept-beta" {
		t.Fatalf("second department = %+v, want beta", beta)
	}
	if int(beta["member_count"].(float64)) != 1 || int(beta["matched_user_count"].(float64)) != 1 {
		t.Fatalf("beta counts = %+v, want member_count=1 matched_user_count=1", beta)
	}
}

func TestAdminUsersDepartmentSummariesExposeHierarchyAndSubtreeCounts(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	seedAdminUsersHierarchicalDirectorySnapshot(t, env, fixture)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users/departments", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}

	alpha := items[0].(map[string]interface{})
	if alpha["external_id"] != "dept-alpha" {
		t.Fatalf("first department = %+v, want alpha root", alpha)
	}
	if int(alpha["depth"].(float64)) != 0 || int(alpha["child_count"].(float64)) != 1 {
		t.Fatalf("alpha hierarchy = %+v, want depth=0 child_count=1", alpha)
	}
	if int(alpha["member_count"].(float64)) != 1 || int(alpha["subtree_member_count"].(float64)) != 2 {
		t.Fatalf("alpha counts = %+v, want direct=1 subtree=2", alpha)
	}
	if alpha["path"] != "1.781448" || alpha["display_path"] != "Department Alpha" {
		t.Fatalf("alpha paths = %+v, want raw path plus name-based display path", alpha)
	}
	if int(alpha["matched_user_count"].(float64)) != 1 || int(alpha["subtree_matched_user_count"].(float64)) != 2 {
		t.Fatalf("alpha matched counts = %+v, want direct=1 subtree=2", alpha)
	}

	child := items[1].(map[string]interface{})
	if child["external_id"] != "dept-alpha-team-one" || child["parent_external_id"] != "dept-alpha" {
		t.Fatalf("second department = %+v, want alpha child", child)
	}
	if int(child["depth"].(float64)) != 1 || int(child["child_count"].(float64)) != 0 {
		t.Fatalf("child hierarchy = %+v, want depth=1 child_count=0", child)
	}
	if child["path"] != "1.781448.1683962" || child["display_path"] != "Department Alpha / Team One" {
		t.Fatalf("child paths = %+v, want raw path plus name-based display path", child)
	}
	if int(child["representative_count"].(float64)) != 2 || int(child["matched_representative_count"].(float64)) != 1 {
		t.Fatalf("child representatives = %+v, want representative_count=2 matched_representative_count=1", child)
	}

	beta := items[2].(map[string]interface{})
	if beta["external_id"] != "dept-beta" || int(beta["depth"].(float64)) != 0 {
		t.Fatalf("third department = %+v, want beta root", beta)
	}
}

func TestAdminUsersDepartmentOptionsPagingSelectionAndBounds(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	seedAdminUsersHierarchicalDirectorySnapshot(t, env, fixture)
	seedAdminUsersDepartmentOptionFillers(t, env, 25)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users/department-options?page=0&page_size=0", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("default status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if int(data["page"].(float64)) != 1 || int(data["page_size"].(float64)) != 20 || int(data["total"].(float64)) != 28 || len(data["items"].([]interface{})) != 20 {
		t.Fatalf("default option data = %+v, want page 1/20 total 28 with 20 items", data)
	}

	w = doFullRequest(env, http.MethodGet, "/api/v1/admin/users/department-options?q=team&selected_id=dept-alpha&page=1&page_size=101", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if int(data["page"].(float64)) != 1 || int(data["page_size"].(float64)) != 100 || int(data["total"].(float64)) != 26 {
		t.Fatalf("option metadata = %+v, want page 1/100 total 26", data)
	}
	items := data["items"].([]interface{})
	if len(items) != 26 {
		t.Fatalf("option items = %d, want 26", len(items))
	}
	if items[0].(map[string]interface{})["external_id"] != "dept-http-filler-00" || items[1].(map[string]interface{})["external_id"] != "dept-http-filler-01" {
		t.Fatalf("normalized option order starts %+v, %+v", items[0], items[1])
	}
	selected := data["selected"].(map[string]interface{})
	if selected["external_id"] != "dept-alpha" || selected["display_path"] != "Department Alpha" {
		t.Fatalf("selected = %+v, want independent alpha lookup", selected)
	}

	w = doFullRequest(env, http.MethodGet, "/api/v1/admin/users/department-options?page=2&page_size=20&selected_id=dept-unknown", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("second page status = %d, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := len(data["items"].([]interface{})); got != 8 {
		t.Fatalf("second option page items = %d, want 8", got)
	}
	if data["selected"] != nil {
		t.Fatalf("unknown selected = %+v, want null", data["selected"])
	}

	maxInt := int(^uint(0) >> 1)
	w = doFullRequest(env, http.MethodGet, fmt.Sprintf("/api/v1/admin/users/department-options?page=%d&page_size=100", maxInt), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("overflow status = %d, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := len(data["items"].([]interface{})); got != 0 {
		t.Fatalf("overflow option items = %d, want 0", got)
	}
}

func TestAdminUsersDepartmentOptionsAndChildrenWithoutCurrentSource(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users/department-options?selected_id=dept-alpha", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("options status = %d, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if len(data["items"].([]interface{})) != 0 || int(data["total"].(float64)) != 0 || data["selected"] != nil || int(data["page"].(float64)) != 1 || int(data["page_size"].(float64)) != 20 {
		t.Fatalf("source-less options = %+v", data)
	}

	w = doFullRequest(env, http.MethodGet, "/api/v1/admin/users/department-children?parent_department_id=dept-alpha", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("children status = %d, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if len(data["items"].([]interface{})) != 0 || int(data["total"].(float64)) != 0 || data["parent_department_id"] != "dept-alpha" || int(data["page"].(float64)) != 1 || int(data["page_size"].(float64)) != 25 {
		t.Fatalf("source-less children = %+v", data)
	}
}

func TestAdminUsersDepartmentChildrenPagingBounds(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	seedAdminUsersHierarchicalDirectorySnapshot(t, env, fixture)
	seedAdminUsersDepartmentOptionFillers(t, env, 25)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users/department-children?page=0&page_size=0", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("default status = %d, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if int(data["page"].(float64)) != 1 || int(data["page_size"].(float64)) != 25 || int(data["total"].(float64)) != 27 || len(data["items"].([]interface{})) != 25 || data["parent_department_id"] != "" {
		t.Fatalf("default children data = %+v, want roots page 1/25 total 27", data)
	}

	w = doFullRequest(env, http.MethodGet, "/api/v1/admin/users/department-children?page=2&page_size=25", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("second page status = %d, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if len(data["items"].([]interface{})) != 2 || int(data["total"].(float64)) != 27 {
		t.Fatalf("second children page = %+v, want 2 of 27", data)
	}

	w = doFullRequest(env, http.MethodGet, "/api/v1/admin/users/department-children?page=1&page_size=101", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("capped page status = %d, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if int(data["page_size"].(float64)) != 100 || len(data["items"].([]interface{})) != 27 {
		t.Fatalf("capped children page = %+v, want size 100 and 27 roots", data)
	}

	maxInt := int(^uint(0) >> 1)
	w = doFullRequest(env, http.MethodGet, fmt.Sprintf("/api/v1/admin/users/department-children?page=%d&page_size=100", maxInt), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("overflow status = %d, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if len(data["items"].([]interface{})) != 0 || int(data["total"].(float64)) != 27 {
		t.Fatalf("overflow children page = %+v, want empty with total 27", data)
	}
}

func TestAdminUsersDepartmentChildrenRequiresCurrentSourceParent(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	seedAdminUsersHierarchicalDirectorySnapshot(t, env, fixture)
	seedAdminUsersOrphanAndStaleParent(t, env)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users/department-children?parent_department_id=dept-alpha&page=1&page_size=25", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != 1 || items[0].(map[string]interface{})["external_id"] != "dept-alpha-team-one" {
		t.Fatalf("alpha children = %+v, want immediate team one only", items)
	}

	for _, parentID := range []string{"dept-missing", "dept-unknown"} {
		w = doFullRequest(env, http.MethodGet, "/api/v1/admin/users/department-children?parent_department_id="+parentID, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", parentID, w.Code, w.Body.String())
		}
		data = parseFullResponse(t, w)["data"].(map[string]interface{})
		if int(data["total"].(float64)) != 0 || len(data["items"].([]interface{})) != 0 {
			t.Fatalf("missing parent %s data = %+v, want empty 200", parentID, data)
		}
	}

	w = doFullRequest(env, http.MethodGet, "/api/v1/admin/users/department-children?page=1&page_size=100", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("root status = %d, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	orphanCount := 0
	for _, item := range data["items"].([]interface{}) {
		row := item.(map[string]interface{})
		if row["external_id"] == "dept-orphan" {
			orphanCount++
			if row["parent_external_id"] != "dept-missing" || int(row["depth"].(float64)) != 0 || row["display_path"] != "Current Orphan" {
				t.Fatalf("orphan row = %+v", row)
			}
		}
	}
	if orphanCount != 1 {
		t.Fatalf("orphan root count = %d, want 1", orphanCount)
	}
}

func TestAdminUsersDepartmentChildrenClosedCycleNavigation(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	seedAdminUsersEffectiveCycleSnapshot(t, env)

	assertIDs := func(path string, want []string) map[string]interface{} {
		t.Helper()
		w := doFullRequest(env, http.MethodGet, path, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, body=%s", path, w.Code, w.Body.String())
		}
		data := parseFullResponse(t, w)["data"].(map[string]interface{})
		items := data["items"].([]interface{})
		got := make([]string, 0, len(items))
		for _, item := range items {
			got = append(got, item.(map[string]interface{})["external_id"].(string))
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("GET %s ids = %v, want %v", path, got, want)
		}
		return data
	}
	assertIDs("/api/v1/admin/users/department-children?page_size=100", []string{"dept-cycle-a"})
	aChildren := assertIDs("/api/v1/admin/users/department-children?parent_department_id=dept-cycle-a&page_size=100", []string{"dept-cycle-b"})
	b := aChildren["items"].([]interface{})[0].(map[string]interface{})
	if int(b["member_count"].(float64)) != 1 || int(b["subtree_member_count"].(float64)) != 2 || int(b["matched_user_count"].(float64)) != 1 || int(b["subtree_matched_user_count"].(float64)) != 2 {
		t.Fatalf("cycle B summary = %+v, want direct/subtree 1/2", b)
	}
	assertIDs("/api/v1/admin/users/department-children?parent_department_id=dept-cycle-b&page_size=100", []string{"dept-cycle-c"})
	assertIDs("/api/v1/admin/users/department-children?parent_department_id=dept-cycle-c&page_size=100", []string{})
}

func TestAdminUsersDepartmentChildrenEffectiveSubtreeParity(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	userIDs, _ := seedAdminUsersEffectiveCycleSnapshot(t, env)
	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users/department-children?parent_department_id=dept-cycle-a&page_size=100", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("children status = %d, body=%s", w.Code, w.Body.String())
	}
	b := parseFullResponse(t, w)["data"].(map[string]interface{})["items"].([]interface{})[0].(map[string]interface{})
	w = doFullRequest(env, http.MethodGet, "/api/v1/admin/users?department_id=dept-cycle-b&page=1&page_size=100", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", w.Code, w.Body.String())
	}
	items := parseFullResponse(t, w)["data"].(map[string]interface{})["items"].([]interface{})
	got := make([]int, 0, len(items))
	for _, item := range items {
		got = append(got, int(item.(map[string]interface{})["id"].(float64)))
	}
	want := []int{userIDs["b"], userIDs["c"]}
	if fmt.Sprint(got) != fmt.Sprint(want) || int(b["subtree_member_count"].(float64)) != len(want) || int(b["subtree_matched_user_count"].(float64)) != len(want) {
		t.Fatalf("cycle B HTTP parity = ids %v summary %+v, want %v", got, b, want)
	}
}

func TestAdminUsersDepartmentChildrenRepresentativeJSONShapes(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	seedAdminUsersRepresentativeShapes(t, env)
	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users/department-children?page_size=100", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	items := parseFullResponse(t, w)["data"].(map[string]interface{})["items"].([]interface{})
	rows := map[string]map[string]interface{}{}
	for _, item := range items {
		row := item.(map[string]interface{})
		rows[row["external_id"].(string)] = row
	}
	main := rows["dept-representative-main"]
	if int(main["representative_count"].(float64)) != 5 || int(main["matched_representative_count"].(float64)) != 3 {
		t.Fatalf("main representative row = %+v, want 5/3", main)
	}
	if main["display_path"] != "Current Representative Main" || int(main["depth"].(float64)) != 0 {
		t.Fatalf("main current-source presentation = %+v", main)
	}
	scalar := rows["dept-representative-scalar"]
	if int(scalar["representative_count"].(float64)) != 1 || int(scalar["matched_representative_count"].(float64)) != 0 {
		t.Fatalf("scalar representative row = %+v, want 1/0", scalar)
	}
}

func seedAdminUsersDepartmentOptionFillers(t *testing.T, env *fullTestEnv, count int) {
	t.Helper()
	sourceID, runID := currentAdminUsersDirectorySource(t, env)
	for i := 0; i < count; i++ {
		if _, err := env.client.DirectoryDepartment.Create().
			SetSourceID(sourceID).
			SetExternalID(fmt.Sprintf("dept-http-filler-%02d", i)).
			SetName(fmt.Sprintf("Team %02d", i)).
			SetPath(fmt.Sprintf("synthetic/team/%02d", i)).
			SetLastSeenRunID(runID).
			Save(context.Background()); err != nil {
			t.Fatalf("create option filler %d: %v", i, err)
		}
	}
}

func seedAdminUsersOrphanAndStaleParent(t *testing.T, env *fullTestEnv) {
	t.Helper()
	sourceID, runID := currentAdminUsersDirectorySource(t, env)
	if _, err := env.client.DirectoryDepartment.Create().
		SetSourceID(sourceID).
		SetExternalID("dept-orphan").
		SetParentExternalID("dept-missing").
		SetName("Current Orphan").
		SetPath("synthetic/orphan").
		SetLastSeenRunID(runID).
		Save(context.Background()); err != nil {
		t.Fatalf("create current orphan: %v", err)
	}
	staleSource, staleRun := seedAdminUsersBareDirectorySource(t, env, "Stale Parent Directory", time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC))
	if _, err := env.client.DirectoryDepartment.Create().
		SetSourceID(staleSource).
		SetExternalID("dept-missing").
		SetName("Stale Missing Parent").
		SetPath("synthetic/missing").
		SetLastSeenRunID(staleRun).
		Save(context.Background()); err != nil {
		t.Fatalf("create stale missing parent: %v", err)
	}
}

func seedAdminUsersRepresentativeShapes(t *testing.T, env *fullTestEnv) {
	t.Helper()
	staleSource, staleRun := seedAdminUsersBareDirectorySource(t, env, "Stale Representative Directory", time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC))
	currentSource, currentRun := seedAdminUsersBareDirectorySource(t, env, "Current Representative Directory", time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))
	for _, source := range []struct {
		id      int
		runID   int
		prefix  string
		matched map[string]bool
	}{
		{id: staleSource, runID: staleRun, prefix: "Stale", matched: map[string]bool{"rep-department-unmatched": true, "rep-leader-unmatched": true, "rep-scalar-unmatched": true}},
		{id: currentSource, runID: currentRun, prefix: "Current", matched: map[string]bool{"rep-department-matched": true, "rep-leader-matched": true, "rep-duplicate": true}},
	} {
		if _, err := env.client.DirectoryDepartment.Create().
			SetSourceID(source.id).
			SetExternalID("dept-representative-main").
			SetName(source.prefix + " Representative Main").
			SetPath("synthetic/representative-main").
			SetMetadata(map[string]any{"representative_external_ids": []any{"rep-department-matched", "rep-department-unmatched", "rep-duplicate", "rep-duplicate"}}).
			SetLastSeenRunID(source.runID).
			Save(context.Background()); err != nil {
			t.Fatalf("create %s representative main: %v", source.prefix, err)
		}
		if _, err := env.client.DirectoryDepartment.Create().
			SetSourceID(source.id).
			SetExternalID("dept-representative-scalar").
			SetName(source.prefix + " Representative Scalar").
			SetPath("synthetic/representative-scalar").
			SetMetadata(map[string]any{"representative_external_ids": "rep-scalar-unmatched"}).
			SetLastSeenRunID(source.runID).
			Save(context.Background()); err != nil {
			t.Fatalf("create %s representative scalar: %v", source.prefix, err)
		}
		for index, member := range []struct {
			id     string
			leader any
		}{
			{id: "rep-department-matched"},
			{id: "rep-department-unmatched"},
			{id: "rep-leader-matched", leader: "dept-representative-main"},
			{id: "rep-leader-unmatched", leader: []any{"dept-representative-main", "dept-representative-main"}},
			{id: "rep-duplicate", leader: []any{"dept-representative-main"}},
			{id: "rep-scalar-unmatched"},
		} {
			builder := env.client.DirectoryMember.Create().
				SetSourceID(source.id).
				SetExternalID(member.id).
				SetEmailNormalized(fmt.Sprintf("%s-%d@example.org", strings.ToLower(source.prefix), index)).
				SetDisplayName(member.id).
				SetLastSeenRunID(source.runID)
			if source.matched[member.id] {
				builder.SetMatchedUserID(800000 + source.runID + index)
			}
			if member.leader != nil {
				builder.SetMetadata(map[string]any{"leader_department_ids": member.leader})
			}
			if _, err := builder.Save(context.Background()); err != nil {
				t.Fatalf("create %s representative %s: %v", source.prefix, member.id, err)
			}
		}
	}
}

func currentAdminUsersDirectorySource(t *testing.T, env *fullTestEnv) (int, int) {
	t.Helper()
	source, err := env.client.DirectorySource.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("load only directory source: %v", err)
	}
	if source.LastSuccessfulRunID == nil {
		t.Fatal("directory source has no last successful run")
	}
	return source.ID, *source.LastSuccessfulRunID
}

func seedAdminUsersBareDirectorySource(t *testing.T, env *fullTestEnv, name string, completedAt time.Time) (int, int) {
	t.Helper()
	ctx := context.Background()
	source, err := env.client.DirectorySource.Create().
		SetName(name).
		SetDescription("Synthetic organization directory").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		Save(ctx)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	run, err := env.client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetCompletedAt(completedAt).
		Save(ctx)
	if err != nil {
		t.Fatalf("create %s run: %v", name, err)
	}
	if _, err := env.client.DirectorySource.UpdateOneID(source.ID).SetLastRunID(run.ID).SetLastSuccessfulRunID(run.ID).Save(ctx); err != nil {
		t.Fatalf("update %s run pointers: %v", name, err)
	}
	return source.ID, run.ID
}

func TestAdminUsersListRejectsNonAdmin(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodGet, "/api/v1/admin/users", nil, nonAdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body=%s", w.Code, w.Body.String())
	}
}

func TestAdminUsersRevealRelayPassword(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)

	w := doFullRequest(env, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%d/relay-password/reveal", fixture.aliceID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if data["password"] != "test-password" {
		t.Fatalf("password = %v, want test-password", data["password"])
	}
}

func TestAdminUsersRevealErrors(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	nonAdminToken := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%d/relay-password/reveal", fixture.aliceID), nil, nonAdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want 403, body=%s", w.Code, w.Body.String())
	}

	w = doFullRequest(env, http.MethodPost, "/api/v1/admin/users/999999/relay-password/reveal", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing user status = %d, want 404, body=%s", w.Code, w.Body.String())
	}

	w = doFullRequest(env, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%d/relay-password/reveal", fixture.bobID), nil)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing password status = %d, want 422, body=%s", w.Code, w.Body.String())
	}

	if _, err := env.client.User.UpdateOneID(fixture.aliceID).SetRelayAuthPassword("not-hex-ciphertext").Save(context.Background()); err != nil {
		t.Fatalf("corrupt relay password: %v", err)
	}
	w = doFullRequest(env, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%d/relay-password/reveal", fixture.aliceID), nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("decrypt failure status = %d, want 500, body=%s", w.Code, w.Body.String())
	}
}

func TestAdminUsersRevealMissingEncryptionKey(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)

	router := gin.New()
	handler := NewAdminUsersHandler(env.client, "")
	router.POST("/admin/users/:id/relay-password/reveal", handler.RevealRelayPassword)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/users/%d/relay-password/reveal", fixture.aliceID), nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("missing key status = %d, want 500, body=%s", w.Code, w.Body.String())
	}
}

func TestAdminUsersDisableAccessDisablesRelayUserWithoutRevokingTokens(t *testing.T) {
	ctx := context.Background()
	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	provider := env.client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	userToken, err := env.authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       fixture.aliceID,
		Username: "alice",
		Role:     "user",
	})
	if err != nil {
		t.Fatalf("generate user token: %v", err)
	}
	fakeRelay := &adminUsersRelayFake{}
	handler := NewAdminUsersHandler(env.client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			t.Fatalf("provider id = %d, want %d", providerID, provider.ID)
		}
		return fakeRelay, nil
	}))
	router := gin.New()
	router.POST("/admin/users/:id/disable-access", handler.DisableAccess)

	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/admin/users/%d/disable-access", fixture.aliceID),
		strings.NewReader(`{"confirm_email":"alice@example.com"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("disable access status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if fakeRelay.disabledUserID != 42 {
		t.Fatalf("disabled relay user id = %d, want 42", fakeRelay.disabledUserID)
	}
	u := env.client.User.GetX(ctx, fixture.aliceID)
	if u.RelayDisabledAt == nil {
		t.Fatalf("relay_disabled_at was not set")
	}
	if _, err := env.authSvc.ValidateAccessToken(ctx, userToken.AccessToken); err != nil {
		t.Fatalf("old access token should remain valid after user disable: %v", err)
	}
	resp := parseFullResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["status"] != "disabled" || int(data["relay_user_id"].(float64)) != 42 {
		t.Fatalf("disable response = %+v", data)
	}
	if data["relay_disabled_at"] == nil {
		t.Fatalf("disable response missing relay_disabled_at: %+v", data)
	}
}

func TestAdminUsersDisableAccessRequiresEmailConfirmation(t *testing.T) {
	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	handler := NewAdminUsersHandler(env.client, adminUsersTestEncryptionKey)
	router := gin.New()
	router.POST("/admin/users/:id/disable-access", handler.DisableAccess)

	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/admin/users/%d/disable-access", fixture.aliceID),
		strings.NewReader(`{"confirm_email":"bob@example.org"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("confirmation mismatch status = %d, want 422, body=%s", w.Code, w.Body.String())
	}
}
