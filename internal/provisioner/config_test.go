package provisioner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provisioners.json")
	data := `{
		"provisioners": [
			{"name": "test-prov", "bash": "echo hello"},
			{"name": "matched", "bash": "echo matched", "match": {"terminal": "xterm-ghostty"}}
		]
	}`
	os.WriteFile(path, []byte(data), 0o644)

	provisioners, err := LoadConfigFrom(path)
	if err != nil {
		t.Fatalf("LoadConfigFrom: %v", err)
	}
	if len(provisioners) != 2 {
		t.Fatalf("got %d provisioners, want 2", len(provisioners))
	}
	if provisioners[0].Name() != "test-prov" {
		t.Errorf("got name %q, want %q", provisioners[0].Name(), "test-prov")
	}
}

func TestLoadConfig_NoFile(t *testing.T) {
	provisioners, err := LoadConfigFrom("/nonexistent/path.json")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(provisioners) != 0 {
		t.Errorf("got %d provisioners, want 0", len(provisioners))
	}
}

func TestLoadConfig_WithMatch_Terminal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provisioners.json")
	data := `{"provisioners": [{"name": "ghostty", "bash": "echo hi", "match": {"terminal": "xterm-ghostty"}}]}`
	os.WriteFile(path, []byte(data), 0o644)

	provisioners, _ := LoadConfigFrom(path)
	if len(provisioners) != 1 {
		t.Fatal("expected 1 provisioner")
	}

	// Should run when terminal matches
	if !provisioners[0].ShouldRun(RunContext{Terminal: "xterm-ghostty"}) {
		t.Error("should run when terminal matches")
	}

	// Should not run when terminal doesn't match
	if provisioners[0].ShouldRun(RunContext{Terminal: "xterm-256color"}) {
		t.Error("should not run when terminal doesn't match")
	}
}

func TestLoadConfig_WithMatch_Repository(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provisioners.json")
	data := `{"provisioners": [{"name": "github-only", "bash": "echo hi", "match": {"repository": "github/github"}}]}`
	os.WriteFile(path, []byte(data), 0o644)

	provisioners, _ := LoadConfigFrom(path)

	if !provisioners[0].ShouldRun(RunContext{Repository: "github/github"}) {
		t.Error("should run when repo matches")
	}
	if provisioners[0].ShouldRun(RunContext{Repository: "other/repo"}) {
		t.Error("should not run when repo doesn't match")
	}
}

func TestLoadConfig_NoMatch_AlwaysRuns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provisioners.json")
	data := `{"provisioners": [{"name": "always", "bash": "echo hi"}]}`
	os.WriteFile(path, []byte(data), 0o644)

	provisioners, _ := LoadConfigFrom(path)

	if !provisioners[0].ShouldRun(RunContext{Terminal: "anything", Repository: "any/repo"}) {
		t.Error("provisioner without match should always run")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provisioners.json")
	os.WriteFile(path, []byte("not json"), 0o644)

	_, err := LoadConfigFrom(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
