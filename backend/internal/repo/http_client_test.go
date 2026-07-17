package repo

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/scmprovider"
	"go.uber.org/zap"
)

func TestServiceUsesInjectedHTTPClientForSCMProviders(t *testing.T) {
	var calls int
	injected := &http.Client{
		Timeout: 29 * time.Second,
		Transport: repoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			body := `{"slug":"project","name":"Project","project":{"key":"PROJ"},"links":{"clone":[]}}`
			if strings.HasPrefix(req.URL.Path, "/repos/") {
				body = `{"full_name":"acme/project","name":"project","clone_url":"https://github.example/acme/project.git","default_branch":"main"}`
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}
	service := NewService(nil, "test-key", zap.NewNop(), ServiceOptions{HTTPClient: injected})
	if service.httpClient != injected || service.httpClient.Timeout != 29*time.Second {
		t.Fatal("NewService() did not retain the injected HTTP client")
	}

	githubProvider, err := service.newSCMProviderWithCallback(string(scmprovider.TypeGithub), "https://api.github.com", "test-token", "")
	if err != nil {
		t.Fatalf("new GitHub provider: %v", err)
	}
	if _, err := githubProvider.GetRepo(context.Background(), "acme/project"); err != nil {
		t.Fatalf("GitHub GetRepo() error = %v", err)
	}

	bitbucketProvider, err := service.newSCMProviderWithCallback(string(scmprovider.TypeBitbucketServer), "https://bitbucket.example.com", "test-token", "")
	if err != nil {
		t.Fatalf("new Bitbucket provider: %v", err)
	}
	if _, err := bitbucketProvider.GetRepo(context.Background(), "PROJ/project"); err != nil {
		t.Fatalf("Bitbucket GetRepo() error = %v", err)
	}

	if calls != 2 {
		t.Fatalf("injected transport calls = %d, want 2", calls)
	}
}

type repoRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn repoRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
