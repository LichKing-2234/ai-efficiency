package oauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/ai-efficiency/backend/internal/oauth"
)

func TestVerifyCodeChallenge(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	if !oauth.VerifyCodeChallenge(verifier, expectedChallenge, "S256") {
		t.Fatal("expected valid challenge to pass")
	}
}

func TestVerifyCodeChallengeMismatch(t *testing.T) {
	if oauth.VerifyCodeChallenge("wrong-verifier", "wrong-challenge", "S256") {
		t.Fatal("expected mismatched challenge to fail")
	}
}

func TestVerifyCodeChallengeUnsupportedMethod(t *testing.T) {
	if oauth.VerifyCodeChallenge("verifier", "challenge", "plain") {
		t.Fatal("expected unsupported method to fail")
	}
}
