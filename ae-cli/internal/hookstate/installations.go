package hookstate

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/clistate"
)

const CurrentHookTemplateVersion = 3

type InstallationRecord struct {
	Mode            string    `json:"mode"`
	RepoKey         string    `json:"repo_key,omitempty"`
	GitDir          string    `json:"git_dir,omitempty"`
	GitCommonDir    string    `json:"git_common_dir,omitempty"`
	ConfigScope     string    `json:"config_scope,omitempty"`
	HooksPath       string    `json:"hooks_path"`
	Enabled         bool      `json:"enabled"`
	TemplateVersion int       `json:"template_version"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Installations struct {
	Version   int                  `json:"version"`
	UpdatedAt time.Time            `json:"updated_at"`
	Records   []InstallationRecord `json:"records"`
}

func InstallationsPath() string {
	return filepath.Join(clistate.HooksStateDir(), "installations.json")
}

func LoadInstallations() (*Installations, error) {
	registry := newInstallations()
	if err := clistate.LoadJSON(InstallationsPath(), registry); err != nil {
		if os.IsNotExist(err) {
			return registry, nil
		}
		return nil, err
	}
	registry.ensure()
	return registry, nil
}

func (i *Installations) Upsert(rec InstallationRecord) {
	if i == nil {
		return
	}
	i.ensure()
	rec = normalizeInstallation(rec)
	for idx, existing := range i.Records {
		if installationIdentity(existing) == installationIdentity(rec) {
			i.Records[idx] = rec
			i.UpdatedAt = rec.UpdatedAt
			return
		}
	}
	i.Records = append(i.Records, rec)
	i.UpdatedAt = rec.UpdatedAt
}

func (i *Installations) Disable(match InstallationRecord, now time.Time) bool {
	if i == nil {
		return false
	}
	i.ensure()
	identity := installationIdentity(normalizeInstallation(match))
	for idx, existing := range i.Records {
		if installationIdentity(existing) != identity {
			continue
		}
		i.Records[idx].Enabled = false
		i.Records[idx].UpdatedAt = now
		i.UpdatedAt = now
		return true
	}
	return false
}

func (i *Installations) Find(match InstallationRecord) (InstallationRecord, bool) {
	if i == nil {
		return InstallationRecord{}, false
	}
	i.ensure()
	identity := installationIdentity(normalizeInstallation(match))
	for _, existing := range i.Records {
		if installationIdentity(existing) == identity {
			return existing, true
		}
	}
	return InstallationRecord{}, false
}

func (i *Installations) Save() error {
	if i == nil {
		return nil
	}
	i.ensure()
	return clistate.SaveJSON(InstallationsPath(), i)
}

func newInstallations() *Installations {
	registry := &Installations{}
	registry.ensure()
	return registry
}

func (i *Installations) ensure() {
	if i.Version == 0 {
		i.Version = 1
	}
	if i.Records == nil {
		i.Records = []InstallationRecord{}
	}
}

func normalizeInstallation(rec InstallationRecord) InstallationRecord {
	rec.Mode = strings.TrimSpace(rec.Mode)
	rec.RepoKey = strings.TrimSpace(rec.RepoKey)
	rec.GitDir = cleanPhysicalPath(rec.GitDir)
	rec.GitCommonDir = cleanPhysicalPath(rec.GitCommonDir)
	rec.ConfigScope = strings.TrimSpace(rec.ConfigScope)
	rec.HooksPath = cleanPhysicalPath(rec.HooksPath)
	return rec
}

func installationIdentity(rec InstallationRecord) string {
	rec = normalizeInstallation(rec)
	switch rec.Mode {
	case "global":
		return "global:" + rec.Mode
	case "worktree":
		return "worktree:" + rec.Mode + "\x1f" + rec.GitDir + "\x1f" + rec.ConfigScope + "\x1f" + rec.HooksPath
	default:
		return "local:" + rec.Mode + "\x1f" + rec.GitCommonDir + "\x1f" + rec.ConfigScope + "\x1f" + rec.HooksPath
	}
}

func cleanPhysicalPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if resolvedDir, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Clean(filepath.Join(resolvedDir, base))
	}
	return path
}
