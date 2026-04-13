package deployment

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyBinaryUpdateReplacesRuntimeBinaryAndCreatesBackup(t *testing.T) {
	root := t.TempDir()
	paths := RuntimeBinaryPaths(root)
	if err := os.MkdirAll(filepath.Dir(paths.RuntimeBinary), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(paths.RuntimeBinary, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("write runtime: %v", err)
	}
	newBinary := filepath.Join(root, "new-binary")
	if err := os.WriteFile(newBinary, []byte("new-binary"), 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	if err := ApplyBinarySwap(context.Background(), paths, newBinary); err != nil {
		t.Fatalf("ApplyBinarySwap: %v", err)
	}
	got, err := os.ReadFile(paths.RuntimeBinary)
	if err != nil {
		t.Fatalf("read runtime binary: %v", err)
	}
	if string(got) != "new-binary" {
		t.Fatalf("runtime binary = %q", string(got))
	}
	backup, err := os.ReadFile(paths.BackupBinary)
	if err != nil {
		t.Fatalf("read backup binary: %v", err)
	}
	if string(backup) != "old-binary" {
		t.Fatalf("backup binary = %q", string(backup))
	}
}
