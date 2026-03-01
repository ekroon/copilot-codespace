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

Single Go binary that operates in three modes, selected by the first argument:

1. **Launcher** (`copilot-codespace [flags]`) — `cmd/copilot-codespace/main.go`
   - Lists codespaces via `gh`, picks one, starts it if stopped
   - Deploys exec agent binary to the codespace (`deploy.go`)
   - Fetches project-level components (instructions, skills, agents, commands, hooks, MCP configs) into a local mirror dir
   - Rewrites MCP servers and hooks for SSH forwarding (using remote exec agent when available)
   - Execs `copilot` with `--excluded-tools` (disabling 10 local file/shell tools) and `--additional-mcp-config` (adding itself as the MCP server)

2. **MCP server** (`copilot-codespace mcp`) — `internal/mcp/server.go`
   - Spawned by Copilot CLI as a child process, communicates via stdio JSON-RPC
   - Provides `remote_*` tools (view, edit, create, bash, grep, glob, write/read/stop/list_bash) that mirror Copilot's built-in local tools
   - Delegates all operations to `ssh.Executor` interface

3. **Exec agent** (`copilot-codespace exec`) — `cmd/copilot-codespace/exec.go`
   - Deployed to codespace at startup, used for structured remote command execution
   - `exec [--workdir DIR] [--env K=V]... -- COMMAND [ARGS...]`
   - Replaces fragile `bash -c 'cd WD && export K=V && exec CMD'` shell assembly with proper Go process management

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
