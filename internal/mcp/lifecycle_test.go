package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ekroon/gh-copilot-codespace/internal/registry"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// mockGHRunner simulates gh CLI calls for testing.
type mockGHRunner struct {
	results map[string]mockGHResult // key: first arg (e.g., "codespace")
	calls   [][]string
}

type mockGHResult struct {
	output string
	err    error
}

func (m *mockGHRunner) Run(_ context.Context, args ...string) (string, error) {
	m.calls = append(m.calls, args)
	// Match on the command pattern
	key := strings.Join(args[:2], " ")
	if r, ok := m.results[key]; ok {
		return r.output, r.err
	}
	// Default: match on first two args for codespace commands
	if len(args) >= 2 {
		if r, ok := m.results[args[0]+" "+args[1]]; ok {
			return r.output, r.err
		}
	}
	return "", nil
}

func TestCreateCodespaceHandler_MissingRepo(t *testing.T) {
	reg := registry.New()
	gh := &mockGHRunner{}
	handler := createCodespaceHandler(reg, gh)

	res, _ := handler(context.Background(), makeReq(map[string]any{}))
	if !res.IsError {
		t.Fatal("expected error for missing repository")
	}
	if !strings.Contains(resultText(res), "missing required parameter") {
		t.Errorf("expected 'missing required parameter', got %q", resultText(res))
	}
}

func TestCreateCodespaceHandler_AliasConflict(t *testing.T) {
	reg := registry.New()
	reg.Register(&registry.ManagedCodespace{Alias: "github", Name: "cs-old", Executor: &mockExecutor{}})

	gh := &mockGHRunner{}
	handler := createCodespaceHandler(reg, gh)

	res, _ := handler(context.Background(), makeReq(map[string]any{
		"repository": "github/github",
		"alias":      "github",
	}))
	if !res.IsError {
		t.Fatal("expected error for alias conflict")
	}
	if !strings.Contains(resultText(res), "already in use") {
		t.Errorf("expected 'already in use', got %q", resultText(res))
	}
}

func TestCreateCodespaceHandler_CreateFails(t *testing.T) {
	reg := registry.New()
	gh := &mockGHRunner{
		results: map[string]mockGHResult{
			"codespace create": {output: "error", err: fmt.Errorf("quota exceeded")},
		},
	}
	handler := createCodespaceHandler(reg, gh)

	res, _ := handler(context.Background(), makeReq(map[string]any{
		"repository": "github/github",
	}))
	if !res.IsError {
		t.Fatal("expected error when creation fails")
	}
	if !strings.Contains(resultText(res), "quota exceeded") {
		t.Errorf("expected quota error, got %q", resultText(res))
	}
}

func TestDeleteCodespaceHandler_Disconnect(t *testing.T) {
	reg := registry.New()
	reg.Register(&registry.ManagedCodespace{Alias: "github", Name: "cs-abc", Executor: &mockExecutor{}})

	gh := &mockGHRunner{}
	handler := deleteCodespaceHandler(reg, gh)

	res, _ := handler(context.Background(), makeReq(map[string]any{
		"codespace": "github",
	}))
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), "Disconnected") {
		t.Errorf("expected disconnect message, got %q", resultText(res))
	}
	if !strings.Contains(resultText(res), "still running") {
		t.Errorf("expected 'still running' message, got %q", resultText(res))
	}

	// Verify deregistered
	if reg.Len() != 0 {
		t.Error("expected registry to be empty after disconnect")
	}
}

func TestDeleteCodespaceHandler_DeleteFromGitHub(t *testing.T) {
	reg := registry.New()
	reg.Register(&registry.ManagedCodespace{Alias: "github", Name: "cs-abc", Executor: &mockExecutor{}})

	gh := &mockGHRunner{
		results: map[string]mockGHResult{
			"codespace delete": {output: "deleted"},
		},
	}
	handler := deleteCodespaceHandler(reg, gh)

	res, _ := handler(context.Background(), makeReq(map[string]any{
		"codespace": "github",
		"delete":    true,
	}))
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), "deleted") {
		t.Errorf("expected delete message, got %q", resultText(res))
	}
}

func TestDeleteCodespaceHandler_NotFound(t *testing.T) {
	reg := registry.New()
	gh := &mockGHRunner{}
	handler := deleteCodespaceHandler(reg, gh)

	res, _ := handler(context.Background(), makeReq(map[string]any{
		"codespace": "nonexistent",
	}))
	if !res.IsError {
		t.Fatal("expected error for nonexistent codespace")
	}
}

func TestConnectCodespaceHandler_MissingName(t *testing.T) {
	reg := registry.New()
	handler := connectCodespaceHandler(reg)

	res, _ := handler(context.Background(), makeReq(map[string]any{}))
	if !res.IsError {
		t.Fatal("expected error for missing name")
	}
}

// Helper for lifecycle tests
func makeLifecycleReq(args map[string]any) mcpsdk.CallToolRequest {
	return mcpsdk.CallToolRequest{
		Params: mcpsdk.CallToolParams{
			Arguments: args,
		},
	}
}
