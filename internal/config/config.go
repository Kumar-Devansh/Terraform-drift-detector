package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/driftctl/driftctl/internal/model"
)

// File represents the driftctl.yaml configuration.
type File struct {
	Database      string              `yaml:"database"`
	API           APIConfig           `yaml:"api"`
	Notifications NotificationsConfig `yaml:"notifications"`
	Workspaces    []model.Workspace   `yaml:"workspaces"`
}

// NotificationsConfig configures optional drift notification providers.
type NotificationsConfig struct {
	Slack SlackConfig `yaml:"slack"`
	Email EmailConfig `yaml:"email"`
}

type EmailConfig struct {
	Enabled  bool     `yaml:"enabled"`
	SMTPHost string   `yaml:"smtp_host"`
	SMTPPort int      `yaml:"smtp_port"`
	Username string   `yaml:"username"`
	Password string   `yaml:"password"`
	From     string   `yaml:"from"`
	To       []string `yaml:"to"`
}

type SlackConfig struct {
	Enabled    bool   `yaml:"enabled"`
	WebhookURL string `yaml:"webhook_url"`
}

// APIConfig holds server settings.
type APIConfig struct {
	Addr   string `yaml:"addr"`
	APIKey string `yaml:"api_key"`
}

// Load reads configuration from a YAML file.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg File
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Database == "" {
		cfg.Database = "driftctl.db"
	}
	if cfg.API.Addr == "" {
		cfg.API.Addr = ":8080"
	}
	return &cfg, nil
}
