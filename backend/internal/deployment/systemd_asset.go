package deployment

import (
	"fmt"
	"strings"
)

type ReleaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size,omitempty"`
}

func SelectSystemdReleaseAssets(assets []ReleaseAsset, goos, goarch string) (ReleaseAsset, ReleaseAsset, error) {
	expectedArchiveSuffix := fmt.Sprintf("_%s_%s.tar.gz", goos, goarch)

	var archive ReleaseAsset
	var checksums ReleaseAsset

	for _, asset := range assets {
		switch {
		case asset.Name == "checksums.txt":
			checksums = asset
		case strings.HasPrefix(asset.Name, "ai-efficiency-backend_") && strings.HasSuffix(asset.Name, expectedArchiveSuffix):
			archive = asset
		}
	}

	if archive.Name == "" {
		return ReleaseAsset{}, ReleaseAsset{}, fmt.Errorf("no backend archive found for %s/%s", goos, goarch)
	}
	if checksums.Name == "" {
		return ReleaseAsset{}, ReleaseAsset{}, fmt.Errorf("checksums.txt not found")
	}

	return archive, checksums, nil
}
