package hookstate

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/clistate"
)

const (
	eligibilityCacheVersion = 1
	positiveEligibilityTTL  = 24 * time.Hour
	negativeEligibilityTTL  = 5 * time.Minute
)

type EligibilityRecord struct {
	Eligible       bool      `json:"eligible"`
	ServerURL      string    `json:"server_url"`
	AuthSubject    string    `json:"auth_subject"`
	RepoConfigID   int       `json:"repo_config_id,omitempty"`
	RepoKey        string    `json:"repo_key"`
	FullName       string    `json:"full_name,omitempty"`
	CloneURL       string    `json:"clone_url,omitempty"`
	Status         string    `json:"status,omitempty"`
	BindingState   string    `json:"binding_state,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	LastResolvedAt time.Time `json:"last_resolved_at,omitempty"`
	LastObservedAt time.Time `json:"last_observed_at,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type EligibilityCache struct {
	Version            int                          `json:"version"`
	UpdatedAt          time.Time                    `json:"updated_at"`
	ETag               string                       `json:"etag,omitempty"`
	EligibilityVersion string                       `json:"eligibility_version"`
	Repos              map[string]EligibilityRecord `json:"repos"`
	Negative           map[string]EligibilityRecord `json:"negative"`
}

func EligibilityPath() string {
	return filepath.Join(clistate.HooksStateDir(), "repos.json")
}

func LoadEligibilityCache() (*EligibilityCache, error) {
	cache := newEligibilityCache()
	if err := clistate.LoadJSON(EligibilityPath(), cache); err != nil {
		if os.IsNotExist(err) {
			return cache, nil
		}
		return nil, err
	}
	cache.ensure()
	return cache, nil
}

func (c *EligibilityCache) PutPositive(ctx Context, resp client.RepoEligibilityResponse, now time.Time) {
	if c == nil {
		return
	}
	c.ensure()
	n := ctx.Normalized()
	repoKey := strings.TrimSpace(resp.RepoKey)
	if repoKey == "" {
		repoKey = n.RepoKey
	}
	rec := EligibilityRecord{
		Eligible:       resp.Eligible,
		ServerURL:      n.ServerURL,
		AuthSubject:    n.AuthSubject,
		RepoConfigID:   resp.RepoConfigID,
		RepoKey:        repoKey,
		FullName:       strings.TrimSpace(resp.FullName),
		CloneURL:       strings.TrimSpace(resp.CloneURL),
		Status:         strings.TrimSpace(resp.Status),
		BindingState:   strings.TrimSpace(resp.BindingState),
		Reason:         strings.TrimSpace(resp.Reason),
		LastResolvedAt: now,
		LastObservedAt: now,
		ExpiresAt:      now.Add(positiveEligibilityTTL),
	}
	c.Repos[Context{ServerURL: n.ServerURL, AuthSubject: n.AuthSubject, RepoKey: repoKey}.CacheKey()] = rec
	c.UpdatedAt = now
}

func (c *EligibilityCache) PutNegative(ctx Context, remoteURL, reason string, now time.Time) {
	if c == nil {
		return
	}
	c.ensure()
	n := ctx.Normalized()
	rec := EligibilityRecord{
		Eligible:       false,
		ServerURL:      n.ServerURL,
		AuthSubject:    n.AuthSubject,
		RepoKey:        n.RepoKey,
		CloneURL:       strings.TrimSpace(remoteURL),
		Reason:         strings.TrimSpace(reason),
		LastResolvedAt: now,
		LastObservedAt: now,
		ExpiresAt:      now.Add(negativeEligibilityTTL),
	}
	c.Negative[n.CacheKey()] = rec
	c.UpdatedAt = now
}

func (c *EligibilityCache) Lookup(ctx Context, now time.Time, hasUsableCredential bool) (EligibilityRecord, bool) {
	if c == nil || !hasUsableCredential {
		return EligibilityRecord{}, false
	}
	c.ensure()
	n := ctx.Normalized()
	key := n.CacheKey()
	if rec, ok := c.Repos[key]; ok {
		if eligibilityRecordMatches(rec, n) && rec.Eligible && rec.RepoConfigID > 0 && !expired(rec.ExpiresAt, now) {
			return rec, true
		}
	}
	if rec, ok := c.Negative[key]; ok {
		if eligibilityRecordMatches(rec, n) && !expired(rec.ExpiresAt, now) {
			return rec, true
		}
	}
	return EligibilityRecord{}, false
}

func (c *EligibilityCache) Save() error {
	if c == nil {
		return nil
	}
	c.ensure()
	return clistate.SaveJSON(EligibilityPath(), c)
}

func newEligibilityCache() *EligibilityCache {
	cache := &EligibilityCache{}
	cache.ensure()
	return cache
}

func (c *EligibilityCache) ensure() {
	if c.Version == 0 {
		c.Version = eligibilityCacheVersion
	}
	if c.EligibilityVersion == "" {
		c.EligibilityVersion = client.RepoEligibilityVersion
	}
	if c.Repos == nil {
		c.Repos = map[string]EligibilityRecord{}
	}
	if c.Negative == nil {
		c.Negative = map[string]EligibilityRecord{}
	}
}

func eligibilityRecordMatches(rec EligibilityRecord, ctx Context) bool {
	return NormalizeServerURL(rec.ServerURL) == ctx.ServerURL &&
		strings.TrimSpace(rec.AuthSubject) == ctx.AuthSubject &&
		strings.TrimSpace(rec.RepoKey) == ctx.RepoKey
}

func expired(expiresAt, now time.Time) bool {
	return !expiresAt.IsZero() && !now.Before(expiresAt)
}
