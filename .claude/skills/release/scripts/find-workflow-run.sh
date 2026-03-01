#!/usr/bin/env bash
# find-workflow-run.sh — Find the workflow run triggered by a specific commit.
#
# Usage: find-workflow-run.sh <owner> <repo> <commit_sha> [workflow_file] [max_attempts]
#
# Outputs the run ID on success, exits 1 if not found after retries.
# The workflow may not appear immediately after push, so this retries.

set -euo pipefail

OWNER="${1:?Usage: find-workflow-run.sh <owner> <repo> <commit_sha> [workflow_file] [max_attempts]}"
REPO="${2:?}"
COMMIT_SHA="${3:?}"
WORKFLOW_FILE="${4:-release.yml}"
MAX_ATTEMPTS="${5:-12}"

for attempt in $(seq 1 "$MAX_ATTEMPTS"); do
  run_id=$(gh api "repos/${OWNER}/${REPO}/actions/workflows/${WORKFLOW_FILE}/runs?head_sha=${COMMIT_SHA}&per_page=1" \
    --jq '.workflow_runs[0].id // empty' 2>/dev/null) || true

  if [ -n "$run_id" ]; then
    echo "$run_id"
    exit 0
  fi

  echo "Waiting for workflow run to appear (attempt ${attempt}/${MAX_ATTEMPTS})..." >&2
  sleep 5
done

echo "Error: No workflow run found for commit ${COMMIT_SHA}" >&2
exit 1
