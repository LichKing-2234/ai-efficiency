package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeCollectorsDirUsesRuntimeRoot(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("Setenv(HOME): %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("HOME", origHome) })

	got := RuntimeCollectorsDir("sess-123")
	want := filepath.Join(tmpHome, ".ae-cli", "runtime", "sess-123", "collectors")
	if got != want {
		t.Fatalf("RuntimeCollectorsDir = %q, want %q", got, want)
	}
}
