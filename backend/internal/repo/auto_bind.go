package repo

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"go.uber.org/zap"
)

const (
	AutoBindMatched         = "matched"
	AutoBindAlreadyBound    = "already_bound"
	AutoBindNoMatch         = "no_match"
	AutoBindAmbiguous       = "ambiguous"
	AutoBindInvalidRepoHost = "invalid_repo_host"
	AutoBindProviderError   = "provider_error"

	AutoBindWebhookSkipped    = "skipped"
	AutoBindWebhookRegistered = "registered"
	AutoBindWebhookFailed     = "failed"
)

type AutoBindSummary struct {
	Scanned          int `json:"scanned"`
	Bound            int `json:"bound"`
	AlreadyBound     int `json:"already_bound"`
	SkippedNoMatch   int `json:"skipped_no_match"`
	SkippedAmbiguous int `json:"skipped_ambiguous"`
	WebhookFailed    int `json:"webhook_failed"`
	Errors           int `json:"errors"`
}

type AutoBindBatchResult struct {
	Summary AutoBindSummary  `json:"summary"`
	Items   []AutoBindResult `json:"items"`
}

type AutoBindResult struct {
	RepoConfigID    int    `json:"repo_config_id"`
	RepoKey         string `json:"repo_key,omitempty"`
	FullName        string `json:"full_name,omitempty"`
	Result          string `json:"result"`
	SCMProviderID   int    `json:"scm_provider_id,omitempty"`
	SCMProviderName string `json:"scm_provider_name,omitempty"`
	WebhookStatus   string `json:"webhook_status,omitempty"`
	Error           string `json:"error,omitempty"`
}

type autoBindPostBindFunc func(ctx context.Context, repoID, providerID int) (string, error)

func canonicalRepoHost(repo *ent.RepoConfig) (string, bool) {
	if repo == nil {
		return "", false
	}
	if identity, err := DeriveRepoIdentity(repo.CloneURL); err == nil {
		return hostFromRepoKey(identity.RepoKey)
	}
	return hostFromRepoKey(repo.RepoKey)
}

func canonicalProviderHost(provider *ent.ScmProvider) (string, bool) {
	if provider == nil {
		return "", false
	}
	host, ok := hostFromURL(provider.BaseURL)
	if !ok {
		return "", false
	}
	if provider.Type == scmprovider.TypeGithub && host == "api.github.com" {
		host = "github.com"
	}
	return host, true
}

func hostFromRepoKey(repoKey string) (string, bool) {
	repoKey = strings.Trim(strings.TrimSpace(repoKey), "/")
	if repoKey == "" {
		return "", false
	}
	parts := strings.Split(repoKey, "/")
	if parts[0] == "" {
		return "", false
	}
	return normalizeHost(parts[0]), true
}

func hostFromURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if strings.Contains(raw, "@") && strings.Contains(raw, ":") && !strings.Contains(raw, "://") {
		userSplit := strings.SplitN(raw, "@", 2)
		hostPath := userSplit[1]
		hostSplit := strings.SplitN(hostPath, ":", 2)
		if hostSplit[0] == "" {
			return "", false
		}
		return normalizeHost(hostSplit[0]), true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	return normalizeHost(parsed.Host), true
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if withoutPort, port, err := net.SplitHostPort(host); err == nil && (port == "80" || port == "443") {
		host = withoutPort
	}
	host = strings.TrimSuffix(host, ".")
	return host
}

func (s *Service) findAutoBindProvider(ctx context.Context, repo *ent.RepoConfig) (*ent.ScmProvider, string, error) {
	repoHost, ok := canonicalRepoHost(repo)
	if !ok {
		return nil, AutoBindInvalidRepoHost, nil
	}
	providers, err := s.entClient.ScmProvider.Query().
		Where(scmprovider.StatusEQ(scmprovider.StatusActive)).
		All(ctx)
	if err != nil {
		return nil, AutoBindProviderError, fmt.Errorf("list active scm providers: %w", err)
	}

	matches := make([]*ent.ScmProvider, 0, 1)
	for _, provider := range providers {
		providerHost, ok := canonicalProviderHost(provider)
		if !ok {
			continue
		}
		if providerHost == repoHost {
			matches = append(matches, provider)
		}
	}

	switch len(matches) {
	case 0:
		return nil, AutoBindNoMatch, nil
	case 1:
		return matches[0], AutoBindMatched, nil
	default:
		return nil, AutoBindAmbiguous, nil
	}
}

func baseAutoBindResult(repo *ent.RepoConfig, result string) AutoBindResult {
	item := AutoBindResult{Result: result, WebhookStatus: AutoBindWebhookSkipped}
	if repo != nil {
		item.RepoConfigID = repo.ID
		item.RepoKey = repo.RepoKey
		item.FullName = repo.FullName
	}
	return item
}

func (r AutoBindResult) addToSummary(summary *AutoBindSummary) {
	summary.Scanned++
	switch r.Result {
	case AutoBindMatched:
		summary.Bound++
	case AutoBindAlreadyBound:
		summary.AlreadyBound++
	case AutoBindNoMatch, AutoBindInvalidRepoHost:
		summary.SkippedNoMatch++
	case AutoBindAmbiguous:
		summary.SkippedAmbiguous++
	case AutoBindProviderError:
		summary.Bound++
		summary.Errors++
	}
	if r.WebhookStatus == AutoBindWebhookFailed {
		summary.WebhookFailed++
	}
}

func (s *Service) logAutoBindResult(result AutoBindResult) {
	if s == nil || s.logger == nil {
		return
	}
	fields := []zap.Field{
		zap.Int("repo_config_id", result.RepoConfigID),
		zap.String("repo_key", result.RepoKey),
		zap.String("full_name", result.FullName),
		zap.String("result", result.Result),
		zap.Int("scm_provider_id", result.SCMProviderID),
		zap.String("webhook_status", result.WebhookStatus),
	}
	if result.Error != "" {
		fields = append(fields, zap.String("error", result.Error))
	}
	s.logger.Info("repo auto-bind result", fields...)
}
