package sessionbootstrap

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/uuid"
)

type BootstrapRequest struct {
	RepoFullName   string `json:"repo_full_name" binding:"required"`
	BranchSnapshot string `json:"branch_snapshot" binding:"required"`
	HeadSHA        string `json:"head_sha"`
	WorkspaceRoot  string `json:"workspace_root"`
	GitDir         string `json:"git_dir"`
	GitCommonDir   string `json:"git_common_dir"`
	WorkspaceID    string `json:"workspace_id"`
}

type BootstrapResponse struct {
	SessionID          uuid.UUID         `json:"session_id"`
	StartedAt          time.Time         `json:"started_at"`
	RelayUserID        int64             `json:"relay_user_id"`
	RelayAPIKeyID      int64             `json:"relay_api_key_id"`
	ProviderName       string            `json:"provider_name"`
	GroupID            string            `json:"group_id"`
	RouteBindingSource string            `json:"route_binding_source"`
	RuntimeRef         string            `json:"runtime_ref"`
	EnvBundle          map[string]string `json:"env_bundle"`
	KeyExpiresAt       time.Time         `json:"key_expires_at"`
}

type ProviderCredentialResponse struct {
	ProviderName string `json:"provider_name"`
	Platform     string `json:"platform"`
	APIKeyID     int64  `json:"api_key_id"`
	APIKey       string `json:"api_key"`
	BaseURL      string `json:"base_url"`
}

type Service struct {
	entClient             *ent.Client
	relayProvider         relay.Provider
	relayIdentityResolver *auth.RelayIdentityResolver
	credentialLocksMu     sync.Mutex
	credentialLocks       map[string]*sync.Mutex
	credentialCacheMu     sync.Mutex
	credentialCache       map[string]*ProviderCredentialResponse

	defaultProviderName string
	providerBaseURL     string
	defaultGroupID      string
	keyTTL              time.Duration
	encryptionKey       string
}

func NewService(
	entClient *ent.Client,
	relayProvider relay.Provider,
	relayIdentityResolver *auth.RelayIdentityResolver,
	defaultProviderName string,
	providerBaseURL string,
	defaultGroupID string,
	keyTTL time.Duration,
	encryptionKeys ...string,
) *Service {
	if keyTTL <= 0 {
		keyTTL = 24 * time.Hour
	}
	return &Service{
		entClient:             entClient,
		relayProvider:         relayProvider,
		relayIdentityResolver: relayIdentityResolver,
		defaultProviderName:   strings.TrimSpace(defaultProviderName),
		providerBaseURL:       strings.TrimRight(strings.TrimSpace(providerBaseURL), "/"),
		defaultGroupID:        strings.TrimSpace(defaultGroupID),
		keyTTL:                keyTTL,
		encryptionKey:         strings.TrimSpace(firstNonEmpty(encryptionKeys...)),
		credentialLocks:       map[string]*sync.Mutex{},
		credentialCache:       map[string]*ProviderCredentialResponse{},
	}
}

type routeBinding struct {
	ProviderName       string
	GroupID            string
	RouteBindingSource string
}

type defaultGroupResolver interface {
	ResolveDefaultGroupID(ctx context.Context) (string, error)
}

type platformGroupResolver interface {
	ResolveDefaultGroupIDForPlatform(ctx context.Context, platform string) (string, error)
}

func (s *Service) resolveRouteBinding(ctx context.Context, rc *ent.RepoConfig) (*routeBinding, error) {
	if rc == nil {
		return nil, fmt.Errorf("route binding: repo config is required")
	}

	if s.relayProvider == nil {
		return nil, fmt.Errorf("route binding: relay provider is not configured")
	}

	// Treat the relay provider identity as the authoritative provider name source.
	configuredProviderName := strings.TrimSpace(s.relayProvider.Name())
	if configuredProviderName == "" {
		return nil, fmt.Errorf("route binding: relay provider name is empty")
	}

	// v1 server wiring currently supports a single configured relay provider.
	requestedProviderName := s.defaultProviderName
	if rc.RelayProviderName != nil && strings.TrimSpace(*rc.RelayProviderName) != "" {
		requestedProviderName = strings.TrimSpace(*rc.RelayProviderName)
	}
	if strings.TrimSpace(requestedProviderName) != "" && strings.TrimSpace(requestedProviderName) != configuredProviderName {
		return nil, fmt.Errorf("route binding: repo requires provider %q, but server configured %q", strings.TrimSpace(requestedProviderName), configuredProviderName)
	}

	groupID := s.defaultGroupID
	source := "default"
	if rc.RelayGroupID != nil && strings.TrimSpace(*rc.RelayGroupID) != "" {
		groupID = strings.TrimSpace(*rc.RelayGroupID)
		source = "repo_config"
	}
	if groupID == "" {
		if resolver, ok := s.relayProvider.(defaultGroupResolver); ok {
			resolved, err := resolver.ResolveDefaultGroupID(ctx)
			if err != nil {
				return nil, fmt.Errorf("route binding: resolve default group: %w", err)
			}
			if strings.TrimSpace(resolved) != "" {
				groupID = strings.TrimSpace(resolved)
				source = "relay_default"
			}
		}
	}
	if groupID == "" {
		return nil, fmt.Errorf("route binding: group_id is required")
	}

	return &routeBinding{
		ProviderName:       configuredProviderName,
		GroupID:            groupID,
		RouteBindingSource: source,
	}, nil
}

func (s *Service) Bootstrap(ctx context.Context, localUserID int, req BootstrapRequest) (*BootstrapResponse, error) {
	if strings.TrimSpace(req.RepoFullName) == "" {
		return nil, fmt.Errorf("bootstrap: repo_full_name is required")
	}
	if strings.TrimSpace(req.BranchSnapshot) == "" {
		return nil, fmt.Errorf("bootstrap: branch_snapshot is required")
	}
	if strings.TrimSpace(req.HeadSHA) == "" {
		return nil, fmt.Errorf("bootstrap: head_sha is required")
	}
	if strings.TrimSpace(req.WorkspaceRoot) == "" {
		return nil, fmt.Errorf("bootstrap: workspace_root is required")
	}
	if strings.TrimSpace(req.GitDir) == "" {
		return nil, fmt.Errorf("bootstrap: git_dir is required")
	}
	if strings.TrimSpace(req.GitCommonDir) == "" {
		return nil, fmt.Errorf("bootstrap: git_common_dir is required")
	}
	if strings.TrimSpace(req.WorkspaceID) == "" {
		return nil, fmt.Errorf("bootstrap: workspace_id is required")
	}
	if s.entClient == nil {
		return nil, fmt.Errorf("bootstrap: ent client is required")
	}
	if s.relayProvider == nil {
		return nil, fmt.Errorf("bootstrap: relay provider is not configured")
	}

	rc, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.FullNameEQ(req.RepoFullName)).
		Only(ctx)
	if err != nil && ent.IsNotFound(err) {
		rc, err = s.entClient.RepoConfig.Query().
			Where(repoconfig.CloneURLEQ(req.RepoFullName)).
			Only(ctx)
	}
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("bootstrap: repo not found: %s", req.RepoFullName)
		}
		return nil, fmt.Errorf("bootstrap: query repo: %w", err)
	}

	binding, err := s.resolveRouteBinding(ctx, rc)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}

	u, err := s.entClient.User.Get(ctx, localUserID)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: get user: %w", err)
	}

	var relayUserID int64
	if u.RelayUserID != nil {
		relayUserID = int64(*u.RelayUserID)
	} else {
		if s.relayIdentityResolver == nil {
			return nil, fmt.Errorf("bootstrap: relay identity resolver is not configured")
		}
		relayUser, err := s.relayIdentityResolver.ResolveOrProvision(ctx, u.Username, u.Email)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: resolve relay identity: %w", err)
		}
		relayUserID = relayUser.ID
		_, _ = s.entClient.User.UpdateOneID(u.ID).SetRelayUserID(int(relayUserID)).Save(ctx)
	}

	now := time.Now()
	sessionID := uuid.New()
	expiresAt := now.Add(s.keyTTL)

	runtimeRef := fmt.Sprintf("runtime/%s", sessionID.String())

	create := s.entClient.Session.Create().
		SetID(sessionID).
		SetRepoConfigID(rc.ID).
		SetBranch(req.BranchSnapshot).
		SetRelayUserID(int(relayUserID)).
		SetProviderName(binding.ProviderName).
		SetRuntimeRef(runtimeRef).
		SetLastSeenAt(now).
		SetStartedAt(now).
		SetInitialWorkspaceRoot(req.WorkspaceRoot).
		SetInitialGitDir(req.GitDir).
		SetInitialGitCommonDir(req.GitCommonDir)

	create.SetHeadShaAtStart(strings.TrimSpace(req.HeadSHA))
	if localUserID != 0 {
		create.SetUserID(localUserID)
	}

	if _, err := create.Save(ctx); err != nil {
		return nil, fmt.Errorf("bootstrap: create session: %w", err)
	}

	envBundle := map[string]string{
		"AE_SESSION_ID":    sessionID.String(),
		"AE_RUNTIME_REF":   runtimeRef,
		"AE_PROVIDER_NAME": binding.ProviderName,
		"AE_ENV_VERSION":   "1",
	}

	return &BootstrapResponse{
		SessionID:          sessionID,
		StartedAt:          now,
		RelayUserID:        relayUserID,
		RelayAPIKeyID:      0,
		ProviderName:       binding.ProviderName,
		GroupID:            binding.GroupID,
		RouteBindingSource: binding.RouteBindingSource,
		RuntimeRef:         runtimeRef,
		EnvBundle:          envBundle,
		KeyExpiresAt:       expiresAt,
	}, nil
}

func (s *Service) ResolveProviderCredential(ctx context.Context, localUserID int, sessionID uuid.UUID, platform string) (*ProviderCredentialResponse, error) {
	key := fmt.Sprintf("%d:%s:%s", localUserID, sessionID.String(), strings.TrimSpace(platform))
	lock := s.credentialLock(key)
	lock.Lock()
	defer lock.Unlock()
	if cached := s.cachedCredential(key); cached != nil {
		return cached, nil
	}
	return s.resolveProviderCredentialOnce(ctx, localUserID, sessionID, platform)
}

func (s *Service) credentialLock(key string) *sync.Mutex {
	s.credentialLocksMu.Lock()
	defer s.credentialLocksMu.Unlock()
	if s.credentialLocks == nil {
		s.credentialLocks = map[string]*sync.Mutex{}
	}
	lock, ok := s.credentialLocks[key]
	if !ok {
		lock = &sync.Mutex{}
		s.credentialLocks[key] = lock
	}
	return lock
}

func (s *Service) cachedCredential(key string) *ProviderCredentialResponse {
	s.credentialCacheMu.Lock()
	defer s.credentialCacheMu.Unlock()
	if s.credentialCache == nil {
		return nil
	}
	cred := s.credentialCache[key]
	if cred == nil {
		return nil
	}
	copy := *cred
	return &copy
}

func (s *Service) storeCredential(key string, cred *ProviderCredentialResponse) {
	if cred == nil {
		return
	}
	s.credentialCacheMu.Lock()
	defer s.credentialCacheMu.Unlock()
	if s.credentialCache == nil {
		s.credentialCache = map[string]*ProviderCredentialResponse{}
	}
	copy := *cred
	s.credentialCache[key] = &copy
}

func (s *Service) resolveProviderCredentialOnce(ctx context.Context, localUserID int, sessionID uuid.UUID, platform string) (*ProviderCredentialResponse, error) {
	cacheKey := fmt.Sprintf("%d:%s:%s", localUserID, sessionID.String(), strings.TrimSpace(platform))
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil, fmt.Errorf("resolve provider credential: platform is required")
	}
	if s.entClient == nil {
		return nil, fmt.Errorf("resolve provider credential: ent client is required")
	}
	if s.relayProvider == nil {
		return nil, fmt.Errorf("resolve provider credential: relay provider is not configured")
	}

	query := s.entClient.Session.Query().
		Where(session.IDEQ(sessionID)).
		WithRepoConfig()
	if localUserID != 0 {
		query = query.Where(session.HasUserWith(user.IDEQ(localUserID)))
	}
	sess, err := query.Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve provider credential: get session: %w", err)
	}

	u, err := s.entClient.User.Get(ctx, localUserID)
	if err != nil {
		return nil, fmt.Errorf("resolve provider credential: get user: %w", err)
	}
	if u.RelayUserID == nil {
		return nil, fmt.Errorf("resolve provider credential: relay user is not bound")
	}

	keys, err := s.relayProvider.ListUserAPIKeys(ctx, int64(*u.RelayUserID))
	if err != nil {
		return nil, fmt.Errorf("resolve provider credential: list api keys: %w", err)
	}

	emailPrefix := preferredKeyName("", u.Email)
	selected := selectReusableKey(keys, platform, strings.TrimSpace(u.Username), emailPrefix)
	if selected == nil {
		inactive := selectReactivatableKey(keys, platform, strings.TrimSpace(u.Username), emailPrefix)
		if inactive != nil {
			updateCtx, err := s.contextWithRelayCredentials(ctx, u)
			if err != nil {
				return nil, fmt.Errorf("resolve provider credential: %w", err)
			}
			if err := s.relayProvider.UpdateUserAPIKeyStatus(updateCtx, inactive.ID, "active"); err != nil {
				return nil, fmt.Errorf("resolve provider credential: reactivate api key: %w", err)
			}
			inactive.Status = "active"
			selected = inactive
		}
	}
	if selected == nil {
		groupID, err := s.resolveCredentialGroupID(ctx, sess.Edges.RepoConfig, platform)
		if err != nil {
			return nil, fmt.Errorf("resolve provider credential: %w", err)
		}
		createCtx, err := s.contextWithRelayCredentials(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("resolve provider credential: %w", err)
		}
		created, err := s.relayProvider.CreateUserAPIKey(createCtx, int64(*u.RelayUserID), relay.APIKeyCreateRequest{
			Name:    preferredKeyName(strings.TrimSpace(u.Username), strings.TrimSpace(u.Email)),
			GroupID: groupID,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve provider credential: create api key: %w", err)
		}
		selected = &created.APIKey
		selected.Key = created.Secret
	}

	resp := &ProviderCredentialResponse{
		ProviderName: s.relayProvider.Name(),
		Platform:     platform,
		APIKeyID:     selected.ID,
		APIKey:       selected.Key,
		BaseURL:      s.providerBaseURL,
	}
	s.storeCredential(cacheKey, resp)
	return resp, nil
}

func (s *Service) resolveCredentialGroupID(ctx context.Context, rc *ent.RepoConfig, platform string) (string, error) {
	if resolver, ok := s.relayProvider.(platformGroupResolver); ok {
		resolved, err := resolver.ResolveDefaultGroupIDForPlatform(ctx, platform)
		if err != nil {
			return "", fmt.Errorf("resolve provider credential group: %w", err)
		}
		if strings.TrimSpace(resolved) != "" {
			return strings.TrimSpace(resolved), nil
		}
	}

	binding, err := s.resolveRouteBinding(ctx, rc)
	if err != nil {
		return "", err
	}
	return binding.GroupID, nil
}

func (s *Service) contextWithRelayCredentials(ctx context.Context, u *ent.User) (context.Context, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if u == nil || u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
		return ctx, nil
	}
	if strings.TrimSpace(s.encryptionKey) == "" {
		return nil, fmt.Errorf("relay auth password is stored but encryption key is unavailable")
	}
	password, err := pkg.Decrypt(strings.TrimSpace(*u.RelayAuthPassword), s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt relay auth password: %w", err)
	}
	login := strings.TrimSpace(u.Email)
	if login == "" {
		login = strings.TrimSpace(u.Username)
	}
	if login == "" || strings.TrimSpace(password) == "" {
		return ctx, nil
	}
	return relay.WithUserCredentials(ctx, login, password), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (s *Service) Heartbeat(ctx context.Context, sessionID uuid.UUID) (*ent.Session, error) {
	if s.entClient == nil {
		return nil, fmt.Errorf("heartbeat: ent client is required")
	}
	now := time.Now()
	updated, err := s.entClient.Session.UpdateOneID(sessionID).
		SetLastSeenAt(now).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("heartbeat: %w", err)
	}
	return updated, nil
}

func (s *Service) Stop(ctx context.Context, sessionID uuid.UUID) (*ent.Session, error) {
	if s.entClient == nil {
		return nil, fmt.Errorf("stop: ent client is required")
	}

	existing, err := s.entClient.Session.Get(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("stop: get session: %w", err)
	}

	now := time.Now()
	updated, err := s.entClient.Session.UpdateOneID(sessionID).
		SetEndedAt(now).
		SetStatus(session.StatusCompleted).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("stop: update session: %w", err)
	}

	if s.relayProvider != nil && existing.RelayAPIKeyID != nil {
		_ = s.relayProvider.RevokeUserAPIKey(ctx, int64(*existing.RelayAPIKeyID))
	}

	return updated, nil
}
