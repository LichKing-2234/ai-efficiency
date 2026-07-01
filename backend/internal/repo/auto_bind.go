package repo

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
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
	hosts := canonicalProviderHosts(provider)
	if len(hosts) == 0 {
		return "", false
	}
	return hosts[0], true
}

func canonicalProviderHosts(provider *ent.ScmProvider) []string {
	if provider == nil {
		return nil
	}
	hosts := make([]string, 0, 2)
	host, ok := hostFromURL(provider.BaseURL)
	if ok {
		if provider.Type == scmprovider.TypeGithub && host == "api.github.com" {
			host = "github.com"
		}
		hosts = appendUniqueHost(hosts, host)
	}
	if provider.SSHHost != nil {
		if sshHost, ok := hostFromHostOrURL(*provider.SSHHost); ok {
			hosts = appendUniqueHost(hosts, sshHost)
		}
	}
	return hosts
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

func hostFromHostOrURL(raw string) (string, bool) {
	if host, ok := hostFromURL(raw); ok {
		return host, true
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "/") {
		return "", false
	}
	return normalizeHost(raw), true
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if withoutPort, port, err := net.SplitHostPort(host); err == nil && (port == "80" || port == "443") {
		host = withoutPort
	}
	host = strings.TrimSuffix(host, ".")
	return host
}

func appendUniqueHost(hosts []string, host string) []string {
	if host == "" {
		return hosts
	}
	for _, existing := range hosts {
		if existing == host {
			return hosts
		}
	}
	return append(hosts, host)
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
		for _, providerHost := range canonicalProviderHosts(provider) {
			if providerHost == repoHost {
				matches = append(matches, provider)
				break
			}
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

func (s *Service) AutoBindRepo(ctx context.Context, repoID int) (AutoBindResult, error) {
	repo, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.IDEQ(repoID)).
		WithScmProvider().
		Only(ctx)
	if err != nil {
		return AutoBindResult{}, fmt.Errorf("auto-bind repo: load repo: %w", err)
	}
	if repo.Edges.ScmProvider != nil {
		result := baseAutoBindResult(repo, AutoBindAlreadyBound)
		result.SCMProviderID = repo.Edges.ScmProvider.ID
		result.SCMProviderName = repo.Edges.ScmProvider.Name
		return result, nil
	}

	provider, reason, err := s.findAutoBindProvider(ctx, repo)
	if err != nil {
		result := baseAutoBindResult(repo, reason)
		result.Error = err.Error()
		s.logAutoBindResult(result)
		return result, nil
	}
	if provider == nil {
		result := baseAutoBindResult(repo, reason)
		s.logAutoBindResult(result)
		return result, nil
	}

	if _, err := s.entClient.RepoConfig.UpdateOneID(repo.ID).SetScmProviderID(provider.ID).Save(ctx); err != nil {
		return AutoBindResult{}, fmt.Errorf("auto-bind repo: set scm provider: %w", err)
	}

	result := baseAutoBindResult(repo, AutoBindMatched)
	result.SCMProviderID = provider.ID
	result.SCMProviderName = provider.Name
	result.WebhookStatus = AutoBindWebhookSkipped

	webhookStatus, postErr := s.runAutoBindPostBind(ctx, repo.ID, provider.ID)
	if webhookStatus != "" {
		result.WebhookStatus = webhookStatus
	}
	if postErr != nil {
		result.Result = AutoBindProviderError
		result.Error = postErr.Error()
	}
	s.logAutoBindResult(result)
	return result, nil
}

func (s *Service) AutoBindUnbound(ctx context.Context) (*AutoBindBatchResult, error) {
	repos, err := s.entClient.RepoConfig.Query().
		Where(
			repoconfig.Not(repoconfig.HasScmProvider()),
			repoconfig.StatusIn(repoconfig.StatusActive, repoconfig.StatusWebhookFailed),
		).
		Order(ent.Asc(repoconfig.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("auto-bind unbound repos: list repos: %w", err)
	}

	batch := &AutoBindBatchResult{Items: make([]AutoBindResult, 0, len(repos))}
	for _, repo := range repos {
		item, err := s.AutoBindRepo(ctx, repo.ID)
		if err != nil {
			item = baseAutoBindResult(repo, AutoBindProviderError)
			item.Error = err.Error()
		}
		item.addToSummary(&batch.Summary)
		batch.Items = append(batch.Items, item)
	}
	return batch, nil
}

func (s *Service) runAutoBindPostBind(ctx context.Context, repoID, providerID int) (string, error) {
	if s.autoBindPostBind != nil {
		return s.autoBindPostBind(ctx, repoID, providerID)
	}
	return s.defaultAutoBindPostBind(ctx, repoID, providerID)
}

func (s *Service) defaultAutoBindPostBind(ctx context.Context, repoID, providerID int) (string, error) {
	repo, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.IDEQ(repoID)).
		Only(ctx)
	if err != nil {
		return AutoBindWebhookSkipped, fmt.Errorf("load bound repo: %w", err)
	}
	provider, err := s.entClient.ScmProvider.Query().
		Where(scmprovider.IDEQ(providerID)).
		WithAPICredential().
		Only(ctx)
	if err != nil {
		return AutoBindWebhookSkipped, fmt.Errorf("load scm provider: %w", err)
	}
	apiPayload, err := s.resolveAPICredentialPayload(ctx, provider)
	if err != nil {
		return AutoBindWebhookSkipped, fmt.Errorf("resolve api credential: %w", err)
	}
	scmProvider, err := s.newSCMProvider(string(provider.Type), provider.BaseURL, apiPayload)
	if err != nil {
		return AutoBindWebhookSkipped, fmt.Errorf("create scm provider: %w", err)
	}

	repoInfo, err := scmProvider.GetRepo(ctx, repo.FullName)
	if err != nil {
		return AutoBindWebhookSkipped, fmt.Errorf("verify repo with scm provider: %w", err)
	}

	update := s.entClient.RepoConfig.UpdateOneID(repo.ID).
		SetName(repoInfo.Name).
		SetFullName(repoInfo.FullName).
		SetCloneURL(repoInfo.CloneURL).
		SetDefaultBranch(repoInfo.DefaultBranch)

	webhookSecret, err := generateSecret(32)
	if err != nil {
		return AutoBindWebhookSkipped, fmt.Errorf("generate webhook secret: %w", err)
	}
	webhookID, err := scmProvider.RegisterWebhook(ctx, repoInfo.FullName, []string{"pull_request", "push"}, webhookSecret)
	if err != nil {
		if _, saveErr := update.SetStatus(repoconfig.StatusWebhookFailed).Save(ctx); saveErr != nil {
			return AutoBindWebhookFailed, fmt.Errorf("register webhook: %v; save webhook_failed status: %w", err, saveErr)
		}
		return AutoBindWebhookFailed, err
	}

	if webhookID != "" {
		update.SetWebhookID(webhookID).SetWebhookSecret(webhookSecret)
	}
	if _, err := update.SetStatus(repoconfig.StatusActive).Save(ctx); err != nil {
		return AutoBindWebhookRegistered, fmt.Errorf("save post-bind repo metadata: %w", err)
	}
	return AutoBindWebhookRegistered, nil
}
