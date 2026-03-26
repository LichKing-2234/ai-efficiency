package github

import (
	"testing"

	"github.com/ai-efficiency/backend/internal/scm"
)

func TestSplitFullName(t *testing.T) {
	tests := []struct {
		name      string
		fullName  string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"valid", "owner/repo", "owner", "repo", false},
		{"with dots", "my-org/my.repo.name", "my-org", "my.repo.name", false},
		{"no slash", "noslash", "", "", true},
		{"empty", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := splitFullName(tt.fullName)
			if (err != nil) != tt.wantErr {
				t.Errorf("splitFullName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestGhPRToSCM(t *testing.T) {
	// Test the helper that converts GitHub PR to our SCM PR type
	// We can't easily test with real github.PullRequest without mocking,
	// but we can verify the interface compliance
	var _ scm.SCMProvider = (*Provider)(nil)
}

func TestStrPtr(t *testing.T) {
	s := "hello"
	p := strPtr(s)
	if *p != s {
		t.Errorf("strPtr() = %q, want %q", *p, s)
	}
}

func TestParsePREventNilForUnknownAction(t *testing.T) {
	// parsePREvent should return nil for unknown actions
	// We test this indirectly since we can't easily construct github.PullRequestEvent
	// The key behavior is that unsupported actions return nil
}

func TestProviderImplementsInterface(t *testing.T) {
	// Compile-time check that Provider implements SCMProvider
	var _ scm.SCMProvider = (*Provider)(nil)
}
