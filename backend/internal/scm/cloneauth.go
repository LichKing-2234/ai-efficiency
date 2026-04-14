package scm

import (
	"fmt"

	entscmprovider "github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/credential"
)

type CloneAuthConfig struct {
	Protocol      string
	HTTPSUsername string
	HTTPSPassword string
	SSHUsername   string
	SSHPrivateKey string
	SSHPassphrase string
}

func BuildCloneAuthConfig(
	providerType entscmprovider.Type,
	cloneProtocol string,
	apiPayload credential.Payload,
	clonePayload credential.Payload,
) (*CloneAuthConfig, error) {
	switch cloneProtocol {
	case "https":
		effective := clonePayload
		if effective == nil {
			effective = apiPayload
		}
		if effective == nil {
			return nil, fmt.Errorf("https clone requires a credential")
		}

		switch payload := effective.(type) {
		case credential.SecretTextPayload:
			return &CloneAuthConfig{
				Protocol:      "https",
				HTTPSUsername: defaultHTTPSUsername(providerType),
				HTTPSPassword: payload.Text,
			}, nil
		case credential.UsernamePasswordPayload:
			return &CloneAuthConfig{
				Protocol:      "https",
				HTTPSUsername: payload.Username,
				HTTPSPassword: payload.Password,
			}, nil
		default:
			return nil, fmt.Errorf("https clone does not support %s", effective.Kind())
		}
	case "ssh":
		payload, ok := clonePayload.(credential.SSHUsernameWithPrivateKeyPayload)
		if !ok {
			return nil, fmt.Errorf("ssh clone requires ssh_username_with_private_key credential")
		}
		return &CloneAuthConfig{
			Protocol:      "ssh",
			SSHUsername:   payload.Username,
			SSHPrivateKey: payload.PrivateKey,
			SSHPassphrase: payload.Passphrase,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported clone protocol %q", cloneProtocol)
	}
}

func defaultHTTPSUsername(providerType entscmprovider.Type) string {
	switch providerType {
	case entscmprovider.TypeGithub:
		return "x-access-token"
	default:
		return "git"
	}
}
