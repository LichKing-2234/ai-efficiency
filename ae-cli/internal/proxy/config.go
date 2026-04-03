package proxy

type RuntimeConfig struct {
	SessionID    string            `json:"session_id"`
	WorkspaceID  string            `json:"workspace_id,omitempty"`
	ListenAddr   string            `json:"listen_addr"`
	AuthToken    string            `json:"auth_token"`
	ProviderURL  string            `json:"provider_url"`
	ProviderKey  string            `json:"provider_key"`
	BackendURL   string            `json:"backend_url,omitempty"`
	BackendToken string            `json:"backend_token,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
}
