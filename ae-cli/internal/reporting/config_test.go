package reporting

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateKeepsStableInstallationAndPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "reporting.json")
	first, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.InstallationID == "" || first.InstallationID != second.InstallationID {
		t.Fatalf("installation ids = %q/%q", first.InstallationID, second.InstallationID)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}
