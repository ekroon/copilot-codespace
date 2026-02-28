# Copilot Instructions

## Build, test, and lint

```bash
go build ./cmd/copilot-codespace    # build the binary
go vet ./...                        # lint
go test -race ./...                 # all tests
go test -race -run TestParseInput ./internal/ssh  # single test
```

Integration tests require a real codespace and `gh` CLI auth — run `./scripts/integration-test.sh` locally (not in CI). Sign off with `gh signoff integration` after they pass.

## Architecture

Single Go binary that operates in two modes, selected by the first argument:

1. **Launcher** (`copilot-codespace [flags]`) — `cmd/copilot-codespace/main.go`
   - Lists codespaces via `gh`, picks one, starts it if stopped
   - Fetches instruction files (`.github/copilot-instructions.md`, `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`) from the codespace into a local temp dir
   - Execs `copilot` with `--excluded-tools` (disabling 10 local file/shell tools) and `--additional-mcp-config` (adding itself as the MCP server)

2. **MCP server** (`copilot-codespace mcp`) — `internal/mcp/server.go`
   - Spawned by Copilot CLI as a child process, communicates via stdio JSON-RPC
   - Provides `remote_*` tools (view, edit, create, bash, grep, glob, write/read/stop/list_bash) that mirror Copilot's built-in local tools
   - Delegates all operations to `ssh.Executor` interface

Key packages:
- `internal/ssh` — `Client` implements `Executor` by running commands over SSH (via `gh codespace ssh` or multiplexed ControlMaster). Async sessions use tmux on the codespace.
- `internal/shellpatch` — CJS monkey-patch for `--experimental-shell` flag; intercepts Copilot's `!` shell escape and redirects spawn calls over SSH.

## Conventions

- The `ssh.Executor` interface is the seam for testing MCP handlers — tests use `mockExecutor` (defined in `server_test.go`), not real SSH.
- File transfers use base64 encoding over SSH (`base64 < file` to read, `echo <b64> | base64 -d > file` to write).
- Async bash sessions are backed by tmux on the codespace. Session names are prefixed with `copilot-` (see `tmuxPrefix` constant).
- MCP tool handlers never return Go errors — they return `toolError()` results with `IsError: true` so the MCP protocol layer stays clean.
- The binary uses `syscall.Exec` to replace itself with `copilot` (or `node` for `--experimental-shell`), so the launcher process doesn't stay resident.

## Release flow

Every push to `main` triggers CI (vet, test, cross-platform build). If CI passes, a pre-release (`dev-{sha}`) is created. The `latest` tag is only updated by running the "Promote to Latest" workflow (`gh workflow run promote-to-latest.yml`) after `gh signoff integration` has been run against the commit.
