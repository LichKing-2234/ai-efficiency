package repo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

const webhookRepairTestKey = "0000000000000000000000000000000000000000000000000000000000000000"

type bitbucketRepairServer struct {
	server        *httptest.Server
	deleteCalled  bool
	registerCalls int
	failRegister  bool
}

func newBitbucketRepairServer(t *testing.T) *bitbucketRepairServer {
	t.Helper()
	s := &bitbucketRepairServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/PROJ/repos/repo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("repo method = %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"slug":    "repo",
			"name":    "repo",
			"project": map[string]any{"key": "PROJ"},
			"links": map[string]any{
				"clone": []map[string]string{{"name": "http", "href": "https://bitbucket.example.com/scm/proj/repo.git"}},
			},
		})
	})
	mux.HandleFunc("/rest/api/1.0/projects/PROJ/repos/repo/webhooks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("webhook method = %s", r.Method)
		}
		s.registerCalls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode register body: %v", err)
		}
		if body["url"] != "https://ai-efficiency.example.com/api/v1/webhooks/bitbucket" {
			t.Fatalf("url = %v, want public callback URL", body["url"])
		}
		if s.failRegister {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"message":"requires REPO_ADMIN"}]}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 99})
	})
	mux.HandleFunc("/rest/api/1.0/projects/PROJ/repos/repo/webhooks/old-hook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("delete method = %s", r.Method)
		}
		s.deleteCalled = true
		w.WriteHeader(http.StatusNoContent)
	})
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func newRepairTestService(t *testing.T, publicURL string) (*ent.Client, *Service) {
	t.Helper()
	client := testdb.Open(t)
	svc := NewService(client, webhookRepairTestKey, zap.NewNop(), ServiceOptions{
		WebhookPublicURL: publicURL,
		ServerMode:       "release",
	})
	return client, svc
}

func createRepairProvider(t *testing.T, client *ent.Client, baseURL string) *ent.ScmProvider {
	t.Helper()
	encrypted, err := pkg.Encrypt(`{"token":"test-token"}`, webhookRepairTestKey)
	if err != nil {
		t.Fatalf("encrypt credentials: %v", err)
	}
	provider, err := client.ScmProvider.Create().
		SetName("bitbucket").
		SetType(scmprovider.TypeBitbucketServer).
		SetBaseURL(baseURL).
		SetCredentials(encrypted).
		SetStatus(scmprovider.StatusActive).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	return provider
}

func createRepairRepo(t *testing.T, client *ent.Client, provider *ent.ScmProvider, status repoconfig.Status) *ent.RepoConfig {
	return createRepairRepoNamed(t, client, provider, "PROJ/repo", "repo", status)
}

func createRepairRepoNamed(t *testing.T, client *ent.Client, provider *ent.ScmProvider, fullName string, name string, status repoconfig.Status) *ent.RepoConfig {
	t.Helper()
	create := client.RepoConfig.Create().
		SetRepoKey("bitbucket.example.com/" + strings.ToLower(fullName)).
		SetName(name).
		SetFullName(fullName).
		SetCloneURL("https://bitbucket.example.com/scm/proj/" + name + ".git").
		SetDefaultBranch("main").
		SetStatus(status)
	if provider != nil {
		create.SetScmProviderID(provider.ID)
	}
	repo, err := create.Save(context.Background())
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	return repo
}

func TestRepairWebhookRejectsUnboundRepo(t *testing.T) {
	client, svc := newRepairTestService(t, "https://ai-efficiency.example.com")
	repo := createRepairRepo(t, client, nil, repoconfig.StatusWebhookFailed)

	_, err := svc.RepairWebhook(context.Background(), repo.ID, RepairWebhookRequest{})
	if !errors.Is(err, ErrRepoUnbound) {
		t.Fatalf("err = %v, want ErrRepoUnbound", err)
	}
}

func TestRepairWebhookRejectsInactiveRepo(t *testing.T) {
	server := newBitbucketRepairServer(t)
	client, svc := newRepairTestService(t, "https://ai-efficiency.example.com")
	provider := createRepairProvider(t, client, server.server.URL)
	repo := createRepairRepo(t, client, provider, repoconfig.StatusInactive)

	_, err := svc.RepairWebhook(context.Background(), repo.ID, RepairWebhookRequest{})
	if !errors.Is(err, ErrRepoInactive) {
		t.Fatalf("err = %v, want ErrRepoInactive", err)
	}
}

func TestRepairWebhookMissingPublicURLFailsBeforeSCM(t *testing.T) {
	server := newBitbucketRepairServer(t)
	client, svc := newRepairTestService(t, "")
	provider := createRepairProvider(t, client, server.server.URL)
	repo := createRepairRepo(t, client, provider, repoconfig.StatusWebhookFailed)

	_, err := svc.RepairWebhook(context.Background(), repo.ID, RepairWebhookRequest{})
	if !errors.Is(err, ErrWebhookPublicURLRequired) {
		t.Fatalf("err = %v, want ErrWebhookPublicURLRequired", err)
	}
	if server.registerCalls != 0 {
		t.Fatalf("register calls = %d, want 0", server.registerCalls)
	}
}

func TestRepairWebhookRegistersBoundWebhookFailedRepo(t *testing.T) {
	server := newBitbucketRepairServer(t)
	client, svc := newRepairTestService(t, "https://ai-efficiency.example.com")
	provider := createRepairProvider(t, client, server.server.URL)
	repo := createRepairRepo(t, client, provider, repoconfig.StatusWebhookFailed)

	result, err := svc.RepairWebhook(context.Background(), repo.ID, RepairWebhookRequest{})
	if err != nil {
		t.Fatalf("RepairWebhook: %v", err)
	}
	if result.WebhookStatus != WebhookRepairRegistered {
		t.Fatalf("WebhookStatus = %q, want %q", result.WebhookStatus, WebhookRepairRegistered)
	}

	loaded := client.RepoConfig.GetX(context.Background(), repo.ID)
	if loaded.Status != repoconfig.StatusActive {
		t.Fatalf("status = %q, want active", loaded.Status)
	}
	if loaded.WebhookID == nil || *loaded.WebhookID != "99" {
		t.Fatalf("webhook id = %v, want 99", loaded.WebhookID)
	}
	if loaded.WebhookSecret == nil || *loaded.WebhookSecret == "" {
		t.Fatal("expected webhook secret")
	}
}

func TestRepairWebhookForceDeletesExistingWebhookBeforeReplacement(t *testing.T) {
	server := newBitbucketRepairServer(t)
	client, svc := newRepairTestService(t, "https://ai-efficiency.example.com")
	provider := createRepairProvider(t, client, server.server.URL)
	repo := createRepairRepo(t, client, provider, repoconfig.StatusWebhookFailed)
	client.RepoConfig.UpdateOneID(repo.ID).SetWebhookID("old-hook").SetWebhookSecret("old-secret").SaveX(context.Background())

	_, err := svc.RepairWebhook(context.Background(), repo.ID, RepairWebhookRequest{Force: true})
	if err != nil {
		t.Fatalf("RepairWebhook: %v", err)
	}
	if !server.deleteCalled {
		t.Fatal("expected delete call")
	}
}

func TestRepairWebhookRegistrationFailureKeepsWebhookFailed(t *testing.T) {
	server := newBitbucketRepairServer(t)
	server.failRegister = true
	client, svc := newRepairTestService(t, "https://ai-efficiency.example.com")
	provider := createRepairProvider(t, client, server.server.URL)
	repo := createRepairRepo(t, client, provider, repoconfig.StatusWebhookFailed)

	result, err := svc.RepairWebhook(context.Background(), repo.ID, RepairWebhookRequest{})
	if err != nil {
		t.Fatalf("RepairWebhook should return item failure without endpoint error: %v", err)
	}
	if result.WebhookStatus != WebhookRepairFailed {
		t.Fatalf("WebhookStatus = %q, want failed", result.WebhookStatus)
	}
	if !strings.Contains(result.Error, "requires REPO_ADMIN") {
		t.Fatalf("error = %q, want upstream permission text", result.Error)
	}
	loaded := client.RepoConfig.GetX(context.Background(), repo.ID)
	if loaded.Status != repoconfig.StatusWebhookFailed {
		t.Fatalf("status = %q, want webhook_failed", loaded.Status)
	}
}

func TestRepairFailedWebhooksScansOnlyBoundWebhookFailedRepos(t *testing.T) {
	server := newBitbucketRepairServer(t)
	client, svc := newRepairTestService(t, "https://ai-efficiency.example.com")
	provider := createRepairProvider(t, client, server.server.URL)
	target := createRepairRepo(t, client, provider, repoconfig.StatusWebhookFailed)
	createRepairRepoNamed(t, client, provider, "PROJ/active", "active", repoconfig.StatusActive)
	createRepairRepoNamed(t, client, nil, "PROJ/unbound", "unbound", repoconfig.StatusWebhookFailed)

	result, err := svc.RepairFailedWebhooks(context.Background(), RepairWebhookRequest{})
	if err != nil {
		t.Fatalf("RepairFailedWebhooks: %v", err)
	}
	if result.Summary.Scanned != 1 || result.Summary.Repaired != 1 {
		t.Fatalf("summary = %+v, want scanned=1 repaired=1", result.Summary)
	}
	if len(result.Items) != 1 || result.Items[0].RepoConfigID != target.ID {
		t.Fatalf("items = %+v, want only repo %d", result.Items, target.ID)
	}
}
