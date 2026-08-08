package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveAttributionRepoFromRemoteUsesReporterEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/attribution/repos/resolve-remote" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer reporter-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"eligible":true,"repo_config_id":42,"repo_key":"repo:example"}}`))
	}))
	defer server.Close()

	result, err := New(server.URL, "reporter-token").ResolveAttributionRepoFromRemote(context.Background(), ResolveRepoRequest{RemoteURL: "https://git.example.com/org/repo.git"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Eligible || result.RepoConfigID != 42 {
		t.Fatalf("result = %+v", result)
	}
}
