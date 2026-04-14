package credential

import (
	"context"
	"testing"

	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestBackfillLegacySCMCredentials(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()

	key := "0000000000000000000000000000000000000000000000000000000000000000"
	encrypted, err := pkg.Encrypt(`{"token":"ghp_test"}`, key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	legacy := client.ScmProvider.Create().
		SetName("legacy-gh").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://api.github.com").
		SetCredentials(encrypted).
		SaveX(ctx)

	result, err := BackfillLegacySCMCredentials(ctx, client, key)
	if err != nil {
		t.Fatalf("BackfillLegacySCMCredentials: %v", err)
	}
	if result.Migrated != 1 {
		t.Fatalf("migrated = %d, want 1", result.Migrated)
	}

	updated := client.ScmProvider.GetX(ctx, legacy.ID)
	if updated.APICredentialID == 0 {
		t.Fatal("expected api_credential_id to be populated")
	}
	if updated.CloneProtocol != scmprovider.CloneProtocolHTTPS {
		t.Fatalf("clone_protocol = %s", updated.CloneProtocol)
	}

	cred := client.Credential.GetX(ctx, updated.APICredentialID)
	if cred.Kind != entcredential.KindSecretText {
		t.Fatalf("kind = %s", cred.Kind)
	}
}

func TestBackfillLegacySCMCredentialsSkipsUndecryptableRows(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()

	legacy := client.ScmProvider.Create().
		SetName("legacy-gh").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://api.github.com").
		SetCredentials("1413a8ca32c3005147c13d8f5709461a390482d18b5bc494dd5e5c2f8266ed455e64916c3a1a2e82").
		SaveX(ctx)

	key := "0000000000000000000000000000000000000000000000000000000000000000"
	result, err := BackfillLegacySCMCredentials(ctx, client, key)
	if err != nil {
		t.Fatalf("BackfillLegacySCMCredentials: %v", err)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("skipped = %v, want 1 entry", result.Skipped)
	}

	updated := client.ScmProvider.GetX(ctx, legacy.ID)
	if updated.APICredentialID != 0 {
		t.Fatalf("api_credential_id = %d, want 0 for skipped legacy row", updated.APICredentialID)
	}
}
