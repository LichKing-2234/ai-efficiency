package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
)

const EligibilityVersion = "repo-eligibility-v1"

type ResolveRemoteRequest struct {
	RemoteURL          string `json:"remote_url" binding:"required"`
	Branch             string `json:"branch"`
	ClientCacheVersion string `json:"client_cache_version"`
}

type HookEligibleRepoRequest struct {
	RepoKey   string `json:"repo_key"`
	RemoteURL string `json:"remote_url"`
}

type EligibilityResult struct {
	Eligible      bool   `json:"eligible"`
	RepoConfigID  int    `json:"repo_config_id,omitempty"`
	RepoKey       string `json:"repo_key,omitempty"`
	FullName      string `json:"full_name,omitempty"`
	CloneURL      string `json:"clone_url,omitempty"`
	Status        string `json:"status,omitempty"`
	BindingState  string `json:"binding_state,omitempty"`
	SCMProviderID *int   `json:"scm_provider_id,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func (s *Service) ResolveRemoteEligibility(ctx context.Context, req ResolveRemoteRequest) (*EligibilityResult, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("resolve repo eligibility: service is not initialized")
	}
	remoteURL := strings.TrimSpace(req.RemoteURL)
	if remoteURL == "" {
		return &EligibilityResult{Eligible: false, Reason: "invalid_remote"}, nil
	}

	identity, err := DeriveRepoIdentity(remoteURL)
	if err != nil {
		identity = FallbackRepoIdentity(remoteURL, remoteURL)
	}
	result := &EligibilityResult{
		Eligible: false,
		RepoKey:  identity.RepoKey,
	}

	rc, err := s.findExistingRepoByIdentity(ctx, identity, remoteURL)
	if err != nil {
		return nil, err
	}
	if rc == nil {
		result.Reason = "not_found"
		return result, nil
	}
	return s.eligibilityForRepo(rc), nil
}

func (s *Service) BatchHookEligibility(ctx context.Context, repos []HookEligibleRepoRequest) ([]EligibilityResult, []EligibilityResult, error) {
	eligible := make([]EligibilityResult, 0, len(repos))
	ineligible := make([]EligibilityResult, 0, len(repos))
	for _, item := range repos {
		result, err := s.ResolveRemoteEligibility(ctx, ResolveRemoteRequest{RemoteURL: item.RemoteURL})
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(result.RepoKey) == "" {
			result.RepoKey = strings.TrimSpace(item.RepoKey)
		}
		if result.Eligible {
			eligible = append(eligible, *result)
			continue
		}
		ineligible = append(ineligible, *result)
	}
	return eligible, ineligible, nil
}

func (s *Service) eligibilityForRepo(rc *ent.RepoConfig) *EligibilityResult {
	result := &EligibilityResult{
		RepoConfigID: rc.ID,
		RepoKey:      rc.RepoKey,
		FullName:     rc.FullName,
		CloneURL:     rc.CloneURL,
		Status:       string(rc.Status),
		BindingState: "unbound",
	}
	if rc.Edges.ScmProvider != nil {
		result.BindingState = "bound"
		id := rc.Edges.ScmProvider.ID
		result.SCMProviderID = &id
	}
	switch rc.Status {
	case repoconfig.StatusActive, repoconfig.StatusWebhookFailed:
		result.Eligible = true
	default:
		result.Eligible = false
		result.Reason = "inactive"
	}
	return result
}
