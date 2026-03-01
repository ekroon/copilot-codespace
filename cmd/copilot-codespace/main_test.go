package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildMCPConfig(t *testing.T) {
	result := buildMCPConfig("/usr/local/bin/self", "my-codespace", "/workspaces/repo", nil)

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

func TestBuildMCPConfigWithRemoteServers(t *testing.T) {
	remoteMCP := map[string]any{
		"my-tool": map[string]any{
			"type":    "local",
			"command": "gh",
			"args":    []string{"codespace", "ssh", "-c", "cs", "--", "my-tool"},
		},
	}

	result := buildMCPConfig("/usr/local/bin/self", "cs", "/workspaces/repo", remoteMCP)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	servers := parsed["mcpServers"].(map[string]any)

	// Our server should still be present
	if _, ok := servers["codespace"]; !ok {
		t.Error("missing codespace server")
	}
	// Remote server should be merged
	if _, ok := servers["my-tool"]; !ok {
		t.Error("missing remote my-tool server")
	}
}

func TestBuildMCPConfigRemoteCannotOverrideCodespace(t *testing.T) {
	remoteMCP := map[string]any{
		"codespace": map[string]any{
			"type":    "local",
			"command": "evil",
		},
	}

	result := buildMCPConfig("/usr/local/bin/self", "cs", "/workspaces/repo", remoteMCP)

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)

	servers := parsed["mcpServers"].(map[string]any)
	cs := servers["codespace"].(map[string]any)

	// Should still be our binary, not the overridden one
	if got := cs["command"]; got != "/usr/local/bin/self" {
		t.Errorf("codespace command = %v, should not be overridden", got)
	}
}

func TestRewriteMCPServerForSSH(t *testing.T) {
	server := map[string]any{
		"type":    "stdio",
		"command": "/usr/local/bin/test-mcp",
		"args":    []any{"--mode", "dev"},
		"env": map[string]any{
			"API_KEY": "secret",
		},
	}

	result := rewriteMCPServerForSSH(server, "my-cs", "/workspaces/repo")

	if result == nil {
		t.Fatal("rewriteMCPServerForSSH returned nil")
	}

	if got := result["command"]; got != "gh" {
		t.Errorf("command = %v, want gh", got)
	}

	args, ok := result["args"].([]string)
	if !ok {
		t.Fatal("args not []string")
	}

	// Should contain: codespace ssh -c my-cs -- bash -c <remote-cmd>
	if len(args) < 6 {
		t.Fatalf("args too short: %v", args)
	}
	if args[0] != "codespace" || args[1] != "ssh" {
		t.Errorf("args should start with [codespace ssh], got %v", args[:2])
	}

	// The remote command should contain the original command
	remoteCmd := args[len(args)-1]
	if !contains(remoteCmd, "/usr/local/bin/test-mcp") {
		t.Errorf("remote command should contain original command, got %q", remoteCmd)
	}
	if !contains(remoteCmd, "--mode") {
		t.Errorf("remote command should contain args, got %q", remoteCmd)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestCleanMirrorDir(t *testing.T) {
	dir := t.TempDir()

	// Create some files and directories including .git
	os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".github"), 0o755)
	os.WriteFile(filepath.Join(dir, ".github", "copilot-instructions.md"), []byte("hi"), 0o644)
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agents"), 0o644)
	os.MkdirAll(filepath.Join(dir, "docs"), 0o755)
	os.WriteFile(filepath.Join(dir, "docs", "AGENTS.md"), []byte("docs agents"), 0o644)

	cleanMirrorDir(dir)

	// .git should survive
	if _, err := os.Stat(filepath.Join(dir, ".git", "HEAD")); err != nil {
		t.Error(".git/HEAD should survive cleanup")
	}

	// Everything else should be gone
	for _, name := range []string{".github", "AGENTS.md", "docs"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s should have been removed", name)
		}
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
