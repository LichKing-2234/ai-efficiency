package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/google/uuid"
)

func TestDeriveWorkspaceIDUsesCanonicalGitContext(t *testing.T) {
	tmp := t.TempDir()

	real := filepath.Join(tmp, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}

	realWork := filepath.Join(real, "work")
	realGitDir := filepath.Join(real, "gitdir")
	realCommon := filepath.Join(real, "commondir")
	for _, p := range []string{realWork, realGitDir, realCommon} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	link := filepath.Join(tmp, "link")
	if err := os.Symlink(real, link); err != nil {
		// Some platforms/filesystems may not support symlinks without extra privileges.
		t.Skipf("symlink not supported: %v", err)
	}

	workspaceRoot := filepath.Join(link, "work", "..", "work")
	gitDir := filepath.Join(link, "gitdir")
	gitCommonDir := filepath.Join(link, "commondir")

	got, err := deriveWorkspaceID(workspaceRoot, gitDir, gitCommonDir)
	if err != nil {
		t.Fatalf("deriveWorkspaceID: %v", err)
	}

	cWork, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks(workspaceRoot): %v", err)
	}
	cGitDir, err := filepath.EvalSymlinks(gitDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(gitDir): %v", err)
	}
	cCommon, err := filepath.EvalSymlinks(gitCommonDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(gitCommonDir): %v", err)
	}

	cWork, _ = filepath.Abs(filepath.Clean(cWork))
	cGitDir, _ = filepath.Abs(filepath.Clean(cGitDir))
	cCommon, _ = filepath.Abs(filepath.Clean(cCommon))

	ns := uuid.NewSHA1(uuid.NameSpaceDNS, []byte("ae-workspace"))
	name := cWork + "\x1f" + cWork + "\x1f" + cGitDir + "\x1f" + cCommon
	want := uuid.NewSHA1(ns, []byte(name)).String()

	if got != want {
		t.Fatalf("workspace_id = %q, want %q", got, want)
	}
}

func TestCurrentPrefersWorkspaceMarkerOverLegacyState(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Legacy global state file.
	legacy := &State{
		ID:        "legacy-id",
		Repo:      "legacy-repo",
		Branch:    "legacy-branch",
		StartedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := writeState(legacy); err != nil {
		t.Fatalf("writeState(legacy): %v", err)
	}

	// Workspace marker in cwd should take precedence.
	ws := t.TempDir()
	marker := &Marker{
		SessionID:    "marker-id",
		WorkspaceID:  "wsid-1",
		RuntimeRef:   "rt-1",
		ProviderName: "sub2api",
	}
	if err := WriteMarker(ws, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	oldWD, _ := os.Getwd()
	if err := os.Chdir(ws); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	mgr := NewManager(c, cfg)

	got, err := mgr.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if got == nil {
		t.Fatal("Current returned nil, expected state")
	}
	if got.ID != "marker-id" {
		t.Fatalf("Current ID = %q, want %q", got.ID, "marker-id")
	}

	// Ensure marker on disk is valid JSON for forward compatibility.
	data, err := os.ReadFile(markerPath(ws))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("marker json: %v", err)
	}
	if decoded["session_id"] != "marker-id" {
		t.Fatalf("marker session_id = %v, want %v", decoded["session_id"], "marker-id")
	}
}

