package usersetup

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/relay"
)

var ErrManagedKeyAlreadyExists = errors.New("managed key already exists")

const managedKeyName = "ae-cli-auto"

type ProviderResolver interface {
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
}

type ProviderResolverFunc func(ctx context.Context, providerID int) (relay.Provider, error)

func (f ProviderResolverFunc) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return f(ctx, providerID)
}

type Service struct {
	entClient *ent.Client
	resolver  ProviderResolver
}

type ListProvidersRequest struct {
	UserID int
}

type ListProvidersResponse struct {
	Providers []ProviderSummary `json:"providers"`
	Message   string            `json:"message,omitempty"`
}

type ManagedKeySummary struct {
	State      string     `json:"state"`
	APIKeyID   int64      `json:"api_key_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	Status     string     `json:"status,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type ProviderSummary struct {
	ID           int               `json:"id"`
	Name         string            `json:"name"`
	DisplayName  string            `json:"display_name"`
	BaseURL      string            `json:"base_url"`
	DefaultModel string            `json:"default_model"`
	IsPrimary    bool              `json:"is_primary"`
	ManagedKey   ManagedKeySummary `json:"managed_key"`
}

type CreateManagedKeyRequest struct {
	UserID     int
	ProviderID int
}

type RegenerateManagedKeyRequest struct {
	UserID     int
	ProviderID int
}

type CreateManagedKeyResult struct {
	APIKeyID int64  `json:"api_key_id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Secret   string `json:"secret"`
}

func NewService(entClient *ent.Client, resolver ProviderResolver) *Service {
	return &Service{
		entClient: entClient,
		resolver:  resolver,
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
		matched := activeManagedKeys(keys)
		summary := ManagedKeySummary{State: "missing"}
		if len(matched) > 0 {
			key := matched[0]
			summary = ManagedKeySummary{
				State:      "existing_hidden",
				APIKeyID:   key.ID,
				Name:       key.Name,
				Status:     key.Status,
				CreatedAt:  timePtr(key.CreatedAt),
				LastUsedAt: key.LastUsedAt,
			}
		}
		result.Providers = append(result.Providers, ProviderSummary{
			ID:           p.ID,
			Name:         p.Name,
			DisplayName:  p.DisplayName,
			BaseURL:      p.BaseURL,
			DefaultModel: p.DefaultModel,
			IsPrimary:    p.IsPrimary,
			ManagedKey:   summary,
		})
	}
	return result, nil
}

func (s *Service) CreateManagedKey(ctx context.Context, req CreateManagedKeyRequest) (*CreateManagedKeyResult, error) {
	u, rp, err := s.resolveUserAndProvider(ctx, req.UserID, req.ProviderID)
	if err != nil {
		return nil, err
	}
	keys, err := rp.ListUserAPIKeys(ctx, int64(*u.RelayUserID))
	if err != nil {
		return nil, fmt.Errorf("list user api keys: %w", err)
	}
	if len(activeManagedKeys(keys)) > 0 {
		return nil, ErrManagedKeyAlreadyExists
	}
	created, err := rp.CreateUserAPIKey(ctx, int64(*u.RelayUserID), relay.APIKeyCreateRequest{Name: managedKeyName})
	if err != nil {
		return nil, fmt.Errorf("create managed key: %w", err)
	}
	return toCreateResult(created), nil
}

func (s *Service) RegenerateManagedKey(ctx context.Context, req RegenerateManagedKeyRequest) (*CreateManagedKeyResult, error) {
	u, rp, err := s.resolveUserAndProvider(ctx, req.UserID, req.ProviderID)
	if err != nil {
		return nil, err
	}
	keys, err := rp.ListUserAPIKeys(ctx, int64(*u.RelayUserID))
	if err != nil {
		return nil, fmt.Errorf("list user api keys: %w", err)
	}
	for _, key := range activeManagedKeys(keys) {
		if err := rp.RevokeUserAPIKey(ctx, key.ID); err != nil {
			return nil, fmt.Errorf("revoke managed key %d: %w", key.ID, err)
		}
	}
	created, err := rp.CreateUserAPIKey(ctx, int64(*u.RelayUserID), relay.APIKeyCreateRequest{Name: managedKeyName})
	if err != nil {
		return nil, fmt.Errorf("create managed key: %w", err)
	}
	return toCreateResult(created), nil
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

func activeManagedKeys(keys []relay.APIKey) []relay.APIKey {
	filtered := make([]relay.APIKey, 0, len(keys))
	for _, key := range keys {
		if key.Name != managedKeyName {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(key.Status), "active") {
			continue
		}
		filtered = append(filtered, key)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].ID > filtered[j].ID
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	return filtered
}

func toCreateResult(created *relay.APIKeyWithSecret) *CreateManagedKeyResult {
	return &CreateManagedKeyResult{
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
