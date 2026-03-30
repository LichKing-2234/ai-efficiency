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
// Spec formula uses repo_config_id. In the Task 5 ae-cli hook slice we use a stable local scope
// identifier (workspace_id) so queue/upload share the same id across retries.
func CheckpointEventID(scopeID, commitSHA string) (string, error) {
	scopeID = strings.TrimSpace(scopeID)
	commitSHA = strings.TrimSpace(commitSHA)
	if scopeID == "" {
		return "", fmt.Errorf("scope_id is required")
	}
	if commitSHA == "" {
		return "", fmt.Errorf("commit_sha is required")
	}
	return sha256Hex("checkpoint\x1f" + scopeID + "\x1f" + commitSHA), nil
}

// RewriteEventID returns the stable idempotency key for a post-rewrite event.
func RewriteEventID(scopeID, oldCommitSHA, newCommitSHA, rewriteType string) (string, error) {
	scopeID = strings.TrimSpace(scopeID)
	oldCommitSHA = strings.TrimSpace(oldCommitSHA)
	newCommitSHA = strings.TrimSpace(newCommitSHA)
	rewriteType = strings.TrimSpace(rewriteType)
	if scopeID == "" {
		return "", fmt.Errorf("scope_id is required")
	}
	if oldCommitSHA == "" || newCommitSHA == "" {
		return "", fmt.Errorf("old/new commit sha are required")
	}
	if rewriteType == "" {
		return "", fmt.Errorf("rewrite_type is required")
	}
	return sha256Hex("rewrite\x1f" + scopeID + "\x1f" + oldCommitSHA + "\x1f" + newCommitSHA + "\x1f" + rewriteType), nil
}

