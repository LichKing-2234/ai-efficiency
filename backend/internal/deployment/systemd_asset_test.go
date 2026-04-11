package deployment

import "testing"

func TestSelectSystemdReleaseAssets(t *testing.T) {
	assets := []ReleaseAsset{
		{Name: "ai-efficiency-backend_1.2.3_linux_amd64.tar.gz", DownloadURL: "https://example.com/linux-amd64.tgz"},
		{Name: "checksums.txt", DownloadURL: "https://example.com/checksums.txt"},
	}

	archive, checksums, err := SelectSystemdReleaseAssets(assets, "linux", "amd64")
	if err != nil {
		t.Fatalf("SelectSystemdReleaseAssets: %v", err)
	}
	if archive.Name != "ai-efficiency-backend_1.2.3_linux_amd64.tar.gz" {
		t.Fatalf("archive = %+v", archive)
	}
	if checksums.Name != "checksums.txt" {
		t.Fatalf("checksums = %+v", checksums)
	}
}

func TestSelectSystemdReleaseAssetsErrorsWithoutMatch(t *testing.T) {
	assets := []ReleaseAsset{
		{Name: "ai-efficiency-backend_1.2.3_linux_arm64.tar.gz", DownloadURL: "https://example.com/linux-arm64.tgz"},
	}

	if _, _, err := SelectSystemdReleaseAssets(assets, "linux", "amd64"); err == nil {
		t.Fatal("expected no-match error")
	}
}
