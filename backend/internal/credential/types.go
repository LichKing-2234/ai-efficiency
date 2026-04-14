package credential

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Kind string

const (
	KindSecretText                Kind = "secret_text"
	KindUsernamePassword          Kind = "username_password"
	KindSSHUsernameWithPrivateKey Kind = "ssh_username_with_private_key"
)

func (k Kind) String() string {
	return string(k)
}

type Payload interface {
	Kind() Kind
	MaskedSummary() map[string]any
}

type SecretTextPayload struct {
	Text string `json:"text"`
}

func (p SecretTextPayload) Kind() Kind { return KindSecretText }

func (p SecretTextPayload) MaskedSummary() map[string]any {
	return map[string]any{"preview": MaskSecret(p.Text)}
}

type UsernamePasswordPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (p UsernamePasswordPayload) Kind() Kind { return KindUsernamePassword }

func (p UsernamePasswordPayload) MaskedSummary() map[string]any {
	return map[string]any{
		"username":         p.Username,
		"password_preview": MaskSecret(p.Password),
	}
}

type SSHUsernameWithPrivateKeyPayload struct {
	Username   string `json:"username"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase,omitempty"`
}

func (p SSHUsernameWithPrivateKeyPayload) Kind() Kind { return KindSSHUsernameWithPrivateKey }

func (p SSHUsernameWithPrivateKeyPayload) MaskedSummary() map[string]any {
	return map[string]any{
		"username":            p.Username,
		"private_key_preview": "configured",
		"has_passphrase":      strings.TrimSpace(p.Passphrase) != "",
	}
}

func ParsePayload(kind Kind, raw json.RawMessage) (Payload, error) {
	switch kind {
	case KindSecretText:
		var payload SecretTextPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decode secret_text payload: %w", err)
		}
		if strings.TrimSpace(payload.Text) == "" {
			return nil, errors.New("secret_text.text is required")
		}
		return payload, nil
	case KindUsernamePassword:
		var payload UsernamePasswordPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decode username_password payload: %w", err)
		}
		if strings.TrimSpace(payload.Username) == "" || strings.TrimSpace(payload.Password) == "" {
			return nil, errors.New("username_password.username and password are required")
		}
		return payload, nil
	case KindSSHUsernameWithPrivateKey:
		var payload SSHUsernameWithPrivateKeyPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decode ssh_username_with_private_key payload: %w", err)
		}
		if strings.TrimSpace(payload.Username) == "" || strings.TrimSpace(payload.PrivateKey) == "" {
			return nil, errors.New("ssh_username_with_private_key.username and private_key are required")
		}
		return payload, nil
	default:
		return nil, fmt.Errorf("unsupported credential kind %q", kind)
	}
}

func ValidateProviderCredentialRefs(apiKind Kind, cloneProtocol string, cloneKind Kind) error {
	if apiKind == KindSSHUsernameWithPrivateKey {
		return errors.New("api credential cannot be ssh_username_with_private_key")
	}

	switch cloneProtocol {
	case "https":
		if cloneKind != "" && cloneKind != KindSecretText && cloneKind != KindUsernamePassword {
			return fmt.Errorf("https clone does not allow %s", cloneKind)
		}
	case "ssh":
		if cloneKind != KindSSHUsernameWithPrivateKey {
			return fmt.Errorf("ssh clone requires %s", KindSSHUsernameWithPrivateKey)
		}
	default:
		return fmt.Errorf("unsupported clone protocol %q", cloneProtocol)
	}

	return nil
}

func MaskSecret(value string) string {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "":
		return ""
	case len(trimmed) <= 4:
		return "****"
	default:
		return trimmed[:2] + "..." + trimmed[len(trimmed)-2:]
	}
}
