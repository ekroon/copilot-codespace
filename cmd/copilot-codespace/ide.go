package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ekroon/copilot-codespace/internal/ssh"
)

// ideLockFile represents a lock file written by a VSCode extension at ~/.copilot/ide/.
type ideLockFile struct {
	SocketPath       string            `json:"socketPath"`
	Scheme           string            `json:"scheme"`
	Headers          map[string]string `json:"headers"`
	PID              int               `json:"pid"`
	IDEName          string            `json:"ideName"`
	Timestamp        int64             `json:"timestamp"`
	WorkspaceFolders []string          `json:"workspaceFolders"`
	IsTrusted        bool              `json:"isTrusted"`
}

const ideLockDir = "ide"
const forwardedLockPrefix = "copilot-codespace-"

// forwardIDEConnections discovers IDE lock files on the codespace, forwards their
// Unix sockets locally via SSH, and writes modified lock files so copilot CLI can
// auto-connect.
//
// Stale forwarded lock files from previous runs are cleaned up on startup (by checking
// if the PID in the lock file is still running). This is necessary because syscall.Exec
// replaces the process, preventing defer-based cleanup.
func forwardIDEConnections(sshClient *ssh.Client, codespaceName, localWorkdir string) (int, error) {
	if sshClient.SSHConfigPath() == "" {
		return 0, nil // no multiplexing, skip silently
	}

	// Determine local IDE lock dir
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return 0, fmt.Errorf("getting home dir: %w", err)
	}
	localIDEDir := filepath.Join(homeDir, ".copilot", ideLockDir)
	if err := os.MkdirAll(localIDEDir, 0o755); err != nil {
		return 0, fmt.Errorf("creating IDE lock dir: %w", err)
	}

	// Clean up stale forwarded lock files from previous runs
	cleanStaleIDEForwards(localIDEDir)

	// Fetch lock files from codespace
	lockFiles, err := fetchIDELockFiles(sshClient, codespaceName)
	if err != nil {
		return 0, fmt.Errorf("fetching IDE lock files: %w", err)
	}
	if len(lockFiles) == 0 {
		return 0, nil
	}

	ctx := context.Background()
	forwarded := 0

	for name, lf := range lockFiles {
		// Generate deterministic local socket path
		hash := shortHash(codespaceName + ":" + name)
		localSocket := filepath.Join(os.TempDir(), fmt.Sprintf("copilot-ide-fwd-%s.sock", hash))

		// Forward the remote socket to the local one
		if err := sshClient.ForwardSocket(ctx, localSocket, lf.SocketPath); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ IDE forward failed for %s: %v\n", lf.IDEName, err)
			continue
		}

		// Write modified lock file locally
		localLF := ideLockFile{
			SocketPath:       localSocket,
			Scheme:           lf.Scheme,
			Headers:          lf.Headers,
			PID:              os.Getpid(), // becomes copilot's PID after syscall.Exec
			IDEName:          lf.IDEName,
			Timestamp:        time.Now().UnixMilli(),
			WorkspaceFolders: []string{localWorkdir},
			IsTrusted:        lf.IsTrusted,
		}

		lockData, err := json.MarshalIndent(localLF, "", "  ")
		if err != nil {
			continue
		}

		localLockPath := filepath.Join(localIDEDir, forwardedLockPrefix+hash+".lock")
		if err := os.WriteFile(localLockPath, lockData, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ Failed to write IDE lock file: %v\n", err)
			continue
		}

		fmt.Printf("  ✓ IDE: %s (forwarded over SSH)\n", lf.IDEName)
		forwarded++
	}

	return forwarded, nil
}

// cleanStaleIDEForwards removes forwarded lock files from previous runs whose
// PID is no longer running. This handles cleanup since syscall.Exec prevents
// defer-based cleanup.
func cleanStaleIDEForwards(ideDir string) {
	entries, err := os.ReadDir(ideDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), forwardedLockPrefix) {
			continue
		}
		lockPath := filepath.Join(ideDir, e.Name())
		data, err := os.ReadFile(lockPath)
		if err != nil {
			continue
		}
		var lf ideLockFile
		if err := json.Unmarshal(data, &lf); err != nil {
			os.Remove(lockPath)
			continue
		}
		// Check if the PID is still running locally
		if !isLocalPIDRunning(lf.PID) {
			os.Remove(lockPath)
			os.Remove(lf.SocketPath) // clean up forwarded socket too
		}
	}
}

// isLocalPIDRunning checks if a PID is still running on the local machine.
func isLocalPIDRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks if process exists without sending a signal
	return process.Signal(syscall.Signal(0)) == nil
}

// fetchIDELockFiles reads and parses IDE lock files from the codespace.
// Returns a map of filename → parsed lock file.
func fetchIDELockFiles(sshClient *ssh.Client, codespaceName string) (map[string]ideLockFile, error) {
	ctx := context.Background()

	// Batch-read all lock files with boundary separators (same pattern as instruction files)
	script := `
SEP="===IDE_LOCK_BOUNDARY==="
DIR="$HOME/.copilot/ide"
if [ -d "$DIR" ]; then
  for f in "$DIR"/*.lock; do
    [ -f "$f" ] || continue
    echo "$SEP"
    basename "$f"
    cat "$f"
  done
  echo "$SEP"
fi
`
	stdout, stderr, exitCode, err := sshClient.Exec(ctx, script)
	if err != nil {
		return nil, fmt.Errorf("exec: %w", err)
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("exit %d: %s", exitCode, strings.TrimSpace(stderr))
	}

	if strings.TrimSpace(stdout) == "" {
		return nil, nil
	}

	result := make(map[string]ideLockFile)
	parts := strings.Split(stdout, "===IDE_LOCK_BOUNDARY===")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// First line is filename, rest is JSON content
		lines := strings.SplitN(part, "\n", 2)
		if len(lines) < 2 {
			continue
		}
		name := strings.TrimSpace(lines[0])
		content := strings.TrimSpace(lines[1])

		var lf ideLockFile
		if err := json.Unmarshal([]byte(content), &lf); err != nil {
			continue
		}
		if lf.SocketPath == "" || lf.IDEName == "" || len(lf.WorkspaceFolders) == 0 {
			continue
		}

		// Validate PID is still running on codespace
		checkCmd := fmt.Sprintf("kill -0 %d 2>/dev/null && echo alive", lf.PID)
		pidOut, _, _, _ := sshClient.Exec(ctx, checkCmd)
		if !strings.Contains(pidOut, "alive") {
			continue
		}

		result[name] = lf
	}

	return result, nil
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8])
}
