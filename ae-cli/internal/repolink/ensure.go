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
	status, _, err := EnsureWithResponse(ctx, c, remoteURL, branch)
	return status, err
}

func EnsureWithResponse(ctx context.Context, c Ensurer, remoteURL, branch string) (string, *client.RepoEnsureResponse, error) {
	if c == nil {
		return "skipped", nil, nil
	}
	remoteURL = strings.TrimSpace(remoteURL)
	branch = strings.TrimSpace(branch)
	if remoteURL == "" {
		return "skipped", nil, nil
	}

	resp, err := c.EnsureRepoFromRemote(ctx, remoteURL, branch)
	if err != nil {
		return "failed", nil, err
	}
	return "linked", resp, nil
}
