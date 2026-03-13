package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ekroon/gh-copilot-codespace/internal/provisioner"
)

func TestLoadProvisioners_IncludesBuiltIns(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if got, want := provisionerNames(loadProvisioners()), []string{"terminfo", "git-fetch"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loadProvisioners() = %v, want %v", got, want)
	}
}

func TestLoadProvisioners_AppendsCustomConfig(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	configDir := filepath.Join(configHome, "copilot-codespace")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data := `{"provisioners":[{"name":"custom-setup","bash":"echo hi"}]}`
	if err := os.WriteFile(filepath.Join(configDir, "provisioners.json"), []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got, want := provisionerNames(loadProvisioners()), []string{"terminfo", "git-fetch", "custom-setup"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loadProvisioners() = %v, want %v", got, want)
	}
}

func TestLoadProvisioners_CanDisableBuiltIns(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	configDir := filepath.Join(configHome, "copilot-codespace")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data := `{
		"builtins": {
			"terminfo": false,
			"git-fetch": false
		},
		"provisioners": [
			{"name":"custom-setup","bash":"echo hi"}
		]
	}`
	if err := os.WriteFile(filepath.Join(configDir, "provisioners.json"), []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got, want := provisionerNames(loadProvisioners()), []string{"custom-setup"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loadProvisioners() = %v, want %v", got, want)
	}
}

func TestLoadProvisioners_InvalidConfigStillReturnsBuiltIns(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	configDir := filepath.Join(configHome, "copilot-codespace")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "provisioners.json"), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got, want := provisionerNames(loadProvisioners()), []string{"terminfo", "git-fetch"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loadProvisioners() = %v, want %v", got, want)
	}
}

func provisionerNames(provs []provisioner.Provisioner) []string {
	names := make([]string, 0, len(provs))
	for _, p := range provs {
		names = append(names, p.Name())
	}
	return names
}
