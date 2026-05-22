package config

import (
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	DB         DBConfig         `mapstructure:"db"`
	Redis      RedisConfig      `mapstructure:"redis"`
	Auth       AuthConfig       `mapstructure:"auth"`
	Encryption EncryptionConfig `mapstructure:"encryption"`
	Relay      RelayConfig      `mapstructure:"relay"`
}

type ServerConfig struct {
	Port        int    `mapstructure:"port"`
	Mode        string `mapstructure:"mode"` // debug / release
	FrontendURL string `mapstructure:"frontend_url"`
}

type RelayConfig struct {
	Provider       string `mapstructure:"provider"`
	URL            string `mapstructure:"url"`
	AdminURL       string `mapstructure:"admin_url"`
	APIKey         string `mapstructure:"api_key"`
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
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
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

func Load(path string) (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("server.port", 8081)
	v.SetDefault("server.mode", "debug")
	v.SetDefault("server.frontend_url", "http://localhost:5173")
	v.SetDefault("db.max_open_conns", 25)
	v.SetDefault("db.max_idle_conns", 5)
	v.SetDefault("db.conn_max_lifetime", 300)
	v.SetDefault("redis.addr", "redis:6379")
	v.SetDefault("redis.db", 0)
	v.SetDefault("relay.provider", "sub2api")
	v.SetDefault("relay.model", "claude-sonnet-4-20250514")
	v.SetDefault("relay.default_group_id", "")
	v.SetDefault("auth.access_token_ttl", 7200)
	v.SetDefault("auth.refresh_token_ttl", 604800)
	v.SetDefault("auth.ldap.user_filter", "(uid=%s)")
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
		"db.dsn",
		"db.max_open_conns",
		"db.max_idle_conns",
		"db.conn_max_lifetime",
		"relay.provider",
		"relay.url",
		"relay.admin_url",
		"relay.api_key",
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

	return &cfg, nil
}
