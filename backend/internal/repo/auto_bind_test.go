package repo

import (
	"context"
	"fmt"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

func newAutoBindTestService(t *testing.T) (*ent.Client, *Service) {
	t.Helper()
	client := testdb.Open(t)
	svc := NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", zap.NewNop())
	svc.autoBindPostBind = func(ctx context.Context, repoID, providerID int) (string, error) {
		return AutoBindWebhookRegistered, nil
	}
	return client, svc
}

func createAutoBindProvider(t *testing.T, client *ent.Client, name string, providerType scmprovider.Type, baseURL string, status scmprovider.Status) *ent.ScmProvider {
	t.Helper()
	provider, err := client.ScmProvider.Create().
		SetName(name).
		SetType(providerType).
		SetBaseURL(baseURL).
		SetStatus(status).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider %s: %v", name, err)
	}
	return provider
}

func createAutoBindRepo(t *testing.T, client *ent.Client, repoKey, fullName, cloneURL string, status repoconfig.Status) *ent.RepoConfig {
	t.Helper()
	repo, err := client.RepoConfig.Create().
		SetRepoKey(repoKey).
		SetName("platform").
		SetFullName(fullName).
		SetCloneURL(cloneURL).
		SetDefaultBranch("main").
		SetStatus(status).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create repo %s: %v", repoKey, err)
	}
	return repo
}

func TestCanonicalProviderHostMapsGitHubAPIToCloneHost(t *testing.T) {
	host, ok := canonicalProviderHost(&ent.ScmProvider{
		Type:    scmprovider.TypeGithub,
		BaseURL: "https://api.github.com",
	})
	if !ok {
		t.Fatal("canonicalProviderHost returned not ok")
	}
	if host != "github.com" {
		t.Fatalf("host = %q, want github.com", host)
	}
}

func TestCanonicalRepoHostHandlesGitHubSSHRemote(t *testing.T) {
	host, ok := canonicalRepoHost(&ent.RepoConfig{
		RepoKey:  "github.com/acme/platform",
		CloneURL: "git@github.com:acme/platform.git",
	})
	if !ok {
		t.Fatal("canonicalRepoHost returned not ok")
	}
	if host != "github.com" {
		t.Fatalf("host = %q, want github.com", host)
	}
}

func TestFindAutoBindProviderMatchesSingleGitHubSaaSProvider(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	want := createAutoBindProvider(t, client, "GitHub", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	provider, reason, err := svc.findAutoBindProvider(ctx, repo)
	if err != nil {
		t.Fatalf("findAutoBindProvider: %v", err)
	}
	if reason != AutoBindMatched {
		t.Fatalf("reason = %q, want %q", reason, AutoBindMatched)
	}
	if provider == nil || provider.ID != want.ID {
		t.Fatalf("provider = %#v, want id %d", provider, want.ID)
	}
}

func TestFindAutoBindProviderMatchesGitHubSSHRemote(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	want := createAutoBindProvider(t, client, "GitHub", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "git@github.com:acme/platform.git", repoconfig.StatusActive)

	provider, reason, err := svc.findAutoBindProvider(ctx, repo)
	if err != nil {
		t.Fatalf("findAutoBindProvider: %v", err)
	}
	if reason != AutoBindMatched {
		t.Fatalf("reason = %q, want %q", reason, AutoBindMatched)
	}
	if provider == nil || provider.ID != want.ID {
		t.Fatalf("provider = %#v, want id %d", provider, want.ID)
	}
}

func TestFindAutoBindProviderMatchesBitbucketSameHost(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	want := createAutoBindProvider(t, client, "Bitbucket", scmprovider.TypeBitbucketServer, "https://bitbucket.example.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "bitbucket.example.com/acme/platform", "ACME/platform", "ssh://git@bitbucket.example.com/acme/platform.git", repoconfig.StatusActive)

	provider, reason, err := svc.findAutoBindProvider(ctx, repo)
	if err != nil {
		t.Fatalf("findAutoBindProvider: %v", err)
	}
	if reason != AutoBindMatched {
		t.Fatalf("reason = %q, want %q", reason, AutoBindMatched)
	}
	if provider == nil || provider.ID != want.ID {
		t.Fatalf("provider = %#v, want id %d", provider, want.ID)
	}
}

func TestFindAutoBindProviderMatchesBitbucketSSHHost(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	want, err := client.ScmProvider.Create().
		SetName("Bitbucket").
		SetType(scmprovider.TypeBitbucketServer).
		SetBaseURL("https://bitbucket-api.example.com").
		SetSSHHost("git.example.com").
		SetStatus(scmprovider.StatusActive).
		Save(ctx)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	repo := createAutoBindRepo(t, client, "git.example.com/acme/platform", "ACME/platform", "ssh://git@git.example.com/acme/platform.git", repoconfig.StatusActive)

	provider, reason, err := svc.findAutoBindProvider(ctx, repo)
	if err != nil {
		t.Fatalf("findAutoBindProvider: %v", err)
	}
	if reason != AutoBindMatched {
		t.Fatalf("reason = %q, want %q", reason, AutoBindMatched)
	}
	if provider == nil || provider.ID != want.ID {
		t.Fatalf("provider = %#v, want id %d", provider, want.ID)
	}
}

func TestFindAutoBindProviderSkipsNoMatch(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	createAutoBindProvider(t, client, "Bitbucket", scmprovider.TypeBitbucketServer, "https://bitbucket.example.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	provider, reason, err := svc.findAutoBindProvider(ctx, repo)
	if err != nil {
		t.Fatalf("findAutoBindProvider: %v", err)
	}
	if provider != nil {
		t.Fatalf("provider = %#v, want nil", provider)
	}
	if reason != AutoBindNoMatch {
		t.Fatalf("reason = %q, want %q", reason, AutoBindNoMatch)
	}
}

func TestFindAutoBindProviderSkipsAmbiguousSameHost(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	createAutoBindProvider(t, client, "GitHub A", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	createAutoBindProvider(t, client, "GitHub B", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	provider, reason, err := svc.findAutoBindProvider(ctx, repo)
	if err != nil {
		t.Fatalf("findAutoBindProvider: %v", err)
	}
	if provider != nil {
		t.Fatalf("provider = %#v, want nil", provider)
	}
	if reason != AutoBindAmbiguous {
		t.Fatalf("reason = %q, want %q", reason, AutoBindAmbiguous)
	}
}

func TestFindAutoBindProviderIgnoresInactiveProviders(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	createAutoBindProvider(t, client, "GitHub", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusInactive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	provider, reason, err := svc.findAutoBindProvider(ctx, repo)
	if err != nil {
		t.Fatalf("findAutoBindProvider: %v", err)
	}
	if provider != nil {
		t.Fatalf("provider = %#v, want nil", provider)
	}
	if reason != AutoBindNoMatch {
		t.Fatalf("reason = %q, want %q", reason, AutoBindNoMatch)
	}
}

func TestAutoBindRepoBindsSingleMatchedProvider(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	provider := createAutoBindProvider(t, client, "GitHub", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	result, err := svc.AutoBindRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("AutoBindRepo: %v", err)
	}
	if result.Result != AutoBindMatched {
		t.Fatalf("result = %q, want %q", result.Result, AutoBindMatched)
	}
	if result.SCMProviderID != provider.ID {
		t.Fatalf("provider id = %d, want %d", result.SCMProviderID, provider.ID)
	}

	loaded := client.RepoConfig.Query().Where(repoconfig.IDEQ(repo.ID)).WithScmProvider().OnlyX(ctx)
	if loaded.Edges.ScmProvider == nil || loaded.Edges.ScmProvider.ID != provider.ID {
		t.Fatalf("loaded provider = %#v, want %d", loaded.Edges.ScmProvider, provider.ID)
	}
}

func TestAutoBindRepoKeepsAmbiguousRepoUnbound(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	createAutoBindProvider(t, client, "GitHub A", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	createAutoBindProvider(t, client, "GitHub B", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	result, err := svc.AutoBindRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("AutoBindRepo: %v", err)
	}
	if result.Result != AutoBindAmbiguous {
		t.Fatalf("result = %q, want %q", result.Result, AutoBindAmbiguous)
	}

	loaded := client.RepoConfig.Query().Where(repoconfig.IDEQ(repo.ID)).WithScmProvider().OnlyX(ctx)
	if loaded.Edges.ScmProvider != nil {
		t.Fatalf("provider = %#v, want nil", loaded.Edges.ScmProvider)
	}
}

func TestAutoBindRepoKeepsBindingOnPostBindError(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	svc.autoBindPostBind = func(ctx context.Context, repoID, providerID int) (string, error) {
		return AutoBindWebhookFailed, fmt.Errorf("provider verification failed")
	}
	provider := createAutoBindProvider(t, client, "GitHub", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	repo := createAutoBindRepo(t, client, "github.com/acme/platform", "acme/platform", "https://github.com/acme/platform.git", repoconfig.StatusActive)

	result, err := svc.AutoBindRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("AutoBindRepo: %v", err)
	}
	if result.Result != AutoBindProviderError {
		t.Fatalf("result = %q, want %q", result.Result, AutoBindProviderError)
	}
	if result.WebhookStatus != AutoBindWebhookFailed {
		t.Fatalf("webhook status = %q, want %q", result.WebhookStatus, AutoBindWebhookFailed)
	}

	loaded := client.RepoConfig.Query().Where(repoconfig.IDEQ(repo.ID)).WithScmProvider().OnlyX(ctx)
	if loaded.Edges.ScmProvider == nil || loaded.Edges.ScmProvider.ID != provider.ID {
		t.Fatalf("provider = %#v, want %d", loaded.Edges.ScmProvider, provider.ID)
	}
}

func TestAutoBindUnboundProcessesOnlyUnboundActiveRepos(t *testing.T) {
	client, svc := newAutoBindTestService(t)
	ctx := context.Background()
	provider := createAutoBindProvider(t, client, "GitHub", scmprovider.TypeGithub, "https://api.github.com", scmprovider.StatusActive)
	activeRepo := createAutoBindRepo(t, client, "github.com/acme/active", "acme/active", "https://github.com/acme/active.git", repoconfig.StatusActive)
	inactiveRepo := createAutoBindRepo(t, client, "github.com/acme/inactive", "acme/inactive", "https://github.com/acme/inactive.git", repoconfig.StatusInactive)
	boundRepo := createAutoBindRepo(t, client, "github.com/acme/bound", "acme/bound", "https://github.com/acme/bound.git", repoconfig.StatusActive)
	client.RepoConfig.UpdateOneID(boundRepo.ID).SetScmProviderID(provider.ID).SaveX(ctx)

	result, err := svc.AutoBindUnbound(ctx)
	if err != nil {
		t.Fatalf("AutoBindUnbound: %v", err)
	}
	if result.Summary.Scanned != 1 || result.Summary.Bound != 1 {
		t.Fatalf("summary = %+v, want scanned=1 bound=1", result.Summary)
	}
	if len(result.Items) != 1 || result.Items[0].RepoConfigID != activeRepo.ID {
		t.Fatalf("items = %+v, want only active repo %d", result.Items, activeRepo.ID)
	}

	if client.RepoConfig.Query().Where(repoconfig.IDEQ(inactiveRepo.ID), repoconfig.HasScmProvider()).CountX(ctx) != 0 {
		t.Fatal("inactive repo should remain unbound")
	}
}
