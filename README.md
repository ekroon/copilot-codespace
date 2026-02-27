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

- Go 1.21+
- `gh` CLI authenticated with `codespace` scope
- At least one GitHub Codespace

## Quick start

```bash
# Build
go build -o copilot-codespace ./cmd/copilot-codespace

# Run (interactive codespace picker → copilot with remote tools)
./copilot-codespace

# Pass extra copilot flags
./copilot-codespace --model claude-sonnet-4.5
```

## Known limitations

- **`!` shell escape runs locally** — Copilot's built-in `!` shell escape mode uses its own internal shell execution and ignores `--excluded-tools` and `$SHELL`. Commands typed via `!` will run on your local machine, not on the codespace. Use the `remote_bash` tool through the agent instead.

## Environment variables

| Variable | Description | Set by |
|---|---|---|
| `CODESPACE_NAME` | Codespace name | Launcher → MCP server |
| `CODESPACE_WORKDIR` | Working directory on codespace | Launcher → MCP server |
| `COPILOT_CUSTOM_INSTRUCTIONS_DIRS` | Temp dir with fetched instruction files | Launcher → copilot |
