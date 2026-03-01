package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIDELockFileParsing(t *testing.T) {
	raw := `{
		"socketPath": "/tmp/mcp-abc/mcp.sock",
		"scheme": "unix",
		"headers": {"Authorization": "Nonce test-nonce"},
		"pid": 12345,
		"ideName": "Visual Studio Code",
		"timestamp": 1700000000000,
		"workspaceFolders": ["/workspaces/my-repo"],
		"isTrusted": true
	}`

	var lf ideLockFile
	if err := json.Unmarshal([]byte(raw), &lf); err != nil {
		t.Fatalf("failed to parse lock file: %v", err)
	}

	if lf.SocketPath != "/tmp/mcp-abc/mcp.sock" {
		t.Errorf("socketPath = %q, want /tmp/mcp-abc/mcp.sock", lf.SocketPath)
	}
	if lf.IDEName != "Visual Studio Code" {
		t.Errorf("ideName = %q, want Visual Studio Code", lf.IDEName)
	}
	if lf.PID != 12345 {
		t.Errorf("pid = %d, want 12345", lf.PID)
	}
	if len(lf.WorkspaceFolders) != 1 || lf.WorkspaceFolders[0] != "/workspaces/my-repo" {
		t.Errorf("workspaceFolders = %v, want [/workspaces/my-repo]", lf.WorkspaceFolders)
	}
	if lf.Headers["Authorization"] != "Nonce test-nonce" {
		t.Errorf("headers.Authorization = %q, want Nonce test-nonce", lf.Headers["Authorization"])
	}
	if !lf.IsTrusted {
		t.Error("isTrusted should be true")
	}
}

func TestIDELockFileValidation(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name:    "valid",
			json:    `{"socketPath":"/s","scheme":"unix","headers":{},"pid":1,"ideName":"vscode","timestamp":0,"workspaceFolders":["/w"],"isTrusted":true}`,
			wantErr: false,
		},
		{
			name:    "missing socketPath",
			json:    `{"scheme":"unix","headers":{},"pid":1,"ideName":"vscode","timestamp":0,"workspaceFolders":["/w"]}`,
			wantErr: true,
		},
		{
			name:    "missing ideName",
			json:    `{"socketPath":"/s","scheme":"unix","headers":{},"pid":1,"timestamp":0,"workspaceFolders":["/w"]}`,
			wantErr: true,
		},
		{
			name:    "empty workspaceFolders",
			json:    `{"socketPath":"/s","scheme":"unix","headers":{},"pid":1,"ideName":"vscode","timestamp":0,"workspaceFolders":[]}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lf ideLockFile
			if err := json.Unmarshal([]byte(tt.json), &lf); err != nil {
				if !tt.wantErr {
					t.Fatalf("unexpected parse error: %v", err)
				}
				return
			}
			invalid := lf.SocketPath == "" || lf.IDEName == "" || len(lf.WorkspaceFolders) == 0
			if invalid != tt.wantErr {
				t.Errorf("validation = %v, wantErr = %v", invalid, tt.wantErr)
			}
		})
	}
}

func TestShortHash(t *testing.T) {
	h1 := shortHash("test-codespace:file1.lock")
	h2 := shortHash("test-codespace:file2.lock")
	h3 := shortHash("test-codespace:file1.lock") // same input

	if h1 == h2 {
		t.Error("different inputs should produce different hashes")
	}
	if h1 != h3 {
		t.Error("same inputs should produce same hash")
	}
	if len(h1) != 16 {
		t.Errorf("hash length = %d, want 16", len(h1))
	}
}

func TestIDELockFileRoundTrip(t *testing.T) {
	original := ideLockFile{
		SocketPath:       "/tmp/local.sock",
		Scheme:           "unix",
		Headers:          map[string]string{"Authorization": "Nonce abc-123"},
		PID:              99999,
		IDEName:          "Visual Studio Code - Insiders",
		Timestamp:        1700000000000,
		WorkspaceFolders: []string{"/home/user/.copilot/codespace-workdirs/my-cs"},
		IsTrusted:        true,
	}

	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed ideLockFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.SocketPath != original.SocketPath {
		t.Errorf("socketPath mismatch: %q vs %q", parsed.SocketPath, original.SocketPath)
	}
	if parsed.IDEName != original.IDEName {
		t.Errorf("ideName mismatch: %q vs %q", parsed.IDEName, original.IDEName)
	}
	if parsed.PID != original.PID {
		t.Errorf("pid mismatch: %d vs %d", parsed.PID, original.PID)
	}
	if parsed.Headers["Authorization"] != original.Headers["Authorization"] {
		t.Error("headers mismatch")
	}
}

func TestIsLocalPIDRunning(t *testing.T) {
	// Current process should be running
	if !isLocalPIDRunning(os.Getpid()) {
		t.Error("current PID should be running")
	}

	// PID 0 and negative should not be running
	if isLocalPIDRunning(0) {
		t.Error("PID 0 should not be considered running")
	}
	if isLocalPIDRunning(-1) {
		t.Error("PID -1 should not be considered running")
	}

	// Very high PID is almost certainly not running
	if isLocalPIDRunning(999999999) {
		t.Error("PID 999999999 should not be running")
	}
}

func TestCleanStaleIDEForwards(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a forwarded lock file with a dead PID (999999999)
	staleLF := ideLockFile{
		SocketPath:       filepath.Join(tmpDir, "stale.sock"),
		Scheme:           "unix",
		Headers:          map[string]string{},
		PID:              999999999, // not running
		IDEName:          "VSCode",
		Timestamp:        1700000000000,
		WorkspaceFolders: []string{"/tmp/test"},
		IsTrusted:        true,
	}
	staleData, _ := json.Marshal(staleLF)
	stalePath := filepath.Join(tmpDir, forwardedLockPrefix+"dead.lock")
	os.WriteFile(stalePath, staleData, 0o644)

	// Write a forwarded lock file with a live PID (current process)
	liveLF := staleLF
	liveLF.PID = os.Getpid()
	liveData, _ := json.Marshal(liveLF)
	livePath := filepath.Join(tmpDir, forwardedLockPrefix+"live.lock")
	os.WriteFile(livePath, liveData, 0o644)

	// Write a non-forwarded lock file (no prefix) — should be untouched
	otherPath := filepath.Join(tmpDir, "other-ide.lock")
	os.WriteFile(otherPath, staleData, 0o644)

	cleanStaleIDEForwards(tmpDir)

	// Stale forwarded lock file should be removed
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Error("stale forwarded lock file should have been removed")
	}

	// Live forwarded lock file should remain
	if _, err := os.Stat(livePath); err != nil {
		t.Error("live forwarded lock file should not have been removed")
	}

	// Non-forwarded lock file should remain
	if _, err := os.Stat(otherPath); err != nil {
		t.Error("non-forwarded lock file should not have been removed")
	}
}
