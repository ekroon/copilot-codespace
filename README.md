# copilot-codespace

Launch Copilot CLI with all file/bash operations executing on a remote GitHub Codespace via SSH.

## How it works

A single Go binary (`copilot-codespace`) serves two roles:

1. **Launcher mode** (default) — Lists your codespaces, lets you pick one, starts it if needed, fetches instruction files, then launches `copilot` with:
   - `--excluded-tools` — disables 11 built-in local tools
   - `--additional-mcp-config` — adds itself as the MCP server
   - `COPILOT_CUSTOM_INSTRUCTIONS_DIRS` — points to fetched remote instruction files

2. **MCP server mode** (`copilot-codespace mcp`) — Spawned by copilot, provides 10 remote tools over SSH:
   - `remote_view`, `remote_edit`, `remote_create` — file operations
   - `remote_bash` (sync + async), `remote_grep`, `remote_glob` — commands & search
   - `remote_write_bash`, `remote_read_bash`, `remote_stop_bash`, `remote_list_bash` — async session management (tmux-based)

## Prerequisites

- `gh` CLI authenticated with `codespace` scope
- At least one GitHub Codespace
- [Copilot CLI](https://docs.github.com/copilot/how-tos/copilot-cli) installed

## Installation

```bash
# With mise (recommended)
mise use -g github:ekroon/copilot-codespace

# Or build from source
go build -o copilot-codespace ./cmd/copilot-codespace
```

## Quick start

```bash
# Run (interactive codespace picker → copilot with remote tools)
copilot-codespace

# Pass extra copilot flags
copilot-codespace --model claude-sonnet-4.5
```

## Known limitations

- **`!` shell escape runs locally** — Copilot's built-in `!` shell escape mode uses its own internal shell execution and ignores `--excluded-tools` and `$SHELL`. Commands typed via `!` will run on your local machine, not on the codespace. Use the `remote_bash` tool through the agent instead, or try the `--experimental-shell` flag (see below).

## Experimental: remote `!` shell escape

```bash
copilot-codespace --experimental-shell
```

When this flag is set, `!` commands execute on the codespace instead of locally. This works by running copilot's JS bundle via Node.js with a monkey-patch that intercepts the `!` spawn call and redirects it over SSH (using the same multiplexed connection as the MCP tools).

**Trade-offs:**
- Uses the JS bundle instead of the native binary (slightly slower startup)
- Relies on a heuristic to detect `!` spawns (`shell: true` + `stdio: "pipe"`)
- Requires `node` (v24+) in PATH

## Development

### Running tests

```bash
go test -race ./...
```

### Integration testing & signoff

Integration tests require a real codespace and `gh` CLI authentication. They run locally, not in CI.

```bash
# One-time setup: install gh-signoff
./scripts/setup-signoff.sh

# Run integration tests
./scripts/integration-test.sh

# Sign off on the current commit (sets a GitHub commit status)
gh signoff integration
```

### Release flow

Every push to `main` triggers CI (vet, test, cross-platform build). If CI passes, a pre-release (`dev-{sha}`) is created automatically.

To promote to `latest`, run the "Promote to Latest" workflow from the GitHub Actions tab (or `gh workflow run promote-to-latest.yml`). It checks signoff on the latest main commit and promotes the existing pre-release to `latest`.

## Environment variables

| Variable | Description | Set by |
|---|---|---|
| `CODESPACE_NAME` | Codespace name | Launcher → MCP server |
| `CODESPACE_WORKDIR` | Working directory on codespace | Launcher → MCP server |
| `COPILOT_CUSTOM_INSTRUCTIONS_DIRS` | Temp dir with fetched instruction files | Launcher → copilot |
