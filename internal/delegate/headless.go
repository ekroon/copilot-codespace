package delegate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

type CommandFactory func(ctx context.Context, opts StartOptions, envPairs []string) *exec.Cmd
type TokenResolver func(ctx context.Context) (string, error)

type HeadlessRunner struct {
	commandFactory CommandFactory
	tokenResolver  TokenResolver
}

func NewHeadlessRunner() *HeadlessRunner {
	return &HeadlessRunner{
		commandFactory: defaultCommandFactory,
		tokenResolver:  resolveGitHubToken,
	}
}

func (r *HeadlessRunner) RunTask(ctx context.Context, opts StartOptions, progress ProgressFunc) (string, error) {
	token, err := r.tokenResolver(ctx)
	if err != nil {
		return "", err
	}

	cmd := r.commandFactory(ctx, opts, authEnvPairs(token))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("headless delegate stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("headless delegate stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("headless delegate stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting remote headless worker: %w", err)
	}

	stderrBuf := &limitedBuffer{max: 8 * 1024}
	var stderrWG sync.WaitGroup
	stderrWG.Add(1)
	go func() {
		defer stderrWG.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			stderrBuf.WriteString(line + "\n")
		}
	}()

	rpc := newRPCClient(&stdioReadWriteCloser{reader: stdout, writer: stdin, closer: stdin})

	result, runErr := runHeadlessSession(ctx, rpc, opts, progress)
	closeErr := rpc.Close()
	waitErr := cmd.Wait()
	stderrWG.Wait()

	if runErr != nil {
		return "", decorateHeadlessError(runErr, stderrBuf.String())
	}
	if closeErr != nil && ctx.Err() == nil {
		return "", decorateHeadlessError(fmt.Errorf("closing remote headless stdin: %w", closeErr), stderrBuf.String())
	}
	if waitErr != nil && ctx.Err() == nil {
		return "", decorateHeadlessError(fmt.Errorf("remote headless worker exited: %w", waitErr), stderrBuf.String())
	}

	return result, nil
}

func runHeadlessSession(ctx context.Context, rpc *rpcClient, opts StartOptions, progress ProgressFunc) (string, error) {
	progress(fmt.Sprintf("Starting ACP delegate on %s.", opts.CodespaceName))

	// Step 1: Initialize the ACP connection.
	if err := rpc.Call(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
	}, nil); err != nil {
		return "", fmt.Errorf("initializing ACP connection: %w", err)
	}

	// Step 2: Create a session.
	var sessionResp struct {
		SessionID string `json:"sessionId"`
	}
	sessionParams := map[string]any{
		"cwd":        opts.Cwd,
		"mcpServers": []any{},
	}
	if opts.Model != "" {
		sessionParams["model"] = opts.Model
	}
	if err := rpc.Call(ctx, "session/new", sessionParams, &sessionResp); err != nil {
		return "", fmt.Errorf("creating ACP session: %w", err)
	}
	progress(fmt.Sprintf("Created ACP session %s.", sessionResp.SessionID))

	// Accumulate text chunks delivered via session/update notifications.
	var textMu sync.Mutex
	var accumulated strings.Builder

	rpc.SetEventHandler(func(method string, params json.RawMessage) {
		if method != "session/update" {
			return
		}
		var notification struct {
			SessionID string `json:"sessionId"`
			Update    struct {
				SessionUpdate string `json:"sessionUpdate"`
				Content       struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		if err := json.Unmarshal(params, &notification); err != nil {
			return
		}
		if notification.SessionID != sessionResp.SessionID {
			return
		}
		if notification.Update.SessionUpdate == "agent_message_chunk" && notification.Update.Content.Type == "text" {
			textMu.Lock()
			accumulated.WriteString(notification.Update.Content.Text)
			textMu.Unlock()
		}
	})

	// Step 3: Send the prompt. This call blocks until the agent finishes.
	promptContent := []map[string]any{{"type": "text", "text": opts.Prompt}}
	var promptResp struct {
		StopReason string `json:"stopReason"`
	}
	if err := rpc.Call(ctx, "session/prompt", map[string]any{
		"sessionId": sessionResp.SessionID,
		"prompt":    promptContent,
	}, &promptResp); err != nil {
		return "", fmt.Errorf("sending ACP prompt: %w", err)
	}
	progress(fmt.Sprintf("ACP session completed (stopReason=%s).", promptResp.StopReason))

	textMu.Lock()
	result := accumulated.String()
	textMu.Unlock()

	return result, nil
}

func defaultCommandFactory(ctx context.Context, opts StartOptions, envPairs []string) *exec.Cmd {
	workdir := opts.Cwd
	if workdir == "" {
		workdir = opts.Workdir
	}

	args := []string{"codespace", "ssh", "-c", opts.CodespaceName, "--"}
	if opts.ExecAgent != "" {
		args = append(args, opts.ExecAgent, "exec", "--workdir", workdir)
		for _, pair := range envPairs {
			args = append(args, "--env", pair)
		}
		args = append(args, "--", "copilot", "--acp", "--stdio", "--yolo", "--log-level", "error")
		return exec.CommandContext(ctx, "gh", args...)
	}

	var exports []string
	for _, pair := range envPairs {
		exports = append(exports, "export "+shellQuote(pair))
	}
	scriptParts := []string{"cd " + shellQuote(workdir)}
	scriptParts = append(scriptParts, exports...)
	scriptParts = append(scriptParts, "exec copilot --acp --stdio --yolo --log-level error")
	args = append(args, "bash", "-lc", strings.Join(scriptParts, " && "))
	return exec.CommandContext(ctx, "gh", args...)
}

func resolveGitHubToken(ctx context.Context) (string, error) {
	for _, key := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value, nil
		}
	}

	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return "", fmt.Errorf("resolving GitHub token with gh auth token: %w", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("gh auth token returned an empty token")
	}
	return token, nil
}

func authEnvPairs(token string) []string {
	pairs := []string{
		"COPILOT_GITHUB_TOKEN=" + token,
		"GH_TOKEN=" + token,
		"GITHUB_TOKEN=" + token,
	}
	sort.Strings(pairs)
	return pairs
}

func decorateHeadlessError(err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return err
	}
	return fmt.Errorf("%w (stderr: %s)", err, stderr)
}

type stdioReadWriteCloser struct {
	reader io.Reader
	writer io.Writer
	closer io.Closer
}

func (s *stdioReadWriteCloser) Read(p []byte) (int, error)  { return s.reader.Read(p) }
func (s *stdioReadWriteCloser) Write(p []byte) (int, error) { return s.writer.Write(p) }
func (s *stdioReadWriteCloser) Close() error                { return s.closer.Close() }

type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	n, err := b.buf.Write(p)
	if b.max > 0 && b.buf.Len() > b.max {
		trimmed := b.buf.Bytes()
		b.buf.Reset()
		b.buf.Write(trimmed[len(trimmed)-b.max:])
	}
	return n, err
}

func (b *limitedBuffer) WriteString(s string) {
	_, _ = b.Write([]byte(s))
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
