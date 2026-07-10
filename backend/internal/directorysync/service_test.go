package directorysync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorymember"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type fakeTokenRevoker struct {
	calls []revocationCall
}

type revocationCall struct {
	UserID    int
	RevokedAt time.Time
}

func (f *fakeTokenRevoker) RevokeUserTokens(_ context.Context, userID int, revokedAt time.Time) error {
	f.calls = append(f.calls, revocationCall{UserID: userID, RevokedAt: revokedAt})
	return nil
}

type fakeRelayDisablerResolver struct {
	disabler relay.UserDisabler
}

func (f fakeRelayDisablerResolver) ResolveRelayDisabler(_ context.Context, _ int) (relay.UserDisabler, error) {
	return f.disabler, nil
}

type fakeRelayDisabler struct {
	disabled []int64
}

func (f *fakeRelayDisabler) DisableUser(_ context.Context, userID int64) error {
	f.disabled = append(f.disabled, userID)
	return nil
}

func TestServicePreviewDoesNotUpdateFactsAndApplyDoes(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	svc := NewService(client, ServiceOptions{
		Executor:    NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})

	preview, err := svc.RunSource(ctx, source.ID, "preview", "manual")
	if err != nil {
		t.Fatalf("preview RunSource: %v", err)
	}
	if preview.Status != "queued" {
		t.Fatalf("preview status = %s, want queued", preview.Status)
	}
	preview, err = svc.ExecuteRun(ctx, preview.ID)
	if err != nil {
		t.Fatalf("preview ExecuteRun: %v", err)
	}
	if preview.Status != "completed" {
		t.Fatalf("executed preview status = %s, want completed", preview.Status)
	}
	if count := client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(source.ID)).CountX(ctx); count != 0 {
		t.Fatalf("preview member count = %d, want 0", count)
	}

	apply, err := svc.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("apply RunSource: %v", err)
	}
	if apply.Status != "queued" {
		t.Fatalf("apply status = %s, want queued", apply.Status)
	}
	apply, err = svc.ExecuteRun(ctx, apply.ID)
	if err != nil {
		t.Fatalf("apply ExecuteRun: %v", err)
	}
	if apply.Status != "completed" {
		t.Fatalf("executed apply status = %s, want completed", apply.Status)
	}
	if count := client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(source.ID)).CountX(ctx); count != 1 {
		t.Fatalf("apply member count = %d, want 1", count)
	}
	reloaded := client.DirectorySource.GetX(ctx, source.ID)
	if reloaded.LastSuccessfulRunID == nil || *reloaded.LastSuccessfulRunID != apply.ID {
		t.Fatalf("last_successful_run_id = %v, want %d", reloaded.LastSuccessfulRunID, apply.ID)
	}
}

func TestServiceRunSourceRejectsOverlappingApply(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	svc := NewService(client, ServiceOptions{
		Executor:    NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})

	first, err := svc.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("first RunSource: %v", err)
	}
	if _, err := svc.RunSource(ctx, source.ID, "apply", "manual"); err == nil {
		t.Fatal("second apply RunSource succeeded, want conflict")
	} else if _, ok := err.(*ConflictError); !ok {
		t.Fatalf("second apply error = %T %v, want ConflictError", err, err)
	}

	if _, err := svc.ExecuteRun(ctx, first.ID); err != nil {
		t.Fatalf("first ExecuteRun: %v", err)
	}
	if _, err := svc.RunSource(ctx, source.ID, "apply", "manual"); err != nil {
		t.Fatalf("apply after first completion: %v", err)
	}
}

func TestServiceRunSourceRejectsConcurrentApplyCreation(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	firstCreateEntered := make(chan struct{})
	releaseFirstCreate := make(chan struct{})
	var hookMu sync.Mutex
	blockedFirstCreate := false
	client.DirectorySyncRun.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			hookMu.Lock()
			shouldBlock := m.Op().Is(ent.OpCreate) && !blockedFirstCreate
			if shouldBlock {
				blockedFirstCreate = true
			}
			hookMu.Unlock()
			if shouldBlock {
				close(firstCreateEntered)
				<-releaseFirstCreate
			}
			return next.Mutate(ctx, m)
		})
	})
	svc := NewService(client, ServiceOptions{
		Executor:    NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})

	firstErr := make(chan error, 1)
	go func() {
		_, err := svc.RunSource(ctx, source.ID, "apply", "manual")
		firstErr <- err
	}()
	<-firstCreateEntered
	if _, err := svc.RunSource(ctx, source.ID, "apply", "manual"); err == nil {
		close(releaseFirstCreate)
		t.Fatal("concurrent apply RunSource succeeded, want conflict")
	} else if _, ok := err.(*ConflictError); !ok {
		close(releaseFirstCreate)
		t.Fatalf("concurrent apply error = %T %v, want ConflictError", err, err)
	}
	close(releaseFirstCreate)
	if err := <-firstErr; err != nil {
		t.Fatalf("first RunSource: %v", err)
	}
}

func TestServiceApplyRollsBackFactsWhenSourcePointerUpdateFails(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	client.DirectorySource.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			mutation, ok := m.(*ent.DirectorySourceMutation)
			if ok && m.Op().Is(ent.OpUpdateOne) {
				if _, exists := mutation.LastSuccessfulRunID(); exists {
					return nil, fmt.Errorf("injected source pointer failure")
				}
			}
			return next.Mutate(ctx, m)
		})
	})
	svc := NewService(client, ServiceOptions{
		Executor:    NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})

	run, err := svc.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("apply RunSource: %v", err)
	}
	if _, err := svc.ExecuteRun(ctx, run.ID); err == nil {
		t.Fatal("ExecuteRun succeeded, want injected source pointer failure")
	}
	if count := client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(source.ID)).CountX(ctx); count != 0 {
		t.Fatalf("member count after failed apply = %d, want 0", count)
	}
	reloaded := client.DirectorySource.GetX(ctx, source.ID)
	if reloaded.LastSuccessfulRunID != nil {
		t.Fatalf("last_successful_run_id = %v, want nil", reloaded.LastSuccessfulRunID)
	}
}

func TestCurrentSourceIDUsesLatestSuccessfulApplyRun(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	oldCompletedAt := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)
	newCompletedAt := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	oldSource := createDirectorySnapshot(t, ctx, client, "Old Directory", "dept-old", "alice@example.com", oldCompletedAt)
	newSource := createDirectorySnapshot(t, ctx, client, "New Directory", "dept-new", "alice@example.com", newCompletedAt)
	if _, err := client.DirectorySource.UpdateOneID(oldSource.ID).SetDescription("Edited after latest sync").Save(ctx); err != nil {
		t.Fatalf("update old source: %v", err)
	}

	sourceID, ok, err := CurrentSourceID(ctx, client)
	if err != nil {
		t.Fatalf("CurrentSourceID: %v", err)
	}
	if !ok || sourceID != newSource.ID {
		t.Fatalf("current source = %d/%v, want new source %d", sourceID, ok, newSource.ID)
	}
}

func TestServiceListDepartmentsReturnsDisplayPathAndFiltersByIt(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := client.DirectorySource.Create().
		SetName("Example Directory").
		SetDescription("Synthetic organization directory").
		SetScope("full_company").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SetScheduleEnabled(false).
		SetScheduleInterval("daily").
		SetScheduleTimezone("UTC").
		SaveX(ctx)
	run := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetDepartmentCount(2).
		SetMemberCount(0).
		SetCompletedAt(time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)).
		SaveX(ctx)
	client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("1684075").
		SetName("Department Alpha").
		SetPath("1.488797.1684075").
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("1684207").
		SetParentExternalID("1684075").
		SetName("Team One").
		SetPath("1.488797.1684075.1684077.1684207").
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	svc := NewService(client, ServiceOptions{})

	items, err := svc.ListDepartments(ctx, source.ID, "Team One")
	if err != nil {
		t.Fatalf("ListDepartments: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1: %#v", len(items), items)
	}
	if items[0].Path != "1.488797.1684075.1684077.1684207" {
		t.Fatalf("path = %q, want raw numeric source path", items[0].Path)
	}
	if items[0].DisplayPath != "Department Alpha / Team One" {
		t.Fatalf("display_path = %q, want name-based hierarchy", items[0].DisplayPath)
	}

	items, err = svc.ListDepartments(ctx, source.ID, "Department Alpha / Team One")
	if err != nil {
		t.Fatalf("ListDepartments by display path: %v", err)
	}
	if len(items) != 1 || items[0].ExternalID != "1684207" {
		t.Fatalf("display path search items = %#v, want child department", items)
	}
}

func TestServiceListOffboardingCandidatesUsesCurrentSourceWhenSourceIDMissing(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	older := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)
	latest := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	oldSource := createDirectorySnapshot(t, ctx, client, "Old Directory", "dept-old", "bob@example.org", older)
	createDirectorySnapshot(t, ctx, client, "New Directory", "dept-new", "alice@example.com", latest)
	if _, err := client.DirectorySource.UpdateOneID(oldSource.ID).SetDescription("Edited after latest sync").Save(ctx); err != nil {
		t.Fatalf("update old source: %v", err)
	}
	bob := client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		SetRelayUserID(99).
		SaveX(ctx)
	client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		SetRelayUserID(42).
		SaveX(ctx)
	svc := NewService(client, ServiceOptions{})

	candidates, err := svc.ListOffboardingCandidates(ctx, 0, "")
	if err != nil {
		t.Fatalf("ListOffboardingCandidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].UserID != bob.ID {
		t.Fatalf("candidates = %+v, want bob missing from current source", candidates)
	}
}

func TestServiceDisableCandidateRejectsStaleSourceIDWhenUserExistsInCurrentSource(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	older := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)
	latest := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	oldSource := createDirectorySnapshot(t, ctx, client, "Old Directory", "dept-old", "alice@example.com", older)
	createDirectorySnapshot(t, ctx, client, "New Directory", "dept-new", "bob@example.org", latest)
	bob := client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		SetRelayUserID(99).
		SaveX(ctx)
	disabler := &fakeRelayDisabler{}
	revoker := &fakeTokenRevoker{}
	svc := NewService(client, ServiceOptions{
		RelayDisablers: fakeRelayDisablerResolver{disabler: disabler},
		TokenRevoker:   revoker,
	})

	_, err := svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
		SourceID:          oldSource.ID,
		UserID:            bob.ID,
		ConfirmEmail:      "bob@example.org",
		Reason:            offboardingReasonMissingFromDirectory,
		PerformedByUserID: bob.ID,
	})
	if err == nil {
		t.Fatal("DisableRelayUserForCandidate succeeded with stale source id, want conflict")
	}
	if _, ok := err.(*ConflictError); !ok {
		t.Fatalf("error = %T %v, want ConflictError", err, err)
	}
	if len(disabler.disabled) != 0 || len(revoker.calls) != 0 {
		t.Fatalf("side effects disabled=%v revocations=%v, want none", disabler.disabled, revoker.calls)
	}
}

func TestServiceCreateAndUpdateSourceRejectLiteralSecretsBeforePersisting(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	svc := NewService(client, ServiceOptions{
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})
	secretDSL := strings.Replace(validDirectoryDSL, "      url: https://directory.example.com/api/departments\n", "      url: https://directory.example.com/api/departments\n      headers:\n        Authorization: Bearer test-token\n", 1)

	if _, err := svc.CreateSource(ctx, SourceInput{
		Name:    "Unsafe Directory",
		Enabled: true,
		DSL:     secretDSL,
	}); err == nil {
		t.Fatal("CreateSource succeeded with literal secret, want validation error")
	} else if _, ok := err.(*ValidationError); !ok {
		t.Fatalf("CreateSource error = %T %v, want ValidationError", err, err)
	}
	if count := client.DirectorySource.Query().CountX(ctx); count != 0 {
		t.Fatalf("source count = %d, want 0", count)
	}

	source, err := svc.CreateSource(ctx, SourceInput{
		Name:    "Safe Directory",
		Enabled: true,
		DSL:     validDirectoryDSL,
	})
	if err != nil {
		t.Fatalf("CreateSource safe: %v", err)
	}
	if _, err := svc.UpdateSource(ctx, source.ID, SourceInput{
		Name:    "Unsafe Directory",
		Enabled: true,
		DSL:     secretDSL,
	}); err == nil {
		t.Fatal("UpdateSource succeeded with literal secret, want validation error")
	} else if _, ok := err.(*ValidationError); !ok {
		t.Fatalf("UpdateSource error = %T %v, want ValidationError", err, err)
	}
	reloaded := client.DirectorySource.GetX(ctx, source.ID)
	if strings.Contains(reloaded.Dsl, "Authorization") {
		t.Fatalf("persisted unsafe DSL: %s", reloaded.Dsl)
	}
}

func TestCurrentSourceIDIgnoresSuccessfulRunWithoutCompletedAt(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	latest := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	incompleteSource := client.DirectorySource.Create().
		SetName("Incomplete Directory").
		SetDescription("Synthetic organization directory").
		SetScope("full_company").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SetScheduleEnabled(false).
		SetScheduleInterval("daily").
		SetScheduleTimezone("UTC").
		SaveX(ctx)
	incompleteRun := client.DirectorySyncRun.Create().
		SetSourceID(incompleteSource.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SaveX(ctx)
	client.DirectorySource.UpdateOneID(incompleteSource.ID).
		SetLastRunID(incompleteRun.ID).
		SetLastSuccessfulRunID(incompleteRun.ID).
		SaveX(ctx)
	currentSource := createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "alice@example.com", latest)

	sourceID, ok, err := CurrentSourceID(ctx, client)
	if err != nil {
		t.Fatalf("CurrentSourceID: %v", err)
	}
	if !ok || sourceID != currentSource.ID {
		t.Fatalf("current source = %d/%v, want %d", sourceID, ok, currentSource.ID)
	}
}

func TestServiceOffboardingCandidateAndDisableRevokesTokens(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{"alice@example.com"})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	svc := NewService(client, ServiceOptions{
		Executor:    NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})
	run, err := svc.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("apply RunSource: %v", err)
	}
	if _, err := svc.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("apply ExecuteRun: %v", err)
	}

	bob := client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		SetRelayUserID(99).
		SaveX(ctx)
	client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		SetRelayUserID(42).
		SaveX(ctx)

	candidates, err := svc.ListOffboardingCandidates(ctx, source.ID, "")
	if err != nil {
		t.Fatalf("ListOffboardingCandidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].UserID != bob.ID {
		t.Fatalf("candidates = %+v, want bob only", candidates)
	}

	disabler := &fakeRelayDisabler{}
	revoker := &fakeTokenRevoker{}
	svc = NewService(client, ServiceOptions{
		Executor:       NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials:    staticCredentialResolver{"directory_api_key": "test-directory-secret"},
		RelayDisablers: fakeRelayDisablerResolver{disabler: disabler},
		TokenRevoker:   revoker,
		Now:            func() time.Time { return time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC) },
	})
	action, err := svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
		SourceID:          source.ID,
		UserID:            bob.ID,
		ConfirmEmail:      "bob@example.org",
		Reason:            "missing_from_latest_full_company_directory",
		PerformedByUserID: bob.ID,
	})
	if err != nil {
		t.Fatalf("DisableRelayUserForCandidate: %v", err)
	}
	if action.Status != "succeeded" {
		t.Fatalf("action status = %s, want succeeded", action.Status)
	}
	if len(disabler.disabled) != 1 || disabler.disabled[0] != 99 {
		t.Fatalf("disabled = %v, want [99]", disabler.disabled)
	}
	if len(revoker.calls) != 1 || revoker.calls[0].UserID != bob.ID {
		t.Fatalf("revocations = %+v, want bob", revoker.calls)
	}
}

func TestServiceProviderWithoutDisableCapabilityReturnsValidationError(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	server := newDirectoryServiceTestServer(t, []string{})
	source := createDirectoryTestSource(t, ctx, client, server.URL)
	svc := NewService(client, ServiceOptions{
		Executor:    NewExecutor(ExecutorOptions{AllowHTTP: true}),
		Credentials: staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	})
	run, err := svc.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("apply RunSource: %v", err)
	}
	if _, err := svc.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("apply ExecuteRun: %v", err)
	}
	user := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		SetRelayUserID(42).
		SaveX(ctx)

	_, err = svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
		SourceID:          source.ID,
		UserID:            user.ID,
		ConfirmEmail:      "alice@example.com",
		Reason:            "missing_from_latest_full_company_directory",
		PerformedByUserID: user.ID,
	})
	if err == nil {
		t.Fatal("expected validation error when relay disabler is not configured")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Fatalf("error = %T %v, want ValidationError", err, err)
	}
}

func newDirectoryServiceTestServer(t *testing.T, memberEmails []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Directory-API-Key") != "test-directory-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/departments":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"departments": []map[string]any{
				{"id": "dept-alpha", "name": "Department Alpha", "path": "Department Alpha"},
			}}})
		case "/users":
			users := make([]map[string]any, 0, len(memberEmails))
			for _, email := range memberEmails {
				users = append(users, map[string]any{"id": email, "email": email, "name": email, "status": "active"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"users": users}})
		default:
			http.NotFound(w, r)
		}
	}))
}

func createDirectoryTestSource(t *testing.T, ctx context.Context, client *ent.Client, baseURL string) *ent.DirectorySource {
	t.Helper()
	raw := stringsReplaceAll(validDirectoryDSL, map[string]string{
		"https://directory.example.com/api/departments": baseURL + "/departments",
		"https://directory.example.com/api/users":       baseURL + "/users",
	})
	return client.DirectorySource.Create().
		SetName("Example Directory").
		SetDescription("Synthetic directory source").
		SetScope("full_company").
		SetEnabled(true).
		SetDsl(raw).
		SetScheduleEnabled(false).
		SetScheduleInterval("daily").
		SetScheduleTimezone("UTC").
		SaveX(ctx)
}

func createDirectorySnapshot(t *testing.T, ctx context.Context, client *ent.Client, name, departmentID, memberEmail string, completedAt time.Time) *ent.DirectorySource {
	t.Helper()
	source := client.DirectorySource.Create().
		SetName(name).
		SetDescription("Synthetic organization directory").
		SetScope("full_company").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SetScheduleEnabled(false).
		SetScheduleInterval("daily").
		SetScheduleTimezone("UTC").
		SaveX(ctx)
	run := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetDepartmentCount(1).
		SetMemberCount(1).
		SetCompletedAt(completedAt).
		SaveX(ctx)
	client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		SaveX(ctx)
	client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID(departmentID).
		SetName("Department " + strings.TrimPrefix(departmentID, "dept-")).
		SetPath("Department " + strings.TrimPrefix(departmentID, "dept-")).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID(memberEmail).
		SetEmailNormalized(memberEmail).
		SetDisplayName(memberEmail).
		SetDepartmentExternalID(departmentID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	return source
}

func stringsReplaceAll(input string, replacements map[string]string) string {
	out := input
	for old, replacement := range replacements {
		out = strings.ReplaceAll(out, old, replacement)
	}
	return out
}
