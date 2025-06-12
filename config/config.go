package config

import (
	"encoding/json"
	"os"
)

// Config stores bot configuration
type Config struct {
	BotToken string          `json:"bot_token"`
	MongoURI string          `json:"mongo_uri"`
	Admins   map[string]bool `json:"admins"`
}

// LoadConfig loads configuration from JSON file
func LoadConfig(path string) (*Config, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(file, &config); err != nil {
		return nil, err
	}

	// Initialize admins map if it's nil
	if config.Admins == nil {
		config.Admins = make(map[string]bool)
	}

	return &config, nil
}

// SaveConfig saves configuration to JSON file
func SaveConfig(config *Config, path string) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// IsAdmin checks if the username is in the admins list
func (c *Config) IsAdmin(username string) bool {
	return c.Admins[username]
}

// SetAdmin adds a username to the admin list
func (c *Config) SetAdmin(username string, isAdmin bool) {
	c.Admins[username] = isAdmin
}
