package repo

import (
	"fmt"

	"github.com/ai-efficiency/backend/internal/credential"
	"github.com/ai-efficiency/backend/internal/scm"
	"github.com/ai-efficiency/backend/internal/scm/bitbucket"
	"github.com/ai-efficiency/backend/internal/scm/github"
	"go.uber.org/zap"
)

func parseToken(raw string) string {
	payload, err := credential.ParseLegacySCMProviderSecret(raw)
	if err != nil {
		return raw
	}
	return payload.Text
}

func normalizeAPIPayload(input any) (credential.Payload, error) {
	switch v := input.(type) {
	case credential.Payload:
		return v, nil
	case string:
		payload, err := credential.ParseLegacySCMProviderSecret(v)
		if err != nil {
			return nil, err
		}
		return payload, nil
	default:
		return nil, fmt.Errorf("unsupported api credential type %T", input)
	}
}

// newGitHubProvider creates a GitHub SCM provider from an API credential payload.
func newGitHubProvider(baseURL string, apiCredential any, logger *zap.Logger) (scm.SCMProvider, error) {
	apiPayload, err := normalizeAPIPayload(apiCredential)
	if err != nil {
		return nil, err
	}
	secret, err := credential.ResolveAPISecret(apiPayload)
	if err != nil {
		return nil, err
	}
	return github.New(baseURL, secret, logger)
}

// newBitbucketProvider creates a Bitbucket Server SCM provider from an API credential payload.
func newBitbucketProvider(baseURL string, apiCredential any, logger *zap.Logger) (scm.SCMProvider, error) {
	apiPayload, err := normalizeAPIPayload(apiCredential)
	if err != nil {
		return nil, err
	}
	secret, err := credential.ResolveAPISecret(apiPayload)
	if err != nil {
		return nil, err
	}
	return bitbucket.New(baseURL, secret, logger)
}
