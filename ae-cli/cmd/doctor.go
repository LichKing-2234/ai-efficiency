package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Inspect sessionless attribution readiness for the current repo",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, err := detectAttributionContext()
		if err != nil {
			return err
		}
		configToken := ""
		if cfg != nil {
			configToken = cfg.Server.Token
		}
		token := resolveToken(configToken, "")
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Sessionless attribution doctor\n")
		fmt.Fprintf(out, "  Repo:          %s\n", ctx.repoRoot)
		fmt.Fprintf(out, "  Workspace ID:  %s\n", ctx.workspaceID)
		fmt.Fprintf(out, "  Git Dir:       %s\n", ctx.gitDir)
		fmt.Fprintf(out, "  Git Common:    %s\n", ctx.gitCommonDir)
		fmt.Fprintf(out, "  State Dir:     %s\n", ctx.attributionRoot)
		fmt.Fprintf(out, "  Logged In:     %t\n", token != "")
		fmt.Fprintf(out, "  Hooks Ready:   %t\n", hooksInstalled(ctx.gitCommonDir))
		if _, err := os.Stat(ctx.attributionRoot); err == nil {
			fmt.Fprintf(out, "  State Exists:  true\n")
		} else if os.IsNotExist(err) {
			fmt.Fprintf(out, "  State Exists:  false\n")
		} else {
			return fmt.Errorf("stat attribution state dir: %w", err)
		}
		return nil
	},
}

func hooksInstalled(gitCommonDir string) bool {
	postCommit, err := os.ReadFile(filepath.Join(gitCommonDir, "hooks", "post-commit"))
	if err != nil {
		return false
	}
	postRewrite, err := os.ReadFile(filepath.Join(gitCommonDir, "hooks", "post-rewrite"))
	if err != nil {
		return false
	}
	return strings.Contains(string(postCommit), "ae-cli hook post-commit") &&
		strings.Contains(string(postRewrite), "ae-cli hook post-rewrite")
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
