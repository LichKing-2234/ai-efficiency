package scm

import (
	"testing"

	entscmprovider "github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/credential"
)

func TestBuildHTTPSCloneConfigFromSecretText(t *testing.T) {
	cfg, err := BuildCloneAuthConfig(
		entscmprovider.TypeGithub,
		"https",
		credential.SecretTextPayload{Text: "ghp_test"},
		nil,
	)
	if err != nil {
		t.Fatalf("BuildCloneAuthConfig: %v", err)
	}
	if cfg.HTTPSUsername != "x-access-token" {
		t.Fatalf("username = %q", cfg.HTTPSUsername)
	}
	if cfg.HTTPSPassword != "ghp_test" {
		t.Fatalf("password = %q", cfg.HTTPSPassword)
	}
}

func TestBuildSSHCloneConfigRequiresPrivateKey(t *testing.T) {
	_, err := BuildCloneAuthConfig(
		entscmprovider.TypeBitbucketServer,
		"ssh",
		credential.SecretTextPayload{Text: "token"},
		nil,
	)
	if err == nil {
		t.Fatal("expected missing ssh clone credential to fail")
	}
}
