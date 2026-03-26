package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type ToolConfig struct {
	Command string   `mapstructure:"command"`
	Args    []string `mapstructure:"args"`
}

type ServerConfig struct {
	URL   string `mapstructure:"url"`
	Token string `mapstructure:"token"`
}

type Sub2apiConfig struct {
	APIKeyEnv string `mapstructure:"api_key_env"`
	URL       string `mapstructure:"url"`
	Model     string `mapstructure:"model"`
}

type Config struct {
	Server  ServerConfig          `mapstructure:"server"`
	Sub2api Sub2apiConfig         `mapstructure:"sub2api"`
	Tools   map[string]ToolConfig `mapstructure:"tools"`
}

func Load(cfgFile string) (*Config, error) {
	viper.Reset()

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("finding home directory: %w", err)
		}
		viper.AddConfigPath(filepath.Join(home, ".ae-cli"))
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	viper.AutomaticEnv()

	cfg := &Config{}
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	return cfg, nil
}
