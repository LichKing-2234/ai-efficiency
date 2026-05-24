package auth_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/auth"
)

func TestWriteAndReadToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")

	token := &auth.TokenFile{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
		ServerURL:    "http://localhost:8081",
	}

	if err := auth.WriteToken(path, token); err != nil {
		t.Fatalf("WriteToken failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected 0600 permissions, got %o", info.Mode().Perm())
	}

	got, err := auth.ReadToken(path)
	if err != nil {
		t.Fatalf("ReadToken failed: %v", err)
	}
	if got.AccessToken != token.AccessToken {
		t.Fatalf("access token mismatch: %s != %s", got.AccessToken, token.AccessToken)
	}
}

func TestReadTokenNotFound(t *testing.T) {
	_, err := auth.ReadToken("/nonexistent/token.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestTokenIsValid(t *testing.T) {
	valid := &auth.TokenFile{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	if !valid.IsValid() {
		t.Fatal("expected valid token")
	}

	expired := &auth.TokenFile{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
	}
	if expired.IsValid() {
		t.Fatal("expected expired token to be invalid")
	}
}

func TestTokenNeedsRefresh(t *testing.T) {
	soon := &auth.TokenFile{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(3 * time.Minute),
	}
	if !soon.NeedsRefresh() {
		t.Fatal("expected token expiring in 3min to need refresh")
	}

	later := &auth.TokenFile{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	if later.NeedsRefresh() {
		t.Fatal("expected token expiring in 1h to not need refresh")
	}
}

func TestSubjectFromAccessTokenUserID(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"user_id":123,"type":"access"}`))
	token := "header." + payload + ".signature"
	if got := auth.SubjectFromAccessToken(token); got != "user:123" {
		t.Fatalf("subject = %q", got)
	}
}

func TestStableAuthSubjectPrefersPersistedSubject(t *testing.T) {
	tf := &auth.TokenFile{AccessToken: "bad", AuthSubject: "user:456"}
	if got := tf.StableAuthSubject(); got != "user:456" {
		t.Fatalf("subject = %q", got)
	}
}
