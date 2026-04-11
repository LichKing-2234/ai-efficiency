package deployment

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemdBinaryUpdaterApplyAndRollback(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "opt")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	current := filepath.Join(installDir, "ai-efficiency-server")
	if err := os.WriteFile(current, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile current: %v", err)
	}

	archive := filepath.Join(root, "bundle.tar.gz")
	writeTestArchive(t, archive, map[string]string{
		"ai-efficiency-server": "new-binary",
	})
	checksums := filepath.Join(root, "checksums.txt")
	writeChecksumFile(t, checksums, archive)

	updater := NewSystemdBinaryUpdater(SystemdBinaryConfig{
		InstallDir:  installDir,
		BinaryName:  "ai-efficiency-server",
		BackupName:  "ai-efficiency-server.backup",
		DownloadDir: filepath.Join(root, "tmp"),
	})

	result, err := updater.ApplyArchive(context.Background(), archive, checksums)
	if err != nil {
		t.Fatalf("ApplyArchive: %v", err)
	}
	if !result.NeedRestart {
		t.Fatalf("NeedRestart = false, want true")
	}

	data, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("ReadFile current: %v", err)
	}
	if string(data) != "new-binary" {
		t.Fatalf("current = %q, want new-binary", string(data))
	}

	if err := updater.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	data, err = os.ReadFile(current)
	if err != nil {
		t.Fatalf("ReadFile current after rollback: %v", err)
	}
	if string(data) != "old-binary" {
		t.Fatalf("current after rollback = %q, want old-binary", string(data))
	}
}

func TestSystemdBinaryUpdaterRejectsChecksumMismatch(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "opt")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	current := filepath.Join(installDir, "ai-efficiency-server")
	if err := os.WriteFile(current, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile current: %v", err)
	}

	archive := filepath.Join(root, "bundle.tar.gz")
	writeTestArchive(t, archive, map[string]string{
		"ai-efficiency-server": "new-binary",
	})
	checksums := filepath.Join(root, "checksums.txt")
	if err := os.WriteFile(checksums, []byte("deadbeef  bundle.tar.gz\n"), 0o644); err != nil {
		t.Fatalf("WriteFile checksums: %v", err)
	}

	updater := NewSystemdBinaryUpdater(SystemdBinaryConfig{
		InstallDir: installDir,
		BinaryName: "ai-efficiency-server",
		BackupName: "ai-efficiency-server.backup",
	})

	if _, err := updater.ApplyArchive(context.Background(), archive, checksums); err == nil {
		t.Fatal("expected checksum mismatch error")
	}

	data, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("ReadFile current: %v", err)
	}
	if string(data) != "old-binary" {
		t.Fatalf("current = %q, want old-binary", string(data))
	}
}

func TestSystemdBinaryUpdaterApplyReleaseDownloadsAndApplies(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "opt")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	current := filepath.Join(installDir, "ai-efficiency-server")
	if err := os.WriteFile(current, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile current: %v", err)
	}

	archivePath := filepath.Join(root, "bundle.tar.gz")
	writeTestArchive(t, archivePath, map[string]string{
		"ai-efficiency-server": "new-binary",
	})
	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("ReadFile archive: %v", err)
	}
	sum := sha256.Sum256(archiveBytes)
	checksumBody := fmt.Sprintf("%s  bundle.tar.gz\n", hex.EncodeToString(sum[:]))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bundle.tar.gz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(archiveBytes)
		case "/checksums.txt":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(checksumBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	updater := NewSystemdBinaryUpdater(SystemdBinaryConfig{
		InstallDir:  installDir,
		BinaryName:  "ai-efficiency-server",
		BackupName:  "ai-efficiency-server.backup",
		DownloadDir: filepath.Join(root, "downloads"),
	})

	result, err := updater.ApplyRelease(context.Background(), srv.URL+"/bundle.tar.gz", srv.URL+"/checksums.txt")
	if err != nil {
		t.Fatalf("ApplyRelease: %v", err)
	}
	if !result.NeedRestart {
		t.Fatalf("NeedRestart = false, want true")
	}

	data, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("ReadFile current: %v", err)
	}
	if string(data) != "new-binary" {
		t.Fatalf("current = %q, want new-binary", string(data))
	}
	if _, err := os.Stat(filepath.Join(root, "downloads", "bundle.tar.gz")); !os.IsNotExist(err) {
		t.Fatalf("expected downloaded archive to be removed, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "downloads", "checksums.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected downloaded checksums to be removed, got err=%v", err)
	}
}

func TestSystemdBinaryUpdaterApplyArchiveAllowsMissingCurrentBinary(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "opt")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	archive := filepath.Join(root, "bundle.tar.gz")
	writeTestArchive(t, archive, map[string]string{
		"ai-efficiency-server": "new-binary",
	})
	checksums := filepath.Join(root, "checksums.txt")
	writeChecksumFile(t, checksums, archive)

	updater := NewSystemdBinaryUpdater(SystemdBinaryConfig{
		InstallDir: installDir,
		BinaryName: "ai-efficiency-server",
		BackupName: "ai-efficiency-server.backup",
	})

	result, err := updater.ApplyArchive(context.Background(), archive, checksums)
	if err != nil {
		t.Fatalf("ApplyArchive: %v", err)
	}
	if !result.NeedRestart {
		t.Fatalf("NeedRestart = false, want true")
	}
	data, err := os.ReadFile(filepath.Join(installDir, "ai-efficiency-server"))
	if err != nil {
		t.Fatalf("ReadFile current: %v", err)
	}
	if string(data) != "new-binary" {
		t.Fatalf("current = %q, want new-binary", string(data))
	}
	if _, err := os.Stat(filepath.Join(installDir, "ai-efficiency-server.backup")); !os.IsNotExist(err) {
		t.Fatalf("expected no backup for first install, got err=%v", err)
	}
}

func TestSystemdBinaryUpdaterRejectsUnsafeBackupName(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "opt")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	archive := filepath.Join(root, "bundle.tar.gz")
	writeTestArchive(t, archive, map[string]string{
		"ai-efficiency-server": "new-binary",
	})
	checksums := filepath.Join(root, "checksums.txt")
	writeChecksumFile(t, checksums, archive)

	updater := NewSystemdBinaryUpdater(SystemdBinaryConfig{
		InstallDir: installDir,
		BinaryName: "ai-efficiency-server",
		BackupName: "../bad",
	})

	if _, err := updater.ApplyArchive(context.Background(), archive, checksums); err == nil {
		t.Fatal("expected invalid backup name error")
	}
}

func writeTestArchive(t *testing.T, archivePath string, files map[string]string) {
	t.Helper()

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Create archive: %v", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	for name, data := range files {
		payload := []byte(data)
		hdr := &tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(payload)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader %s: %v", name, err)
		}
		if _, err := tw.Write(payload); err != nil {
			t.Fatalf("Write %s: %v", name, err)
		}
	}
}

func writeChecksumFile(t *testing.T, checksumPath, archivePath string) {
	t.Helper()

	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("ReadFile archive: %v", err)
	}
	sum := sha256.Sum256(data)
	line := hex.EncodeToString(sum[:]) + "  " + filepath.Base(archivePath) + "\n"
	if err := os.WriteFile(checksumPath, []byte(line), 0o644); err != nil {
		t.Fatalf("WriteFile checksum: %v", err)
	}
}
