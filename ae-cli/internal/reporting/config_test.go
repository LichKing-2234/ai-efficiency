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

func TestLoadOrCreateRecoversInstallationAfterCredentialConfigIsDeleted(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(stateDir, "reporting.json")
	first, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove credential config: %v", err)
	}

	recovered, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.InstallationID != first.InstallationID {
		t.Fatalf("recovered installation_id = %q, want %q", recovered.InstallationID, first.InstallationID)
	}
	identityInfo, err := os.Stat(filepath.Join(stateDir, "reporting-installation.json"))
	if err != nil {
		t.Fatalf("stable installation identity: %v", err)
	}
	if identityInfo.Mode().Perm() != 0o600 {
		t.Fatalf("identity mode = %o, want 600", identityInfo.Mode().Perm())
	}
}
