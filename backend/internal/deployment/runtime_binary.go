package deployment

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type RuntimePaths struct {
	RuntimeDir    string
	RuntimeBinary string
	BackupBinary  string
}

func RuntimeBinaryPaths(stateDir string) RuntimePaths {
	runtimeDir := filepath.Join(stateDir, "runtime")
	return RuntimePaths{
		RuntimeDir:    runtimeDir,
		RuntimeBinary: filepath.Join(runtimeDir, "ai-efficiency-server"),
		BackupBinary:  filepath.Join(runtimeDir, "ai-efficiency-server.backup"),
	}
}

func EnsureRuntimeBinary(stateDir, bootstrapBinary, runtimeVersion, bootstrapVersion string) (string, bool, error) {
	paths := RuntimeBinaryPaths(stateDir)
	if err := os.MkdirAll(paths.RuntimeDir, 0o755); err != nil {
		return "", false, fmt.Errorf("mkdir runtime dir: %w", err)
	}

	if _, err := os.Stat(paths.RuntimeBinary); os.IsNotExist(err) {
		if err := copyExecutable(bootstrapBinary, paths.RuntimeBinary); err != nil {
			return "", false, err
		}
		return paths.RuntimeBinary, true, nil
	}

	if runtimeVersion == "" || bootstrapVersion == "" {
		return paths.RuntimeBinary, false, nil
	}
	if CompareVersions(runtimeVersion, bootstrapVersion) >= 0 {
		return paths.RuntimeBinary, false, nil
	}

	if err := copyExecutable(bootstrapBinary, paths.RuntimeBinary); err != nil {
		return "", false, err
	}
	return paths.RuntimeBinary, true, nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source binary: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create runtime binary: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy runtime binary: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close runtime binary: %w", err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return fmt.Errorf("chmod runtime binary: %w", err)
	}
	return nil
}

func CompareVersions(a, b string) int {
	parse := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		head := strings.SplitN(v, "-", 2)[0]
		parts := strings.Split(head, ".")
		out := make([]int, 0, len(parts))
		for _, part := range parts {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil
			}
			out = append(out, n)
		}
		return out
	}

	aa := parse(a)
	bb := parse(b)
	if aa == nil || bb == nil {
		return 0
	}
	maxLen := len(aa)
	if len(bb) > maxLen {
		maxLen = len(bb)
	}
	for i := 0; i < maxLen; i++ {
		av, bv := 0, 0
		if i < len(aa) {
			av = aa[i]
		}
		if i < len(bb) {
			bv = bb[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}
