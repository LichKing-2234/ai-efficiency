package directorysync

import (
	"context"
	"database/sql"
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
	"github.com/ai-efficiency/backend/ent/directoryoffboardingaction"
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

	page, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].UserID != bob.ID {
		t.Fatalf("candidates = %+v, want bob missing from current source", page.Items)
	}
}

func TestServiceOffboardingCandidatePageAndCountShareBoundedContract(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	completedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	source := createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "present@example.com", completedAt)
	source = client.DirectorySource.GetX(ctx, source.ID)
	runID := *source.LastSuccessfulRunID

	initialUsers := createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{
		{username: "action-failed", email: "failed@example.com"},
		{username: "action-partial", email: "partial@example.com"},
		{username: "disabled", email: "disabled@example.com"},
		{username: "present", email: "present@example.com"},
		{username: "regular-alpha", email: "regular-alpha@example.com"},
		{username: "regular-beta", email: "regular-beta@example.com"},
	})
	failedAction := createDirectoryOffboardingAction(t, ctx, client, source.ID, runID, initialUsers[0], directoryoffboardingaction.StatusFailed)
	partialAction := createDirectoryOffboardingAction(t, ctx, client, source.ID, runID, initialUsers[1], directoryoffboardingaction.StatusPartialFailed)
	createDirectoryOffboardingAction(t, ctx, client, source.ID, runID, initialUsers[2], directoryoffboardingaction.StatusSucceeded)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open postgres for duplicate username fixture: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `DROP INDEX users_username_key`); err != nil {
		t.Fatalf("drop isolated username index for tie fixture: %v", err)
	}
	tiedUsers := createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{
		{username: "same-name", email: "same-name-one@example.com"},
		{username: "same-name", email: "same-name-two@example.com"},
	})
	if tiedUsers[0].ID >= tiedUsers[1].ID {
		t.Fatalf("tie fixture ids = %d, %d, want ascending creation ids", tiedUsers[0].ID, tiedUsers[1].ID)
	}

	bulkFixtures := make([]offboardingUserFixture, 500)
	for index := range bulkFixtures {
		bulkFixtures[index] = offboardingUserFixture{
			username: fmt.Sprintf("bulk-%03d", index),
			email:    fmt.Sprintf("bulk-%03d@example.com", index),
		}
	}
	createRelayBoundUsers(t, ctx, client, bulkFixtures)

	recorder := &entQueryRecorder{}
	loggedClient, err := ent.Open("postgres", dsn, ent.Debug(), ent.Log(recorder.Log))
	if err != nil {
		t.Fatalf("open logged ent client: %v", err)
	}
	t.Cleanup(func() { _ = loggedClient.Close() })
	svc := NewService(loggedClient, ServiceOptions{})

	recorder.Reset()
	count, err := svc.CountOffboardingCandidates(ctx, source.ID)
	if err != nil {
		t.Fatalf("CountOffboardingCandidates: %v", err)
	}
	if count != 506 {
		t.Fatalf("candidate count = %d, want 506", count)
	}
	if queryCount := recorder.Count(); queryCount != 2 {
		t.Fatalf("count query count = %d, want snapshot + COUNT only; queries:\n%s", queryCount, recorder.Joined())
	}

	recorder.Reset()
	defaultPage, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{SourceID: source.ID})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates default page: %v", err)
	}
	if defaultPage.Page != 1 || defaultPage.PageSize != 20 || len(defaultPage.Items) != 20 || defaultPage.Total != count {
		t.Fatalf("default page = %+v, want page=1 page_size=20 len=20 total=%d", defaultPage, count)
	}
	if queryCount := recorder.Count(); queryCount != 4 {
		t.Fatalf("page query count = %d, want snapshot + count + page + action batch; queries:\n%s", queryCount, recorder.Joined())
	}
	assertOffboardingActionMetadata(t, defaultPage.Items, failedAction)
	assertOffboardingActionMetadata(t, defaultPage.Items, partialAction)
	assertCandidateAbsent(t, defaultPage.Items, initialUsers[2].ID, "succeeded disable action")
	assertCandidateAbsent(t, defaultPage.Items, initialUsers[3].ID, "current directory membership")

	maxPage, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{SourceID: source.ID, Page: 1, PageSize: 1000})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates max page: %v", err)
	}
	if maxPage.PageSize != 100 || len(maxPage.Items) != 100 {
		t.Fatalf("max page size = %d len=%d, want 100", maxPage.PageSize, len(maxPage.Items))
	}

	tiePage, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{SourceID: source.ID, Query: "same-name", PageSize: 20})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates tie page: %v", err)
	}
	if len(tiePage.Items) != 2 || tiePage.Items[0].UserID != tiedUsers[0].ID || tiePage.Items[1].UserID != tiedUsers[1].ID {
		t.Fatalf("tie page ids = %v, want [%d %d]", candidateUserIDs(tiePage.Items), tiedUsers[0].ID, tiedUsers[1].ID)
	}

	seen := make(map[int]struct{}, count)
	for pageNumber := 1; len(seen) < count; pageNumber++ {
		page, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{SourceID: source.ID, Page: pageNumber, PageSize: 73})
		if err != nil {
			t.Fatalf("ListOffboardingCandidates page %d: %v", pageNumber, err)
		}
		if page.Total != count {
			t.Fatalf("page %d total = %d, want %d", pageNumber, page.Total, count)
		}
		if len(page.Items) == 0 {
			t.Fatalf("page %d empty before collecting total=%d, collected=%d", pageNumber, count, len(seen))
		}
		for _, item := range page.Items {
			if _, duplicate := seen[item.UserID]; duplicate {
				t.Fatalf("candidate user %d repeated across stable pages", item.UserID)
			}
			seen[item.UserID] = struct{}{}
		}
	}
	if len(seen) != count {
		t.Fatalf("page union size = %d, want count %d", len(seen), count)
	}
}

func TestServiceOffboardingPageQueryCountStaysConstantAsFixtureGrows(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "present@example.com", time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC))

	initial := make([]offboardingUserFixture, 25)
	for index := range initial {
		initial[index] = offboardingUserFixture{username: fmt.Sprintf("initial-%03d", index), email: fmt.Sprintf("initial-%03d@example.com", index)}
	}
	createRelayBoundUsers(t, ctx, client, initial)

	recorder := &entQueryRecorder{}
	loggedClient, err := ent.Open("postgres", dsn, ent.Debug(), ent.Log(recorder.Log))
	if err != nil {
		t.Fatalf("open logged ent client: %v", err)
	}
	t.Cleanup(func() { _ = loggedClient.Close() })
	svc := NewService(loggedClient, ServiceOptions{})

	recorder.Reset()
	if _, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{PageSize: 20}); err != nil {
		t.Fatalf("ListOffboardingCandidates small fixture: %v", err)
	}
	smallFixtureQueries := recorder.Count()
	if !recorder.ContainsBoundedCurrentSnapshotQuery() {
		t.Fatalf("current snapshot query was not database-bounded with ORDER BY/LIMIT; queries:\n%s", recorder.Joined())
	}

	growth := make([]offboardingUserFixture, 500)
	for index := range growth {
		growth[index] = offboardingUserFixture{username: fmt.Sprintf("growth-%03d", index), email: fmt.Sprintf("growth-%03d@example.com", index)}
	}
	createRelayBoundUsers(t, ctx, client, growth)

	recorder.Reset()
	if _, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{PageSize: 20}); err != nil {
		t.Fatalf("ListOffboardingCandidates grown fixture: %v", err)
	}
	if grownFixtureQueries := recorder.Count(); grownFixtureQueries != smallFixtureQueries || grownFixtureQueries != 4 {
		t.Fatalf("query count small=%d grown=%d, want constant 4; queries:\n%s", smallFixtureQueries, grownFixtureQueries, recorder.Joined())
	}
}

func TestServiceDisableCandidateRejectsMismatchedConfirmationWithoutSideEffects(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "alice@example.com", time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC))
	bob := createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{{username: "bob", email: "bob@example.org"}})[0]
	disabler := &fakeRelayDisabler{}
	revoker := &fakeTokenRevoker{}
	svc := NewService(client, ServiceOptions{RelayDisablers: fakeRelayDisablerResolver{disabler: disabler}, TokenRevoker: revoker})

	_, err := svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
		UserID:            bob.ID,
		ConfirmEmail:      "alice@example.com",
		Reason:            offboardingReasonMissingFromDirectory,
		PerformedByUserID: bob.ID,
	})
	if err == nil {
		t.Fatal("DisableRelayUserForCandidate succeeded with mismatched confirmation, want validation error")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Fatalf("error = %T %v, want ValidationError", err, err)
	}
	assertNoOffboardingSideEffects(t, ctx, client, disabler, revoker)
}

func TestServiceDisableCandidateRechecksNewDirectoryMembershipWithoutSideEffects(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createDirectorySnapshot(t, ctx, client, "Current Directory", "dept-current", "alice@example.com", time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC))
	bob := createRelayBoundUsers(t, ctx, client, []offboardingUserFixture{{username: "bob", email: "bob@example.org"}})[0]
	disabler := &fakeRelayDisabler{}
	revoker := &fakeTokenRevoker{}
	svc := NewService(client, ServiceOptions{RelayDisablers: fakeRelayDisablerResolver{disabler: disabler}, TokenRevoker: revoker})

	page, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{SourceID: source.ID})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates before member re-add: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].UserID != bob.ID {
		t.Fatalf("initial candidates = %+v, want bob", page.Items)
	}
	client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-bob").
		SetEmailNormalized("bob@example.org").
		SetDisplayName("Bob").
		SetDepartmentExternalID("dept-current").
		SetLastSeenRunID(page.Items[0].DirectoryRunID).
		SaveX(ctx)

	_, err = svc.DisableRelayUserForCandidate(ctx, DisableCandidateRequest{
		SourceID:          source.ID,
		UserID:            bob.ID,
		ConfirmEmail:      "bob@example.org",
		Reason:            offboardingReasonMissingFromDirectory,
		PerformedByUserID: bob.ID,
	})
	if err == nil {
		t.Fatal("DisableRelayUserForCandidate succeeded after directory member re-add, want conflict")
	}
	if _, ok := err.(*ConflictError); !ok {
		t.Fatalf("error = %T %v, want ConflictError", err, err)
	}
	assertNoOffboardingSideEffects(t, ctx, client, disabler, revoker)
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

	page, err := svc.ListOffboardingCandidates(ctx, OffboardingCandidateListParams{SourceID: source.ID})
	if err != nil {
		t.Fatalf("ListOffboardingCandidates: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].UserID != bob.ID {
		t.Fatalf("candidates = %+v, want bob only", page.Items)
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

type offboardingUserFixture struct {
	username string
	email    string
}

func createRelayBoundUsers(t *testing.T, ctx context.Context, client *ent.Client, fixtures []offboardingUserFixture) []*ent.User {
	t.Helper()
	builders := make([]*ent.UserCreate, 0, len(fixtures))
	for index, fixture := range fixtures {
		builders = append(builders, client.User.Create().
			SetUsername(fixture.username).
			SetEmail(fixture.email).
			SetAuthSource(entuser.AuthSourceRelaySSO).
			SetRole(entuser.RoleUser).
			SetRelayUserID(100000+index))
	}
	users, err := client.User.CreateBulk(builders...).Save(ctx)
	if err != nil {
		t.Fatalf("create %d relay-bound users: %v", len(fixtures), err)
	}
	return users
}

func createDirectoryOffboardingAction(t *testing.T, ctx context.Context, client *ent.Client, sourceID, runID int, user *ent.User, status directoryoffboardingaction.Status) *ent.DirectoryOffboardingAction {
	t.Helper()
	action, err := client.DirectoryOffboardingAction.Create().
		SetSourceID(sourceID).
		SetUserID(user.ID).
		SetRelayUserID(*user.RelayUserID).
		SetDirectoryRunID(runID).
		SetAction(directoryoffboardingaction.ActionDisableRelayUser).
		SetStatus(status).
		SetReason(offboardingReasonMissingFromDirectory).
		SetPerformedByUserID(user.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create %s offboarding action for user %d: %v", status, user.ID, err)
	}
	return action
}

func assertOffboardingActionMetadata(t *testing.T, candidates []OffboardingCandidate, want *ent.DirectoryOffboardingAction) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.UserID != want.UserID {
			continue
		}
		if candidate.OffboardingStatus != string(want.Status) || candidate.OffboardingActionID == nil || *candidate.OffboardingActionID != want.ID {
			t.Fatalf("candidate action metadata = %+v, want status=%s id=%d", candidate, want.Status, want.ID)
		}
		return
	}
	t.Fatalf("candidate for action user %d not found in page", want.UserID)
}

func assertCandidateAbsent(t *testing.T, candidates []OffboardingCandidate, userID int, reason string) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.UserID == userID {
			t.Fatalf("candidate user %d present despite %s", userID, reason)
		}
	}
}

func candidateUserIDs(candidates []OffboardingCandidate) []int {
	ids := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.UserID)
	}
	return ids
}

func assertNoOffboardingSideEffects(t *testing.T, ctx context.Context, client *ent.Client, disabler *fakeRelayDisabler, revoker *fakeTokenRevoker) {
	t.Helper()
	if len(disabler.disabled) != 0 || len(revoker.calls) != 0 {
		t.Fatalf("side effects disabled=%v revocations=%v, want none", disabler.disabled, revoker.calls)
	}
	if count := client.DirectoryOffboardingAction.Query().CountX(ctx); count != 0 {
		t.Fatalf("offboarding action count = %d, want 0", count)
	}
}

type entQueryRecorder struct {
	mu      sync.Mutex
	queries []string
}

func (r *entQueryRecorder) Log(values ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = append(r.queries, fmt.Sprint(values...))
}

func (r *entQueryRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = nil
}

func (r *entQueryRecorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queries)
}

func (r *entQueryRecorder) Joined() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.queries, "\n")
}

func (r *entQueryRecorder) ContainsBoundedCurrentSnapshotQuery() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, query := range r.queries {
		lower := strings.ToLower(query)
		if strings.Contains(lower, "directory_sync_runs") && strings.Contains(lower, "order by") && strings.Contains(lower, "limit") {
			return true
		}
	}
	return false
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
