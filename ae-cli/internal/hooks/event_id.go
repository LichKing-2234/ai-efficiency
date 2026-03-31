package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// CheckpointEventID returns the stable idempotency key for a post-commit checkpoint event.
//
// The design formula uses repo_config_id. On the ae-cli side we use a stable repo hint
// (marker repo_full_name or origin URL) so retries and later HTTP ingestion share the same key.
func CheckpointEventID(repoHint, commitSHA string) (string, error) {
	repoHint = strings.TrimSpace(repoHint)
	commitSHA = strings.TrimSpace(commitSHA)
	if repoHint == "" {
		return "", fmt.Errorf("repo_hint is required")
	}
	if commitSHA == "" {
		return "", fmt.Errorf("commit_sha is required")
	}
	return sha256Hex("checkpoint\x1f" + repoHint + "\x1f" + commitSHA), nil
}

// RewriteEventID returns the stable idempotency key for a post-rewrite event.
func RewriteEventID(repoHint, oldCommitSHA, newCommitSHA, rewriteType string) (string, error) {
	repoHint = strings.TrimSpace(repoHint)
	oldCommitSHA = strings.TrimSpace(oldCommitSHA)
	newCommitSHA = strings.TrimSpace(newCommitSHA)
	rewriteType = strings.TrimSpace(rewriteType)
	if repoHint == "" {
		return "", fmt.Errorf("repo_hint is required")
	}
	if oldCommitSHA == "" || newCommitSHA == "" {
		return "", fmt.Errorf("old/new commit sha are required")
	}
	if rewriteType == "" {
		return "", fmt.Errorf("rewrite_type is required")
	}
	return sha256Hex("rewrite\x1f" + repoHint + "\x1f" + oldCommitSHA + "\x1f" + newCommitSHA + "\x1f" + rewriteType), nil
}
