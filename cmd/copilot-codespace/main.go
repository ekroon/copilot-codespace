package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ekroon/copilot-codespace/internal/mcp"
	"github.com/ekroon/copilot-codespace/internal/shellpatch"
	"github.com/ekroon/copilot-codespace/internal/ssh"
	"github.com/mark3labs/mcp-go/server"
)

type codespace struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Repository  string `json:"repository"`
	State       string `json:"state"`
}

func main() {
	// If first arg is "mcp", run as MCP server (called by copilot via --additional-mcp-config)
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		runMCPServer()
		return
	}

	// Otherwise, run as interactive launcher
	if err := runLauncher(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runMCPServer() {
	codespaceName := os.Getenv("CODESPACE_NAME")
	if codespaceName == "" {
		fmt.Fprintln(os.Stderr, "CODESPACE_NAME environment variable is required")
		os.Exit(1)
	}

	sshClient := ssh.NewClient(codespaceName)

	// Establish SSH multiplexing for fast command execution
	ctx := context.Background()
	if err := sshClient.SetupMultiplexing(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "codespace-mcp: multiplexing setup warning: %v\n", err)
	}

	mcpServer := mcp.NewServer(sshClient, codespaceName)

	log.SetOutput(os.Stderr)
	log.Printf("codespace-mcp: starting for codespace %q", codespaceName)

	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("codespace-mcp: server error: %v", err)
	}
}

func runLauncher(args []string) error {
	// Parse --experimental-shell flag (consume it, don't pass to copilot)
	experimentalShell := false
	var copilotArgs []string
	for _, arg := range args {
		if arg == "--experimental-shell" {
			experimentalShell = true
		} else {
			copilotArgs = append(copilotArgs, arg)
		}
	}

	// The binary serves as both launcher and MCP server
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}

	// Use gh's built-in interactive codespace picker
	selected, err := selectCodespace()
	if err != nil {
		return err
	}
	fmt.Printf("Selected: %s (%s)\n", selected.DisplayName, selected.Repository)

	// Start codespace if needed
	if selected.State != "Available" {
		if err := startCodespace(selected.Name); err != nil {
			return err
		}
	}

	// Detect workspace directory
	workdir, err := detectWorkdir(selected.Name)
	if err != nil {
		return err
	}
	fmt.Printf("Workspace: %s\n", workdir)

	// Fetch instruction files into a deterministic dir that acts as the cwd
	instructionsDir, err := fetchInstructionFiles(selected.Name, workdir)
	if err != nil {
		return fmt.Errorf("fetching instructions: %w", err)
	}

	// Ensure the directory is trusted by copilot so it doesn't prompt each time
	if err := ensureTrustedFolder(instructionsDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not auto-trust directory: %v\n", err)
	}

	// Initialize as git repo so copilot treats it as a repo root and loads instructions
	exec.Command("git", "-C", instructionsDir, "init", "-q").Run()

	// Change to the instructions dir so copilot finds the instruction files
	if err := os.Chdir(instructionsDir); err != nil {
		return fmt.Errorf("changing to instructions dir: %w", err)
	}

	// Build MCP config — points to this same binary with "mcp" subcommand
	mcpConfig := buildMCPConfig(self, selected.Name, workdir)

	// Excluded tools — only local file/shell tools that have remote equivalents
	// Keep task (sub-agents), web_fetch, ask_user, sql, etc.
	excludedTools := []string{
		"edit", "create", "bash", "write_bash", "read_bash",
		"stop_bash", "list_bash", "view", "grep", "glob",
	}

	fmt.Printf("\nLaunching Copilot CLI with remote codespace tools...\n")
	fmt.Printf("  Codespace: %s\n", selected.Name)
	fmt.Printf("  Workspace: %s\n", workdir)
	fmt.Printf("  Excluded:  %d local tools\n", len(excludedTools))
	if experimentalShell {
		fmt.Printf("  Shell:     ! commands execute on codespace (experimental)\n")
	}
	fmt.Printf("\n  Shell access (from another terminal):\n")
	fmt.Printf("    gh codespace ssh -c %s\n\n", selected.Name)

	// Exec copilot from the instructions dir (cwd is already set)
	if experimentalShell {
		// Get SSH connection details for the shell patch
		sshClient := ssh.NewClient(selected.Name)
		ctx := context.Background()
		if err := sshClient.SetupMultiplexing(ctx); err != nil {
			return fmt.Errorf("setting up SSH for shell patch: %w", err)
		}
		return execCopilotWithShellPatch(excludedTools, mcpConfig, copilotArgs, sshClient, workdir)
	}
	return execCopilot(excludedTools, mcpConfig, copilotArgs)
}

// selectCodespace lets the user pick a codespace interactively.
// Uses gum filter for fuzzy search if available, otherwise falls back to a numbered list.
func selectCodespace() (codespace, error) {
	out, err := exec.Command("gh", "codespace", "list",
		"--json", "name,displayName,repository,state",
		"--limit", "50").Output()
	if err != nil {
		return codespace{}, fmt.Errorf("listing codespaces: %w", err)
	}

	var codespaces []codespace
	if err := json.Unmarshal(out, &codespaces); err != nil {
		return codespace{}, fmt.Errorf("parsing codespace list: %w", err)
	}
	if len(codespaces) == 0 {
		return codespace{}, fmt.Errorf("no codespaces found")
	}

	// Sort: available first, then by display name
	sort.Slice(codespaces, func(i, j int) bool {
		if (codespaces[i].State == "Available") != (codespaces[j].State == "Available") {
			return codespaces[i].State == "Available"
		}
		return codespaces[i].DisplayName < codespaces[j].DisplayName
	})

	// Build display lines: "name\ticon repo: display [state]"
	lines := make([]string, len(codespaces))
	for i, cs := range codespaces {
		icon := "🟢"
		if cs.State != "Available" {
			icon = "⏸️"
		}
		lines[i] = fmt.Sprintf("%s\t%s %s: %s [%s]", cs.Name, icon, cs.Repository, cs.DisplayName, cs.State)
	}

	// Try gum filter for fuzzy interactive picker
	if gumPath, err := exec.LookPath("gum"); err == nil {
		displayLines := make([]string, len(lines))
		for i, l := range lines {
			// Show only the display part (after tab) in the picker
			parts := strings.SplitN(l, "\t", 2)
			displayLines[i] = parts[1]
		}

		cmd := exec.Command(gumPath, "filter", "--placeholder", "Choose codespace...")
		cmd.Stdin = strings.NewReader(strings.Join(displayLines, "\n"))
		cmd.Stderr = os.Stderr
		selected, err := cmd.Output()
		if err == nil {
			choice := strings.TrimSpace(string(selected))
			for i, dl := range displayLines {
				if dl == choice {
					return codespaces[i], nil
				}
			}
		}
		// gum failed (e.g., no TTY), fall through to numbered list
	}

	// Fallback: numbered list
	for i, l := range lines {
		parts := strings.SplitN(l, "\t", 2)
		fmt.Printf("  %2d) %s\n", i+1, parts[1])
	}

	fmt.Printf("\nSelect [1-%d]: ", len(codespaces))
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return codespace{}, fmt.Errorf("reading input: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || n < 1 || n > len(codespaces) {
		return codespace{}, fmt.Errorf("invalid selection")
	}
	return codespaces[n-1], nil
}

func startCodespace(name string) error {
	fmt.Println("Starting codespace (this may take a moment)...")
	time.Sleep(3 * time.Second)

	for i := 0; i < 30; i++ {
		if exec.Command("gh", "codespace", "ssh", "-c", name, "--", "echo ready").Run() == nil {
			fmt.Println("Codespace is ready!")
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out waiting for codespace SSH")
}

func detectWorkdir(codespaceName string) (string, error) {
	out, err := exec.Command("gh", "codespace", "ssh", "-c", codespaceName,
		"--", "ls -d /workspaces/*/ 2>/dev/null | head -1",
	).Output()
	if err != nil {
		return "/workspaces", nil
	}
	workdir := strings.TrimRight(strings.TrimSpace(string(out)), "/")
	if workdir == "" {
		return "/workspaces", nil
	}
	return workdir, nil
}

func sshCommand(codespaceName, command string) (string, error) {
	out, err := exec.Command("gh", "codespace", "ssh", "-c", codespaceName,
		"--", command,
	).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func fetchInstructionFiles(codespaceName, workdir string) (string, error) {
	// Use a deterministic directory so copilot only needs to trust it once per codespace
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home dir: %w", err)
	}
	baseDir := filepath.Join(homeDir, ".copilot", "codespace-workdirs", codespaceName)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", fmt.Errorf("creating workdir: %w", err)
	}
	// Clean existing instruction files so stale ones don't persist
	os.RemoveAll(filepath.Join(baseDir, ".github"))
	os.Remove(filepath.Join(baseDir, "AGENTS.md"))
	os.Remove(filepath.Join(baseDir, "CLAUDE.md"))
	os.Remove(filepath.Join(baseDir, "GEMINI.md"))

	fmt.Println("Fetching instruction files from codespace...")

	// Fetch known instruction files
	knownFiles := []string{
		".github/copilot-instructions.md",
		"AGENTS.md",
		"CLAUDE.md",
		"GEMINI.md",
	}

	for _, relPath := range knownFiles {
		remotePath := workdir + "/" + relPath
		// Check if file exists
		if exec.Command("gh", "codespace", "ssh", "-c", codespaceName,
			"--", fmt.Sprintf("test -f %s", remotePath)).Run() != nil {
			continue
		}
		// Fetch it
		content, err := sshCommand(codespaceName, fmt.Sprintf("cat %s", remotePath))
		if err != nil {
			continue
		}
		localPath := filepath.Join(baseDir, relPath)
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			continue
		}
		if err := os.WriteFile(localPath, []byte(content), 0o644); err != nil {
			continue
		}
		fmt.Printf("  ✓ %s\n", relPath)
	}

	// Fetch scoped instruction files
	scopedOutput, err := sshCommand(codespaceName,
		fmt.Sprintf("find %s/.github/instructions -name '*.instructions.md' 2>/dev/null", workdir))
	if err == nil && strings.TrimSpace(scopedOutput) != "" {
		for _, remotePath := range strings.Split(strings.TrimSpace(scopedOutput), "\n") {
			remotePath = strings.TrimSpace(remotePath)
			if remotePath == "" {
				continue
			}
			relPath := strings.TrimPrefix(remotePath, workdir+"/")
			content, err := sshCommand(codespaceName, fmt.Sprintf("cat %s", remotePath))
			if err != nil {
				continue
			}
			localPath := filepath.Join(baseDir, relPath)
			if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
				continue
			}
			if err := os.WriteFile(localPath, []byte(content), 0o644); err != nil {
				continue
			}
			fmt.Printf("  ✓ %s\n", relPath)
		}
	}

	return baseDir, nil
}

// ensureTrustedFolder adds the directory to copilot's trusted_folders config if not already present.
func ensureTrustedFolder(dir string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configPath := filepath.Join(homeDir, ".copilot", "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	// Check if already trusted
	trusted, _ := config["trusted_folders"].([]any)
	for _, f := range trusted {
		if s, ok := f.(string); ok && s == dir {
			return nil // already trusted
		}
	}

	// Add to trusted folders
	trusted = append(trusted, dir)
	config["trusted_folders"] = trusted

	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, out, 0o644)
}

func buildMCPConfig(selfBinary, codespaceName, workdir string) string {
	config := map[string]any{
		"mcpServers": map[string]any{
			"codespace": map[string]any{
				"type":    "local",
				"command": selfBinary,
				"args":    []string{"mcp"},
				"env": map[string]string{
					"CODESPACE_NAME":    codespaceName,
					"CODESPACE_WORKDIR": workdir,
				},
				"tools": []string{"*"},
			},
		},
	}
	b, _ := json.Marshal(config)
	return string(b)
}

func execCopilot(excludedTools []string, mcpConfig string, extraArgs []string) error {
	copilotPath, err := exec.LookPath("copilot")
	if err != nil {
		return fmt.Errorf("copilot not found in PATH: %w", err)
	}

	args := []string{"copilot",
		"--excluded-tools",
	}
	args = append(args, excludedTools...)
	args = append(args, "--additional-mcp-config", mcpConfig)
	args = append(args, extraArgs...)

	return syscall.Exec(copilotPath, args, os.Environ())
}

// execCopilotWithShellPatch runs copilot's JS bundle via node with a require
// patch that intercepts the "!" shell escape and redirects it over SSH.
// This bypasses the native binary so the CJS patch can monkey-patch spawn.
func execCopilotWithShellPatch(excludedTools []string, mcpConfig string, extraArgs []string, sshClient *ssh.Client, workdir string) error {
	// Write the CJS patch to a temp file
	patchPath, err := shellpatch.WritePatch()
	if err != nil {
		return fmt.Errorf("writing shell patch: %w", err)
	}
	defer os.RemoveAll(filepath.Dir(patchPath))

	// Find copilot's index.js (the JS bundle)
	indexJS, err := findCopilotIndexJS()
	if err != nil {
		return fmt.Errorf("finding copilot index.js: %w", err)
	}

	// Find node binary
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("node not found in PATH: %w", err)
	}

	// Build args: node --require <patch.cjs> <index.js> <copilot-args...>
	args := []string{"node", "--require", patchPath, indexJS,
		"--excluded-tools",
	}
	args = append(args, excludedTools...)
	args = append(args, "--additional-mcp-config", mcpConfig)
	args = append(args, extraArgs...)

	// Add SSH connection info and workdir to env for the patch script
	env := os.Environ()
	if sshClient.SSHConfigPath() != "" {
		env = append(env, "COPILOT_SSH_CONFIG="+sshClient.SSHConfigPath())
		env = append(env, "COPILOT_SSH_HOST="+sshClient.SSHHost())
	}
	env = append(env, "CODESPACE_WORKDIR="+workdir)

	// Pre-fetch the auth token from keychain so node doesn't trigger a
	// macOS keychain prompt (the keychain ACL only trusts the native binary).
	if token := readCopilotToken(); token != "" {
		env = append(env, "COPILOT_GITHUB_TOKEN="+token)
	}

	return syscall.Exec(nodePath, args, env)
}

// findCopilotIndexJS locates copilot's index.js by following the symlink chain
// from the `copilot` binary → npm-loader.js → index.js in the same directory.
func findCopilotIndexJS() (string, error) {
	copilotPath, err := exec.LookPath("copilot")
	if err != nil {
		return "", fmt.Errorf("copilot not found in PATH: %w", err)
	}

	// Resolve symlinks to get the actual npm-loader.js path
	realPath, err := filepath.EvalSymlinks(copilotPath)
	if err != nil {
		return "", fmt.Errorf("resolving copilot path: %w", err)
	}

	// index.js is in the same directory as npm-loader.js
	dir := filepath.Dir(realPath)
	indexJS := filepath.Join(dir, "index.js")

	if _, err := os.Stat(indexJS); err != nil {
		return "", fmt.Errorf("copilot index.js not found at %s", indexJS)
	}

	return indexJS, nil
}

// readCopilotToken obtains a GitHub token for copilot auth.
// Uses `gh auth token` to avoid macOS keychain popups (the keychain ACL
// only trusts the native copilot binary, not node).
// Returns empty string on any failure.
func readCopilotToken() string {
	// Skip if already set via env
	for _, key := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if os.Getenv(key) != "" {
			return ""
		}
	}

	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
