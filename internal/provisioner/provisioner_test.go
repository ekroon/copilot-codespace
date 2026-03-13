package provisioner

import (
	"context"
	"os"
	"testing"
)

type mockCSInfo struct {
	name             string
	repository       string
	workdir          string
	uploadedTerminfo []string
	uploadErr        error
}

func (m *mockCSInfo) CodespaceName() string { return m.name }
func (m *mockCSInfo) Repository() string    { return m.repository }
func (m *mockCSInfo) Workdir() string       { return m.workdir }
func (m *mockCSInfo) RunSSH(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockCSInfo) UploadTerminfo(_ context.Context, term string) error {
	m.uploadedTerminfo = append(m.uploadedTerminfo, term)
	return m.uploadErr
}

func TestTerminfoProvisioner_ShouldRun_GhosttySessionWithOverriddenTERM(t *testing.T) {
	p := &TerminfoProvisioner{}
	t.Setenv("TERM", "xterm-color")
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "/tmp/ghostty")

	if !p.ShouldRun(RunContext{Terminal: DetectedTerminal(os.Getenv("TERM"))}) {
		t.Error("should run when Ghostty is the terminal program")
	}
}

func TestTerminfoProvisioner_ShouldRun_StandardTerminalWithoutGhostty(t *testing.T) {
	p := &TerminfoProvisioner{}
	t.Setenv("TERM", "xterm-color")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "")

	if p.ShouldRun(RunContext{Terminal: DetectedTerminal(os.Getenv("TERM"))}) {
		t.Error("should not run for standard terminal")
	}
}

func TestTerminfoProvisioner_ShouldRun_Empty(t *testing.T) {
	p := &TerminfoProvisioner{}
	t.Setenv("TERM", "xterm-color")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "")

	if p.ShouldRun(RunContext{Terminal: DetectedTerminal("")}) {
		t.Error("should not run when terminal is empty")
	}
}

func TestDetectedTerminal_GhosttyNormalizesToXtermGhostty(t *testing.T) {
	t.Setenv("TERM", "xterm-color")
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "/tmp/ghostty")

	if got := DetectedTerminal(os.Getenv("TERM")); got != "xterm-ghostty" {
		t.Fatalf("DetectedTerminal() = %q, want %q", got, "xterm-ghostty")
	}
}

func TestTerminfoProvisioner_Run_UploadsGhosttyTerminfoWhenTERMOverridden(t *testing.T) {
	p := &TerminfoProvisioner{}
	target := &mockCSInfo{}
	t.Setenv("TERM", "xterm-color")
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "/tmp/ghostty")

	if err := p.Run(context.Background(), target); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := target.uploadedTerminfo, []string{"xterm-ghostty"}; !equalStrings(got, want) {
		t.Fatalf("uploadedTerminfo = %v, want %v", got, want)
	}
}

func TestTerminfoProvisioner_Run_UploadsCurrentNonStandardTerm(t *testing.T) {
	p := &TerminfoProvisioner{}
	target := &mockCSInfo{}
	t.Setenv("TERM", "wezterm")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "")

	if err := p.Run(context.Background(), target); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := target.uploadedTerminfo, []string{"wezterm"}; !equalStrings(got, want) {
		t.Fatalf("uploadedTerminfo = %v, want %v", got, want)
	}
}

func TestTerminfoProvisioner_Run_DedupesGhosttyTerminfo(t *testing.T) {
	p := &TerminfoProvisioner{}
	target := &mockCSInfo{}
	t.Setenv("TERM", "xterm-ghostty")
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "/tmp/ghostty")

	if err := p.Run(context.Background(), target); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := target.uploadedTerminfo, []string{"xterm-ghostty"}; !equalStrings(got, want) {
		t.Fatalf("uploadedTerminfo = %v, want %v", got, want)
	}
}

func TestTerminfoProvisioner_Name(t *testing.T) {
	p := &TerminfoProvisioner{}
	if p.Name() != "terminfo" {
		t.Errorf("got name %q, want %q", p.Name(), "terminfo")
	}
}

func TestGitFetchProvisioner_Name(t *testing.T) {
	p := &GitFetchProvisioner{}
	if p.Name() != "git-fetch" {
		t.Errorf("got name %q, want %q", p.Name(), "git-fetch")
	}
}

func TestGitFetchProvisioner_ShouldRun(t *testing.T) {
	p := &GitFetchProvisioner{}
	if !p.ShouldRun(RunContext{}) {
		t.Error("git-fetch should always run")
	}
}

func TestWaitForConfigProvisioner_Name(t *testing.T) {
	p := &WaitForConfigProvisioner{}
	if p.Name() != "wait-for-config" {
		t.Errorf("got name %q, want %q", p.Name(), "wait-for-config")
	}
}

func TestWaitForConfigProvisioner_ShouldRun_NewCodespace(t *testing.T) {
	p := &WaitForConfigProvisioner{}
	if !p.ShouldRun(RunContext{IsNewCodespace: true}) {
		t.Error("should run for newly created codespaces")
	}
}

func TestWaitForConfigProvisioner_ShouldRun_ExistingCodespace(t *testing.T) {
	p := &WaitForConfigProvisioner{}
	if p.ShouldRun(RunContext{IsNewCodespace: false}) {
		t.Error("should not run for existing codespaces")
	}
}

func TestRunAll_SkipsNonMatching(t *testing.T) {
	ran := false
	provisioners := []Provisioner{
		&testProvisioner{
			name:      "test",
			shouldRun: false,
			runFunc:   func() error { ran = true; return nil },
		},
	}

	RunAll(context.Background(), provisioners, RunContext{}, nil)

	if ran {
		t.Error("provisioner should not have run")
	}
}

func TestRunAll_RunsMatching(t *testing.T) {
	ran := false
	provisioners := []Provisioner{
		&testProvisioner{
			name:      "test",
			shouldRun: true,
			runFunc:   func() error { ran = true; return nil },
		},
	}

	RunAll(context.Background(), provisioners, RunContext{}, nil)

	if !ran {
		t.Error("provisioner should have run")
	}
}

type testProvisioner struct {
	name      string
	shouldRun bool
	runFunc   func() error
}

func (p *testProvisioner) Name() string                { return p.name }
func (p *testProvisioner) ShouldRun(_ RunContext) bool { return p.shouldRun }
func (p *testProvisioner) Run(_ context.Context, _ CodespaceTarget) error {
	if p.runFunc != nil {
		return p.runFunc()
	}
	return nil
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
