package repoidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var scpLikeRemotePattern = regexp.MustCompile(`^[^/@:\s]+@[^:/\s]+:.+$`)

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

func FallbackRepoIdentity(remoteURL, fullName string) RepoIdentity {
	remoteURL = strings.TrimSpace(remoteURL)
	fullName = strings.Trim(strings.TrimSpace(fullName), "/")

	if normalized, err := normalizeRemoteURL(remoteURL); err == nil {
		trimmedPath := strings.Trim(strings.TrimPrefix(normalized.Path, "/"), "/")
		if trimmedPath != "" {
			cleanedPath := strings.ToLower(strings.TrimSuffix(trimmedPath, ".git"))
			segments := strings.Split(cleanedPath, "/")
			repoName := segments[len(segments)-1]
			bestFullName := fullName
			if bestFullName == "" {
				bestFullName = strings.Join(segments, "/")
			}
			return RepoIdentity{
				RepoKey:  normalized.Host + "/" + cleanedPath,
				Name:     repoName,
				FullName: bestFullName,
				CloneURL: remoteURL,
			}
		}
	}

	if fullName != "" {
		normalizedFullName := strings.ToLower(fullName)
		parts := strings.Split(normalizedFullName, "/")
		return RepoIdentity{
			RepoKey:  "manual/" + normalizedFullName,
			Name:     parts[len(parts)-1],
			FullName: fullName,
			CloneURL: remoteURL,
		}
	}

	sum := sha256.Sum256([]byte(remoteURL))
	short := hex.EncodeToString(sum[:8])
	return RepoIdentity{
		RepoKey:  "fallback/" + short,
		Name:     short,
		FullName: short,
		CloneURL: remoteURL,
	}
}

func normalizeRemoteURL(remoteURL string) (*url.URL, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL == "" {
		return nil, fmt.Errorf("derive repo identity: remote URL is empty")
	}

	if scpLikeRemotePattern.MatchString(remoteURL) {
		parts := strings.SplitN(remoteURL, "@", 2)
		hostAndPath := parts[1]
		parts = strings.SplitN(hostAndPath, ":", 2)
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
		return []string{owner, repo}, parts[len(parts)-2] + "/" + repo, nil
	case len(parts) == 1:
		repo := strings.ToLower(parts[0])
		return []string{repo}, repo, nil
	default:
		return nil, "", fmt.Errorf("derive repo identity: unsupported remote path %q", cleanPath)
	}
}
