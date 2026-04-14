package credential

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	entscmprovider "github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/pkg"
)

// BackfillLegacySCMCredentials migrates inline scm_provider.credentials payloads into credentials rows.
func BackfillLegacySCMCredentials(ctx context.Context, client *ent.Client, encryptionKey string) error {
	providers, err := client.ScmProvider.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("list scm providers: %w", err)
	}

	for _, provider := range providers {
		if provider.APICredentialID != 0 || provider.Credentials == "" {
			continue
		}

		decrypted, err := pkg.Decrypt(provider.Credentials, encryptionKey)
		if err != nil {
			continue
		}

		payload, err := ParseLegacySCMProviderSecret(decrypted)
		if err != nil {
			continue
		}

		rawPayload, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal secret_text payload for provider %d: %w", provider.ID, err)
		}

		encryptedPayload, err := pkg.Encrypt(string(rawPayload), encryptionKey)
		if err != nil {
			return fmt.Errorf("encrypt credential payload for provider %d: %w", provider.ID, err)
		}

		cred, err := client.Credential.Create().
			SetName(provider.Name + " API credential").
			SetDescription("Migrated from legacy scm_provider.credentials").
			SetKind(entcredential.KindSecretText).
			SetPayload(encryptedPayload).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("create credential for provider %d: %w", provider.ID, err)
		}

		if _, err := client.ScmProvider.UpdateOneID(provider.ID).
			SetAPICredentialID(cred.ID).
			SetCloneProtocol(entscmprovider.CloneProtocolHTTPS).
			Save(ctx); err != nil {
			return fmt.Errorf("update provider %d with api credential: %w", provider.ID, err)
		}
	}

	return nil
}
