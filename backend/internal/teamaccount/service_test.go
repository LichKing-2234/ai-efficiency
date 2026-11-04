package teamaccount

import (
	"strings"
	"testing"
)

func TestDeriveUsernameNormalizesDepartmentID(t *testing.T) {
	cases := map[string]string{
		"department-alpha":    "team-department-alpha",
		"Department/Alpha 01": "team-department-alpha-01",
		"部门-01":               "team-01",
		"":                    "team-dept",
	}
	for input, want := range cases {
		if got := deriveUsername(input); got != want {
			t.Errorf("deriveUsername(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDeriveUsernameCapsLength(t *testing.T) {
	got := deriveUsername(strings.Repeat("a", 80))
	if len(got) > len(teamUsernamePrefix)+32 {
		t.Errorf("username too long: %d", len(got))
	}
}

func TestPreviewSecretKeepsEdges(t *testing.T) {
	secret := "sk-0123456789abcdef"
	got := previewSecret(secret)
	if !strings.HasPrefix(got, "sk-012") || !strings.HasSuffix(got, "cdef") {
		t.Errorf("preview = %q", got)
	}
	if strings.Contains(got, "3456789abc") {
		t.Errorf("preview leaks the middle of the secret: %q", got)
	}
}

func TestPreviewSecretShortValueUntouched(t *testing.T) {
	if got := previewSecret("short"); got != "short" {
		t.Errorf("preview = %q", got)
	}
}
