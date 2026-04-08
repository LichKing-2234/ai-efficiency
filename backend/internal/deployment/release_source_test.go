package deployment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubReleaseSourceLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.5.0","html_url":"https://example.com/release/v0.5.0","published_at":"2026-04-08T12:00:00Z"}`))
	}))
	defer srv.Close()

	source := NewGitHubReleaseSource(srv.Client(), srv.URL)

	info, err := source.Latest(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if info.Version != "v0.5.0" {
		t.Fatalf("expected version v0.5.0, got %q", info.Version)
	}
}
