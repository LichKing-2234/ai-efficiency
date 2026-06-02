package repo

import (
	"context"
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
