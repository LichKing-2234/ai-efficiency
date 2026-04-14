package credential

import (
	"encoding/json"
	"testing"
)

func TestParsePayloadRejectsInvalidSSHKey(t *testing.T) {
	_, err := ParsePayload(KindSSHUsernameWithPrivateKey, json.RawMessage(`{"username":"git"}`))
	if err == nil {
		t.Fatal("expected missing private_key to fail")
	}
}

func TestParsePayloadAcceptsSecretText(t *testing.T) {
	got, err := ParsePayload(KindSecretText, json.RawMessage(`{"text":"ghp_test"}`))
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if got.Kind() != KindSecretText {
		t.Fatalf("kind = %s", got.Kind())
	}
}

func TestValidateProviderCredentialRefs(t *testing.T) {
	if err := ValidateProviderCredentialRefs(KindSecretText, "ssh", KindSecretText); err == nil {
		t.Fatal("expected ssh clone to reject secret_text clone credential")
	}
	if err := ValidateProviderCredentialRefs(KindSecretText, "ssh", KindSSHUsernameWithPrivateKey); err != nil {
		t.Fatalf("ValidateProviderCredentialRefs: %v", err)
	}
}
