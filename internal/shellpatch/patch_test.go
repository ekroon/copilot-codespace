package shellpatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePatch_CreatesValidFile(t *testing.T) {
	path, err := WritePatch()
	if err != nil {
		t.Fatalf("WritePatch() returned error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(path))

	if filepath.Ext(path) != ".cjs" {
		t.Errorf("expected .cjs extension, got %q", filepath.Ext(path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read returned path: %v", err)
	}

	content := string(data)
	for _, marker := range []string{`"use strict"`, "child_process", "COPILOT_SSH_CONFIG"} {
		if !strings.Contains(content, marker) {
			t.Errorf("file content missing expected marker %q", marker)
		}
	}
}

func TestWritePatch_CleanupWorks(t *testing.T) {
	path, err := WritePatch()
	if err != nil {
		t.Fatalf("WritePatch() returned error: %v", err)
	}

	dir := filepath.Dir(path)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("os.RemoveAll(%q) returned error: %v", dir, err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected directory %q to be removed, but it still exists", dir)
	}
}
