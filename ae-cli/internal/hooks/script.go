package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/hookstate"
)

const ManagedHeaderPrefix = "# ae-cli-managed-hook:"

var templateVersionRE = regexp.MustCompile(`template_version=([0-9]+)`)

func RenderManagedHookScript(hookName, generatorVersion string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString(fmt.Sprintf("%s template_version=%d generator_version=%s\n", ManagedHeaderPrefix, hookstate.CurrentHookTemplateVersion, generatorVersion))
	b.WriteString(`ae_cli="${AE_CLI_BIN:-}"` + "\n")
	b.WriteString("if [ -z \"$ae_cli\" ] && [ -x \"$HOME/.local/bin/ae-cli\" ]; then\n")
	b.WriteString("  ae_cli=\"$HOME/.local/bin/ae-cli\"\n")
	b.WriteString("fi\n")
	b.WriteString("if [ -z \"$ae_cli\" ]; then\n")
	b.WriteString("  ae_cli=\"$(command -v ae-cli 2>/dev/null || true)\"\n")
	b.WriteString("fi\n")
	b.WriteString("if [ -z \"$ae_cli\" ] || [ ! -x \"$ae_cli\" ]; then\n")
	b.WriteString("  exit 0\n")
	b.WriteString("fi\n")
	switch hookName {
	case "post-rewrite":
		b.WriteString("tmp=\"${TMPDIR:-/tmp}/ae-cli-post-rewrite.$$\"\n")
		b.WriteString("cat >\"$tmp\" || true\n")
		b.WriteString("\"$ae_cli\" hook post-rewrite \"$@\" <\"$tmp\" || true\n")
		b.WriteString("rm -f \"$tmp\"\n")
	default:
		b.WriteString("\"$ae_cli\" hook " + hookName + " \"$@\" || true\n")
	}
	return b.String()
}

func WriteManagedScripts(dir, generatorVersion string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("hooks dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create hooks dir: %w", err)
	}
	for _, hookName := range []string{"post-commit", "post-rewrite"} {
		path := filepath.Join(dir, hookName)
		if err := os.WriteFile(path, []byte(RenderManagedHookScript(hookName, generatorVersion)), 0o755); err != nil {
			return fmt.Errorf("write managed hook %s: %w", hookName, err)
		}
	}
	return nil
}

func ParseTemplateVersion(data []byte) (int, bool) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, ManagedHeaderPrefix) {
			continue
		}
		match := templateVersionRE.FindStringSubmatch(line)
		if len(match) != 2 {
			return 0, false
		}
		var version int
		if _, err := fmt.Sscanf(match[1], "%d", &version); err != nil {
			return 0, false
		}
		return version, true
	}
	return 0, false
}
