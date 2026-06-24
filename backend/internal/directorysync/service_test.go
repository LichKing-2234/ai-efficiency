package directorysync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func stringsReplaceAll(input string, replacements map[string]string) string {
	out := input
	for old, replacement := range replacements {
		out = strings.ReplaceAll(out, old, replacement)
	}
	return out
}
