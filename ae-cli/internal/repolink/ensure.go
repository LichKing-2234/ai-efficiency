package repolink

import (
	"context"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type Ensurer interface {
	EnsureRepoFromRemote(ctx context.Context, remoteURL, branch string) (*client.RepoEnsureResponse, error)
}

func Ensure(ctx context.Context, c Ensurer, remoteURL, branch string) (string, error) {
	if c == nil {
		return "skipped", nil
	}
	remoteURL = strings.TrimSpace(remoteURL)
	branch = strings.TrimSpace(branch)
	if remoteURL == "" {
		return "skipped", nil
	}

	_, err := c.EnsureRepoFromRemote(ctx, remoteURL, branch)
	if err != nil {
		return "failed", err
	}
	return "linked", nil
}
