package ssh

import (
	"testing"
)

func TestParseInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"literal text", "hello", []string{"hello"}},
		{"single enter", "{enter}", []string{"\x00Enter"}},
		{"text then enter", "ls{enter}", []string{"ls", "\x00Enter"}},
		{"two special keys", "{up}{down}", []string{"\x00Up", "\x00Down"}},
		{"text-key-text", "foo{enter}bar", []string{"foo", "\x00Enter", "bar"}},
		{"empty string", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseInput(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("parseInput(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseInput(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGlobToFindName(t *testing.T) {
	tests := []struct {
		pattern string
		want    string
	}{
		{"**/*.go", "*.go"},
		{"src/**/*.test.js", "*.test.js"},
		{"*.ts", "*.ts"},
		{"a/b/c/d.go", "d.go"},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			if got := globToFindName(tt.pattern); got != tt.want {
				t.Errorf("globToFindName(%q) = %q, want %q", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want string
	}{
		{"simple", "hello", "'hello'"},
		{"with space", "hello world", "'hello world'"},
		{"with single quote", "it's", "'it'\"'\"'s'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shellQuote(tt.s); got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.s, got, tt.want)
			}
		})
	}
}

func TestPathDir(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/workspaces/repo", "/workspaces"},
		{"file.txt", "."},
		{"/a", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := pathDir(tt.path); got != tt.want {
				t.Errorf("pathDir(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestTmuxSessionName(t *testing.T) {
	if got := tmuxSessionName("abc"); got != "copilot-abc" {
		t.Errorf("tmuxSessionName(%q) = %q, want %q", "abc", got, "copilot-abc")
	}
}

func TestCleanPaneOutput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"strips pane is dead line",
			"hello world\nPane is dead (status 0, Thu Mar  5 08:17:27 2026)\n",
			"hello world",
		},
		{
			"strips pane is dead with non-zero status",
			"some output\nPane is dead (status 1, Thu Mar  5 09:00:00 2026)\n\n",
			"some output",
		},
		{
			"no pane is dead line",
			"just output\nmore output\n",
			"just output\nmore output",
		},
		{
			"trims trailing blank lines",
			"output\n\n\n\n",
			"output",
		},
		{
			"empty output",
			"\n\n",
			"",
		},
		{
			"pane is dead only",
			"Pane is dead (status 0, Thu Mar  5 08:17:27 2026)\n",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanPaneOutput(tt.input); got != tt.want {
				t.Errorf("cleanPaneOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetGetWorkdir(t *testing.T) {
c := NewClient("test-codespace")

// Default should fall back to env or /workspaces
got := c.GetWorkdir()
if got == "" {
t.Fatal("GetWorkdir() returned empty string")
}

// Set a custom workdir
c.SetWorkdir("/workspaces/myproject/src")
if got := c.GetWorkdir(); got != "/workspaces/myproject/src" {
t.Errorf("GetWorkdir() = %q, want %q", got, "/workspaces/myproject/src")
}

// Override again
c.SetWorkdir("/workspaces/other")
if got := c.GetWorkdir(); got != "/workspaces/other" {
t.Errorf("GetWorkdir() = %q, want %q", got, "/workspaces/other")
}
}
