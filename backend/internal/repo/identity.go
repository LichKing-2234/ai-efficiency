package repo

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

type RepoIdentity struct {
	RepoKey  string
	Name     string
	FullName string
	CloneURL string
}

func DeriveRepoIdentity(remoteURL string) (RepoIdentity, error) {
	normalized, err := normalizeRemoteURL(remoteURL)
	if err != nil {
		return RepoIdentity{}, err
	}

	segments, fullName, err := extractRepoPathParts(normalized.Path)
	if err != nil {
		return RepoIdentity{}, err
	}
	repoName := segments[len(segments)-1]

	return RepoIdentity{
		RepoKey:  normalized.Host + "/" + strings.Join(segments, "/"),
		Name:     repoName,
		FullName: fullName,
		CloneURL: strings.TrimSpace(remoteURL),
	}, nil
}

func normalizeRemoteURL(remoteURL string) (*url.URL, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL == "" {
		return nil, fmt.Errorf("derive repo identity: remote URL is empty")
	}

	if strings.HasPrefix(remoteURL, "git@") && strings.Contains(remoteURL, ":") {
		parts := strings.SplitN(strings.TrimPrefix(remoteURL, "git@"), ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("derive repo identity: unsupported ssh remote %q", remoteURL)
		}
		remoteURL = "ssh://" + parts[0] + "/" + parts[1]
	}

	u, err := url.Parse(remoteURL)
	if err != nil {
		return nil, fmt.Errorf("derive repo identity: parse remote URL: %w", err)
	}
	if strings.TrimSpace(u.Host) == "" {
		return nil, fmt.Errorf("derive repo identity: remote host is empty")
	}

	u.Host = strings.ToLower(strings.TrimSpace(u.Host))
	u.Path = strings.TrimSuffix(path.Clean(strings.TrimSuffix(strings.TrimSpace(u.Path), ".git")), "/")
	return u, nil
}

func extractRepoPathParts(cleanPath string) ([]string, string, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(cleanPath), "/")
	if trimmed == "" {
		return nil, "", fmt.Errorf("derive repo identity: repo path is empty")
	}

	parts := strings.Split(trimmed, "/")
	switch {
	case len(parts) >= 3 && parts[0] == "scm":
		project := strings.ToLower(parts[1])
		repo := strings.ToLower(parts[2])
		return []string{project, repo}, strings.ToUpper(parts[1]) + "/" + repo, nil
	case len(parts) >= 4 && parts[0] == "projects" && parts[2] == "repos":
		project := strings.ToLower(parts[1])
		repo := strings.ToLower(parts[3])
		return []string{project, repo}, strings.ToUpper(parts[1]) + "/" + repo, nil
	case len(parts) >= 2:
		owner := strings.ToLower(parts[len(parts)-2])
		repo := strings.ToLower(parts[len(parts)-1])
		return []string{owner, repo}, owner + "/" + repo, nil
	default:
		return nil, "", fmt.Errorf("derive repo identity: unsupported remote path %q", cleanPath)
	}
}
