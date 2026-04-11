package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type ReleaseInfo struct {
	Version string         `json:"version"`
	URL     string         `json:"url"`
	Assets  []ReleaseAsset `json:"-"`
}

type ReleaseSource interface {
	Latest(context.Context) (ReleaseInfo, error)
}

type GitHubReleaseSource struct {
	client *http.Client
	url    string
}

func NewGitHubReleaseSource(client *http.Client, url string) *GitHubReleaseSource {
	if client == nil {
		client = http.DefaultClient
	}
	return &GitHubReleaseSource{
		client: client,
		url:    url,
	}
}

func (s *GitHubReleaseSource) Latest(ctx context.Context) (ReleaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return ReleaseInfo{}, fmt.Errorf("build request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return ReleaseInfo{}, fmt.Errorf("request latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return ReleaseInfo{}, fmt.Errorf("latest release request failed: status=%d body=%q", resp.StatusCode, string(body))
	}

	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ReleaseInfo{}, fmt.Errorf("decode latest release response: %w", err)
	}

	assets := make([]ReleaseAsset, 0, len(payload.Assets))
	for _, asset := range payload.Assets {
		assets = append(assets, ReleaseAsset{
			Name:        asset.Name,
			DownloadURL: asset.BrowserDownloadURL,
			Size:        asset.Size,
		})
	}

	return ReleaseInfo{
		Version: payload.TagName,
		URL:     payload.HTMLURL,
		Assets:  assets,
	}, nil
}
