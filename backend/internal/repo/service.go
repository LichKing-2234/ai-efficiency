package repo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/aiscanresult"
	"github.com/ai-efficiency/backend/ent/efficiencymetric"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/scm"
	"go.uber.org/zap"
)

// CreateRequest is the request to create a repo config.
type CreateRequest struct {
	SCMProviderID     int    `json:"scm_provider_id" binding:"required"`
	FullName          string `json:"full_name" binding:"required"`
	GroupID           string `json:"group_id"`
	RelayProviderName string `json:"relay_provider_name"`
	RelayGroupID      string `json:"relay_group_id"`
}

// CreateDirectRequest creates a repo without SCM validation (dev mode).
type CreateDirectRequest struct {
	SCMProviderID int    `json:"scm_provider_id" binding:"required"`
	Name          string `json:"name" binding:"required"`
	FullName      string `json:"full_name" binding:"required"`
	CloneURL      string `json:"clone_url" binding:"required"`
	DefaultBranch string `json:"default_branch" binding:"required"`
	GroupID       string `json:"group_id"`
}

// UpdateRequest is the request to update a repo config.
type UpdateRequest struct {
	Name                string            `json:"name"`
	GroupID             string            `json:"group_id"`
	Status              string            `json:"status"`
	RelayProviderName   string            `json:"relay_provider_name"`
	RelayGroupID        string            `json:"relay_group_id"`
	ScanPromptOverride  map[string]string `json:"scan_prompt_override,omitempty"`
	ClearScanPrompt     bool              `json:"clear_scan_prompt,omitempty"`
}

// ListOpts are options for listing repos.
type ListOpts struct {
	Page          int
	PageSize      int
	SCMProviderID int
	Status        string
	GroupID       string
}

// Service handles repo configuration business logic.
type Service struct {
	entClient     *ent.Client
	encryptionKey string
	logger        *zap.Logger
}

// NewService creates a new repo service.
func NewService(entClient *ent.Client, encryptionKey string, logger *zap.Logger) *Service {
	return &Service{
		entClient:     entClient,
		encryptionKey: encryptionKey,
		logger:        logger,
	}
}

// Create creates a new repo config with automatic webhook registration.
func (s *Service) Create(ctx context.Context, req CreateRequest) (*ent.RepoConfig, error) {
	// Get SCM provider
	provider, err := s.entClient.ScmProvider.Get(ctx, req.SCMProviderID)
	if err != nil {
		return nil, fmt.Errorf("get scm provider: %w", err)
	}

	// Decrypt credentials
	creds, err := pkg.Decrypt(provider.Credentials, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt credentials: %w", err)
	}

	// Create SCM provider instance
	scmProvider, err := s.newSCMProvider(string(provider.Type), provider.BaseURL, creds)
	if err != nil {
		return nil, fmt.Errorf("create scm provider: %w", err)
	}

	// Verify repo exists and get info
	repoInfo, err := scmProvider.GetRepo(ctx, req.FullName)
	if err != nil {
		return nil, fmt.Errorf("get repo from scm: %w", err)
	}

	// Generate webhook secret
	webhookSecret, err := generateSecret(32)
	if err != nil {
		return nil, fmt.Errorf("generate webhook secret: %w", err)
	}

	// Register webhook
	webhookID, err := scmProvider.RegisterWebhook(ctx, req.FullName, []string{"pull_request", "push"}, webhookSecret)
	status := repoconfig.StatusActive
	if err != nil {
		s.logger.Warn("webhook registration failed", zap.String("repo", req.FullName), zap.Error(err))
		status = repoconfig.StatusWebhookFailed
		webhookID = ""
		webhookSecret = ""
	}

	// Create repo config
	create := s.entClient.RepoConfig.Create().
		SetScmProviderID(req.SCMProviderID).
		SetName(repoInfo.Name).
		SetFullName(repoInfo.FullName).
		SetCloneURL(repoInfo.CloneURL).
		SetDefaultBranch(repoInfo.DefaultBranch).
		SetStatus(status)

	if webhookID != "" {
		create.SetWebhookID(webhookID).SetWebhookSecret(webhookSecret)
	}
	if req.GroupID != "" {
		create.SetGroupID(req.GroupID)
	}
	if req.RelayProviderName != "" {
		create.SetRelayProviderName(req.RelayProviderName)
	}
	if req.RelayGroupID != "" {
		create.SetRelayGroupID(req.RelayGroupID)
	}

	rc, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("save repo config: %w", err)
	}

	return rc, nil
}

// CreateDirect creates a repo config without SCM validation (dev/testing mode).
func (s *Service) CreateDirect(ctx context.Context, req CreateDirectRequest) (*ent.RepoConfig, error) {
	create := s.entClient.RepoConfig.Create().
		SetScmProviderID(req.SCMProviderID).
		SetName(req.Name).
		SetFullName(req.FullName).
		SetCloneURL(req.CloneURL).
		SetDefaultBranch(req.DefaultBranch).
		SetStatus(repoconfig.StatusActive)

	if req.GroupID != "" {
		create.SetGroupID(req.GroupID)
	}

	rc, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("save repo config: %w", err)
	}
	return rc, nil
}

// Get returns a repo config by ID with its SCM provider.
func (s *Service) Get(ctx context.Context, id int) (*ent.RepoConfig, error) {
	return s.entClient.RepoConfig.Query().
		Where(repoconfig.IDEQ(id)).
		WithScmProvider().
		Only(ctx)
}

// List returns a paginated list of repo configs.
func (s *Service) List(ctx context.Context, opts ListOpts) ([]*ent.RepoConfig, int64, error) {
	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}

	query := s.entClient.RepoConfig.Query().WithScmProvider()

	if opts.SCMProviderID > 0 {
		query.Where(repoconfig.HasScmProviderWith(scmprovider.IDEQ(opts.SCMProviderID)))
	}
	if opts.Status != "" {
		query.Where(repoconfig.StatusEQ(repoconfig.Status(opts.Status)))
	}
	if opts.GroupID != "" {
		query.Where(repoconfig.GroupIDEQ(opts.GroupID))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count repos: %w", err)
	}

	repos, err := query.
		Offset((opts.Page - 1) * opts.PageSize).
		Limit(opts.PageSize).
		Order(ent.Desc(repoconfig.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list repos: %w", err)
	}

	return repos, int64(total), nil
}

// Update updates a repo config.
func (s *Service) Update(ctx context.Context, id int, req UpdateRequest) (*ent.RepoConfig, error) {
	update := s.entClient.RepoConfig.UpdateOneID(id)
	if req.Name != "" {
		update.SetName(req.Name)
	}
	if req.GroupID != "" {
		update.SetGroupID(req.GroupID)
	}
	if req.Status != "" {
		update.SetStatus(repoconfig.Status(req.Status))
	}
	if req.RelayProviderName != "" {
		update.SetRelayProviderName(req.RelayProviderName)
	}
	if req.RelayGroupID != "" {
		update.SetRelayGroupID(req.RelayGroupID)
	}
	if req.ClearScanPrompt {
		update.ClearScanPromptOverride()
	} else if req.ScanPromptOverride != nil {
		update.SetScanPromptOverride(req.ScanPromptOverride)
	}

	rc, err := update.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("repo not found")
		}
		return nil, fmt.Errorf("update repo: %w", err)
	}
	return rc, nil
}

// Delete deletes a repo config and cleans up its webhook.
func (s *Service) Delete(ctx context.Context, id int) error {
	rc, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.IDEQ(id)).
		WithScmProvider().
		Only(ctx)
	if err != nil {
		return fmt.Errorf("get repo: %w", err)
	}

	// Try to delete webhook if it exists
	if rc.WebhookID != nil && *rc.WebhookID != "" {
		provider := rc.Edges.ScmProvider
		if provider != nil {
			creds, err := pkg.Decrypt(provider.Credentials, s.encryptionKey)
			if err == nil {
				scmProvider, err := s.newSCMProvider(string(provider.Type), provider.BaseURL, creds)
				if err == nil {
					if err := scmProvider.DeleteWebhook(ctx, rc.FullName, *rc.WebhookID); err != nil {
						s.logger.Warn("failed to delete webhook", zap.String("repo", rc.FullName), zap.Error(err))
					}
				}
			}
		}
	}

	// Delete related records to avoid FK constraint violations
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Delete child records
	if _, err := tx.AiScanResult.Delete().Where(aiscanresult.HasRepoConfigWith(repoconfig.IDEQ(id))).Exec(ctx); err != nil {
		return fmt.Errorf("delete scan results: %w", err)
	}
	if _, err := tx.PrRecord.Delete().Where(prrecord.HasRepoConfigWith(repoconfig.IDEQ(id))).Exec(ctx); err != nil {
		return fmt.Errorf("delete pr records: %w", err)
	}
	if _, err := tx.EfficiencyMetric.Delete().Where(efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(id))).Exec(ctx); err != nil {
		return fmt.Errorf("delete efficiency metrics: %w", err)
	}
	if _, err := tx.Session.Delete().Where(session.HasRepoConfigWith(repoconfig.IDEQ(id))).Exec(ctx); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}

	if err := tx.RepoConfig.DeleteOneID(id).Exec(ctx); err != nil {
		return fmt.Errorf("delete repo: %w", err)
	}

	return tx.Commit()
}

// TriggerScan is a placeholder for Phase 2 — just updates last_scan_at.
func (s *Service) TriggerScan(ctx context.Context, id int) error {
	now := time.Now()
	return s.entClient.RepoConfig.UpdateOneID(id).
		SetLastScanAt(now).
		Exec(ctx)
}

// GetSCMProvider returns an SCM provider instance for the given repo config ID.
func (s *Service) GetSCMProvider(ctx context.Context, repoConfigID int) (scm.SCMProvider, *ent.RepoConfig, error) {
	rc, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.IDEQ(repoConfigID)).
		WithScmProvider().
		Only(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get repo config: %w", err)
	}

	provider := rc.Edges.ScmProvider
	if provider == nil {
		return nil, nil, fmt.Errorf("repo config has no scm provider")
	}

	creds, err := pkg.Decrypt(provider.Credentials, s.encryptionKey)
	if err != nil {
		return nil, nil, fmt.Errorf("decrypt credentials: %w", err)
	}

	scmProvider, err := s.newSCMProvider(string(provider.Type), provider.BaseURL, creds)
	if err != nil {
		return nil, nil, fmt.Errorf("create scm provider: %w", err)
	}

	return scmProvider, rc, nil
}

func (s *Service) newSCMProvider(providerType, baseURL, credentials string) (scm.SCMProvider, error) {
	// Import cycle prevention: use a factory approach
	// For now, we only support GitHub
	switch providerType {
	case "github":
		return newGitHubProvider(baseURL, credentials, s.logger)
	case "bitbucket_server":
		return newBitbucketProvider(baseURL, credentials, s.logger)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}

func generateSecret(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
