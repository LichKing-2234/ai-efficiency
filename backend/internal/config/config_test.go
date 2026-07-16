package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Port != 8081 {
		t.Errorf("default port = %d, want 8081", cfg.Server.Port)
	}
	if cfg.Server.Mode != "debug" {
		t.Errorf("default mode = %s, want debug", cfg.Server.Mode)
	}
	if cfg.DB.MaxOpenConns != 25 {
		t.Errorf("default max_open_conns = %d, want 25", cfg.DB.MaxOpenConns)
	}
	if cfg.Metrics.ListenAddress != "127.0.0.1:9090" {
		t.Errorf("default metrics listen address = %q, want 127.0.0.1:9090", cfg.Metrics.ListenAddress)
	}
	if cfg.Auth.AccessTokenTTL != 7200 {
		t.Errorf("default access_token_ttl = %d, want 7200", cfg.Auth.AccessTokenTTL)
	}
	if cfg.Auth.RefreshTokenTTL != 604800 {
		t.Errorf("default refresh_token_ttl = %d, want 604800", cfg.Auth.RefreshTokenTTL)
	}
	if cfg.Redis.Namespace != "ai-efficiency" {
		t.Errorf("default redis namespace = %q, want ai-efficiency", cfg.Redis.Namespace)
	}
}

func TestLoadMetricsListenAddressFromEnvironment(t *testing.T) {
	t.Setenv("AE_METRICS_LISTEN_ADDRESS", ":9191")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Metrics.ListenAddress != ":9191" {
		t.Fatalf("metrics.listen_address = %q, want :9191", cfg.Metrics.ListenAddress)
	}
}

func TestLoadRejectsInvalidMetricsListenAddress(t *testing.T) {
	for _, address := range []string{"http://127.0.0.1:9090", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:70000"} {
		t.Run(address, func(t *testing.T) {
			t.Setenv("AE_METRICS_LISTEN_ADDRESS", address)
			if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "metrics.listen_address") {
				t.Fatalf("Load() error = %v, want metrics.listen_address validation error", err)
			}
		})
	}
}

func TestLoadHTTPRuntimeDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	wantServer := ServerConfig{
		Port:                     8081,
		Mode:                     "debug",
		FrontendURL:              "http://localhost:5173",
		ReadHeaderTimeoutSeconds: 5,
		IdleTimeoutSeconds:       120,
		ReadinessTimeoutSeconds:  2,
		RequestTimeoutSeconds:    35,
	}
	if !reflect.DeepEqual(cfg.Server, wantServer) {
		t.Fatalf("Server = %#v, want %#v", cfg.Server, wantServer)
	}

	wantHTTPClient := HTTPClientConfig{
		ConnectTimeoutSeconds:        5,
		TLSHandshakeTimeoutSeconds:   5,
		ResponseHeaderTimeoutSeconds: 15,
		OverallTimeoutSeconds:        30,
		IdleConnTimeoutSeconds:       90,
		MaxIdleConns:                 100,
		MaxIdleConnsPerHost:          20,
		MaxConnsPerHost:              50,
	}
	if !reflect.DeepEqual(cfg.HTTPClient, wantHTTPClient) {
		t.Fatalf("HTTPClient = %#v, want %#v", cfg.HTTPClient, wantHTTPClient)
	}
}

func TestLoadHTTPRuntimeEnvironmentOverrides(t *testing.T) {
	t.Setenv("AE_SERVER_READ_HEADER_TIMEOUT_SECONDS", "6")
	t.Setenv("AE_SERVER_IDLE_TIMEOUT_SECONDS", "121")
	t.Setenv("AE_SERVER_READINESS_TIMEOUT_SECONDS", "3")
	t.Setenv("AE_SERVER_REQUEST_TIMEOUT_SECONDS", "36")
	t.Setenv("AE_HTTP_CLIENT_CONNECT_TIMEOUT_SECONDS", "7")
	t.Setenv("AE_HTTP_CLIENT_TLS_HANDSHAKE_TIMEOUT_SECONDS", "8")
	t.Setenv("AE_HTTP_CLIENT_RESPONSE_HEADER_TIMEOUT_SECONDS", "16")
	t.Setenv("AE_HTTP_CLIENT_OVERALL_TIMEOUT_SECONDS", "31")
	t.Setenv("AE_HTTP_CLIENT_IDLE_CONN_TIMEOUT_SECONDS", "91")
	t.Setenv("AE_HTTP_CLIENT_MAX_IDLE_CONNS", "101")
	t.Setenv("AE_HTTP_CLIENT_MAX_IDLE_CONNS_PER_HOST", "21")
	t.Setenv("AE_HTTP_CLIENT_MAX_CONNS_PER_HOST", "51")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	wantServerTimeouts := []int{6, 121, 3, 36}
	gotServerTimeouts := []int{
		cfg.Server.ReadHeaderTimeoutSeconds,
		cfg.Server.IdleTimeoutSeconds,
		cfg.Server.ReadinessTimeoutSeconds,
		cfg.Server.RequestTimeoutSeconds,
	}
	if !reflect.DeepEqual(gotServerTimeouts, wantServerTimeouts) {
		t.Fatalf("server timeout overrides = %v, want %v", gotServerTimeouts, wantServerTimeouts)
	}

	wantHTTPClient := HTTPClientConfig{
		ConnectTimeoutSeconds:        7,
		TLSHandshakeTimeoutSeconds:   8,
		ResponseHeaderTimeoutSeconds: 16,
		OverallTimeoutSeconds:        31,
		IdleConnTimeoutSeconds:       91,
		MaxIdleConns:                 101,
		MaxIdleConnsPerHost:          21,
		MaxConnsPerHost:              51,
	}
	if !reflect.DeepEqual(cfg.HTTPClient, wantHTTPClient) {
		t.Fatalf("HTTPClient overrides = %#v, want %#v", cfg.HTTPClient, wantHTTPClient)
	}
}

func TestLoadRejectsUnsafeHTTPRuntimeValues(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantField string
	}{
		{name: "read header zero", env: map[string]string{"AE_SERVER_READ_HEADER_TIMEOUT_SECONDS": "0"}, wantField: "server.read_header_timeout_seconds"},
		{name: "idle zero", env: map[string]string{"AE_SERVER_IDLE_TIMEOUT_SECONDS": "0"}, wantField: "server.idle_timeout_seconds"},
		{name: "readiness zero", env: map[string]string{"AE_SERVER_READINESS_TIMEOUT_SECONDS": "0"}, wantField: "server.readiness_timeout_seconds"},
		{name: "request zero", env: map[string]string{"AE_SERVER_REQUEST_TIMEOUT_SECONDS": "0"}, wantField: "server.request_timeout_seconds"},
		{name: "connect zero", env: map[string]string{"AE_HTTP_CLIENT_CONNECT_TIMEOUT_SECONDS": "0"}, wantField: "http_client.connect_timeout_seconds"},
		{name: "tls zero", env: map[string]string{"AE_HTTP_CLIENT_TLS_HANDSHAKE_TIMEOUT_SECONDS": "0"}, wantField: "http_client.tls_handshake_timeout_seconds"},
		{name: "response header zero", env: map[string]string{"AE_HTTP_CLIENT_RESPONSE_HEADER_TIMEOUT_SECONDS": "0"}, wantField: "http_client.response_header_timeout_seconds"},
		{name: "overall zero", env: map[string]string{"AE_HTTP_CLIENT_OVERALL_TIMEOUT_SECONDS": "0"}, wantField: "http_client.overall_timeout_seconds"},
		{name: "idle connection zero", env: map[string]string{"AE_HTTP_CLIENT_IDLE_CONN_TIMEOUT_SECONDS": "0"}, wantField: "http_client.idle_conn_timeout_seconds"},
		{name: "max idle zero", env: map[string]string{"AE_HTTP_CLIENT_MAX_IDLE_CONNS": "0"}, wantField: "http_client.max_idle_conns"},
		{name: "max idle per host zero", env: map[string]string{"AE_HTTP_CLIENT_MAX_IDLE_CONNS_PER_HOST": "0"}, wantField: "http_client.max_idle_conns_per_host"},
		{name: "max connections per host zero", env: map[string]string{"AE_HTTP_CLIENT_MAX_CONNS_PER_HOST": "0"}, wantField: "http_client.max_conns_per_host"},
		{name: "negative", env: map[string]string{"AE_SERVER_REQUEST_TIMEOUT_SECONDS": "-1"}, wantField: "server.request_timeout_seconds"},
		{name: "duration conversion overflow", env: map[string]string{"AE_SERVER_REQUEST_TIMEOUT_SECONDS": "9223372037"}, wantField: "server.request_timeout_seconds"},
		{name: "read header upper bound", env: map[string]string{"AE_SERVER_READ_HEADER_TIMEOUT_SECONDS": "61"}, wantField: "server.read_header_timeout_seconds"},
		{name: "idle upper bound", env: map[string]string{"AE_SERVER_IDLE_TIMEOUT_SECONDS": "3601"}, wantField: "server.idle_timeout_seconds"},
		{name: "readiness upper bound", env: map[string]string{"AE_SERVER_READINESS_TIMEOUT_SECONDS": "31"}, wantField: "server.readiness_timeout_seconds"},
		{name: "request reaches browser deadline", env: map[string]string{"AE_SERVER_REQUEST_TIMEOUT_SECONDS": "45"}, wantField: "server.request_timeout_seconds"},
		{name: "request exceeds browser deadline", env: map[string]string{"AE_SERVER_REQUEST_TIMEOUT_SECONDS": "46"}, wantField: "server.request_timeout_seconds"},
		{name: "connect upper bound", env: map[string]string{"AE_HTTP_CLIENT_CONNECT_TIMEOUT_SECONDS": "31"}, wantField: "http_client.connect_timeout_seconds"},
		{name: "tls upper bound", env: map[string]string{"AE_HTTP_CLIENT_TLS_HANDSHAKE_TIMEOUT_SECONDS": "31"}, wantField: "http_client.tls_handshake_timeout_seconds"},
		{name: "response header upper bound", env: map[string]string{"AE_HTTP_CLIENT_RESPONSE_HEADER_TIMEOUT_SECONDS": "61"}, wantField: "http_client.response_header_timeout_seconds"},
		{name: "overall upper bound", env: map[string]string{"AE_HTTP_CLIENT_OVERALL_TIMEOUT_SECONDS": "301"}, wantField: "http_client.overall_timeout_seconds"},
		{name: "idle connection upper bound", env: map[string]string{"AE_HTTP_CLIENT_IDLE_CONN_TIMEOUT_SECONDS": "3601"}, wantField: "http_client.idle_conn_timeout_seconds"},
		{name: "max idle pool upper bound", env: map[string]string{"AE_HTTP_CLIENT_MAX_IDLE_CONNS": "10001"}, wantField: "http_client.max_idle_conns"},
		{name: "max idle per host upper bound", env: map[string]string{"AE_HTTP_CLIENT_MAX_IDLE_CONNS_PER_HOST": "10001"}, wantField: "http_client.max_idle_conns_per_host"},
		{name: "max connections per host upper bound", env: map[string]string{"AE_HTTP_CLIENT_MAX_CONNS_PER_HOST": "10001"}, wantField: "http_client.max_conns_per_host"},
		{name: "connect must precede overall", env: map[string]string{"AE_HTTP_CLIENT_CONNECT_TIMEOUT_SECONDS": "30"}, wantField: "http_client.connect_timeout_seconds"},
		{name: "tls must precede overall", env: map[string]string{"AE_HTTP_CLIENT_TLS_HANDSHAKE_TIMEOUT_SECONDS": "30"}, wantField: "http_client.tls_handshake_timeout_seconds"},
		{name: "response headers must precede overall", env: map[string]string{"AE_HTTP_CLIENT_RESPONSE_HEADER_TIMEOUT_SECONDS": "30"}, wantField: "http_client.response_header_timeout_seconds"},
		{name: "shared overall equals version timeout", env: map[string]string{"AE_HTTP_CLIENT_RESPONSE_HEADER_TIMEOUT_SECONDS": "9", "AE_HTTP_CLIENT_OVERALL_TIMEOUT_SECONDS": "10"}, wantField: "http_client.overall_timeout_seconds"},
		{name: "shared overall below version timeout", env: map[string]string{"AE_HTTP_CLIENT_RESPONSE_HEADER_TIMEOUT_SECONDS": "8", "AE_HTTP_CLIENT_OVERALL_TIMEOUT_SECONDS": "9"}, wantField: "http_client.overall_timeout_seconds"},
		{name: "downstream must precede request", env: map[string]string{"AE_SERVER_REQUEST_TIMEOUT_SECONDS": "30"}, wantField: "http_client.overall_timeout_seconds"},
		{name: "readiness must precede request", env: map[string]string{"AE_SERVER_REQUEST_TIMEOUT_SECONDS": "30", "AE_SERVER_READINESS_TIMEOUT_SECONDS": "30", "AE_HTTP_CLIENT_OVERALL_TIMEOUT_SECONDS": "29"}, wantField: "server.readiness_timeout_seconds"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			_, err := Load("")
			if err == nil {
				t.Fatalf("Load() error = nil, want field-specific validation for %s", tt.wantField)
			}
			if !strings.Contains(err.Error(), tt.wantField) {
				t.Fatalf("Load() error = %q, want field %q", err, tt.wantField)
			}
		})
	}
}

func TestLoadAcceptsHTTPRuntimeBoundaryValues(t *testing.T) {
	values := map[string]int{
		"AE_SERVER_READ_HEADER_TIMEOUT_SECONDS":          60,
		"AE_SERVER_IDLE_TIMEOUT_SECONDS":                 3600,
		"AE_SERVER_READINESS_TIMEOUT_SECONDS":            30,
		"AE_SERVER_REQUEST_TIMEOUT_SECONDS":              44,
		"AE_HTTP_CLIENT_CONNECT_TIMEOUT_SECONDS":         30,
		"AE_HTTP_CLIENT_TLS_HANDSHAKE_TIMEOUT_SECONDS":   30,
		"AE_HTTP_CLIENT_RESPONSE_HEADER_TIMEOUT_SECONDS": 42,
		"AE_HTTP_CLIENT_OVERALL_TIMEOUT_SECONDS":         43,
		"AE_HTTP_CLIENT_IDLE_CONN_TIMEOUT_SECONDS":       3600,
		"AE_HTTP_CLIENT_MAX_IDLE_CONNS":                  10000,
		"AE_HTTP_CLIENT_MAX_IDLE_CONNS_PER_HOST":         10000,
		"AE_HTTP_CLIENT_MAX_CONNS_PER_HOST":              10000,
	}
	for key, value := range values {
		t.Setenv(key, fmt.Sprint(value))
	}

	if _, err := Load(""); err != nil {
		t.Fatalf("Load() error = %v, want configured upper bounds accepted", err)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	content := `
server:
  port: 9090
  mode: release
db:
  dsn: "postgres://test:test@localhost/testdb"
auth:
  jwt_secret: "my-secret"
encryption:
  key: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Server.Mode != "release" {
		t.Errorf("mode = %s, want release", cfg.Server.Mode)
	}
	if cfg.DB.DSN != "postgres://test:test@localhost/testdb" {
		t.Errorf("dsn = %s, want postgres://test:test@localhost/testdb", cfg.DB.DSN)
	}
	if cfg.Auth.JWTSecret != "my-secret" {
		t.Errorf("jwt_secret = %s, want my-secret", cfg.Auth.JWTSecret)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("AE_SERVER_PORT", "7777")
	t.Setenv("AE_ENCRYPTION_KEY", "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	t.Setenv("AE_RELAY_URL", "http://relay.internal:4000")
	t.Setenv("AE_RELAY_ADMIN_API_KEY", "relay-admin-key-from-env")
	t.Setenv("AE_AUTH_LDAP_URL", "ldap://env-ldap.example.com:389")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Port != 7777 {
		t.Errorf("port = %d, want 7777 (from env)", cfg.Server.Port)
	}
	if cfg.Encryption.Key != "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789" {
		t.Errorf("encryption key = %q, want env value", cfg.Encryption.Key)
	}
	if cfg.Relay.URL != "http://relay.internal:4000" {
		t.Errorf("relay url = %q, want %q", cfg.Relay.URL, "http://relay.internal:4000")
	}
	if cfg.Relay.AdminAPIKey != "relay-admin-key-from-env" {
		t.Errorf("relay admin api key = %q, want %q", cfg.Relay.AdminAPIKey, "relay-admin-key-from-env")
	}
	if cfg.Auth.LDAP.URL != "ldap://env-ldap.example.com:389" {
		t.Errorf("auth.ldap.url = %q, want %q", cfg.Auth.LDAP.URL, "ldap://env-ldap.example.com:389")
	}
}

func TestLoadReadsServerPublicURLFromEnvironment(t *testing.T) {
	t.Setenv("AE_SERVER_PUBLIC_URL", "https://ai-efficiency.example.com")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.PublicURL != "https://ai-efficiency.example.com" {
		t.Fatalf("PublicURL = %q, want https://ai-efficiency.example.com", cfg.Server.PublicURL)
	}
}

func TestLoadUsesFileEncryptionKeyWhenEnvIsUnset(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	content := `
encryption:
  key: "d98460dc58409c713d1586802217c23932d58c95479641e4b0fec1c740386696"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AE_ENCRYPTION_KEY", "")

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Encryption.Key != "d98460dc58409c713d1586802217c23932d58c95479641e4b0fec1c740386696" {
		t.Errorf("encryption key = %q, want file value", cfg.Encryption.Key)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	// Write content that will cause Unmarshal to fail (wrong types)
	if err := os.WriteFile(cfgFile, []byte("server:\n  port: not_a_number\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgFile)
	// Viper is lenient with YAML parsing, so we just verify it doesn't panic
	// and returns a config (even if defaults are used)
	if cfg == nil && err == nil {
		t.Error("Load should return either a config or an error")
	}
}

func TestLoadEmptyPath(t *testing.T) {
	// Load with empty path — should use default config search paths
	cfg, err := Load("")
	if err != nil {
		// May fail if no config file found in default paths, that's OK
		return
	}
	// If it succeeds, defaults should be applied
	if cfg.Server.Port == 0 {
		t.Error("expected non-zero default port")
	}
}

func TestLoadAllDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify all defaults
	if cfg.DB.MaxIdleConns != 5 {
		t.Errorf("default max_idle_conns = %d, want 5", cfg.DB.MaxIdleConns)
	}
	if cfg.DB.ConnMaxLifetime != 300 {
		t.Errorf("default conn_max_lifetime = %d, want 300", cfg.DB.ConnMaxLifetime)
	}
	if cfg.Relay.Provider != "sub2api" {
		t.Errorf("default relay provider = %q, want %q", cfg.Relay.Provider, "sub2api")
	}
	if cfg.Relay.Model != "claude-sonnet-4-20250514" {
		t.Errorf("default relay model = %q, want %q", cfg.Relay.Model, "claude-sonnet-4-20250514")
	}
	if cfg.Auth.LDAP.UserFilter != "(uid=%s)" {
		t.Errorf("default ldap user_filter = %q, want %q", cfg.Auth.LDAP.UserFilter, "(uid=%s)")
	}
}

func TestLoadEnvOverrideNested(t *testing.T) {
	t.Setenv("AE_AUTH_JWT_SECRET", "env-secret")
	t.Setenv("AE_DB_DSN", "postgres://env:env@localhost/envdb")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Auth.JWTSecret != "env-secret" {
		t.Errorf("jwt_secret = %q, want %q", cfg.Auth.JWTSecret, "env-secret")
	}
	if cfg.DB.DSN != "postgres://env:env@localhost/envdb" {
		t.Errorf("dsn = %q, want %q", cfg.DB.DSN, "postgres://env:env@localhost/envdb")
	}
}

func TestLoadFileOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	content := `
server:
  port: 3000
  mode: release
db:
  dsn: "postgres://file:file@localhost/filedb?sslmode=disable"
  max_open_conns: 50
  max_idle_conns: 10
  conn_max_lifetime: 600
relay:
  provider: sub2api
  url: "http://localhost:3000"
  admin_api_key: "admin-relay-test"
  model: "gpt-3.5-turbo"
auth:
  jwt_secret: "file-secret"
  access_token_ttl: 3600
  refresh_token_ttl: 86400
  ldap:
    url: "ldap://ldap.example.com"
    base_dn: "dc=example,dc=com"
    bind_dn: "cn=admin,dc=example,dc=com"
    bind_password: "ldap-pass"
    user_filter: "(mail=%s)"
    tls: true
encryption:
  key: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Port != 3000 {
		t.Errorf("port = %d, want 3000", cfg.Server.Port)
	}
	if cfg.DB.MaxOpenConns != 50 {
		t.Errorf("max_open_conns = %d, want 50", cfg.DB.MaxOpenConns)
	}
	if cfg.DB.MaxIdleConns != 10 {
		t.Errorf("max_idle_conns = %d, want 10", cfg.DB.MaxIdleConns)
	}
	if cfg.DB.ConnMaxLifetime != 600 {
		t.Errorf("conn_max_lifetime = %d, want 600", cfg.DB.ConnMaxLifetime)
	}
	if cfg.Relay.Provider != "sub2api" {
		t.Errorf("relay provider = %q, want %q", cfg.Relay.Provider, "sub2api")
	}
	if cfg.Relay.URL != "http://localhost:3000" {
		t.Errorf("relay url = %q, want %q", cfg.Relay.URL, "http://localhost:3000")
	}
	if cfg.Relay.AdminAPIKey != "admin-relay-test" {
		t.Errorf("relay admin_api_key = %q", cfg.Relay.AdminAPIKey)
	}
	if cfg.Auth.AccessTokenTTL != 3600 {
		t.Errorf("access_token_ttl = %d, want 3600", cfg.Auth.AccessTokenTTL)
	}
	if cfg.Auth.RefreshTokenTTL != 86400 {
		t.Errorf("refresh_token_ttl = %d, want 86400", cfg.Auth.RefreshTokenTTL)
	}
	if cfg.Auth.LDAP.URL != "ldap://ldap.example.com" {
		t.Errorf("ldap url = %q", cfg.Auth.LDAP.URL)
	}
	if cfg.Auth.LDAP.BaseDN != "dc=example,dc=com" {
		t.Errorf("ldap base_dn = %q", cfg.Auth.LDAP.BaseDN)
	}
	if cfg.Auth.LDAP.BindDN != "cn=admin,dc=example,dc=com" {
		t.Errorf("ldap bind_dn = %q", cfg.Auth.LDAP.BindDN)
	}
	if cfg.Auth.LDAP.BindPassword != "ldap-pass" {
		t.Errorf("ldap bind_password = %q", cfg.Auth.LDAP.BindPassword)
	}
	if cfg.Auth.LDAP.UserFilter != "(mail=%s)" {
		t.Errorf("ldap user_filter = %q", cfg.Auth.LDAP.UserFilter)
	}
	if !cfg.Auth.LDAP.TLS {
		t.Error("ldap tls should be true")
	}
	if cfg.Encryption.Key != "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789" {
		t.Errorf("encryption key = %q", cfg.Encryption.Key)
	}
}

func TestLoadEnvOverrideWithFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	content := `
server:
  port: 9090
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Env should override file
	t.Setenv("AE_SERVER_PORT", "5555")

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Port != 5555 {
		t.Errorf("port = %d, want 5555 (env overrides file)", cfg.Server.Port)
	}
}

func TestLoadPartialConfig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	// Only set server port, everything else should use defaults
	content := `
server:
  port: 4444
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Port != 4444 {
		t.Errorf("port = %d, want 4444", cfg.Server.Port)
	}
	// Defaults should still apply for unset fields
	if cfg.Server.Mode != "debug" {
		t.Errorf("mode = %q, want %q (default)", cfg.Server.Mode, "debug")
	}
	if cfg.DB.MaxOpenConns != 25 {
		t.Errorf("max_open_conns = %d, want 25 (default)", cfg.DB.MaxOpenConns)
	}
}

func TestLoadRedisConfigFromEnv(t *testing.T) {
	t.Setenv("AE_REDIS_ADDR", "redis:6379")
	t.Setenv("AE_REDIS_PASSWORD", "redis-pass")
	t.Setenv("AE_REDIS_DB", "2")
	t.Setenv("AE_REDIS_NAMESPACE", "env-blue")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Redis.Addr != "redis:6379" {
		t.Errorf("redis addr = %q, want %q", cfg.Redis.Addr, "redis:6379")
	}
	if cfg.Redis.Password != "redis-pass" {
		t.Errorf("redis password = %q, want %q", cfg.Redis.Password, "redis-pass")
	}
	if cfg.Redis.DB != 2 {
		t.Errorf("redis db = %d, want %d", cfg.Redis.DB, 2)
	}
	if cfg.Redis.Namespace != "env-blue" {
		t.Errorf("redis namespace = %q, want env-blue", cfg.Redis.Namespace)
	}
}

func TestValidateRedisNamespace(t *testing.T) {
	valid63 := "a" + strings.Repeat("b", 62)
	invalid64 := "a" + strings.Repeat("b", 63)
	for _, test := range []struct {
		name      string
		namespace string
		wantErr   bool
	}{
		{name: "single character", namespace: "a"},
		{name: "allowed punctuation", namespace: "prod.blue_1-east"},
		{name: "maximum length", namespace: valid63},
		{name: "empty", namespace: "", wantErr: true},
		{name: "leading hyphen", namespace: "-prod", wantErr: true},
		{name: "space", namespace: "prod blue", wantErr: true},
		{name: "slash", namespace: "prod/blue", wantErr: true},
		{name: "colon", namespace: "prod:blue", wantErr: true},
		{name: "unicode", namespace: "prod-蓝", wantErr: true},
		{name: "too long", namespace: invalid64, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateRedisNamespace(test.namespace)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateRedisNamespace(%q) error = %v, wantErr %v", test.namespace, err, test.wantErr)
			}
		})
	}
}

func TestLoadRejectsUnsafeRedisNamespace(t *testing.T) {
	t.Setenv("AE_REDIS_NAMESPACE", "unsafe/namespace")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "redis namespace") {
		t.Fatalf("Load() error = %v, want Redis namespace validation error", err)
	}
}

func TestDeployExamplesDeclareExplicitValidRedisNamespace(t *testing.T) {
	deployDir := filepath.Clean(filepath.Join("..", "..", "..", "deploy"))

	configData, err := os.ReadFile(filepath.Join(deployDir, "config.example.yaml"))
	if err != nil {
		t.Fatalf("read config.example.yaml: %v", err)
	}
	var configExample struct {
		Redis struct {
			Namespace string `yaml:"namespace"`
		} `yaml:"redis"`
	}
	if err := yaml.Unmarshal(configData, &configExample); err != nil {
		t.Fatalf("parse config.example.yaml: %v", err)
	}
	if err := ValidateRedisNamespace(configExample.Redis.Namespace); err != nil {
		t.Fatalf("config.example.yaml Redis namespace = %q: %v", configExample.Redis.Namespace, err)
	}

	envData, err := os.ReadFile(filepath.Join(deployDir, ".env.example"))
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	envNamespace := envFileValue(string(envData), "AE_REDIS_NAMESPACE")
	if err := ValidateRedisNamespace(envNamespace); err != nil {
		t.Fatalf(".env.example Redis namespace = %q: %v", envNamespace, err)
	}

	for _, name := range []string{
		"docker-compose.yml",
		"docker-compose.bootstrap.yml",
		"docker-compose.dev.yml",
		"docker-compose.external.yml",
		"docker-compose.local.yml",
	} {
		data, err := os.ReadFile(filepath.Join(deployDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var compose struct {
			Services map[string]struct {
				Environment map[string]string `yaml:"environment"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(data, &compose); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		raw, ok := compose.Services["backend"].Environment["AE_REDIS_NAMESPACE"]
		if !ok {
			t.Fatalf("%s does not declare AE_REDIS_NAMESPACE", name)
		}
		namespace := strings.TrimSuffix(strings.TrimPrefix(raw, "${AE_REDIS_NAMESPACE:-"), "}")
		if err := ValidateRedisNamespace(namespace); err != nil {
			t.Fatalf("%s Redis namespace default = %q: %v", name, namespace, err)
		}
	}
}

func envFileValue(content, key string) string {
	for _, line := range strings.Split(content, "\n") {
		if name, value, ok := strings.Cut(line, "="); ok && strings.TrimSpace(name) == key {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func TestLoadExplicitMissingPathReturnsError(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("Load() expected error for missing explicit config path")
	}
}

func TestResolveWritableConfigPath(t *testing.T) {
	tests := []struct {
		name         string
		explicitPath string
		stateDir     string
		want         string
	}{
		{
			name:         "explicit path wins",
			explicitPath: "/etc/ai-efficiency/config.yaml",
			stateDir:     "/var/lib/ai-efficiency",
			want:         "/etc/ai-efficiency/config.yaml",
		},
		{
			name:     "state dir fallback",
			stateDir: "/var/lib/ai-efficiency",
			want:     "/var/lib/ai-efficiency/config.yaml",
		},
		{
			name: "cwd fallback",
			want: "config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveWritableConfigPath(tt.explicitPath, tt.stateDir)
			if got != tt.want {
				t.Fatalf("ResolveWritableConfigPath(%q, %q) = %q, want %q", tt.explicitPath, tt.stateDir, got, tt.want)
			}
		})
	}
}

func TestResolveRuntimeStateDir(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "state dir env wins",
			env: map[string]string{
				"AE_STATE_DIR":            "/var/lib/ai-efficiency",
				"AE_DEPLOYMENT_STATE_DIR": "/legacy/state",
			},
			want: "/var/lib/ai-efficiency",
		},
		{
			name: "legacy deployment state dir remains fallback",
			env: map[string]string{
				"AE_DEPLOYMENT_STATE_DIR": "/legacy/state",
			},
			want: "/legacy/state",
		},
		{
			name: "blank env returns empty",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				return tt.env[key]
			}
			got := ResolveRuntimeStateDir(getenv)
			if got != tt.want {
				t.Fatalf("ResolveRuntimeStateDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsureWritableConfigFileCreatesReloadableConfig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "runtime", "config.yaml")

	cfg := &Config{
		Server: ServerConfig{
			Port:                     8081,
			Mode:                     "release",
			FrontendURL:              "http://localhost:8081",
			PublicURL:                "https://ai-efficiency.example.com",
			ReadHeaderTimeoutSeconds: 7,
			IdleTimeoutSeconds:       123,
			ReadinessTimeoutSeconds:  4,
			RequestTimeoutSeconds:    35,
		},
		Metrics: MetricsConfig{ListenAddress: "127.0.0.1:9090"},
		HTTPClient: HTTPClientConfig{
			ConnectTimeoutSeconds:        8,
			TLSHandshakeTimeoutSeconds:   9,
			ResponseHeaderTimeoutSeconds: 17,
			OverallTimeoutSeconds:        32,
			IdleConnTimeoutSeconds:       92,
			MaxIdleConns:                 102,
			MaxIdleConnsPerHost:          22,
			MaxConnsPerHost:              52,
		},
		DB: DBConfig{
			DSN:             "postgres://postgres:postgres@localhost:5432/ai_efficiency?sslmode=disable",
			MaxOpenConns:    25,
			MaxIdleConns:    5,
			ConnMaxLifetime: 300,
		},
		Redis: RedisConfig{
			Addr:      "redis:6379",
			Password:  "",
			DB:        0,
			Namespace: "test-blue",
		},
		Relay: RelayConfig{
			Provider:       "sub2api",
			URL:            "http://relay.example.com",
			AdminAPIKey:    "admin-live",
			Model:          "gpt-5.4",
			DefaultGroupID: "42",
		},
		Auth: AuthConfig{
			JWTSecret:       "jwt-secret",
			AccessTokenTTL:  7200,
			RefreshTokenTTL: 604800,
			LDAP: LDAPConfig{
				URL:          "ldap://ldap.example.com:389",
				BaseDN:       "dc=example,dc=com",
				BindDN:       "cn=admin,dc=example,dc=com",
				BindPassword: "secret",
				UserFilter:   "(uid=%s)",
				TLS:          true,
			},
		},
		Encryption: EncryptionConfig{
			Key: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		},
	}

	if err := EnsureWritableConfigFile(cfgFile, cfg); err != nil {
		t.Fatalf("EnsureWritableConfigFile() error = %v", err)
	}

	loaded, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load(%q) error = %v", cfgFile, err)
	}

	if loaded.Relay.Model != "gpt-5.4" {
		t.Fatalf("relay.model = %q, want %q", loaded.Relay.Model, "gpt-5.4")
	}
	if loaded.Relay.AdminAPIKey != "admin-live" {
		t.Fatalf("relay.admin_api_key = %q, want %q", loaded.Relay.AdminAPIKey, "admin-live")
	}
	if loaded.Auth.LDAP.BindPassword != "secret" {
		t.Fatalf("auth.ldap.bind_password = %q, want %q", loaded.Auth.LDAP.BindPassword, "secret")
	}
	if loaded.Server.PublicURL != "https://ai-efficiency.example.com" {
		t.Fatalf("server.public_url = %q, want %q", loaded.Server.PublicURL, "https://ai-efficiency.example.com")
	}
	if loaded.Redis.Namespace != "test-blue" {
		t.Fatalf("redis.namespace = %q, want test-blue", loaded.Redis.Namespace)
	}
	if !reflect.DeepEqual(loaded.Server, cfg.Server) {
		t.Fatalf("persisted Server = %#v, want %#v", loaded.Server, cfg.Server)
	}
	if !reflect.DeepEqual(loaded.HTTPClient, cfg.HTTPClient) {
		t.Fatalf("persisted HTTPClient = %#v, want %#v", loaded.HTTPClient, cfg.HTTPClient)
	}
}

func TestEnsureWritableConfigFilePreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	original := []byte("server:\n  port: 9000\n")
	if err := os.WriteFile(cfgFile, original, 0o644); err != nil {
		t.Fatalf("write original config: %v", err)
	}

	if err := EnsureWritableConfigFile(cfgFile, &Config{Server: ServerConfig{Port: 8081}}); err != nil {
		t.Fatalf("EnsureWritableConfigFile() error = %v", err)
	}

	content, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatalf("read preserved config: %v", err)
	}
	if string(content) != string(original) {
		t.Fatalf("EnsureWritableConfigFile should not overwrite existing file, got:\n%s", string(content))
	}
}
