package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildMCPConfig(t *testing.T) {
	result := buildMCPConfig("/usr/local/bin/self", "my-codespace", "/workspaces/repo")

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("buildMCPConfig returned invalid JSON: %v", err)
	}

	servers, ok := parsed["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("missing mcpServers key")
	}
	cs, ok := servers["codespace"].(map[string]any)
	if !ok {
		t.Fatal("missing mcpServers.codespace key")
	}

	if got := cs["command"]; got != "/usr/local/bin/self" {
		t.Errorf("command = %v, want /usr/local/bin/self", got)
	}

	args, ok := cs["args"].([]any)
	if !ok || len(args) != 1 || args[0] != "mcp" {
		t.Errorf("args = %v, want [mcp]", cs["args"])
	}

	env, ok := cs["env"].(map[string]any)
	if !ok {
		t.Fatal("missing env key")
	}
	if got := env["CODESPACE_NAME"]; got != "my-codespace" {
		t.Errorf("CODESPACE_NAME = %v, want my-codespace", got)
	}
	if got := env["CODESPACE_WORKDIR"]; got != "/workspaces/repo" {
		t.Errorf("CODESPACE_WORKDIR = %v, want /workspaces/repo", got)
	}
}

func TestEnsureTrustedFolder(t *testing.T) {
	// Point HOME to a temp dir so ensureTrustedFolder writes there
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	configDir := filepath.Join(tmpHome, ".copilot")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"trusted_folders":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := "/some/trusted/dir"

	// First call: should add the folder
	if err := ensureTrustedFolder(dir); err != nil {
		t.Fatalf("first call: %v", err)
	}
	assertTrustedFolders(t, configPath, []string{dir})

	// Second call: should not duplicate
	if err := ensureTrustedFolder(dir); err != nil {
		t.Fatalf("second call: %v", err)
	}
	assertTrustedFolders(t, configPath, []string{dir})
}

func assertTrustedFolders(t *testing.T, configPath string, want []string) {
	t.Helper()
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	raw, _ := config["trusted_folders"].([]any)
	var got []string
	for _, v := range raw {
		if s, ok := v.(string); ok {
			got = append(got, s)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("trusted_folders = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("trusted_folders[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
