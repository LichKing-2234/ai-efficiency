package repo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/aiscanresult"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/efficiencymetric"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/ai-efficiency/backend/internal/credential"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/scm"
	"go.uber.org/zap"
)

var ErrRepoUnbound = errors.New("repo is not bound to an scm provider")

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
	SCMProviderID     int    `json:"scm_provider_id"`
	RepoKey           string `json:"repo_key"`
	Name              string `json:"name" binding:"required"`
	FullName          string `json:"full_name" binding:"required"`
	CloneURL          string `json:"clone_url" binding:"required"`
	DefaultBranch     string `json:"default_branch" binding:"required"`
	GroupID           string `json:"group_id"`
	RelayProviderName string `json:"relay_provider_name"`
	RelayGroupID      string `json:"relay_group_id"`
}

// UpdateRequest is the request to update a repo config.
type UpdateRequest struct {
	Name               string            `json:"name"`
	GroupID            string            `json:"group_id"`
	Status             string            `json:"status"`
	SCMProviderID      *int              `json:"scm_provider_id"`
	ClearSCMProvider   bool              `json:"clear_scm_provider,omitempty"`
	RelayProviderName  *string           `json:"relay_provider_name"`
	RelayGroupID       *string           `json:"relay_group_id"`
	ScanPromptOverride map[string]string `json:"scan_prompt_override,omitempty"`
	ClearScanPrompt    bool              `json:"clear_scan_prompt,omitempty"`
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

func IsRepoUnbound(err error) bool {
	return errors.Is(err, ErrRepoUnbound)
}

func deriveRepoKeyFromCloneURL(cloneURL string) (string, error) {
	identity, err := DeriveRepoIdentity(cloneURL)
	if err != nil {
		return "", err
	}
	return identity.RepoKey, nil
}

// Create creates a new repo config with automatic webhook registration.
func (s *Service) Create(ctx context.Context, req CreateRequest) (*ent.RepoConfig, error) {
	// Get SCM provider
	provider, err := s.entClient.ScmProvider.Query().
		Where(scmprovider.IDEQ(req.SCMProviderID)).
		WithAPICredential().
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("get scm provider: %w", err)
	}

	apiPayload, err := s.resolveAPICredentialPayload(ctx, provider)
	if err != nil {
		return nil, fmt.Errorf("resolve api credential: %w", err)
	}

	// Create SCM provider instance
	scmProvider, err := s.newSCMProvider(string(provider.Type), provider.BaseURL, apiPayload)
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
	repoKey, err := deriveRepoKeyFromCloneURL(repoInfo.CloneURL)
	if err != nil {
		return nil, fmt.Errorf("derive repo key: %w", err)
	}

	create := s.entClient.RepoConfig.Create().
		SetRepoKey(repoKey).
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
	repoKey := strings.TrimSpace(req.RepoKey)
	if repoKey == "" {
		derivedRepoKey, err := deriveRepoKeyFromCloneURL(req.CloneURL)
		if err != nil {
			return nil, fmt.Errorf("derive repo key: %w", err)
		}
		repoKey = derivedRepoKey
	}

	create := s.entClient.RepoConfig.Create().
		SetRepoKey(repoKey).
		SetName(req.Name).
		SetFullName(req.FullName).
		SetCloneURL(req.CloneURL).
		SetDefaultBranch(req.DefaultBranch).
		SetStatus(repoconfig.StatusActive)

	if req.SCMProviderID > 0 {
		create.SetScmProviderID(req.SCMProviderID)
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

// FindOrCreateFromRemote finds an existing repo by stable identity or creates
// an unbound repo from the local git remote metadata.
func (s *Service) FindOrCreateFromRemote(ctx context.Context, remoteURL, branch string) (*ent.RepoConfig, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL == "" {
		return nil, fmt.Errorf("find or create repo: remote URL is empty")
	}

	if identity, err := DeriveRepoIdentity(remoteURL); err == nil {
		if existing, err := s.entClient.RepoConfig.Query().
			Where(repoconfig.RepoKeyEQ(identity.RepoKey)).
			WithScmProvider().
			Only(ctx); err == nil {
			return s.refreshRepoMetadata(ctx, existing.ID, identity, branch)
		} else if err != nil && !ent.IsNotFound(err) {
			return nil, fmt.Errorf("find or create repo: query by repo_key: %w", err)
		}

		if existing, err := s.entClient.RepoConfig.Query().
			Where(repoconfig.CloneURLEQ(remoteURL)).
			WithScmProvider().
			Only(ctx); err == nil {
			return s.refreshRepoMetadata(ctx, existing.ID, identity, branch)
		} else if err != nil && !ent.IsNotFound(err) {
			return nil, fmt.Errorf("find or create repo: query by clone_url: %w", err)
		}

		if existing, err := s.entClient.RepoConfig.Query().
			Where(repoconfig.FullNameEQ(identity.FullName)).
			WithScmProvider().
			Only(ctx); err == nil {
			return s.refreshRepoMetadata(ctx, existing.ID, identity, branch)
		} else if err != nil && !ent.IsNotFound(err) {
			return nil, fmt.Errorf("find or create repo: query by full_name: %w", err)
		}

		defaultBranch := strings.TrimSpace(branch)
		if defaultBranch == "" {
			defaultBranch = "main"
		}

		rc, err := s.entClient.RepoConfig.Create().
			SetRepoKey(identity.RepoKey).
			SetName(identity.Name).
			SetFullName(identity.FullName).
			SetCloneURL(identity.CloneURL).
			SetDefaultBranch(defaultBranch).
			SetStatus(repoconfig.StatusActive).
			Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("find or create repo: create repo: %w", err)
		}
		return rc, nil
	}

	rc, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.FullNameEQ(remoteURL)).
		WithScmProvider().
		Only(ctx)
	if err == nil {
		return rc, nil
	}
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("find or create repo: query by full_name: %w", err)
	}

	rc, err = s.entClient.RepoConfig.Query().
		Where(repoconfig.CloneURLEQ(remoteURL)).
		WithScmProvider().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("find or create repo: repo not found: %s", remoteURL)
		}
		return nil, fmt.Errorf("find or create repo: query by clone_url: %w", err)
	}
	return rc, nil
}

func (s *Service) refreshRepoMetadata(ctx context.Context, repoID int, identity RepoIdentity, branch string) (*ent.RepoConfig, error) {
	update := s.entClient.RepoConfig.UpdateOneID(repoID).
		SetRepoKey(identity.RepoKey).
		SetName(identity.Name).
		SetFullName(identity.FullName).
		SetCloneURL(identity.CloneURL)
	if strings.TrimSpace(branch) != "" {
		update.SetDefaultBranch(strings.TrimSpace(branch))
	}
	rc, err := update.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh repo metadata: %w", err)
	}
	return s.entClient.RepoConfig.Query().
		Where(repoconfig.IDEQ(rc.ID)).
		WithScmProvider().
		Only(ctx)
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
	if req.ClearSCMProvider {
		update.ClearScmProvider()
	} else if req.SCMProviderID != nil {
		update.SetScmProviderID(*req.SCMProviderID)
	}
	if req.RelayProviderName != nil {
		if *req.RelayProviderName == "" {
			update.ClearRelayProviderName()
		} else {
			update.SetRelayProviderName(*req.RelayProviderName)
		}
	}
	if req.RelayGroupID != nil {
		if *req.RelayGroupID == "" {
			update.ClearRelayGroupID()
		} else {
			update.SetRelayGroupID(*req.RelayGroupID)
		}
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
		WithScmProvider(func(query *ent.ScmProviderQuery) {
			query.WithAPICredential()
		}).
		Only(ctx)
	if err != nil {
		return fmt.Errorf("get repo: %w", err)
	}

	// Try to delete webhook if it exists
	if rc.WebhookID != nil && *rc.WebhookID != "" {
		provider := rc.Edges.ScmProvider
		if provider != nil {
			apiPayload, err := s.resolveAPICredentialPayload(ctx, provider)
			if err == nil {
				scmProvider, err := s.newSCMProvider(string(provider.Type), provider.BaseURL, apiPayload)
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
		WithScmProvider(func(query *ent.ScmProviderQuery) {
			query.WithAPICredential()
		}).
		Only(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get repo config: %w", err)
	}

	provider := rc.Edges.ScmProvider
	if provider == nil {
		return nil, nil, fmt.Errorf("repo config has no scm provider")
	}

	apiPayload, err := s.resolveAPICredentialPayload(ctx, provider)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve api credential: %w", err)
	}

	scmProvider, err := s.newSCMProvider(string(provider.Type), provider.BaseURL, apiPayload)
	if err != nil {
		return nil, nil, fmt.Errorf("create scm provider: %w", err)
	}

	return scmProvider, rc, nil
}

func (s *Service) newSCMProvider(providerType, baseURL string, apiCredential any) (scm.SCMProvider, error) {
	// Import cycle prevention: use a factory approach
	// For now, we only support GitHub
	switch providerType {
	case "github":
		return newGitHubProvider(baseURL, apiCredential, s.logger)
	case "bitbucket_server":
		return newBitbucketProvider(baseURL, apiCredential, s.logger)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}

func (s *Service) resolveAPICredentialPayload(ctx context.Context, provider *ent.ScmProvider) (credential.Payload, error) {
	if provider == nil {
		return nil, fmt.Errorf("scm provider is nil")
	}

	cred := provider.Edges.APICredential
	if cred == nil && provider.APICredentialID != 0 {
		row, err := s.entClient.Credential.Get(ctx, provider.APICredentialID)
		if err != nil {
			return nil, fmt.Errorf("get api credential: %w", err)
		}
		cred = row
	}
	if cred == nil {
		if provider.Credentials != "" {
			raw, err := pkg.Decrypt(provider.Credentials, s.encryptionKey)
			if err != nil {
				return nil, fmt.Errorf("decrypt legacy scm provider credentials: %w", err)
			}
			payload, err := credential.ParseLegacySCMProviderSecret(raw)
			if err != nil {
				return nil, fmt.Errorf("parse legacy scm provider credentials: %w", err)
			}
			return payload, nil
		}
		return nil, fmt.Errorf("scm provider has no api credential")
	}

	raw, err := pkg.Decrypt(cred.Payload, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt api credential: %w", err)
	}
	payload, err := credential.ParsePayload(credential.Kind(cred.Kind), []byte(raw))
	if err != nil {
		return nil, fmt.Errorf("parse api credential payload: %w", err)
	}
	if cred.Kind == entcredential.KindSSHUsernameWithPrivateKey {
		return nil, fmt.Errorf("api credential cannot be ssh_username_with_private_key")
	}
	return payload, nil
}

func generateSecret(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
