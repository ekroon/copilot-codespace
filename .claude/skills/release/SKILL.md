---
name: release
description: >-
  Release the copilot-codespace project: commit, push, wait for CI, signoff, and promote to latest.
  Use this skill when the user asks to "release", "ship it", "push and release", "promote to latest",
  "cut a release", "deploy", or any variation of committing + pushing + waiting for CI + promoting.
  Also use when the user says "commit and release" or "push to main and release". This skill handles
  the full end-to-end release pipeline including CI wait loops and integration signoff.
---

# Release Skill

Automates the full release pipeline for copilot-codespace: commit → push → CI → signoff → promote to latest.

## When to use

Any time the user wants to ship changes to production. This includes partial flows (e.g., "just push and wait for CI") — adapt by running only the relevant steps.

## Scripts

Three shell scripts in `scripts/` handle the mechanical parts:

| Script | Purpose |
|---|---|
| `scripts/release.sh` | Full orchestrated release (push → CI → signoff → promote) |
| `scripts/find-workflow-run.sh` | Finds the GH Actions run triggered by a specific commit |
| `scripts/wait-for-workflow.sh` | Polls a workflow run until completion (success/failure/timeout) |

## Full release flow

### Step 1: Commit

Use the `git-commit` skill if available, or commit directly. Handle GPG/SSH signing — if `commit.gpgsign` is `true` and the commit hangs, retry with `-c commit.gpgsign=false`. Ensure the `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>` trailer is included.

### Step 2: Run the release script

The release script handles everything after commit. Run it from the repo root:

```bash
chmod +x .claude/skills/release/scripts/*.sh
.claude/skills/release/scripts/release.sh ekroon copilot-codespace main
```

This will:
1. Push to main
2. Find and wait for the Release CI workflow
3. Run `gh signoff integration`
4. Trigger `promote-to-latest.yml`
5. Wait for the promote workflow to complete

### Step 3: Report results

Tell the user the final status — the release tag, the workflow URLs, and whether everything succeeded.

## Running individual scripts

If you only need part of the flow:

```bash
# Find the workflow run for a commit
SCRIPTS=".claude/skills/release/scripts"
RUN_ID=$("$SCRIPTS/find-workflow-run.sh" ekroon copilot-codespace <sha> release.yml)

# Wait for it to complete (10s poll, 600s timeout)
"$SCRIPTS/wait-for-workflow.sh" ekroon copilot-codespace "$RUN_ID" 10 600

# Just signoff
gh signoff integration

# Trigger promote
gh workflow run promote-to-latest.yml
```

## Error handling

- **CI failure**: The release script exits with code 1 and prints the workflow conclusion. Show the user the workflow URL and offer to investigate failed jobs.
- **Timeout**: Exits with code 2. Default timeout is 600s (10 minutes). Override with `TIMEOUT=900` env var.
- **Signoff not installed**: If `gh signoff` fails, suggest running `./scripts/setup-signoff.sh` first.
- **GPG signing hang**: Use `-c commit.gpgsign=false` when committing.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `POLL_INTERVAL` | `10` | Seconds between CI status checks |
| `TIMEOUT` | `600` | Max seconds to wait for each workflow |
| `SKIP_SIGNOFF` | unset | Set to `1` to skip integration signoff |
