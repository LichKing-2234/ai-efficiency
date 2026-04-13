package deployment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeBinaryPaths(t *testing.T) {
	root := t.TempDir()
	paths := RuntimeBinaryPaths(root)
	if paths.RuntimeBinary != filepath.Join(root, "runtime", "ai-efficiency-server") {
		t.Fatalf("runtime binary = %q", paths.RuntimeBinary)
	}
	if paths.BackupBinary != filepath.Join(root, "runtime", "ai-efficiency-server.backup") {
		t.Fatalf("backup binary = %q", paths.BackupBinary)
	}
}

func TestPreferExistingRuntimeBinaryWhenBootstrapVersionIsOlder(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runtimePath := filepath.Join(runtimeDir, "ai-efficiency-server")
	if err := os.WriteFile(runtimePath, []byte("runtime-newer"), 0o755); err != nil {
		t.Fatalf("write runtime: %v", err)
	}
	bootstrapPath := filepath.Join(root, "bootstrap-server")
	if err := os.WriteFile(bootstrapPath, []byte("bootstrap-older"), 0o755); err != nil {
		t.Fatalf("write bootstrap: %v", err)
	}

	chosen, copied, err := EnsureRuntimeBinary(root, bootstrapPath, "v0.6.0", "v0.5.0")
	if err != nil {
		t.Fatalf("EnsureRuntimeBinary: %v", err)
	}
	if chosen != runtimePath || copied {
		t.Fatalf("chosen=%q copied=%v", chosen, copied)
	}
}
