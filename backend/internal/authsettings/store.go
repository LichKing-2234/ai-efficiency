package authsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/systemsetting"
	"github.com/ai-efficiency/backend/internal/pkg"
)

type oidcDocument struct {
	Enabled            bool     `json:"enabled"`
	Label              string   `json:"label,omitempty"`
	Issuer             string   `json:"issuer,omitempty"`
	ClientID           string   `json:"client_id,omitempty"`
	ClientSecretCipher string   `json:"client_secret_cipher,omitempty"`
	RedirectURL        string   `json:"redirect_url,omitempty"`
	RedirectURLs       []string `json:"redirect_urls,omitempty"`
	AllowEmailLink     bool     `json:"allow_email_link,omitempty"`
	Scopes             []string `json:"scopes,omitempty"`
}

type ldapDocument struct {
	URL                string `json:"url,omitempty"`
	BaseDN             string `json:"base_dn,omitempty"`
	BindDN             string `json:"bind_dn,omitempty"`
	BindPasswordCipher string `json:"bind_password_cipher,omitempty"`
	UserFilter         string `json:"user_filter,omitempty"`
	TLS                bool   `json:"tls,omitempty"`
}

// Store persists administrator-managed auth settings in SystemSetting rows.
type Store struct {
	client        *ent.Client
	encryptionKey string
}

// NewStore creates an auth settings store.
func NewStore(client *ent.Client, encryptionKey string) *Store {
	return &Store{client: client, encryptionKey: encryptionKey}
}

// LoadOIDC returns the stored OIDC settings and whether a row exists.
func (s *Store) LoadOIDC(ctx context.Context) (*OIDC, bool, error) {
	raw, updatedAt, found, err := s.load(ctx, OIDCKey)
	if err != nil || !found {
		return nil, found, err
	}
	var doc oidcDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, true, fmt.Errorf("decode stored OIDC settings: %w", err)
	}
	secret, err := s.open(doc.ClientSecretCipher)
	if err != nil {
		return nil, true, err
	}
	return &OIDC{
		Enabled:        doc.Enabled,
		Label:          doc.Label,
		Issuer:         doc.Issuer,
		ClientID:       doc.ClientID,
		ClientSecret:   secret,
		RedirectURL:    doc.RedirectURL,
		RedirectURLs:   doc.RedirectURLs,
		AllowEmailLink: doc.AllowEmailLink,
		Scopes:         doc.Scopes,
		UpdatedAt:      updatedAt,
	}, true, nil
}

// SaveOIDC encrypts and stores the OIDC settings.
func (s *Store) SaveOIDC(ctx context.Context, settings OIDC) error {
	secret, err := s.seal(settings.ClientSecret)
	if err != nil {
		return err
	}
	doc := oidcDocument{
		Enabled:            settings.Enabled,
		Label:              settings.Label,
		Issuer:             settings.Issuer,
		ClientID:           settings.ClientID,
		ClientSecretCipher: secret,
		RedirectURL:        settings.RedirectURL,
		RedirectURLs:       settings.RedirectURLs,
		AllowEmailLink:     settings.AllowEmailLink,
		Scopes:             settings.Scopes,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode OIDC settings: %w", err)
	}
	return s.save(ctx, OIDCKey, string(raw))
}

// DeleteOIDC removes the stored OIDC override.
func (s *Store) DeleteOIDC(ctx context.Context) error { return s.delete(ctx, OIDCKey) }

// LoadLDAP returns the stored LDAP settings and whether a row exists.
func (s *Store) LoadLDAP(ctx context.Context) (*LDAP, bool, error) {
	raw, _, found, err := s.load(ctx, LDAPKey)
	if err != nil || !found {
		return nil, found, err
	}
	var doc ldapDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, true, fmt.Errorf("decode stored LDAP settings: %w", err)
	}
	password, err := s.open(doc.BindPasswordCipher)
	if err != nil {
		return nil, true, err
	}
	return &LDAP{
		URL:          doc.URL,
		BaseDN:       doc.BaseDN,
		BindDN:       doc.BindDN,
		BindPassword: password,
		UserFilter:   doc.UserFilter,
		TLS:          doc.TLS,
	}, true, nil
}

// SaveLDAP encrypts and stores the LDAP settings.
func (s *Store) SaveLDAP(ctx context.Context, settings LDAP) error {
	password, err := s.seal(settings.BindPassword)
	if err != nil {
		return err
	}
	doc := ldapDocument{
		URL:                settings.URL,
		BaseDN:             settings.BaseDN,
		BindDN:             settings.BindDN,
		BindPasswordCipher: password,
		UserFilter:         settings.UserFilter,
		TLS:                settings.TLS,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode LDAP settings: %w", err)
	}
	return s.save(ctx, LDAPKey, string(raw))
}

// DeleteLDAP removes the stored LDAP override.
func (s *Store) DeleteLDAP(ctx context.Context) error { return s.delete(ctx, LDAPKey) }

func (s *Store) load(ctx context.Context, key string) (string, time.Time, bool, error) {
	row, err := s.client.SystemSetting.Query().Where(systemsetting.KeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, fmt.Errorf("load %s settings: %w", key, err)
	}
	return row.Value, row.UpdatedAt, true, nil
}

func (s *Store) save(ctx context.Context, key, value string) error {
	affected, err := s.client.SystemSetting.Update().
		Where(systemsetting.KeyEQ(key)).
		SetValue(value).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("save %s settings: %w", key, err)
	}
	if affected > 0 {
		return nil
	}
	_, err = s.client.SystemSetting.Create().SetKey(key).SetValue(value).Save(ctx)
	if err == nil {
		return nil
	}
	if !ent.IsConstraintError(err) {
		return fmt.Errorf("save %s settings: %w", key, err)
	}
	// A concurrent replica inserted the row first; write the newer value.
	if _, updateErr := s.client.SystemSetting.Update().
		Where(systemsetting.KeyEQ(key)).
		SetValue(value).
		Save(ctx); updateErr != nil {
		return fmt.Errorf("save %s settings: %w", key, updateErr)
	}
	return nil
}

func (s *Store) delete(ctx context.Context, key string) error {
	if _, err := s.client.SystemSetting.Delete().Where(systemsetting.KeyEQ(key)).Exec(ctx); err != nil {
		return fmt.Errorf("delete %s settings: %w", key, err)
	}
	return nil
}

func (s *Store) seal(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if strings.TrimSpace(s.encryptionKey) == "" {
		return "", fmt.Errorf("encryption key is required to store auth secrets")
	}
	sealed, err := pkg.Encrypt(plaintext, s.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("encrypt auth secret: %w", err)
	}
	return sealed, nil
}

func (s *Store) open(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if strings.TrimSpace(s.encryptionKey) == "" {
		return "", fmt.Errorf("encryption key is required to read auth secrets")
	}
	opened, err := pkg.Decrypt(ciphertext, s.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("decrypt auth secret: %w", err)
	}
	return opened, nil
}
