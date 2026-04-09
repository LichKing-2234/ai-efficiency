package deployment

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SystemdBinaryConfig struct {
	InstallDir  string
	BinaryName  string
	BackupName  string
	DownloadDir string
	HTTPClient  *http.Client
}

type SystemdOperationResult struct {
	Message     string `json:"message"`
	NeedRestart bool   `json:"need_restart"`
}

type SystemdBinaryUpdater struct {
	cfg    SystemdBinaryConfig
	client *http.Client
}

func NewSystemdBinaryUpdater(cfg SystemdBinaryConfig) *SystemdBinaryUpdater {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &SystemdBinaryUpdater{cfg: cfg, client: client}
}

func (u *SystemdBinaryUpdater) ApplyRelease(ctx context.Context, archiveURL, checksumsURL string) (SystemdOperationResult, error) {
	if err := u.validateConfig(); err != nil {
		return SystemdOperationResult{}, err
	}
	downloadDir := u.cfg.DownloadDir
	if downloadDir == "" {
		downloadDir = filepath.Join(u.cfg.InstallDir, ".downloads")
	}
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return SystemdOperationResult{}, fmt.Errorf("ensure download dir: %w", err)
	}

	archivePath := filepath.Join(downloadDir, filepath.Base(archiveURL))
	checksumsPath := filepath.Join(downloadDir, filepath.Base(checksumsURL))

	if err := u.downloadFile(ctx, archiveURL, archivePath); err != nil {
		return SystemdOperationResult{}, err
	}
	if err := u.downloadFile(ctx, checksumsURL, checksumsPath); err != nil {
		return SystemdOperationResult{}, err
	}

	return u.ApplyArchive(ctx, archivePath, checksumsPath)
}

func (u *SystemdBinaryUpdater) ApplyArchive(_ context.Context, archivePath, checksumsPath string) (SystemdOperationResult, error) {
	if err := u.validateConfig(); err != nil {
		return SystemdOperationResult{}, err
	}
	if err := u.verifyArchiveChecksum(archivePath, checksumsPath); err != nil {
		return SystemdOperationResult{}, err
	}

	if err := os.MkdirAll(u.cfg.InstallDir, 0o755); err != nil {
		return SystemdOperationResult{}, fmt.Errorf("ensure install dir: %w", err)
	}

	currentPath := filepath.Join(u.cfg.InstallDir, u.cfg.BinaryName)
	backupPath := u.backupPath()
	nextPath, err := u.extractBinary(archivePath)
	if err != nil {
		return SystemdOperationResult{}, err
	}
	defer os.Remove(nextPath)

	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return SystemdOperationResult{}, fmt.Errorf("remove old backup: %w", err)
	}

	if err := os.Rename(currentPath, backupPath); err != nil {
		return SystemdOperationResult{}, fmt.Errorf("backup current binary: %w", err)
	}

	if err := os.Rename(nextPath, currentPath); err != nil {
		if restoreErr := os.Rename(backupPath, currentPath); restoreErr != nil {
			return SystemdOperationResult{}, fmt.Errorf("replace binary: %w (restore failed: %v)", err, restoreErr)
		}
		return SystemdOperationResult{}, fmt.Errorf("replace binary: %w", err)
	}

	return SystemdOperationResult{
		Message:     "binary update staged",
		NeedRestart: true,
	}, nil
}

func (u *SystemdBinaryUpdater) Rollback(_ context.Context) error {
	if err := u.validateConfig(); err != nil {
		return err
	}
	currentPath := filepath.Join(u.cfg.InstallDir, u.cfg.BinaryName)
	backupPath := u.backupPath()

	if _, err := os.Stat(backupPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("backup binary not found")
		}
		return fmt.Errorf("stat backup binary: %w", err)
	}

	restorePath, err := os.CreateTemp(u.cfg.InstallDir, u.cfg.BinaryName+".rollback-*")
	if err != nil {
		return fmt.Errorf("create rollback temp file: %w", err)
	}
	_ = restorePath.Close()
	_ = os.Remove(restorePath.Name())

	if err := os.Rename(currentPath, restorePath.Name()); err != nil {
		return fmt.Errorf("move current binary aside: %w", err)
	}

	if err := os.Rename(backupPath, currentPath); err != nil {
		if restoreErr := os.Rename(restorePath.Name(), currentPath); restoreErr != nil {
			return fmt.Errorf("restore backup: %w (recover current failed: %v)", err, restoreErr)
		}
		return fmt.Errorf("restore backup: %w", err)
	}

	if err := os.Rename(restorePath.Name(), backupPath); err != nil {
		return fmt.Errorf("refresh backup from replaced binary: %w", err)
	}

	return nil
}

func (u *SystemdBinaryUpdater) backupPath() string {
	return filepath.Join(u.cfg.InstallDir, u.cfg.BackupName)
}

func (u *SystemdBinaryUpdater) validateConfig() error {
	if strings.TrimSpace(u.cfg.InstallDir) == "" {
		return fmt.Errorf("install dir is required")
	}
	for fieldName, value := range map[string]string{
		"binary name": u.cfg.BinaryName,
		"backup name": u.cfg.BackupName,
	} {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return fmt.Errorf("%s is required", fieldName)
		}
		if trimmed == "." || trimmed == ".." || trimmed != filepath.Base(trimmed) {
			return fmt.Errorf("%s must be a file name, got %q", fieldName, value)
		}
	}
	return nil
}

func (u *SystemdBinaryUpdater) verifyArchiveChecksum(archivePath, checksumsPath string) error {
	expected, err := readChecksum(checksumsPath, filepath.Base(archivePath))
	if err != nil {
		return err
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash archive: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s", filepath.Base(archivePath))
	}
	return nil
}

func (u *SystemdBinaryUpdater) extractBinary(archivePath string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("open gzip archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	tmpFile, err := os.CreateTemp(u.cfg.InstallDir, u.cfg.BinaryName+".next-*")
	if err != nil {
		return "", fmt.Errorf("create temp binary: %w", err)
	}
	defer func() {
		_ = tmpFile.Close()
	}()

	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read tar archive: %w", err)
		}
		if filepath.Base(hdr.Name) != u.cfg.BinaryName {
			continue
		}
		if _, err := io.Copy(tmpFile, tr); err != nil {
			return "", fmt.Errorf("extract binary: %w", err)
		}
		found = true
		break
	}

	if !found {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("binary %s not found in archive", u.cfg.BinaryName)
	}

	if err := tmpFile.Chmod(0o755); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("chmod temp binary: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("close temp binary: %w", err)
	}

	return tmpFile.Name(), nil
}

func readChecksum(checksumsPath, targetName string) (string, error) {
	f, err := os.Open(checksumsPath)
	if err != nil {
		return "", fmt.Errorf("open checksums: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if filepath.Base(fields[len(fields)-1]) == targetName {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan checksums: %w", err)
	}

	return "", fmt.Errorf("checksum not found for %s", targetName)
}

func (u *SystemdBinaryUpdater) downloadFile(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", filepath.Base(path), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status=%d", filepath.Base(path), resp.StatusCode)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}
