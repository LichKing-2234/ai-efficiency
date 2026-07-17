package versioncheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/httpclient"
)

const backendAssetPrefix = "ai-efficiency-backend_"

type GitHubReleaseSource struct {
	client *http.Client
	url    string
}

type githubAssetPayload struct {
	Name string `json:"name"`
}

type githubReleasePayload struct {
	TagName string               `json:"tag_name"`
	HTMLURL string               `json:"html_url"`
	Assets  []githubAssetPayload `json:"assets"`
}

func NewGitHubReleaseSource(client *http.Client, url string) *GitHubReleaseSource {
	if client == nil {
		client = httpclient.NewDefault(10 * time.Second)
	}
	return &GitHubReleaseSource{
		client: client,
		url:    strings.TrimSpace(url),
	}
}

func (s *GitHubReleaseSource) Latest(ctx context.Context) (ReleaseInfo, error) {
	if s.url == "" {
		return ReleaseInfo{}, fmt.Errorf("release api url is not configured")
	}

	releases, err := s.fetchReleases(ctx, s.url)
	if err != nil {
		return ReleaseInfo{}, err
	}
	if release, ok := latestBackendRelease(releases); ok {
		return release, nil
	}

	if listURL := deriveReleaseListURL(s.url); listURL != "" && listURL != s.url {
		releases, err = s.fetchReleases(ctx, listURL)
		if err != nil {
			return ReleaseInfo{}, err
		}
		if release, ok := latestBackendRelease(releases); ok {
			return release, nil
		}
	}

	return ReleaseInfo{}, fmt.Errorf("no backend release found")
}

func (s *GitHubReleaseSource) fetchReleases(ctx context.Context, endpoint string) ([]githubReleasePayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build release request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("release request failed: status=%d body=%q", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("read release response: %w", err)
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, fmt.Errorf("decode release response: empty body")
	}

	if body[0] == '[' {
		var payload []githubReleasePayload
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("decode release response: %w", err)
		}
		return payload, nil
	}

	var payload githubReleasePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode release response: %w", err)
	}
	return []githubReleasePayload{payload}, nil
}

func latestBackendRelease(releases []githubReleasePayload) (ReleaseInfo, bool) {
	for _, release := range releases {
		if !release.hasBackendAsset() {
			continue
		}
		return ReleaseInfo{
			Version: release.TagName,
			URL:     release.HTMLURL,
		}, true
	}
	return ReleaseInfo{}, false
}

func (r githubReleasePayload) hasBackendAsset() bool {
	for _, asset := range r.Assets {
		if strings.HasPrefix(asset.Name, backendAssetPrefix) {
			return true
		}
	}
	return false
}

func deriveReleaseListURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !strings.HasSuffix(parsed.Path, "/releases/latest") {
		return ""
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/latest")
	query := parsed.Query()
	if query.Get("per_page") == "" {
		query.Set("per_page", "100")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
