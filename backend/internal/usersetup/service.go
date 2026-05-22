package usersetup

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
)

var ErrManagedKeyAlreadyExists = errors.New("managed key already exists")

type ProviderResolver interface {
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
}

type ProviderResolverFunc func(ctx context.Context, providerID int) (relay.Provider, error)

func (f ProviderResolverFunc) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return f(ctx, providerID)
}

type allowedGroupLister interface {
	ListAllowedGroupsForUser(ctx context.Context, userID int64) ([]relay.Group, error)
}

type Service struct {
	entClient     *ent.Client
	resolver      ProviderResolver
	encryptionKey string
}

type ListProvidersRequest struct {
	UserID int
}

type ListProvidersResponse struct {
	Providers []ProviderSummary `json:"providers"`
	Message   string            `json:"message,omitempty"`
}

type GroupCredentialState struct {
	State      string     `json:"state"`
	APIKeyID   int64      `json:"api_key_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	Status     string     `json:"status,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type GroupCredentialSummary struct {
	GroupID    string               `json:"group_id"`
	GroupName  string               `json:"group_name"`
	Platform   string               `json:"platform"`
	Credential GroupCredentialState `json:"credential"`
}

type ProviderSummary struct {
	ID           int                      `json:"id"`
	Name         string                   `json:"name"`
	DisplayName  string                   `json:"display_name"`
	BaseURL      string                   `json:"base_url"`
	DefaultModel string                   `json:"default_model"`
	IsPrimary    bool                     `json:"is_primary"`
	Groups       []GroupCredentialSummary `json:"groups"`
}

type CreateGroupCredentialRequest struct {
	UserID     int
	ProviderID int
	GroupID    string
}

type RegenerateGroupCredentialRequest struct {
	UserID     int
	ProviderID int
	GroupID    string
}

type CreateGroupCredentialResult struct {
	APIKeyID int64  `json:"api_key_id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Secret   string `json:"secret"`
}

func NewService(entClient *ent.Client, resolver ProviderResolver, encryptionKeys ...string) *Service {
	return &Service{
		entClient:     entClient,
		resolver:      resolver,
		encryptionKey: firstNonEmptyString(encryptionKeys...),
	}
}

func (s *Service) ListProviders(ctx context.Context, req ListProvidersRequest) (*ListProvidersResponse, error) {
	u, err := s.entClient.User.Get(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if u.RelayUserID == nil {
		return &ListProvidersResponse{
			Providers: []ProviderSummary{},
			Message:   "current account is not linked to a relay user",
		}, nil
	}

	providers, err := s.entClient.RelayProvider.Query().
		Where(relayprovider.EnabledEQ(true)).
		Order(ent.Desc(relayprovider.FieldIsPrimary), ent.Asc(relayprovider.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}

	result := &ListProvidersResponse{
		Providers: make([]ProviderSummary, 0, len(providers)),
	}
	for _, p := range providers {
		rp, err := s.resolver.Resolve(ctx, p.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve relay provider %d: %w", p.ID, err)
		}
		keys, err := rp.ListUserAPIKeys(ctx, int64(*u.RelayUserID))
		if err != nil {
			return nil, fmt.Errorf("list user api keys for provider %d: %w", p.ID, err)
		}
		groups, err := s.summarizeGroups(ctx, rp, int64(*u.RelayUserID), u.Username, u.Email, keys)
		if err != nil {
			return nil, fmt.Errorf("summarize provider %d groups: %w", p.ID, err)
		}
		result.Providers = append(result.Providers, ProviderSummary{
			ID:           p.ID,
			Name:         p.Name,
			DisplayName:  p.DisplayName,
			BaseURL:      p.BaseURL,
			DefaultModel: p.DefaultModel,
			IsPrimary:    p.IsPrimary,
			Groups:       groups,
		})
	}
	return result, nil
}

func (s *Service) CreateGroupCredential(ctx context.Context, req CreateGroupCredentialRequest) (*CreateGroupCredentialResult, error) {
	u, rp, err := s.resolveUserAndProvider(ctx, req.UserID, req.ProviderID)
	if err != nil {
		return nil, err
	}
	groupID := strings.TrimSpace(req.GroupID)
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}
	keys, err := rp.ListUserAPIKeys(ctx, int64(*u.RelayUserID))
	if err != nil {
		return nil, fmt.Errorf("list user api keys: %w", err)
	}
	if selected := selectReusableKeyByGroup(keys, groupID, strings.TrimSpace(u.Username), strings.TrimSpace(u.Email)); selected != nil {
		return nil, ErrManagedKeyAlreadyExists
	}
	createCtx := s.withStoredRelayCredentials(ctx, u)
	created, err := rp.CreateUserAPIKey(createCtx, int64(*u.RelayUserID), relay.APIKeyCreateRequest{
		Name:    preferredCredentialName(strings.TrimSpace(u.Username), strings.TrimSpace(u.Email)),
		GroupID: groupID,
	})
	if err != nil {
		return nil, fmt.Errorf("create group credential: %w", err)
	}
	return toCreateResult(created), nil
}

func (s *Service) RegenerateGroupCredential(ctx context.Context, req RegenerateGroupCredentialRequest) (*CreateGroupCredentialResult, error) {
	u, rp, err := s.resolveUserAndProvider(ctx, req.UserID, req.ProviderID)
	if err != nil {
		return nil, err
	}
	groupID := strings.TrimSpace(req.GroupID)
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}
	keys, err := rp.ListUserAPIKeys(ctx, int64(*u.RelayUserID))
	if err != nil {
		return nil, fmt.Errorf("list user api keys: %w", err)
	}
	updateCtx := s.withStoredRelayCredentials(ctx, u)
	for _, key := range filterReusableKeysByGroup(keys, groupID, preferredCredentialName(strings.TrimSpace(u.Username), strings.TrimSpace(u.Email))) {
		if err := rp.UpdateUserAPIKeyStatus(updateCtx, key.ID, "inactive"); err != nil {
			return nil, fmt.Errorf("revoke group credential %d: %w", key.ID, err)
		}
	}
	created, err := rp.CreateUserAPIKey(updateCtx, int64(*u.RelayUserID), relay.APIKeyCreateRequest{
		Name:    preferredCredentialName(strings.TrimSpace(u.Username), strings.TrimSpace(u.Email)),
		GroupID: groupID,
	})
	if err != nil {
		return nil, fmt.Errorf("create group credential: %w", err)
	}
	return toCreateResult(created), nil
}

func (s *Service) summarizeGroups(ctx context.Context, rp relay.Provider, relayUserID int64, username, email string, keys []relay.APIKey) ([]GroupCredentialSummary, error) {
	lister, ok := rp.(allowedGroupLister)
	if !ok {
		return []GroupCredentialSummary{}, nil
	}
	allowedGroups, err := lister.ListAllowedGroupsForUser(ctx, relayUserID)
	if err != nil {
		return nil, fmt.Errorf("list allowed groups: %w", err)
	}
	groupMap := map[string]GroupCredentialSummary{}
	for _, group := range allowedGroups {
		groupID := strconv.FormatInt(group.ID, 10)
		if strings.TrimSpace(groupID) == "" || strings.TrimSpace(group.Platform) == "" {
			continue
		}
		groupMap[groupID] = GroupCredentialSummary{
			GroupID:   groupID,
			GroupName: firstNonEmptyString(strings.TrimSpace(group.Name), groupID),
			Platform:  strings.TrimSpace(group.Platform),
			Credential: GroupCredentialState{
				State: "missing",
			},
		}
	}

	for groupID, summary := range groupMap {
		if selected := selectReusableKeyByGroup(keys, groupID, strings.TrimSpace(username), strings.TrimSpace(email)); selected != nil {
			summary.Credential = GroupCredentialState{
				State:      "existing_hidden",
				APIKeyID:   selected.ID,
				Name:       selected.Name,
				Status:     selected.Status,
				CreatedAt:  timePtr(selected.CreatedAt),
				LastUsedAt: selected.LastUsedAt,
			}
			if selected.Group != nil {
				summary.GroupName = firstNonEmptyString(strings.TrimSpace(selected.Group.Name), summary.GroupName)
				summary.Platform = firstNonEmptyString(strings.TrimSpace(selected.Group.Platform), summary.Platform)
			}
			groupMap[groupID] = summary
		}
	}

	groups := make([]GroupCredentialSummary, 0, len(groupMap))
	for _, summary := range groupMap {
		groups = append(groups, summary)
	}
	canonicalGroupOrder(groups)
	return groups, nil
}

func selectReusableKeyByGroup(keys []relay.APIKey, groupID, username, email string) *relay.APIKey {
	usernameMatches := filterReusableKeysByGroup(keys, groupID, preferredCredentialName(username, ""))
	if selected := pickReusableKey(usernameMatches); selected != nil {
		return selected
	}
	emailPrefix := preferredCredentialName("", email)
	return pickReusableKey(filterReusableKeysByGroup(keys, groupID, emailPrefix))
}

func filterReusableKeysByGroup(keys []relay.APIKey, groupID, name string) []relay.APIKey {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(groupID) == "" {
		return nil
	}
	filtered := make([]relay.APIKey, 0, len(keys))
	for _, key := range keys {
		if !strings.EqualFold(strings.TrimSpace(key.Status), "active") {
			continue
		}
		if key.Group == nil {
			continue
		}
		if strconv.FormatInt(key.Group.ID, 10) != strings.TrimSpace(groupID) {
			continue
		}
		if key.Name != name {
			continue
		}
		filtered = append(filtered, key)
	}
	return filtered
}

func (s *Service) resolveUserAndProvider(ctx context.Context, userID, providerID int) (*ent.User, relay.Provider, error) {
	u, err := s.entClient.User.Get(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("get user: %w", err)
	}
	if u.RelayUserID == nil {
		return nil, nil, fmt.Errorf("user %d is not linked to a relay user", userID)
	}
	if _, err := s.entClient.RelayProvider.Get(ctx, providerID); err != nil {
		return nil, nil, fmt.Errorf("get provider: %w", err)
	}
	rp, err := s.resolver.Resolve(ctx, providerID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve relay provider %d: %w", providerID, err)
	}
	return u, rp, nil
}

func toCreateResult(created *relay.APIKeyWithSecret) *CreateGroupCredentialResult {
	return &CreateGroupCredentialResult{
		APIKeyID: created.ID,
		Name:     created.Name,
		Status:   created.Status,
		Secret:   created.Secret,
	}
}

func timePtr(v time.Time) *time.Time {
	if v.IsZero() {
		return nil
	}
	copy := v
	return &copy
}

func (s *Service) withStoredRelayCredentials(ctx context.Context, u *ent.User) context.Context {
	if u == nil || u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" || strings.TrimSpace(s.encryptionKey) == "" {
		return ctx
	}
	password, err := pkg.Decrypt(strings.TrimSpace(*u.RelayAuthPassword), s.encryptionKey)
	if err != nil || strings.TrimSpace(password) == "" {
		return ctx
	}
	login := firstNonEmptyString(u.Email, u.Username)
	if strings.TrimSpace(login) == "" {
		return ctx
	}
	return relay.WithUserCredentials(ctx, login, password)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
