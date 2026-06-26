package directorysync

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/internal/credential"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
)

type EntCredentialResolver struct {
	client        *ent.Client
	encryptionKey string
}

func NewEntCredentialResolver(client *ent.Client, encryptionKey string) *EntCredentialResolver {
	return &EntCredentialResolver{client: client, encryptionKey: strings.TrimSpace(encryptionKey)}
}

func (r *EntCredentialResolver) ResolveCredential(ctx context.Context, ref string) (string, bool, error) {
	ref = strings.TrimSpace(ref)
	if r == nil || r.client == nil || ref == "" {
		return "", false, nil
	}
	row, err := r.client.Credential.Query().
		Where(entcredential.NameEQ(ref)).
		Order(ent.Asc(entcredential.FieldID)).
		First(ctx)
	if ent.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	raw, err := pkg.Decrypt(row.Payload, r.encryptionKey)
	if err != nil {
		return "", true, fmt.Errorf("decrypt credential %q: %w", ref, err)
	}
	payload, err := credential.ParsePayload(credential.Kind(row.Kind), json.RawMessage(raw))
	if err != nil {
		return "", true, err
	}
	secret, err := credential.ResolveAPISecret(payload)
	if err != nil {
		return "", true, err
	}
	return secret, true, nil
}

type RelayProviderResolver interface {
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
}

type ProviderRelayDisablerResolver struct {
	resolver RelayProviderResolver
}

func NewProviderRelayDisablerResolver(resolver RelayProviderResolver) *ProviderRelayDisablerResolver {
	return &ProviderRelayDisablerResolver{resolver: resolver}
}

func (r *ProviderRelayDisablerResolver) ResolveRelayDisabler(ctx context.Context, providerID int) (relay.UserDisabler, error) {
	if r == nil || r.resolver == nil {
		return nil, nil
	}
	provider, err := r.resolver.Resolve(ctx, providerID)
	if err != nil {
		return nil, err
	}
	disabler, ok := provider.(relay.UserDisabler)
	if !ok {
		return nil, nil
	}
	return disabler, nil
}
