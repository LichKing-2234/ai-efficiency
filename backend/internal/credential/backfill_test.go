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

	if err := BackfillLegacySCMCredentials(ctx, client, key); err != nil {
		t.Fatalf("BackfillLegacySCMCredentials: %v", err)
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
