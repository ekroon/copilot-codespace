package provisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ConfigEntry represents a user-defined provisioner from config JSON.
type ConfigEntry struct {
	Name         string          `json:"name"`
	Match        *MatchCondition `json:"match,omitempty"`
	Bash         string          `json:"bash,omitempty"`
	LocalCommand string          `json:"localCommand,omitempty"`
	PipeToRemote string          `json:"pipeToRemote,omitempty"`
	RunOn        string          `json:"runOn,omitempty"` // "codespace" (default) or "local"
}

// MatchCondition specifies when a provisioner should run.
type MatchCondition struct {
	Terminal   string `json:"terminal,omitempty"`
	Repository string `json:"repository,omitempty"`
}

// Config is the top-level config file structure.
type Config struct {
	Builtins     map[string]bool `json:"builtins,omitempty"`
	Provisioners []ConfigEntry   `json:"provisioners"`
}

// LoadSettings reads provisioner config from the default location.
// Returns an empty config (not error) if no config file exists.
func LoadSettings() (Config, error) {
	path := defaultConfigPath()
	return LoadSettingsFrom(path)
}

// LoadSettingsFrom reads provisioner config from a specific path.
func LoadSettingsFrom(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("reading provisioner config: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parsing provisioner config: %w", err)
	}
	return config, nil
}

// LoadConfig reads user-defined provisioners from the default location.
// Returns an empty list (not error) if no config file exists.
func LoadConfig() ([]Provisioner, error) {
	config, err := LoadSettings()
	if err != nil {
		return nil, err
	}
	return ProvisionersFromConfig(config), nil
}

// LoadConfigFrom reads user-defined provisioners from a specific path.
func LoadConfigFrom(path string) ([]Provisioner, error) {
	config, err := LoadSettingsFrom(path)
	if err != nil {
		return nil, err
	}
	return ProvisionersFromConfig(config), nil
}

func defaultConfigPath() string {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "copilot-codespace", "provisioners.json")
}

// ProvisionersFromConfig builds provisioners from parsed config entries.
func ProvisionersFromConfig(config Config) []Provisioner {
	var result []Provisioner
	for _, entry := range config.Provisioners {
		result = append(result, &configProvisioner{entry: entry})
	}
	return result
}

// configProvisioner wraps a ConfigEntry as a Provisioner.
type configProvisioner struct {
	entry ConfigEntry
}

func (p *configProvisioner) Name() string { return p.entry.Name }

func (p *configProvisioner) ShouldRun(ctx RunContext) bool {
	if p.entry.Match == nil {
		return true
	}
	if p.entry.Match.Terminal != "" && p.entry.Match.Terminal != ctx.Terminal {
		return false
	}
	if p.entry.Match.Repository != "" && p.entry.Match.Repository != ctx.Repository {
		return false
	}
	return true
}

func (p *configProvisioner) Run(ctx context.Context, target CodespaceTarget) error {
	if p.entry.Bash != "" {
		_, err := target.RunSSH(ctx, p.entry.Bash)
		return err
	}
	return nil
}
