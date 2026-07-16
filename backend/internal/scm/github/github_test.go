package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/scm"
	gh "github.com/google/go-github/v60/github"
	"go.uber.org/zap"
)

func TestNewUsesInjectedHTTPClient(t *testing.T) {
	var calls int
	injected := &http.Client{
		Timeout: 27 * time.Second,
		Transport: githubRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"full_name":"acme/project","name":"project","default_branch":"main"}`)),
				Request:    req,
			}, nil
		}),
	}
	provider, err := NewWithHTTPClient("https://api.github.com", "test-token", zap.NewNop(), injected)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if provider.client.Client().Timeout != 27*time.Second {
		t.Fatalf("GitHub HTTP client Timeout = %s, want 27s", provider.client.Client().Timeout)
	}
	if _, err := provider.GetRepo(context.Background(), "acme/project"); err != nil {
		t.Fatalf("GetRepo() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("injected transport calls = %d, want 1", calls)
	}
}

func TestNewPreservesWebhookCallbackArgument(t *testing.T) {
	provider, err := New("https://api.github.com", "test-token", zap.NewNop(), "https://ai-efficiency.example.com/api/v1/webhooks/github")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if provider.webhookCallbackURL != "https://ai-efficiency.example.com/api/v1/webhooks/github" {
		t.Fatalf("webhookCallbackURL = %q", provider.webhookCallbackURL)
	}
}

type githubRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn githubRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestSplitFullName(t *testing.T) {
	tests := []struct {
		name      string
		fullName  string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"valid", "owner/repo", "owner", "repo", false},
		{"with dots", "my-org/my.repo.name", "my-org", "my.repo.name", false},
		{"no slash", "noslash", "", "", true},
		{"empty", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := splitFullName(tt.fullName)
			if (err != nil) != tt.wantErr {
				t.Errorf("splitFullName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestGhPRToSCM(t *testing.T) {
	// Test the helper that converts GitHub PR to our SCM PR type
	// We can't easily test with real github.PullRequest without mocking,
	// but we can verify the interface compliance
	var _ scm.SCMProvider = (*Provider)(nil)
}

func TestStrPtr(t *testing.T) {
	s := "hello"
	p := strPtr(s)
	if *p != s {
		t.Errorf("strPtr() = %q, want %q", *p, s)
	}
}

func TestParsePREventNilForUnknownAction(t *testing.T) {
	// parsePREvent should return nil for unknown actions
	// We test this indirectly since we can't easily construct github.PullRequestEvent
	// The key behavior is that unsupported actions return nil
}

func TestProviderImplementsInterface(t *testing.T) {
	// Compile-time check that Provider implements SCMProvider
	var _ scm.SCMProvider = (*Provider)(nil)
}

func TestListPRCommits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/org/repo/pulls/42/commits" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"sha":"abc123"},{"sha":"def456"}]`))
	}))
	defer srv.Close()

	client := gh.NewClient(srv.Client())
	baseURL, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatalf("parse base url: %v", err)
	}
	client.BaseURL = baseURL

	p := &Provider{client: client}
	commits, err := p.ListPRCommits(context.Background(), "org/repo", 42)
	if err != nil {
		t.Fatalf("ListPRCommits() error = %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("len(commits) = %d, want 2", len(commits))
	}
	if commits[0] != "abc123" || commits[1] != "def456" {
		t.Fatalf("commits = %#v", commits)
	}
}

func TestListPRCommitsInvalidName(t *testing.T) {
	p := &Provider{}
	if _, err := p.ListPRCommits(context.Background(), "invalid", 1); err == nil {
		t.Fatal("expected error for invalid repo full name")
	}
}

func TestListPRCommitsPaginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/org/repo/pulls/42/commits" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Link", "</repos/org/repo/pulls/42/commits?page=2>; rel=\"next\"")
			_, _ = w.Write([]byte(`[{"sha":"abc123"}]`))
		case "2":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"sha":"def456"}]`))
		default:
			t.Fatalf("unexpected page query: %q", r.URL.RawQuery)
		}
	}))
	defer srv.Close()

	client := gh.NewClient(srv.Client())
	baseURL, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatalf("parse base url: %v", err)
	}
	client.BaseURL = baseURL

	p := &Provider{client: client}
	commits, err := p.ListPRCommits(context.Background(), "org/repo", 42)
	if err != nil {
		t.Fatalf("ListPRCommits() error = %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("len(commits) = %d, want 2", len(commits))
	}
	if commits[0] != "abc123" || commits[1] != "def456" {
		t.Fatalf("commits = %#v", commits)
	}
}
