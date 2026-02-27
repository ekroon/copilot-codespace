package ssh

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Client manages SSH connections to a GitHub Codespace via gh CLI.
type Client struct {
	codespaceName string
	mu            sync.Mutex
}

// NewClient creates a new SSH client for the given codespace.
func NewClient(codespaceName string) *Client {
	return &Client{codespaceName: codespaceName}
}

// Exec runs a command on the codespace and returns stdout, stderr, and exit code.
func (c *Client) Exec(ctx context.Context, command string) (stdout string, stderr string, exitCode int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cmd := exec.CommandContext(ctx, "gh", "codespace", "ssh",
		"-c", c.codespaceName,
		"--", command,
	)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()

	if runErr != nil {
		if ctx.Err() != nil {
			return stdout, stderr, -1, fmt.Errorf("command cancelled: %w", ctx.Err())
		}
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return stdout, stderr, -1, fmt.Errorf("failed to execute command: %w", runErr)
		}
	}

	return stdout, stderr, exitCode, nil
}

// ViewFile reads a file with line numbers. If viewRange is provided [start, end], only those lines are shown.
func (c *Client) ViewFile(ctx context.Context, path string, viewRange []int) (string, error) {
	var cmd string
	if len(viewRange) == 2 {
		if viewRange[1] == -1 {
			cmd = fmt.Sprintf("sed -n '%dp,$p' %s | nl -ba -nln -w4 -v%d",
				viewRange[0], shellQuote(path), viewRange[0])
		} else {
			cmd = fmt.Sprintf("sed -n '%d,%dp' %s | nl -ba -nln -w4 -v%d",
				viewRange[0], viewRange[1], shellQuote(path), viewRange[0])
		}
	} else {
		cmd = fmt.Sprintf("cat -n %s", shellQuote(path))
	}

	stdout, stderr, exitCode, err := c.Exec(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("view file: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("view file failed (exit %d): %s", exitCode, stderr)
	}
	return stdout, nil
}

// EditFile replaces exactly one occurrence of oldStr with newStr in the file.
func (c *Client) EditFile(ctx context.Context, path, oldStr, newStr string) error {
	pathB64 := base64.StdEncoding.EncodeToString([]byte(path))
	oldB64 := base64.StdEncoding.EncodeToString([]byte(oldStr))
	newB64 := base64.StdEncoding.EncodeToString([]byte(newStr))

	// All user inputs passed via base64 to prevent injection
	script := fmt.Sprintf(`python3 -c "
import base64, sys
path = base64.b64decode('%s').decode()
old = base64.b64decode('%s').decode()
new = base64.b64decode('%s').decode()
content = open(path).read()
count = content.count(old)
if count == 0:
    print('ERROR: old_str not found in file', file=sys.stderr)
    sys.exit(1)
if count > 1:
    print(f'ERROR: old_str found {count} times, must be unique', file=sys.stderr)
    sys.exit(1)
content = content.replace(old, new, 1)
open(path, 'w').write(content)
print('OK')
"`, pathB64, oldB64, newB64)

	stdout, stderr, exitCode, err := c.Exec(ctx, script)
	if err != nil {
		return fmt.Errorf("edit file: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("edit file failed: %s", strings.TrimSpace(stderr))
	}
	_ = stdout
	return nil
}

// CreateFile creates a new file with the given content, creating parent directories as needed.
func (c *Client) CreateFile(ctx context.Context, path, content string) error {
	b64 := base64.StdEncoding.EncodeToString([]byte(content))
	dir := pathDir(path)

	cmd := fmt.Sprintf("mkdir -p %s && echo %s | base64 -d > %s",
		shellQuote(dir), shellQuote(b64), shellQuote(path))

	_, stderr, exitCode, err := c.Exec(ctx, cmd)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("create file failed (exit %d): %s", exitCode, stderr)
	}
	return nil
}

// RunBash executes a bash command on the codespace.
func (c *Client) RunBash(ctx context.Context, command string) (stdout string, stderr string, exitCode int, err error) {
	workdir := os.Getenv("CODESPACE_WORKDIR")
	if workdir == "" {
		workdir = "/workspaces"
	}

	wrapped := fmt.Sprintf("cd %s && %s", shellQuote(workdir), command)
	return c.Exec(ctx, wrapped)
}

// Grep searches for a pattern in files on the codespace.
func (c *Client) Grep(ctx context.Context, pattern, path, globPattern string) (string, error) {
	var args []string
	args = append(args, "rg", "--color=never")

	if globPattern != "" {
		args = append(args, "--glob", shellQuote(globPattern))
	}

	args = append(args, shellQuote(pattern))

	if path != "" {
		args = append(args, shellQuote(path))
	}

	cmd := strings.Join(args, " ")

	// Fallback to grep if rg is not available
	cmd = fmt.Sprintf("(%s) 2>/dev/null || grep -rn %s %s",
		cmd, shellQuote(pattern), func() string {
			if path != "" {
				return shellQuote(path)
			}
			return "."
		}())

	stdout, _, exitCode, err := c.Exec(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("grep: %w", err)
	}
	// Exit code 1 means no matches (normal for grep/rg)
	if exitCode > 1 {
		return "", fmt.Errorf("grep failed with exit code %d", exitCode)
	}
	return stdout, nil
}

// Glob finds files matching a glob pattern on the codespace.
func (c *Client) Glob(ctx context.Context, pattern, path string) (string, error) {
	searchPath := path
	if searchPath == "" {
		searchPath = os.Getenv("CODESPACE_WORKDIR")
		if searchPath == "" {
			searchPath = "/workspaces"
		}
	}

	cmd := fmt.Sprintf("find %s -path %s -not -path '*/\\.git/*' 2>/dev/null | head -200",
		shellQuote(searchPath), shellQuote(pattern))

	stdout, _, exitCode, err := c.Exec(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("glob: %w", err)
	}
	if exitCode > 1 {
		return "", fmt.Errorf("glob failed with exit code %d", exitCode)
	}
	return stdout, nil
}

const tmuxPrefix = "copilot-"

// tmuxSessionName returns the prefixed tmux session name.
func tmuxSessionName(sessionID string) string {
	return tmuxPrefix + sessionID
}

// StartSession creates a named tmux session running the given command on the codespace.
// Uses remain-on-exit so the pane stays readable even after the command exits.
func (c *Client) StartSession(ctx context.Context, sessionID, command string) error {
	name := tmuxSessionName(sessionID)

	// Ensure tmux is available, install if missing (Debian-based devcontainers)
	_, _, exitCode, _ := c.Exec(ctx, "command -v tmux")
	if exitCode != 0 {
		c.Exec(ctx, "sudo apt-get update -qq && sudo apt-get install -y -qq tmux 2>/dev/null")
		if _, _, ec, _ := c.Exec(ctx, "command -v tmux"); ec != 0 {
			return fmt.Errorf("tmux is not available on the codespace and could not be installed")
		}
	}

	// Create session with remain-on-exit so we can read output after command finishes
	cmd := fmt.Sprintf(
		"tmux new-session -d -s %s -x 200 -y 50 %s && tmux set-option -t %s remain-on-exit on",
		shellQuote(name), shellQuote(command), shellQuote(name))

	_, stderr, exitCode, err := c.Exec(ctx, cmd)
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("start session failed (exit %d): %s", exitCode, strings.TrimSpace(stderr))
	}
	return nil
}

// specialKeys maps brace-delimited key names to tmux key names.
var specialKeys = map[string]string{
	"{enter}":     "Enter",
	"{up}":        "Up",
	"{down}":      "Down",
	"{left}":      "Left",
	"{right}":     "Right",
	"{backspace}": "BSpace",
}

// parseInput splits an input string into segments of literal text and special keys.
// Each segment is either a literal string or a tmux key name (prefixed with \x00 to distinguish).
func parseInput(input string) []string {
	var segments []string
	for len(input) > 0 {
		// Find the earliest special key match
		bestIdx := -1
		bestKey := ""
		bestTmux := ""
		for pattern, tmuxKey := range specialKeys {
			idx := strings.Index(input, pattern)
			if idx >= 0 && (bestIdx < 0 || idx < bestIdx) {
				bestIdx = idx
				bestKey = pattern
				bestTmux = tmuxKey
			}
		}
		if bestIdx < 0 {
			// No more special keys; rest is literal
			segments = append(segments, input)
			break
		}
		if bestIdx > 0 {
			segments = append(segments, input[:bestIdx])
		}
		// Mark special keys with a \x00 prefix
		segments = append(segments, "\x00"+bestTmux)
		input = input[bestIdx+len(bestKey):]
	}
	return segments
}

// WriteSession sends keystrokes to a tmux session on the codespace.
// Special key sequences like {enter}, {up}, {down}, {left}, {right}, {backspace}
// are translated to their tmux equivalents.
func (c *Client) WriteSession(ctx context.Context, sessionID, input string) error {
	name := tmuxSessionName(sessionID)
	segments := parseInput(input)

	for _, seg := range segments {
		var cmd string
		if strings.HasPrefix(seg, "\x00") {
			// Special key
			tmuxKey := seg[1:]
			cmd = fmt.Sprintf("tmux send-keys -t %s %s", shellQuote(name), tmuxKey)
		} else {
			// Literal text
			cmd = fmt.Sprintf("tmux send-keys -t %s %s", shellQuote(name), shellQuote(seg))
		}

		_, stderr, exitCode, err := c.Exec(ctx, cmd)
		if err != nil {
			return fmt.Errorf("write session: %w", err)
		}
		if exitCode != 0 {
			return fmt.Errorf("write session failed (exit %d): %s", exitCode, strings.TrimSpace(stderr))
		}
	}
	return nil
}

// ReadSession captures the current tmux pane content (last 100 lines) from the codespace.
// Works even after the command has exited (thanks to remain-on-exit).
func (c *Client) ReadSession(ctx context.Context, sessionID string) (string, error) {
	name := tmuxSessionName(sessionID)

	// Check if session exists
	checkCmd := fmt.Sprintf("tmux has-session -t %s 2>/dev/null", shellQuote(name))
	if _, _, ec, _ := c.Exec(ctx, checkCmd); ec != 0 {
		return "", fmt.Errorf("session %q does not exist (command may have exited and been cleaned up)", sessionID)
	}

	cmd := fmt.Sprintf("tmux capture-pane -t %s -p -S -100", shellQuote(name))
	stdout, stderr, exitCode, err := c.Exec(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("read session: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("read session failed (exit %d): %s", exitCode, strings.TrimSpace(stderr))
	}

	// Check if the pane is dead (command exited)
	statusCmd := fmt.Sprintf("tmux list-panes -t %s -F '#{pane_dead} #{pane_dead_status}' 2>/dev/null", shellQuote(name))
	statusOut, _, _, _ := c.Exec(ctx, statusCmd)
	if strings.HasPrefix(strings.TrimSpace(statusOut), "1") {
		stdout += "\n[session exited]"
	}

	return stdout, nil
}

// StopSession kills a tmux session on the codespace.
func (c *Client) StopSession(ctx context.Context, sessionID string) error {
	name := tmuxSessionName(sessionID)
	cmd := fmt.Sprintf("tmux kill-session -t %s", shellQuote(name))

	_, stderr, exitCode, err := c.Exec(ctx, cmd)
	if err != nil {
		return fmt.Errorf("stop session: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("stop session failed (exit %d): %s", exitCode, strings.TrimSpace(stderr))
	}
	return nil
}

// ListSessions lists active copilot-prefixed tmux sessions on the codespace.
func (c *Client) ListSessions(ctx context.Context) (string, error) {
	cmd := "tmux list-sessions -F '#{session_name} #{session_created} #{session_activity}' 2>/dev/null | grep '^" + tmuxPrefix + "'"

	stdout, _, exitCode, err := c.Exec(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}
	// Exit code 1 means no matching sessions (grep found nothing)
	if exitCode > 1 {
		return "", fmt.Errorf("list sessions failed with exit code %d", exitCode)
	}
	return stdout, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func pathDir(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return "."
	}
	return path[:i]
}
