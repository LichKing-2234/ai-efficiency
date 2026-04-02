package proxy

type RuntimeConfig struct {
	SessionID   string            `json:"session_id"`
	ListenAddr  string            `json:"listen_addr"`
	AuthToken   string            `json:"auth_token"`
	ProviderURL string            `json:"provider_url"`
	ProviderKey string            `json:"provider_key"`
	Headers     map[string]string `json:"headers,omitempty"`
}
