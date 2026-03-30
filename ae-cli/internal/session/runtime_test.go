package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteRuntimeBundleUsesRestrictedPermissions(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	b := &RuntimeBundle{
		SessionID:   "sess-1",
		RuntimeRef:  "rt-1",
		EnvBundle: map[string]string{
			"AE_SESSION_ID": "sess-1",
		},
		KeyExpiresAt: time.Now().UTC().Truncate(time.Second).Add(1 * time.Hour),
	}

	if err := WriteRuntimeBundle(b); err != nil {
		t.Fatalf("WriteRuntimeBundle: %v", err)
	}

	d := runtimeDir(b.SessionID)
	dirInfo, err := os.Stat(d)
	if err != nil {
		t.Fatalf("stat runtime dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("runtime dir perms = %o, want %o", dirInfo.Mode().Perm(), 0o700)
	}

	f := filepath.Join(d, "runtime.json")
	fileInfo, err := os.Stat(f)
	if err != nil {
		t.Fatalf("stat runtime file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("runtime file perms = %o, want %o", fileInfo.Mode().Perm(), 0o600)
	}

	collectorsInfo, err := os.Stat(filepath.Join(d, "collectors"))
	if err != nil {
		t.Fatalf("stat collectors dir: %v", err)
	}
	if collectorsInfo.Mode().Perm() != 0o700 {
		t.Fatalf("collectors dir perms = %o, want %o", collectorsInfo.Mode().Perm(), 0o700)
	}
}
