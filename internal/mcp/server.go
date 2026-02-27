package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ekroon/copilot-codespace/internal/ssh"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates and configures the MCP server with all remote tools.
func NewServer(sshClient *ssh.Client, codespaceName string) *server.MCPServer {
	s := server.NewMCPServer("codespace-mcp", "0.1.0")

	s.AddTool(viewTool(), viewHandler(sshClient))
	s.AddTool(editTool(), editHandler(sshClient))
	s.AddTool(createTool(), createHandler(sshClient))
	s.AddTool(bashTool(), bashHandler(sshClient))
	s.AddTool(grepTool(), grepHandler(sshClient))
	s.AddTool(globTool(), globHandler(sshClient))
	s.AddTool(writeBashTool(), writeBashHandler(sshClient))
	s.AddTool(readBashTool(), readBashHandler(sshClient))
	s.AddTool(stopBashTool(), stopBashHandler(sshClient))
	s.AddTool(listBashTool(), listBashHandler(sshClient))
	s.AddTool(openShellTool(), openShellHandler(codespaceName))

	return s
}

// --- remote_view ---

func viewTool() mcpsdk.Tool {
	return mcpsdk.Tool{
		Name:        "remote_view",
		Description: "View a file or directory on the remote codespace. Returns file contents with line numbers.",
		InputSchema: mcpsdk.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the file to view",
				},
				"view_range": map[string]any{
					"type":        "array",
					"description": "Optional [start_line, end_line] range. Use -1 for end_line to read to end of file.",
					"items":       map[string]any{"type": "integer"},
				},
			},
			Required: []string{"path"},
		},
	}
}

func viewHandler(c *ssh.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		path, err := requiredString(req, "path")
		if err != nil {
			return toolError(err.Error()), nil
		}

		var viewRange []int
		if raw, ok := req.GetArguments()["view_range"]; ok {
			if arr, ok := raw.([]any); ok && len(arr) == 2 {
				start, ok1 := toInt(arr[0])
				end, ok2 := toInt(arr[1])
				if ok1 && ok2 {
					viewRange = []int{start, end}
				}
			}
		}

		result, err := c.ViewFile(ctx, path, viewRange)
		if err != nil {
			return toolError(err.Error()), nil
		}
		return toolSuccess(result), nil
	}
}

// --- remote_edit ---

func editTool() mcpsdk.Tool {
	return mcpsdk.Tool{
		Name:        "remote_edit",
		Description: "Edit a file on the remote codespace by replacing exactly one occurrence of old_str with new_str.",
		InputSchema: mcpsdk.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the file to edit",
				},
				"old_str": map[string]any{
					"type":        "string",
					"description": "The exact string to find and replace (must match exactly once)",
				},
				"new_str": map[string]any{
					"type":        "string",
					"description": "The replacement string",
				},
			},
			Required: []string{"path", "old_str", "new_str"},
		},
	}
}

func editHandler(c *ssh.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		path, err := requiredString(req, "path")
		if err != nil {
			return toolError(err.Error()), nil
		}
		oldStr, err := requiredString(req, "old_str")
		if err != nil {
			return toolError(err.Error()), nil
		}
		newStr, err := requiredString(req, "new_str")
		if err != nil {
			return toolError(err.Error()), nil
		}

		if err := c.EditFile(ctx, path, oldStr, newStr); err != nil {
			return toolError(err.Error()), nil
		}
		return toolSuccess(fmt.Sprintf("Successfully edited %s", path)), nil
	}
}

// --- remote_create ---

func createTool() mcpsdk.Tool {
	return mcpsdk.Tool{
		Name:        "remote_create",
		Description: "Create a new file on the remote codespace with the given content. Parent directories are created automatically.",
		InputSchema: mcpsdk.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path for the new file",
				},
				"file_text": map[string]any{
					"type":        "string",
					"description": "Content of the file to create",
				},
			},
			Required: []string{"path", "file_text"},
		},
	}
}

func createHandler(c *ssh.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		path, err := requiredString(req, "path")
		if err != nil {
			return toolError(err.Error()), nil
		}
		content, err := requiredString(req, "file_text")
		if err != nil {
			return toolError(err.Error()), nil
		}

		if err := c.CreateFile(ctx, path, content); err != nil {
			return toolError(err.Error()), nil
		}
		return toolSuccess(fmt.Sprintf("Created %s", path)), nil
	}
}

// --- remote_bash ---

func bashTool() mcpsdk.Tool {
	return mcpsdk.Tool{
		Name:        "remote_bash",
		Description: "Execute a bash command on the remote codespace. Use mode 'async' for long-running or interactive commands (returns a shellId for use with remote_write_bash/remote_read_bash).",
		InputSchema: mcpsdk.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The bash command to execute",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "A short description of what this command does",
				},
				"mode": map[string]any{
					"type":        "string",
					"description": "Execution mode: 'sync' (default) waits for completion, 'async' runs in background and returns a shellId",
					"enum":        []string{"sync", "async"},
				},
				"shellId": map[string]any{
					"type":        "string",
					"description": "Session identifier for async mode. Auto-generated if not provided.",
				},
			},
			Required: []string{"command"},
		},
	}
}

func bashHandler(c *ssh.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		command, err := requiredString(req, "command")
		if err != nil {
			return toolError(err.Error()), nil
		}

		mode := optionalString(req, "mode")
		if mode == "async" {
			shellId := optionalString(req, "shellId")
			if shellId == "" {
				shellId = fmt.Sprintf("sh-%d", time.Now().UnixMilli())
			}
			if err := c.StartSession(ctx, shellId, command); err != nil {
				return toolError(err.Error()), nil
			}
			// Wait briefly and capture initial output
			time.Sleep(1 * time.Second)
			output, _ := c.ReadSession(ctx, shellId)
			return toolSuccess(fmt.Sprintf("Started async session: %s\n\n%s", shellId, output)), nil
		}

		stdout, stderr, exitCode, err := c.RunBash(ctx, command)
		if err != nil {
			return toolError(err.Error()), nil
		}

		var result strings.Builder
		if stdout != "" {
			result.WriteString(stdout)
		}
		if stderr != "" {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString("STDERR:\n")
			result.WriteString(stderr)
		}
		if exitCode != 0 {
			result.WriteString(fmt.Sprintf("\n[exit code: %d]", exitCode))
		}

		return toolSuccess(result.String()), nil
	}
}

// --- remote_write_bash ---

func writeBashTool() mcpsdk.Tool {
	return mcpsdk.Tool{
		Name:        "remote_write_bash",
		Description: "Send input to an async bash session on the remote codespace. Supports special keys: {enter}, {up}, {down}, {left}, {right}, {backspace}.",
		InputSchema: mcpsdk.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"shellId": map[string]any{
					"type":        "string",
					"description": "The session ID returned by remote_bash in async mode",
				},
				"input": map[string]any{
					"type":        "string",
					"description": "The input to send. Can include special keys like {enter}, {up}, {down}.",
				},
				"delay": map[string]any{
					"type":        "number",
					"description": "Seconds to wait before reading output (default: 2)",
				},
			},
			Required: []string{"shellId"},
		},
	}
}

func writeBashHandler(c *ssh.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		shellId, err := requiredString(req, "shellId")
		if err != nil {
			return toolError(err.Error()), nil
		}

		input := optionalString(req, "input")
		if input != "" {
			if err := c.WriteSession(ctx, shellId, input); err != nil {
				return toolError(err.Error()), nil
			}
		}

		delay := optionalFloat(req, "delay", 2)
		time.Sleep(time.Duration(delay * float64(time.Second)))

		output, err := c.ReadSession(ctx, shellId)
		if err != nil {
			return toolError(err.Error()), nil
		}
		return toolSuccess(output), nil
	}
}

// --- remote_read_bash ---

func readBashTool() mcpsdk.Tool {
	return mcpsdk.Tool{
		Name:        "remote_read_bash",
		Description: "Read output from an async bash session on the remote codespace.",
		InputSchema: mcpsdk.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"shellId": map[string]any{
					"type":        "string",
					"description": "The session ID returned by remote_bash in async mode",
				},
				"delay": map[string]any{
					"type":        "number",
					"description": "Seconds to wait before reading output (default: 2)",
				},
			},
			Required: []string{"shellId"},
		},
	}
}

func readBashHandler(c *ssh.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		shellId, err := requiredString(req, "shellId")
		if err != nil {
			return toolError(err.Error()), nil
		}

		delay := optionalFloat(req, "delay", 2)
		time.Sleep(time.Duration(delay * float64(time.Second)))

		output, err := c.ReadSession(ctx, shellId)
		if err != nil {
			return toolError(err.Error()), nil
		}
		return toolSuccess(output), nil
	}
}

// --- remote_stop_bash ---

func stopBashTool() mcpsdk.Tool {
	return mcpsdk.Tool{
		Name:        "remote_stop_bash",
		Description: "Stop an async bash session on the remote codespace.",
		InputSchema: mcpsdk.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"shellId": map[string]any{
					"type":        "string",
					"description": "The session ID to stop",
				},
			},
			Required: []string{"shellId"},
		},
	}
}

func stopBashHandler(c *ssh.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		shellId, err := requiredString(req, "shellId")
		if err != nil {
			return toolError(err.Error()), nil
		}

		if err := c.StopSession(ctx, shellId); err != nil {
			return toolError(err.Error()), nil
		}
		return toolSuccess(fmt.Sprintf("Session %s stopped.", shellId)), nil
	}
}

// --- remote_list_bash ---

func listBashTool() mcpsdk.Tool {
	return mcpsdk.Tool{
		Name:        "remote_list_bash",
		Description: "List active async bash sessions on the remote codespace.",
		InputSchema: mcpsdk.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{},
		},
	}
}

func listBashHandler(c *ssh.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		result, err := c.ListSessions(ctx)
		if err != nil {
			return toolError(err.Error()), nil
		}
		if result == "" {
			return toolSuccess("No active sessions."), nil
		}
		return toolSuccess(result), nil
	}
}

// --- remote_grep ---

func grepTool() mcpsdk.Tool {
	return mcpsdk.Tool{
		Name:        "remote_grep",
		Description: "Search for a pattern in files on the remote codespace using ripgrep (with grep fallback).",
		InputSchema: mcpsdk.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "The regex pattern to search for",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Directory or file to search in (defaults to workspace root)",
				},
				"glob": map[string]any{
					"type":        "string",
					"description": "Glob pattern to filter files (e.g., '*.go', '*.ts')",
				},
			},
			Required: []string{"pattern"},
		},
	}
}

func grepHandler(c *ssh.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		pattern, err := requiredString(req, "pattern")
		if err != nil {
			return toolError(err.Error()), nil
		}

		path := optionalString(req, "path")
		glob := optionalString(req, "glob")

		result, err := c.Grep(ctx, pattern, path, glob)
		if err != nil {
			return toolError(err.Error()), nil
		}
		if result == "" {
			return toolSuccess("No matches found."), nil
		}
		return toolSuccess(result), nil
	}
}

// --- remote_glob ---

func globTool() mcpsdk.Tool {
	return mcpsdk.Tool{
		Name:        "remote_glob",
		Description: "Find files matching a glob pattern on the remote codespace.",
		InputSchema: mcpsdk.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "The glob pattern to match files against (e.g., '*.go', '**/*.ts')",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Directory to search in (defaults to workspace root)",
				},
			},
			Required: []string{"pattern"},
		},
	}
}

func globHandler(c *ssh.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		pattern, err := requiredString(req, "pattern")
		if err != nil {
			return toolError(err.Error()), nil
		}

		path := optionalString(req, "path")

		result, err := c.Glob(ctx, pattern, path)
		if err != nil {
			return toolError(err.Error()), nil
		}
		if result == "" {
			return toolSuccess("No matches found."), nil
		}
		return toolSuccess(result), nil
	}
}

// --- helpers ---

func requiredString(req mcpsdk.CallToolRequest, key string) (string, error) {
	args := req.GetArguments()
	val, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("parameter %s must be a string", key)
	}
	return s, nil
}

func optionalString(req mcpsdk.CallToolRequest, key string) string {
	args := req.GetArguments()
	val, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := val.(string)
	return s
}

func optionalFloat(req mcpsdk.CallToolRequest, key string, defaultVal float64) float64 {
	args := req.GetArguments()
	val, ok := args[key]
	if !ok {
		return defaultVal
	}
	f, ok := val.(float64)
	if !ok {
		return defaultVal
	}
	return f
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

func toolSuccess(text string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			mcpsdk.TextContent{
				Type: "text",
				Text: text,
			},
		},
	}
}

func toolError(text string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		IsError: true,
		Content: []mcpsdk.Content{
			mcpsdk.TextContent{
				Type: "text",
				Text: text,
			},
		},
	}
}

// --- open_shell ---

func openShellTool() mcpsdk.Tool {
	return mcpsdk.Tool{
		Name:        "open_shell",
		Description: "Open an interactive SSH shell to the codespace in a new terminal tab/window. Use this when the user asks for a shell, terminal, or SSH access to the codespace.",
		InputSchema: mcpsdk.ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{},
		},
	}
}

func openShellHandler(codespaceName string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		sshCmd := fmt.Sprintf("gh codespace ssh -c %s", codespaceName)

		if err := openTerminalTab(sshCmd, "codespace: "+codespaceName); err != nil {
			return toolError(fmt.Sprintf("Failed to open shell: %v", err)), nil
		}
		return toolSuccess("Opened SSH shell to codespace in a new terminal tab."), nil
	}
}

// openTerminalTab opens a new terminal tab with the given command.
// Uses COPILOT_TERMINAL env var to determine the terminal to use.
// Supported values: "cmux" (default if cmux is detected), "macos" (Terminal.app), or a custom command template.
func openTerminalTab(command, title string) error {
	terminal := os.Getenv("COPILOT_TERMINAL")

	if terminal == "" {
		// Auto-detect: prefer cmux if available
		if cmuxPath := findCmuxCLI(); cmuxPath != "" {
			terminal = "cmux"
		} else {
			terminal = "macos"
		}
	}

	switch terminal {
	case "cmux":
		return openCmuxTab(command, title)
	case "macos":
		return openMacOSTab(command)
	default:
		// Custom command template: replace {} with the SSH command
		customCmd := strings.ReplaceAll(terminal, "{}", command)
		return exec.Command("sh", "-c", customCmd).Run()
	}
}

func findCmuxCLI() string {
	// Check common cmux CLI locations
	paths := []string{
		"/Applications/cmux.app/Contents/Resources/bin/cmux",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func openCmuxTab(command, title string) error {
	cmuxCLI := findCmuxCLI()
	if cmuxCLI == "" {
		return fmt.Errorf("cmux CLI not found")
	}

	// Create a new terminal tab (surface) in the current workspace
	out, err := exec.Command(cmuxCLI, "new-surface", "--type", "terminal").Output()
	if err != nil {
		return fmt.Errorf("cmux new-surface: %w", err)
	}

	// Parse surface ref (e.g., "OK surface:18 pane:5 workspace:5")
	var surfaceRef string
	for _, field := range strings.Fields(string(out)) {
		if strings.HasPrefix(field, "surface:") {
			surfaceRef = field
			break
		}
	}
	if surfaceRef == "" {
		return nil
	}

	// Send the command and press Enter
	exec.Command(cmuxCLI, "send", "--surface", surfaceRef, command).Run()
	exec.Command(cmuxCLI, "send-key", "--surface", surfaceRef, "Enter").Run()

	// Rename the tab
	exec.Command(cmuxCLI, "tab-action", "--action", "rename",
		"--tab", surfaceRef, "--title", title).Run()
	return nil
}

func openMacOSTab(command string) error {
	script := fmt.Sprintf(`tell application "Terminal"
	activate
	do script "%s"
end tell`, strings.ReplaceAll(command, `"`, `\"`))
	return exec.Command("osascript", "-e", script).Run()
}
