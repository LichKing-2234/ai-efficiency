package repo

import (
	"encoding/json"

	"github.com/ai-efficiency/backend/internal/scm"
	"github.com/ai-efficiency/backend/internal/scm/bitbucket"
	"github.com/ai-efficiency/backend/internal/scm/github"
	"go.uber.org/zap"
)

type credentialsJSON struct {
	Token string `json:"token"`
}

func parseToken(credentials string) string {
	var creds credentialsJSON
	if err := json.Unmarshal([]byte(credentials), &creds); err != nil {
		// If not JSON, treat the whole string as the token
		return credentials
	}
	return creds.Token
}

// newGitHubProvider creates a GitHub SCM provider from credentials.
func newGitHubProvider(baseURL, credentials string, logger *zap.Logger) (scm.SCMProvider, error) {
	return github.New(baseURL, parseToken(credentials), logger)
}

// newBitbucketProvider creates a Bitbucket Server SCM provider from credentials.
func newBitbucketProvider(baseURL, credentials string, logger *zap.Logger) (scm.SCMProvider, error) {
	return bitbucket.New(baseURL, parseToken(credentials), logger)
}
