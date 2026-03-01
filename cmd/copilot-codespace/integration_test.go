//go:build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekroon/copilot-codespace/internal/ssh"
)

// These tests require a running codespace with test fixtures.
// Run: TEST_CODESPACE=<name> go test -tags integration -v ./cmd/copilot-codespace/

func testCodespace(t *testing.T) string {
	t.Helper()
	cs := os.Getenv("TEST_CODESPACE")
	if cs == "" {
		t.Skip("TEST_CODESPACE not set")
	}
	return cs
}

func testWorkdir(t *testing.T) string {
	t.Helper()
	wd := os.Getenv("TEST_WORKDIR")
	if wd == "" {
		return "/workspaces/ekroon"
	}
	return wd
}

func testSSHClient(t *testing.T, cs string) *ssh.Client {
	t.Helper()
	client := ssh.NewClient(cs)
	ctx := context.Background()
	if err := client.SetupMultiplexing(ctx); err != nil {
		t.Logf("SSH multiplexing not available, using fallback: %v", err)
	}
	return client
}

// testFetchInstructionFiles wraps fetchInstructionFiles with SSH client setup.
func testFetchInstructionFiles(t *testing.T, cs, wd string) (string, map[string]any, error) {
	t.Helper()
	client := testSSHClient(t, cs)
	return fetchInstructionFiles(client, cs, wd)
}

func setupMirrorDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Initialize as git repo like the real code does
	exec.Command("git", "-C", dir, "init", "-q").Run()
	return dir
}

func TestIntegration_RootInstructionFiles(t *testing.T) {
	cs := testCodespace(t)
	wd := testWorkdir(t)

	// Run fetchInstructionFiles against the real codespace
	dir, _, err := testFetchInstructionFiles(t, cs, wd)
	if err != nil {
		t.Fatalf("fetchInstructionFiles: %v", err)
	}
	defer os.RemoveAll(dir)

	// Root-level files should be fetched
	expectFile(t, dir, ".github/copilot-instructions.md")
	expectFile(t, dir, "AGENTS.md")
	expectFile(t, dir, "CLAUDE.md")
	expectFile(t, dir, "GEMINI.md")
}

func TestIntegration_ScopedInstructionFiles(t *testing.T) {
	cs := testCodespace(t)
	wd := testWorkdir(t)

	dir, _, err := testFetchInstructionFiles(t, cs, wd)
	if err != nil {
		t.Fatalf("fetchInstructionFiles: %v", err)
	}
	defer os.RemoveAll(dir)

	// Scoped instruction files (including nested) should be fetched
	expectFile(t, dir, ".github/instructions/ruby.instructions.md")
	expectFile(t, dir, ".github/instructions/frontend/react.instructions.md")
	expectFile(t, dir, ".github/instructions/backend/api/rest.instructions.md")
}

func TestIntegration_HierarchicalAgentFiles(t *testing.T) {
	cs := testCodespace(t)
	wd := testWorkdir(t)

	dir, _, err := testFetchInstructionFiles(t, cs, wd)
	if err != nil {
		t.Fatalf("fetchInstructionFiles: %v", err)
	}
	defer os.RemoveAll(dir)

	// Root-level agents should be present
	expectFile(t, dir, "AGENTS.md")
	expectFile(t, dir, "CLAUDE.md")

	// Hierarchical (subdirectory) agent files should also be fetched
	expectFile(t, dir, "docs/AGENTS.md")
	expectFile(t, dir, "docs/CLAUDE.md")
	expectFile(t, dir, "teams/backend/AGENTS.md")
}

func TestIntegration_HierarchicalFileContent(t *testing.T) {
	cs := testCodespace(t)
	wd := testWorkdir(t)

	dir, _, err := testFetchInstructionFiles(t, cs, wd)
	if err != nil {
		t.Fatalf("fetchInstructionFiles: %v", err)
	}
	defer os.RemoveAll(dir)

	// Verify content to ensure we got the right files (not duplicates)
	rootContent := readFileContent(t, filepath.Join(dir, "AGENTS.md"))
	docsContent := readFileContent(t, filepath.Join(dir, "docs/AGENTS.md"))

	if rootContent == docsContent {
		t.Error("root AGENTS.md and docs/AGENTS.md should have different content")
	}
	if !strings.Contains(rootContent, "Root") {
		t.Errorf("root AGENTS.md should contain 'Root', got: %s", rootContent)
	}
	if !strings.Contains(docsContent, "Docs") {
		t.Errorf("docs/AGENTS.md should contain 'Docs', got: %s", docsContent)
	}
}

func TestIntegration_MCPConfigRewriting(t *testing.T) {
	cs := testCodespace(t)
	wd := testWorkdir(t)

	_, remoteMCP, err := testFetchInstructionFiles(t, cs, wd)
	if err != nil {
		t.Fatalf("fetchInstructionFiles: %v", err)
	}

	if remoteMCP == nil {
		t.Fatal("remoteMCPServers should not be nil (test codespace has .copilot/mcp-config.json)")
	}

	// remoteMCP contains the raw (unrewritten) server configs from the codespace
	testServer, ok := remoteMCP["test-server"]
	if !ok {
		t.Fatal("missing test-server in raw MCP config")
	}

	server, ok := testServer.(map[string]any)
	if !ok {
		t.Fatal("test-server should be a map")
	}

	// Raw config should have the original command (python3)
	if cmd, _ := server["command"].(string); cmd != "python3" {
		t.Errorf("raw command = %q, want 'python3'", cmd)
	}

	// Verify buildMCPConfig rewrites it to use gh
	mcpConfig := buildMCPConfig("/usr/local/bin/self", cs, wd, remoteMCP)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(mcpConfig), &parsed); err != nil {
		t.Fatalf("invalid merged MCP config JSON: %v", err)
	}

	servers := parsed["mcpServers"].(map[string]any)
	if _, ok := servers["codespace"]; !ok {
		t.Error("merged config should contain 'codespace' server")
	}
	rewrittenServer, ok := servers["test-server"].(map[string]any)
	if !ok {
		t.Fatal("merged config should contain 'test-server'")
	}
	if cmd, _ := rewrittenServer["command"].(string); cmd != "gh" {
		t.Errorf("rewritten command = %q, want 'gh'", cmd)
	}
}

func TestIntegration_MCPForwardingEndToEnd(t *testing.T) {
	cs := testCodespace(t)
	wd := testWorkdir(t)

	// Test that the rewritten MCP server config actually works by sending
	// an initialize request through SSH to the test MCP server
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}`

	// Run the MCP server command via SSH (simulating what the rewritten config does)
	cmd := exec.Command("gh", "codespace", "ssh", "-c", cs, "--",
		"python3", filepath.Join(wd, ".copilot/test-mcp-server.py"))
	cmd.Stdin = strings.NewReader(initReq + "\n")

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("MCP server via SSH failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("invalid JSON-RPC response: %v (raw: %s)", err, string(out))
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result in response: %v", resp)
	}

	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("missing serverInfo in result: %v", result)
	}

	if name, _ := serverInfo["name"].(string); name != "test-mcp" {
		t.Errorf("serverInfo.name = %q, want 'test-mcp'", name)
	}
}

func TestIntegration_StaleFileCleanup(t *testing.T) {
	cs := testCodespace(t)
	wd := testWorkdir(t)

	// First fetch
	dir, _, err := testFetchInstructionFiles(t, cs, wd)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	defer os.RemoveAll(dir)

	// Initialize git repo (normally done in runLauncher)
	exec.Command("git", "-C", dir, "init", "-q").Run()

	// Plant a stale file that shouldn't survive re-fetch
	staleDir := filepath.Join(dir, "old-subdir")
	os.MkdirAll(staleDir, 0o755)
	os.WriteFile(filepath.Join(staleDir, "AGENTS.md"), []byte("stale"), 0o644)

	// Also plant a stale file in .github that doesn't exist on remote
	os.WriteFile(filepath.Join(dir, ".github", "stale-file.md"), []byte("stale"), 0o644)

	// Re-fetch (the function creates a deterministic dir, so we need to
	// simulate by calling cleanMirrorDir + the fetch logic again)
	cleanMirrorDir(dir)

	// Stale files should be gone
	if _, err := os.Stat(filepath.Join(staleDir, "AGENTS.md")); err == nil {
		t.Error("stale old-subdir/AGENTS.md should have been removed")
	}
	if _, err := os.Stat(filepath.Join(dir, ".github", "stale-file.md")); err == nil {
		t.Error("stale .github/stale-file.md should have been removed")
	}

	// .git should survive
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Error(".git should survive cleanup")
	}
}

func TestIntegration_ScopedInstructionFrontmatter(t *testing.T) {
	cs := testCodespace(t)
	wd := testWorkdir(t)

	dir, _, err := testFetchInstructionFiles(t, cs, wd)
	if err != nil {
		t.Fatalf("fetchInstructionFiles: %v", err)
	}
	defer os.RemoveAll(dir)

	// Verify that scoped instruction files have their frontmatter preserved
	content := readFileContent(t, filepath.Join(dir, ".github/instructions/ruby.instructions.md"))

	if !strings.Contains(content, "applyTo") {
		t.Error("ruby.instructions.md should contain applyTo frontmatter")
	}
	if !strings.Contains(content, "**/*.rb") {
		t.Error("ruby.instructions.md should contain the glob pattern **/*.rb")
	}
}

func TestIntegration_ApplyToWorksWithCopilotCLI(t *testing.T) {
	cs := testCodespace(t)
	wd := testWorkdir(t)

	// Set up the mirror directory
	dir, _, err := testFetchInstructionFiles(t, cs, wd)
	if err != nil {
		t.Fatalf("fetchInstructionFiles: %v", err)
	}
	defer os.RemoveAll(dir)

	// Initialize as git repo (like runLauncher does)
	exec.Command("git", "-C", dir, "init", "-q").Run()

	// Check that copilot is available
	copilotPath, err := exec.LookPath("copilot")
	if err != nil {
		t.Skip("copilot not found in PATH")
	}

	// Run copilot -p from the mirror dir asking it to list loaded instructions
	cmd := exec.Command(copilotPath,
		"-p", "List the file paths of ALL custom instruction files you have loaded. Just list the paths, one per line, nothing else.",
		"--allow-all-tools",
		"--quiet",
	)
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		// --quiet might not be supported, try without
		cmd = exec.Command(copilotPath,
			"-p", "List the file paths of ALL custom instruction files you have loaded. Just list the paths, one per line, nothing else.",
			"--allow-all-tools",
		)
		cmd.Dir = dir
		out, err = cmd.Output()
		if err != nil {
			t.Fatalf("copilot -p failed: %v", err)
		}
	}

	output := string(out)

	// Copilot should have loaded the scoped instruction files with applyTo patterns
	if !strings.Contains(output, "react.instructions.md") {
		t.Errorf("copilot should have loaded react.instructions.md (applyTo: **/*.tsx,**/*.jsx)\nOutput: %s", output)
	}

	// Root instruction files should also be loaded
	if !strings.Contains(output, "AGENTS.md") {
		t.Errorf("copilot should have loaded AGENTS.md\nOutput: %s", output)
	}
}

// --- helpers ---

func expectFile(t *testing.T, dir, relPath string) {
	t.Helper()
	fullPath := filepath.Join(dir, relPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		t.Errorf("expected file %s: %v", relPath, err)
		return
	}
	if info.Size() == 0 {
		t.Errorf("file %s exists but is empty", relPath)
	}
}

func readFileContent(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// ensureTestFixtures verifies the test codespace has the expected files.
// Call this to get a clear error if fixtures are missing.
func ensureTestFixtures(t *testing.T, cs, wd string) {
	t.Helper()
	required := []string{
		".github/copilot-instructions.md",
		"AGENTS.md",
		"CLAUDE.md",
		"GEMINI.md",
		"docs/AGENTS.md",
		"teams/backend/AGENTS.md",
		".github/instructions/ruby.instructions.md",
		".copilot/mcp-config.json",
		".copilot/test-mcp-server.py",
	}

	for _, f := range required {
		remotePath := fmt.Sprintf("%s/%s", wd, f)
		if exec.Command("gh", "codespace", "ssh", "-c", cs, "--",
			fmt.Sprintf("test -f %s", remotePath)).Run() != nil {
			t.Fatalf("missing test fixture on codespace: %s\nRun the fixture setup commands first.", f)
		}
	}
}
