package config

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server           ServerConfig           `mapstructure:"server"`
	Metrics          MetricsConfig          `mapstructure:"metrics"`
	HTTPClient       HTTPClientConfig       `mapstructure:"http_client"`
	DB               DBConfig               `mapstructure:"db"`
	Redis            RedisConfig            `mapstructure:"redis"`
	TeamUsagePrewarm TeamUsagePrewarmConfig `mapstructure:"team_usage_prewarm"`
	Auth             AuthConfig             `mapstructure:"auth"`
	Encryption       EncryptionConfig       `mapstructure:"encryption"`
	Relay            RelayConfig            `mapstructure:"relay"`
	VersionCheck     VersionCheckConfig     `mapstructure:"version_check"`
}

type MetricsConfig struct {
	ListenAddress string `mapstructure:"listen_address"`
}

type ServerConfig struct {
	Port                     int    `mapstructure:"port"`
	Mode                     string `mapstructure:"mode"` // debug / release
	FrontendURL              string `mapstructure:"frontend_url"`
	PublicURL                string `mapstructure:"public_url"`
	ReadHeaderTimeoutSeconds int    `mapstructure:"read_header_timeout_seconds"`
	IdleTimeoutSeconds       int    `mapstructure:"idle_timeout_seconds"`
	ReadinessTimeoutSeconds  int    `mapstructure:"readiness_timeout_seconds"`
	RequestTimeoutSeconds    int    `mapstructure:"request_timeout_seconds"`
}

type HTTPClientConfig struct {
	ConnectTimeoutSeconds        int `mapstructure:"connect_timeout_seconds"`
	TLSHandshakeTimeoutSeconds   int `mapstructure:"tls_handshake_timeout_seconds"`
	ResponseHeaderTimeoutSeconds int `mapstructure:"response_header_timeout_seconds"`
	OverallTimeoutSeconds        int `mapstructure:"overall_timeout_seconds"`
	IdleConnTimeoutSeconds       int `mapstructure:"idle_conn_timeout_seconds"`
	MaxIdleConns                 int `mapstructure:"max_idle_conns"`
	MaxIdleConnsPerHost          int `mapstructure:"max_idle_conns_per_host"`
	MaxConnsPerHost              int `mapstructure:"max_conns_per_host"`
}

type RelayConfig struct {
	Provider       string `mapstructure:"provider"`
	URL            string `mapstructure:"url"`
	AdminAPIKey    string `mapstructure:"admin_api_key"`
	Model          string `mapstructure:"model"`
	DefaultGroupID string `mapstructure:"default_group_id"`
}

type DBConfig struct {
	DSN             string `mapstructure:"dsn"`
	MaxOpenConns    int    `mapstructure:"max_open_conns"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetime int    `mapstructure:"conn_max_lifetime"` // seconds
}

type RedisConfig struct {
	Addr      string `mapstructure:"addr"`
	Password  string `mapstructure:"password"`
	DB        int    `mapstructure:"db"`
	Namespace string `mapstructure:"namespace"`
}

type TeamUsagePrewarmConfig struct {
	Enabled   bool     `mapstructure:"enabled"`
	Timezones []string `mapstructure:"timezones"`
}

var redisNamespaceRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

func ValidateRedisNamespace(namespace string) error {
	if !redisNamespaceRE.MatchString(namespace) {
		return fmt.Errorf("redis namespace must match [A-Za-z0-9][A-Za-z0-9._-]{0,62}")
	}
	return nil
}

type AuthConfig struct {
	JWTSecret       string     `mapstructure:"jwt_secret"`
	AccessTokenTTL  int        `mapstructure:"access_token_ttl"`  // seconds, default 7200 (2h)
	RefreshTokenTTL int        `mapstructure:"refresh_token_ttl"` // seconds, default 604800 (7d)
	LDAP            LDAPConfig `mapstructure:"ldap"`
}

type LDAPConfig struct {
	URL          string `mapstructure:"url"`
	BaseDN       string `mapstructure:"base_dn"`
	BindDN       string `mapstructure:"bind_dn"`
	BindPassword string `mapstructure:"bind_password"`
	UserFilter   string `mapstructure:"user_filter"` // e.g. (uid=%s)
	TLS          bool   `mapstructure:"tls"`
}

type EncryptionConfig struct {
	Key string `mapstructure:"key"` // 32-byte hex-encoded AES-256 key
}

type VersionCheckConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	ReleaseAPIURL string `mapstructure:"release_api_url"`
}

func Load(path string) (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("server.port", 8081)
	v.SetDefault("server.mode", "debug")
	v.SetDefault("server.frontend_url", "http://localhost:5173")
	v.SetDefault("server.public_url", "")
	v.SetDefault("server.read_header_timeout_seconds", 5)
	v.SetDefault("server.idle_timeout_seconds", 120)
	v.SetDefault("server.readiness_timeout_seconds", 2)
	v.SetDefault("server.request_timeout_seconds", 35)
	v.SetDefault("metrics.listen_address", "127.0.0.1:9090")
	v.SetDefault("http_client.connect_timeout_seconds", 5)
	v.SetDefault("http_client.tls_handshake_timeout_seconds", 5)
	v.SetDefault("http_client.response_header_timeout_seconds", 15)
	v.SetDefault("http_client.overall_timeout_seconds", 30)
	v.SetDefault("http_client.idle_conn_timeout_seconds", 90)
	v.SetDefault("http_client.max_idle_conns", 100)
	v.SetDefault("http_client.max_idle_conns_per_host", 20)
	v.SetDefault("http_client.max_conns_per_host", 50)
	v.SetDefault("db.max_open_conns", 25)
	v.SetDefault("db.max_idle_conns", 5)
	v.SetDefault("db.conn_max_lifetime", 300)
	v.SetDefault("redis.addr", "redis:6379")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.namespace", "ai-efficiency")
	v.SetDefault("team_usage_prewarm.enabled", false)
	v.SetDefault("team_usage_prewarm.timezones", []string{"UTC", "Asia/Shanghai", "America/Los_Angeles", "Europe/Berlin"})
	v.SetDefault("relay.provider", "sub2api")
	v.SetDefault("relay.model", "claude-sonnet-4-20250514")
	v.SetDefault("relay.default_group_id", "")
	v.SetDefault("auth.access_token_ttl", 7200)
	v.SetDefault("auth.refresh_token_ttl", 604800)
	v.SetDefault("auth.ldap.user_filter", "(uid=%s)")
	v.SetDefault("version_check.enabled", true)
	v.SetDefault("version_check.release_api_url", "https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest")
	// Config file
	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./deploy")
	}

	// Environment variables with AE_ prefix
	v.SetEnvPrefix("AE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	for _, key := range []string{
		"server.port",
		"server.mode",
		"server.frontend_url",
		"server.public_url",
		"server.read_header_timeout_seconds",
		"server.idle_timeout_seconds",
		"server.readiness_timeout_seconds",
		"server.request_timeout_seconds",
		"metrics.listen_address",
		"http_client.connect_timeout_seconds",
		"http_client.tls_handshake_timeout_seconds",
		"http_client.response_header_timeout_seconds",
		"http_client.overall_timeout_seconds",
		"http_client.idle_conn_timeout_seconds",
		"http_client.max_idle_conns",
		"http_client.max_idle_conns_per_host",
		"http_client.max_conns_per_host",
		"db.dsn",
		"db.max_open_conns",
		"db.max_idle_conns",
		"db.conn_max_lifetime",
		"relay.provider",
		"relay.url",
		"relay.admin_api_key",
		"relay.model",
		"relay.default_group_id",
		"auth.jwt_secret",
		"auth.access_token_ttl",
		"auth.refresh_token_ttl",
		"auth.ldap.url",
		"auth.ldap.base_dn",
		"auth.ldap.bind_dn",
		"auth.ldap.bind_password",
		"auth.ldap.user_filter",
		"auth.ldap.tls",
		"encryption.key",
		"redis.addr",
		"redis.password",
		"redis.db",
		"redis.namespace",
		"team_usage_prewarm.enabled",
		"team_usage_prewarm.timezones",
		"version_check.enabled",
		"version_check.release_api_url",
	} {
		if err := v.BindEnv(key); err != nil {
			return nil, err
		}
	}
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok || path != "" {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	if err := ValidateRedisNamespace(cfg.Redis.Namespace); err != nil {
		return nil, fmt.Errorf("invalid redis namespace %q: %w", cfg.Redis.Namespace, err)
	}
	if err := validateMetricsConfig(cfg.Metrics); err != nil {
		return nil, err
	}
	if err := validateHTTPRuntime(cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateMetricsConfig(cfg MetricsConfig) error {
	address := strings.TrimSpace(cfg.ListenAddress)
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid metrics.listen_address %q: expected host:port", cfg.ListenAddress)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid metrics.listen_address %q: port must be between 1 and 65535", cfg.ListenAddress)
	}
	return nil
}

const (
	// BrowserRequestTimeoutSeconds is the fixed first-party browser deadline.
	BrowserRequestTimeoutSeconds = 45
	// VersionCheckTimeoutSeconds is the fixed release-check client deadline.
	VersionCheckTimeoutSeconds = 10
	// QuotaNotificationWebhookTimeoutSeconds is the fixed quota webhook deadline.
	QuotaNotificationWebhookTimeoutSeconds = 5

	maxReadHeaderTimeoutSeconds = 60
	maxIdleTimeoutSeconds       = 3600
	maxReadinessTimeoutSeconds  = 30
	maxRequestTimeoutSeconds    = BrowserRequestTimeoutSeconds - 1

	maxConnectTimeoutSeconds        = 30
	maxTLSHandshakeTimeoutSeconds   = 30
	maxResponseHeaderTimeoutSeconds = 60
	maxOverallTimeoutSeconds        = maxRequestTimeoutSeconds - 1
	maxIdleConnTimeoutSeconds       = 3600
	maxHTTPClientPoolSize           = 10000
)

func validateHTTPRuntime(cfg Config) error {
	durationFields := []struct {
		name  string
		value int
		max   int
	}{
		{name: "server.read_header_timeout_seconds", value: cfg.Server.ReadHeaderTimeoutSeconds, max: maxReadHeaderTimeoutSeconds},
		{name: "server.idle_timeout_seconds", value: cfg.Server.IdleTimeoutSeconds, max: maxIdleTimeoutSeconds},
		{name: "server.readiness_timeout_seconds", value: cfg.Server.ReadinessTimeoutSeconds, max: maxReadinessTimeoutSeconds},
		{name: "server.request_timeout_seconds", value: cfg.Server.RequestTimeoutSeconds, max: maxRequestTimeoutSeconds},
		{name: "http_client.connect_timeout_seconds", value: cfg.HTTPClient.ConnectTimeoutSeconds, max: maxConnectTimeoutSeconds},
		{name: "http_client.tls_handshake_timeout_seconds", value: cfg.HTTPClient.TLSHandshakeTimeoutSeconds, max: maxTLSHandshakeTimeoutSeconds},
		{name: "http_client.response_header_timeout_seconds", value: cfg.HTTPClient.ResponseHeaderTimeoutSeconds, max: maxResponseHeaderTimeoutSeconds},
		{name: "http_client.overall_timeout_seconds", value: cfg.HTTPClient.OverallTimeoutSeconds, max: maxOverallTimeoutSeconds},
		{name: "http_client.idle_conn_timeout_seconds", value: cfg.HTTPClient.IdleConnTimeoutSeconds, max: maxIdleConnTimeoutSeconds},
	}
	for _, field := range durationFields {
		if err := validatePositiveBound(field.name, field.value, field.max); err != nil {
			return err
		}
	}

	poolFields := []struct {
		name  string
		value int
	}{
		{name: "http_client.max_idle_conns", value: cfg.HTTPClient.MaxIdleConns},
		{name: "http_client.max_idle_conns_per_host", value: cfg.HTTPClient.MaxIdleConnsPerHost},
		{name: "http_client.max_conns_per_host", value: cfg.HTTPClient.MaxConnsPerHost},
	}
	for _, field := range poolFields {
		if err := validatePositiveBound(field.name, field.value, maxHTTPClientPoolSize); err != nil {
			return err
		}
	}

	if cfg.HTTPClient.OverallTimeoutSeconds <= VersionCheckTimeoutSeconds {
		return fmt.Errorf("http_client.overall_timeout_seconds must be greater than the fixed %d-second version check timeout", VersionCheckTimeoutSeconds)
	}

	if cfg.HTTPClient.ConnectTimeoutSeconds >= cfg.HTTPClient.OverallTimeoutSeconds {
		return fmt.Errorf("http_client.connect_timeout_seconds must be less than http_client.overall_timeout_seconds")
	}
	if cfg.HTTPClient.TLSHandshakeTimeoutSeconds >= cfg.HTTPClient.OverallTimeoutSeconds {
		return fmt.Errorf("http_client.tls_handshake_timeout_seconds must be less than http_client.overall_timeout_seconds")
	}
	if cfg.HTTPClient.ResponseHeaderTimeoutSeconds >= cfg.HTTPClient.OverallTimeoutSeconds {
		return fmt.Errorf("http_client.response_header_timeout_seconds must be less than http_client.overall_timeout_seconds")
	}
	if cfg.HTTPClient.OverallTimeoutSeconds >= cfg.Server.RequestTimeoutSeconds {
		return fmt.Errorf("http_client.overall_timeout_seconds must be less than server.request_timeout_seconds")
	}
	if cfg.Server.ReadinessTimeoutSeconds >= cfg.Server.RequestTimeoutSeconds {
		return fmt.Errorf("server.readiness_timeout_seconds must be less than server.request_timeout_seconds")
	}
	return nil
}

func validatePositiveBound(name string, value, max int) error {
	if value <= 0 {
		return fmt.Errorf("%s must be greater than zero", name)
	}
	if value > max {
		return fmt.Errorf("%s must be at most %d", name, max)
	}
	return nil
}
