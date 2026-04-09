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
		_, _ = w.Write([]byte(`{
			"tag_name":"v0.5.0",
			"html_url":"https://example.com/release/v0.5.0",
			"published_at":"2026-04-08T12:00:00Z",
			"assets":[
				{"name":"ai-efficiency-backend_0.5.0_linux_amd64.tar.gz","browser_download_url":"https://example.com/linux-amd64.tgz","size":123},
				{"name":"checksums.txt","browser_download_url":"https://example.com/checksums.txt","size":456}
			]
		}`))
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
	if len(info.Assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(info.Assets))
	}
	if info.Assets[0].Name != "ai-efficiency-backend_0.5.0_linux_amd64.tar.gz" {
		t.Fatalf("unexpected first asset: %+v", info.Assets[0])
	}
	if info.Assets[1].DownloadURL != "https://example.com/checksums.txt" {
		t.Fatalf("unexpected checksums asset: %+v", info.Assets[1])
	}
}
