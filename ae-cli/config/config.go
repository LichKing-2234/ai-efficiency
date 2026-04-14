package config

import (
	"fmt"
	"os"
	"os/exec"
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

var defaultToolCandidates = []struct {
	Name string
	Cfg  ToolConfig
}{
	{
		Name: "claude",
		Cfg: ToolConfig{
			Command: "claude",
		},
	},
	{
		Name: "codex",
		Cfg: ToolConfig{
			Command: "codex",
		},
	},
	{
		Name: "kiro",
		Cfg: ToolConfig{
			Command: "kiro",
		},
	},
}

func Load(cfgFile string) (*Config, error) {
	viper.Reset()

	configPath := ""
	if cfgFile != "" {
		configPath = cfgFile
	} else {
		var err error
		configPath, err = findDefaultConfigFile()
		if err != nil {
			return nil, err
		}
	}

	if configPath != "" {
		viper.SetConfigFile(configPath)
		if err := viper.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return nil, fmt.Errorf("reading config: %w", err)
			}
		}
	}

	viper.AutomaticEnv()

	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}
	autoDetectTools(cfg)

	return cfg, nil
}

func findDefaultConfigFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}

	configDir := filepath.Join(home, ".ae-cli")
	for _, name := range []string{"config.yaml", "config.yml"} {
		path := filepath.Join(configDir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat config file %s: %w", path, err)
		}
	}

	return "", nil
}

func autoDetectTools(cfg *Config) {
	if cfg == nil || len(cfg.Tools) != 0 {
		return
	}

	tools := make(map[string]ToolConfig)
	for _, candidate := range defaultToolCandidates {
		if _, err := exec.LookPath(candidate.Cfg.Command); err != nil {
			continue
		}
		tools[candidate.Name] = candidate.Cfg
	}
	if len(tools) == 0 {
		return
	}
	cfg.Tools = tools
}
