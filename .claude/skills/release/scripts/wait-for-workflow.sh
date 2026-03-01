#!/usr/bin/env bash
# wait-for-workflow.sh — Poll a GitHub Actions workflow run until it completes.
#
# Usage: wait-for-workflow.sh <owner> <repo> <run_id> [poll_interval] [timeout]
#
# Exits 0 if the run succeeds, 1 if it fails/is cancelled, 2 on timeout.
# Outputs JSON with conclusion and status on completion.

set -euo pipefail

OWNER="${1:?Usage: wait-for-workflow.sh <owner> <repo> <run_id> [poll_interval] [timeout]}"
REPO="${2:?}"
RUN_ID="${3:?}"
POLL_INTERVAL="${4:-10}"
TIMEOUT="${5:-600}"

elapsed=0

while true; do
  result=$(gh api "repos/${OWNER}/${REPO}/actions/runs/${RUN_ID}" \
    --jq '{status: .status, conclusion: .conclusion, name: .name, html_url: .html_url}' 2>&1) || {
    echo "::warning::API call failed, retrying in ${POLL_INTERVAL}s..." >&2
    sleep "$POLL_INTERVAL"
    elapsed=$((elapsed + POLL_INTERVAL))
    if [ "$elapsed" -ge "$TIMEOUT" ]; then
      echo '{"error": "timeout", "elapsed": '"$elapsed"'}' 
      exit 2
    fi
    continue
  }

  status=$(echo "$result" | jq -r '.status')
  conclusion=$(echo "$result" | jq -r '.conclusion')

  if [ "$status" = "completed" ]; then
    echo "$result"
    if [ "$conclusion" = "success" ]; then
      exit 0
    else
      exit 1
    fi
  fi

  echo "⏳ ${status}... (${elapsed}s elapsed)" >&2
  sleep "$POLL_INTERVAL"
  elapsed=$((elapsed + POLL_INTERVAL))

  if [ "$elapsed" -ge "$TIMEOUT" ]; then
    echo '{"error": "timeout", "elapsed": '"$elapsed"', "last_status": "'"$status"'"}' 
    exit 2
  fi
done
