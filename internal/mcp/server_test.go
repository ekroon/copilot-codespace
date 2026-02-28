package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func makeReq(args map[string]any) mcpsdk.CallToolRequest {
	return mcpsdk.CallToolRequest{
		Params: mcpsdk.CallToolParams{
			Arguments: args,
		},
	}
}

func TestRequiredString(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		key     string
		want    string
		wantErr string
	}{
		{
			name: "key present with string value",
			args: map[string]any{"key": "hello"},
			key:  "key",
			want: "hello",
		},
		{
			name:    "key missing",
			args:    map[string]any{},
			key:     "key",
			wantErr: "missing required parameter",
		},
		{
			name:    "key present with non-string value",
			args:    map[string]any{"key": float64(42)},
			key:     "key",
			wantErr: "must be a string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := requiredString(makeReq(tt.args), tt.key)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOptionalString(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		key  string
		want string
	}{
		{
			name: "key present with string value",
			args: map[string]any{"key": "hello"},
			key:  "key",
			want: "hello",
		},
		{
			name: "key missing",
			args: map[string]any{},
			key:  "key",
			want: "",
		},
		{
			name: "key present with non-string value",
			args: map[string]any{"key": float64(42)},
			key:  "key",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := optionalString(makeReq(tt.args), tt.key)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOptionalFloat(t *testing.T) {
	tests := []struct {
		name       string
		args       map[string]any
		key        string
		defaultVal float64
		want       float64
	}{
		{
			name:       "key present with float64 value",
			args:       map[string]any{"key": float64(3.14)},
			key:        "key",
			defaultVal: 1.0,
			want:       3.14,
		},
		{
			name:       "key missing",
			args:       map[string]any{},
			key:        "key",
			defaultVal: 1.0,
			want:       1.0,
		},
		{
			name:       "key present with non-float64 value",
			args:       map[string]any{"key": "notfloat"},
			key:        "key",
			defaultVal: 1.0,
			want:       1.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := optionalFloat(makeReq(tt.args), tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		want   int
		wantOK bool
	}{
		{name: "float64", input: float64(42), want: 42, wantOK: true},
		{name: "int", input: int(42), want: 42, wantOK: true},
		{name: "string", input: "42", want: 0, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toInt(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestToolSuccess(t *testing.T) {
	result := toolSuccess("ok")
	if result.IsError {
		t.Error("expected IsError to be false")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	tc, ok := result.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if tc.Text != "ok" {
		t.Errorf("got text %q, want %q", tc.Text, "ok")
	}
}

func TestToolError(t *testing.T) {
	result := toolError("fail")
	if !result.IsError {
		t.Error("expected IsError to be true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	tc, ok := result.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if tc.Text != "fail" {
		t.Errorf("got text %q, want %q", tc.Text, "fail")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- Mock Executor ---

type mockExecutor struct {
	viewFileResult     string
	viewFileErr        error
	editFileErr        error
	createFileErr      error
	runBashStdout      string
	runBashStderr      string
	runBashExit        int
	runBashErr         error
	grepResult         string
	grepErr            error
	globResult         string
	globErr            error
	startSessionErr    error
	writeSessionErr    error
	readSessionResult  string
	readSessionErr     error
	stopSessionErr     error
	listSessionsResult string
	listSessionsErr    error
}

func (m *mockExecutor) ViewFile(_ context.Context, _ string, _ []int) (string, error) {
	return m.viewFileResult, m.viewFileErr
}

func (m *mockExecutor) EditFile(_ context.Context, _, _, _ string) error {
	return m.editFileErr
}

func (m *mockExecutor) CreateFile(_ context.Context, _, _ string) error {
	return m.createFileErr
}

func (m *mockExecutor) RunBash(_ context.Context, _ string) (string, string, int, error) {
	return m.runBashStdout, m.runBashStderr, m.runBashExit, m.runBashErr
}

func (m *mockExecutor) Grep(_ context.Context, _, _, _ string) (string, error) {
	return m.grepResult, m.grepErr
}

func (m *mockExecutor) Glob(_ context.Context, _, _ string) (string, error) {
	return m.globResult, m.globErr
}

func (m *mockExecutor) StartSession(_ context.Context, _, _ string) error {
	return m.startSessionErr
}

func (m *mockExecutor) WriteSession(_ context.Context, _, _ string) error {
	return m.writeSessionErr
}

func (m *mockExecutor) ReadSession(_ context.Context, _ string) (string, error) {
	return m.readSessionResult, m.readSessionErr
}

func (m *mockExecutor) StopSession(_ context.Context, _ string) error {
	return m.stopSessionErr
}

func (m *mockExecutor) ListSessions(_ context.Context) (string, error) {
	return m.listSessionsResult, m.listSessionsErr
}

// helper to extract text from a CallToolResult
func resultText(r *mcpsdk.CallToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	tc, ok := r.Content[0].(mcpsdk.TextContent)
	if !ok {
		return ""
	}
	return tc.Text
}

// --- Handler Tests ---

func TestViewHandler(t *testing.T) {
	tests := []struct {
		name      string
		mock      *mockExecutor
		args      map[string]any
		wantErr   bool
		wantText  string
	}{
		{
			name:     "success",
			mock:     &mockExecutor{viewFileResult: "1. hello\n2. world\n"},
			args:     map[string]any{"path": "/tmp/test.txt"},
			wantText: "1. hello\n2. world\n",
		},
		{
			name:     "error from executor",
			mock:     &mockExecutor{viewFileErr: fmt.Errorf("no such file")},
			args:     map[string]any{"path": "/tmp/missing.txt"},
			wantErr:  true,
			wantText: "no such file",
		},
		{
			name:     "missing path arg",
			mock:     &mockExecutor{},
			args:     map[string]any{},
			wantErr:  true,
			wantText: "missing required parameter",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := viewHandler(tt.mock)
			res, err := handler(context.Background(), makeReq(tt.args))
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if tt.wantErr && !res.IsError {
				t.Fatal("expected tool error, got success")
			}
			if !tt.wantErr && res.IsError {
				t.Fatalf("expected success, got tool error: %s", resultText(res))
			}
			if !strings.Contains(resultText(res), tt.wantText) {
				t.Errorf("result text %q does not contain %q", resultText(res), tt.wantText)
			}
		})
	}
}

func TestEditHandler(t *testing.T) {
	tests := []struct {
		name     string
		mock     *mockExecutor
		args     map[string]any
		wantErr  bool
		wantText string
	}{
		{
			name:     "success",
			mock:     &mockExecutor{},
			args:     map[string]any{"path": "/tmp/f.txt", "old_str": "a", "new_str": "b"},
			wantText: "Successfully edited",
		},
		{
			name:     "executor error",
			mock:     &mockExecutor{editFileErr: fmt.Errorf("old_str not found")},
			args:     map[string]any{"path": "/tmp/f.txt", "old_str": "x", "new_str": "y"},
			wantErr:  true,
			wantText: "old_str not found",
		},
		{
			name:     "missing old_str arg",
			mock:     &mockExecutor{},
			args:     map[string]any{"path": "/tmp/f.txt", "new_str": "b"},
			wantErr:  true,
			wantText: "missing required parameter",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := editHandler(tt.mock)
			res, err := handler(context.Background(), makeReq(tt.args))
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if tt.wantErr && !res.IsError {
				t.Fatal("expected tool error, got success")
			}
			if !tt.wantErr && res.IsError {
				t.Fatalf("expected success, got tool error: %s", resultText(res))
			}
			if !strings.Contains(resultText(res), tt.wantText) {
				t.Errorf("result text %q does not contain %q", resultText(res), tt.wantText)
			}
		})
	}
}

func TestCreateHandler(t *testing.T) {
	tests := []struct {
		name     string
		mock     *mockExecutor
		args     map[string]any
		wantErr  bool
		wantText string
	}{
		{
			name:     "success",
			mock:     &mockExecutor{},
			args:     map[string]any{"path": "/tmp/new.txt", "file_text": "content"},
			wantText: "Created /tmp/new.txt",
		},
		{
			name:     "executor error",
			mock:     &mockExecutor{createFileErr: fmt.Errorf("permission denied")},
			args:     map[string]any{"path": "/root/f.txt", "file_text": "x"},
			wantErr:  true,
			wantText: "permission denied",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := createHandler(tt.mock)
			res, err := handler(context.Background(), makeReq(tt.args))
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if tt.wantErr && !res.IsError {
				t.Fatal("expected tool error, got success")
			}
			if !tt.wantErr && res.IsError {
				t.Fatalf("expected success, got tool error: %s", resultText(res))
			}
			if !strings.Contains(resultText(res), tt.wantText) {
				t.Errorf("result text %q does not contain %q", resultText(res), tt.wantText)
			}
		})
	}
}

func TestBashHandler_Sync(t *testing.T) {
	tests := []struct {
		name     string
		mock     *mockExecutor
		args     map[string]any
		wantErr  bool
		wantText string
	}{
		{
			name:     "exit 0 stdout only",
			mock:     &mockExecutor{runBashStdout: "hello world\n"},
			args:     map[string]any{"command": "echo hello world"},
			wantText: "hello world\n",
		},
		{
			name:     "non-zero exit code",
			mock:     &mockExecutor{runBashStdout: "", runBashStderr: "not found\n", runBashExit: 127},
			args:     map[string]any{"command": "badcmd"},
			wantText: "[exit code: 127]",
		},
		{
			name:     "executor error",
			mock:     &mockExecutor{runBashErr: fmt.Errorf("connection lost")},
			args:     map[string]any{"command": "ls"},
			wantErr:  true,
			wantText: "connection lost",
		},
		{
			name:     "stdout and stderr",
			mock:     &mockExecutor{runBashStdout: "out\n", runBashStderr: "warn\n"},
			args:     map[string]any{"command": "cmd"},
			wantText: "STDERR:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := bashHandler(tt.mock)
			res, err := handler(context.Background(), makeReq(tt.args))
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if tt.wantErr && !res.IsError {
				t.Fatal("expected tool error, got success")
			}
			if !tt.wantErr && res.IsError {
				t.Fatalf("expected success, got tool error: %s", resultText(res))
			}
			if !strings.Contains(resultText(res), tt.wantText) {
				t.Errorf("result text %q does not contain %q", resultText(res), tt.wantText)
			}
		})
	}
}

func TestGrepHandler(t *testing.T) {
	tests := []struct {
		name     string
		mock     *mockExecutor
		args     map[string]any
		wantErr  bool
		wantText string
	}{
		{
			name:     "success with results",
			mock:     &mockExecutor{grepResult: "file.go:10:match\n"},
			args:     map[string]any{"pattern": "match"},
			wantText: "file.go:10:match",
		},
		{
			name:     "no matches",
			mock:     &mockExecutor{grepResult: ""},
			args:     map[string]any{"pattern": "nope"},
			wantText: "No matches found.",
		},
		{
			name:     "executor error",
			mock:     &mockExecutor{grepErr: fmt.Errorf("grep failed with exit code 2")},
			args:     map[string]any{"pattern": "bad["},
			wantErr:  true,
			wantText: "grep failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := grepHandler(tt.mock)
			res, err := handler(context.Background(), makeReq(tt.args))
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if tt.wantErr && !res.IsError {
				t.Fatal("expected tool error, got success")
			}
			if !tt.wantErr && res.IsError {
				t.Fatalf("expected success, got tool error: %s", resultText(res))
			}
			if !strings.Contains(resultText(res), tt.wantText) {
				t.Errorf("result text %q does not contain %q", resultText(res), tt.wantText)
			}
		})
	}
}

func TestGlobHandler(t *testing.T) {
	tests := []struct {
		name     string
		mock     *mockExecutor
		args     map[string]any
		wantErr  bool
		wantText string
	}{
		{
			name:     "success with results",
			mock:     &mockExecutor{globResult: "src/main.go\nsrc/util.go\n"},
			args:     map[string]any{"pattern": "**/*.go"},
			wantText: "src/main.go",
		},
		{
			name:     "no matches",
			mock:     &mockExecutor{globResult: ""},
			args:     map[string]any{"pattern": "**/*.xyz"},
			wantText: "No matches found.",
		},
		{
			name:     "executor error",
			mock:     &mockExecutor{globErr: fmt.Errorf("glob failed with exit code 2")},
			args:     map[string]any{"pattern": "**/*"},
			wantErr:  true,
			wantText: "glob failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := globHandler(tt.mock)
			res, err := handler(context.Background(), makeReq(tt.args))
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if tt.wantErr && !res.IsError {
				t.Fatal("expected tool error, got success")
			}
			if !tt.wantErr && res.IsError {
				t.Fatalf("expected success, got tool error: %s", resultText(res))
			}
			if !strings.Contains(resultText(res), tt.wantText) {
				t.Errorf("result text %q does not contain %q", resultText(res), tt.wantText)
			}
		})
	}
}

func TestStopBashHandler(t *testing.T) {
	tests := []struct {
		name     string
		mock     *mockExecutor
		args     map[string]any
		wantErr  bool
		wantText string
	}{
		{
			name:     "success",
			mock:     &mockExecutor{},
			args:     map[string]any{"shellId": "s1"},
			wantText: "stopped",
		},
		{
			name:     "executor error",
			mock:     &mockExecutor{stopSessionErr: fmt.Errorf("session not found")},
			args:     map[string]any{"shellId": "bad"},
			wantErr:  true,
			wantText: "session not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := stopBashHandler(tt.mock)
			res, err := handler(context.Background(), makeReq(tt.args))
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if tt.wantErr && !res.IsError {
				t.Fatal("expected tool error, got success")
			}
			if !tt.wantErr && res.IsError {
				t.Fatalf("expected success, got tool error: %s", resultText(res))
			}
			if !strings.Contains(resultText(res), tt.wantText) {
				t.Errorf("result text %q does not contain %q", resultText(res), tt.wantText)
			}
		})
	}
}

func TestListBashHandler(t *testing.T) {
	tests := []struct {
		name     string
		mock     *mockExecutor
		wantErr  bool
		wantText string
	}{
		{
			name:     "success with sessions",
			mock:     &mockExecutor{listSessionsResult: "copilot-s1 123 456\n"},
			wantText: "copilot-s1",
		},
		{
			name:     "empty returns no active",
			mock:     &mockExecutor{listSessionsResult: ""},
			wantText: "No active sessions.",
		},
		{
			name:     "executor error",
			mock:     &mockExecutor{listSessionsErr: fmt.Errorf("list sessions failed with exit code 2")},
			wantErr:  true,
			wantText: "list sessions failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := listBashHandler(tt.mock)
			res, err := handler(context.Background(), makeReq(map[string]any{}))
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if tt.wantErr && !res.IsError {
				t.Fatal("expected tool error, got success")
			}
			if !tt.wantErr && res.IsError {
				t.Fatalf("expected success, got tool error: %s", resultText(res))
			}
			if !strings.Contains(resultText(res), tt.wantText) {
				t.Errorf("result text %q does not contain %q", resultText(res), tt.wantText)
			}
		})
	}
}
