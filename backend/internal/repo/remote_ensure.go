package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/ent"
)

// EnsureFromRemote returns an existing repo matched by the remote identity, or
// creates an unbound repo row from the local git remote metadata.
func (s *Service) EnsureFromRemote(ctx context.Context, remoteURL, branch string) (*ent.RepoConfig, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("ensure repo: service is not initialized")
	}

	remoteURL = strings.TrimSpace(remoteURL)
	branch = strings.TrimSpace(branch)
	if remoteURL == "" {
		return nil, fmt.Errorf("ensure repo: remote URL is empty")
	}

	return s.FindOrCreateFromRemote(ctx, remoteURL, branch)
}
