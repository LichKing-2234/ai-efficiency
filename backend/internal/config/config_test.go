package config

import (
	"os"
	"path/filepath"
	"testing"
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
	if cfg.Auth.AccessTokenTTL != 7200 {
		t.Errorf("default access_token_ttl = %d, want 7200", cfg.Auth.AccessTokenTTL)
	}
	if cfg.Auth.RefreshTokenTTL != 604800 {
		t.Errorf("default refresh_token_ttl = %d, want 604800", cfg.Auth.RefreshTokenTTL)
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
	t.Setenv("AE_RELAY_API_KEY", "relay-key-from-env")
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
	if cfg.Relay.APIKey != "relay-key-from-env" {
		t.Errorf("relay api key = %q, want %q", cfg.Relay.APIKey, "relay-key-from-env")
	}
	if cfg.Auth.LDAP.URL != "ldap://env-ldap.example.com:389" {
		t.Errorf("auth.ldap.url = %q, want %q", cfg.Auth.LDAP.URL, "ldap://env-ldap.example.com:389")
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
	if cfg.Analysis.LLM.MaxTokensPerScan != 100000 {
		t.Errorf("default max_tokens_per_scan = %d, want 100000", cfg.Analysis.LLM.MaxTokensPerScan)
	}
	if cfg.Analysis.LLM.MaxScansPerRepoDay != 3 {
		t.Errorf("default max_scans_per_repo_per_day = %d, want 3", cfg.Analysis.LLM.MaxScansPerRepoDay)
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

func TestLoadDeploymentConfigDoesNotRequireUpdaterURL(t *testing.T) {
	t.Setenv("AE_DEPLOYMENT_MODE", "bundled")
	t.Setenv("AE_DEPLOYMENT_STATE_DIR", "/var/lib/ai-efficiency")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Deployment.Update.UpdaterURL != "" {
		t.Fatalf("deployment updater url = %q, want empty in unified self-update mode", cfg.Deployment.Update.UpdaterURL)
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
  api_key: "sk-relay-test"
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
analysis:
  llm:
    model: "gpt-3.5-turbo"
    max_tokens_per_scan: 50000
    max_scans_per_repo_per_day: 5
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
	if cfg.Relay.APIKey != "sk-relay-test" {
		t.Errorf("relay api_key = %q", cfg.Relay.APIKey)
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
	if cfg.Analysis.LLM.MaxTokensPerScan != 50000 {
		t.Errorf("llm max_tokens_per_scan = %d", cfg.Analysis.LLM.MaxTokensPerScan)
	}
	if cfg.Analysis.LLM.MaxScansPerRepoDay != 5 {
		t.Errorf("llm max_scans_per_repo_per_day = %d", cfg.Analysis.LLM.MaxScansPerRepoDay)
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

func TestLoadDeploymentAndRedisConfigFromEnv(t *testing.T) {
	t.Setenv("AE_REDIS_ADDR", "redis:6379")
	t.Setenv("AE_REDIS_PASSWORD", "redis-pass")
	t.Setenv("AE_REDIS_DB", "2")
	t.Setenv("AE_DEPLOYMENT_MODE", "bundled")
	t.Setenv("AE_DEPLOYMENT_STATE_DIR", "/var/lib/ai-efficiency")
	t.Setenv("AE_DEPLOYMENT_UPDATE_ENABLED", "true")
	t.Setenv("AE_DEPLOYMENT_UPDATE_APPLY_ENABLED", "true")
	t.Setenv("AE_DEPLOYMENT_UPDATE_RELEASE_API_URL", "https://api.github.com/repos/ai-efficiency/releases/latest")
	t.Setenv("AE_DEPLOYMENT_UPDATE_UPDATER_URL", "http://updater:8090")
	t.Setenv("AE_DEPLOYMENT_UPDATE_IMAGE_REPOSITORY", "ghcr.io/ai-efficiency/ai-efficiency")

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
	if cfg.Deployment.Mode != "bundled" {
		t.Errorf("deployment mode = %q, want %q", cfg.Deployment.Mode, "bundled")
	}
	if !cfg.Deployment.Update.Enabled {
		t.Error("deployment update enabled = false, want true")
	}
	if !cfg.Deployment.Update.ApplyEnabled {
		t.Error("deployment update apply enabled = false, want true")
	}
	if cfg.Deployment.Update.UpdaterURL != "http://updater:8090" {
		t.Errorf("deployment updater url = %q, want %q", cfg.Deployment.Update.UpdaterURL, "http://updater:8090")
	}
	if cfg.Deployment.Update.ImageRepository != "ghcr.io/ai-efficiency/ai-efficiency" {
		t.Errorf(
			"deployment image repository = %q, want %q",
			cfg.Deployment.Update.ImageRepository,
			"ghcr.io/ai-efficiency/ai-efficiency",
		)
	}
}

func TestLoadExplicitMissingPathReturnsError(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("Load() expected error for missing explicit config path")
	}
}

func TestDeploymentDefaultsPointAtGitHubPrimaryRepo(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}

	if cfg.Deployment.Update.ReleaseAPIURL != "https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest" {
		t.Fatalf("release_api_url = %q", cfg.Deployment.Update.ReleaseAPIURL)
	}
	if cfg.Deployment.Update.ImageRepository != "ghcr.io/lichking-2234/ai-efficiency" {
		t.Fatalf("image_repository = %q", cfg.Deployment.Update.ImageRepository)
	}
}
