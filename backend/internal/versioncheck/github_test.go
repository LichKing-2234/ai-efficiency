package versioncheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubReleaseSourceReturnsLatestBackendRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/ai-efficiency/releases/latest" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v0.5.0",
			"html_url": "https://example.com/releases/v0.5.0",
			"assets": [
				{"name": "ai-efficiency-backend_v0.5.0_linux_amd64.tar.gz"},
				{"name": "ae-cli_v0.5.0_darwin_arm64.tar.gz"}
			]
		}`))
	}))
	defer server.Close()

	source := NewGitHubReleaseSource(server.Client(), server.URL+"/repos/acme/ai-efficiency/releases/latest")

	info, err := source.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest returned error: %v", err)
	}
	if info.Version != "v0.5.0" {
		t.Fatalf("version = %q, want v0.5.0", info.Version)
	}
}

func TestGitHubReleaseSourceSkipsCLIOnlyLatestRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/ai-efficiency/releases/latest":
			_, _ = w.Write([]byte(`{
				"tag_name": "v0.2.0-cli.1",
				"html_url": "https://example.com/releases/v0.2.0-cli.1",
				"assets": [
					{"name": "ae-cli_v0.2.0-cli.1_linux_amd64.tar.gz"}
				]
			}`))
		case "/repos/acme/ai-efficiency/releases":
			if r.URL.Query().Get("per_page") != "100" {
				t.Fatalf("per_page = %q, want 100", r.URL.Query().Get("per_page"))
			}
			_, _ = w.Write([]byte(`[
				{
					"tag_name": "v0.2.0-cli.1",
					"html_url": "https://example.com/releases/v0.2.0-cli.1",
					"assets": [
						{"name": "ae-cli_v0.2.0-cli.1_linux_amd64.tar.gz"}
					]
				},
				{
					"tag_name": "v0.1.0-preview.58",
					"html_url": "https://example.com/releases/v0.1.0-preview.58",
					"assets": [
						{"name": "ai-efficiency-backend_v0.1.0-preview.58_linux_amd64.tar.gz"}
					]
				}
			]`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	source := NewGitHubReleaseSource(server.Client(), server.URL+"/repos/acme/ai-efficiency/releases/latest")

	info, err := source.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest returned error: %v", err)
	}
	if info.Version != "v0.1.0-preview.58" {
		t.Fatalf("version = %q, want v0.1.0-preview.58", info.Version)
	}
}

func TestGitHubReleaseSourceReturnsErrorWhenNoBackendReleaseExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v0.2.0-cli.1",
			"html_url": "https://example.com/releases/v0.2.0-cli.1",
			"assets": [
				{"name": "ae-cli_v0.2.0-cli.1_linux_amd64.tar.gz"}
			]
		}`))
	}))
	defer server.Close()

	source := NewGitHubReleaseSource(server.Client(), server.URL+"/repos/acme/ai-efficiency/releases/latest")

	_, err := source.Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no backend release found") {
		t.Fatalf("Latest error = %v, want no backend release found", err)
	}
}
