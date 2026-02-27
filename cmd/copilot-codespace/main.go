package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ekroon/copilot-codespace/internal/mcp"
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

	// If first arg is "shell", SSH into the codespace (used as $SHELL for ! escape)
	if len(os.Args) > 1 && os.Args[1] == "shell" {
		runShell()
		return
	}

	// Otherwise, run as interactive launcher
	if err := runLauncher(); err != nil {
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
	mcpServer := mcp.NewServer(sshClient)

	log.SetOutput(os.Stderr)
	log.Printf("codespace-mcp: starting for codespace %q", codespaceName)

	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("codespace-mcp: server error: %v", err)
	}
}

func runLauncher() error {
	// The binary serves as both launcher and MCP server
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}

	// List codespaces
	fmt.Println("Fetching codespaces...")
	codespaces, err := listCodespaces()
	if err != nil {
		return err
	}
	if len(codespaces) == 0 {
		return fmt.Errorf("no codespaces found. Create one first with: gh codespace create")
	}

	// Display picker
	fmt.Println()
	fmt.Println("Available codespaces:")
	fmt.Println("─────────────────────")
	for i, cs := range codespaces {
		icon := "🟢"
		if cs.State != "Available" {
			icon = "⏸️ "
		}
		fmt.Printf("  %2d) %s %s (%s) [%s]\n", i+1, icon, cs.DisplayName, cs.Repository, cs.State)
	}

	// Read selection
	fmt.Println()
	selection, err := promptSelection(len(codespaces))
	if err != nil {
		return err
	}
	selected := codespaces[selection]
	fmt.Printf("\nSelected: %s\n", selected.Name)

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

	// Excluded tools
	excludedTools := []string{
		"edit", "create", "bash", "write_bash", "read_bash",
		"stop_bash", "list_bash", "view", "grep", "glob", "task",
	}

	fmt.Printf("\nLaunching Copilot CLI with remote codespace tools...\n")
	fmt.Printf("  Codespace: %s\n", selected.Name)
	fmt.Printf("  Workspace: %s\n", workdir)
	fmt.Printf("  Excluded:  %d local tools\n\n", len(excludedTools))

	// Exec copilot from the instructions dir (cwd is already set)
	return execCopilot(excludedTools, mcpConfig)
}

func listCodespaces() ([]codespace, error) {
	out, err := exec.Command("gh", "codespace", "list",
		"--json", "name,displayName,repository,state",
		"--limit", "50",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("listing codespaces: %w", err)
	}

	var codespaces []codespace
	if err := json.Unmarshal(out, &codespaces); err != nil {
		return nil, fmt.Errorf("parsing codespace list: %w", err)
	}
	return codespaces, nil
}

func promptSelection(max int) (int, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Select codespace [1-%d]: ", max)
	input, err := reader.ReadString('\n')
	if err != nil {
		return 0, fmt.Errorf("reading input: %w", err)
	}

	n, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || n < 1 || n > max {
		return 0, fmt.Errorf("invalid selection")
	}
	return n - 1, nil
}

func startCodespace(name string) error {
	fmt.Println("Starting codespace (this may take a moment)...")
	if err := exec.Command("gh", "codespace", "start", "-c", name).Run(); err != nil {
		return fmt.Errorf("starting codespace: %w", err)
	}

	fmt.Println("Waiting for SSH readiness...")
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

func runShell() {
	codespaceName := os.Getenv("CODESPACE_NAME")
	if codespaceName == "" {
		fmt.Fprintln(os.Stderr, "CODESPACE_NAME not set")
		os.Exit(1)
	}

	ghPath, err := exec.LookPath("gh")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gh not found in PATH")
		os.Exit(1)
	}

	// If called as "shell -c <command>", run the command via SSH
	// Otherwise, open an interactive SSH session
	if len(os.Args) >= 4 && os.Args[2] == "-c" {
		cmd := strings.Join(os.Args[3:], " ")
		syscall.Exec(ghPath, []string{"gh", "codespace", "ssh", "-c", codespaceName, "--", cmd}, os.Environ())
	} else {
		syscall.Exec(ghPath, []string{"gh", "codespace", "ssh", "-c", codespaceName}, os.Environ())
	}
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

func execCopilot(excludedTools []string, mcpConfig string) error {
	copilotPath, err := exec.LookPath("copilot")
	if err != nil {
		return fmt.Errorf("copilot not found in PATH: %w", err)
	}

	args := []string{"copilot",
		"--excluded-tools",
	}
	args = append(args, excludedTools...)
	args = append(args, "--additional-mcp-config", mcpConfig)
	// Pass through any extra args from the command line
	args = append(args, os.Args[1:]...)

	// Set SHELL to our own binary's "shell" subcommand so ! escape SSHs into codespace
	self, _ := os.Executable()
	env := os.Environ()
	env = setEnv(env, "SHELL", self)

	return syscall.Exec(copilotPath, args, env)
}

// setEnv sets or replaces an environment variable in a list.
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
