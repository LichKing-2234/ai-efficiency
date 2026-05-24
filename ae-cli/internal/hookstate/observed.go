package hookstate

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/clistate"
)

type ObservedRepoRecord struct {
	RepoKey         string    `json:"repo_key"`
	ServerURL       string    `json:"server_url,omitempty"`
	AuthSubject     string    `json:"auth_subject,omitempty"`
	RemoteURL       string    `json:"remote_url"`
	FirstObservedAt time.Time `json:"first_observed_at"`
	LastObservedAt  time.Time `json:"last_observed_at"`
}

type ObservedRepos struct {
	Version   int                           `json:"version"`
	UpdatedAt time.Time                     `json:"updated_at"`
	Repos     map[string]ObservedRepoRecord `json:"repos"`
}

func ObservedPath() string {
	return filepath.Join(clistate.HooksStateDir(), "observed-repos.json")
}

func LoadObservedRepos() (*ObservedRepos, error) {
	observed := newObservedRepos()
	if err := clistate.LoadJSON(ObservedPath(), observed); err != nil {
		if os.IsNotExist(err) {
			return observed, nil
		}
		return nil, err
	}
	observed.ensure()
	return observed, nil
}

func (o *ObservedRepos) Observe(ctx Context, remoteURL string, now time.Time) {
	if o == nil {
		return
	}
	o.ensure()
	n := ctx.Normalized()
	if strings.TrimSpace(n.RepoKey) == "" {
		return
	}
	key := observedKey(n)
	rec := o.Repos[key]
	if rec.FirstObservedAt.IsZero() {
		rec.FirstObservedAt = now
	}
	rec.LastObservedAt = now
	rec.RepoKey = n.RepoKey
	rec.RemoteURL = strings.TrimSpace(remoteURL)
	if n.Stable() {
		rec.ServerURL = n.ServerURL
		rec.AuthSubject = n.AuthSubject
	}
	o.Repos[key] = rec
	o.UpdatedAt = now
}

func (o *ObservedRepos) Matching(ctx Context) []ObservedRepoRecord {
	if o == nil {
		return nil
	}
	o.ensure()
	n := ctx.Normalized()
	var out []ObservedRepoRecord
	if rec, ok := o.Repos[observedKey(n)]; ok {
		out = append(out, rec)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastObservedAt.After(out[j].LastObservedAt)
	})
	return out
}

func (o *ObservedRepos) Save() error {
	if o == nil {
		return nil
	}
	o.ensure()
	return clistate.SaveJSON(ObservedPath(), o)
}

func newObservedRepos() *ObservedRepos {
	observed := &ObservedRepos{}
	observed.ensure()
	return observed
}

func (o *ObservedRepos) ensure() {
	if o.Version == 0 {
		o.Version = 1
	}
	if o.Repos == nil {
		o.Repos = map[string]ObservedRepoRecord{}
	}
}

func observedKey(ctx Context) string {
	if ctx.Stable() {
		return ctx.CacheKey()
	}
	return "unbound:" + strings.TrimSpace(ctx.RepoKey)
}
