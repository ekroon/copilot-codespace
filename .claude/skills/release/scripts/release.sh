#!/usr/bin/env bash
# release.sh — Full release flow: push → wait CI → signoff → promote → wait.
#
# Usage: release.sh <owner> <repo> [branch]
#
# Prerequisites:
#   - Changes already committed on the current branch
#   - gh CLI authenticated
#   - gh-signoff extension installed
#
# Environment:
#   POLL_INTERVAL  — seconds between CI polls (default: 10)
#   TIMEOUT        — max seconds to wait for each workflow (default: 600)
#   SKIP_SIGNOFF   — set to "1" to skip integration signoff

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OWNER="${1:?Usage: release.sh <owner> <repo> [branch]}"
REPO="${2:?}"
BRANCH="${3:-main}"
POLL_INTERVAL="${POLL_INTERVAL:-10}"
TIMEOUT="${TIMEOUT:-600}"

COMMIT_SHA=$(git rev-parse HEAD)
SHORT_SHA="${COMMIT_SHA:0:7}"

echo "=== Release: ${SHORT_SHA} ==="
echo ""

# Step 1: Push
echo "📤 Pushing to ${BRANCH}..."
git push origin "${BRANCH}" 2>&1
echo ""

# Step 2: Find the CI workflow run
echo "🔍 Finding CI workflow run..."
RUN_ID=$("${SCRIPT_DIR}/find-workflow-run.sh" "$OWNER" "$REPO" "$COMMIT_SHA" "release.yml" 12)
echo "   Run ID: ${RUN_ID}"
echo "   URL: https://github.com/${OWNER}/${REPO}/actions/runs/${RUN_ID}"
echo ""

# Step 3: Wait for CI
echo "⏳ Waiting for CI to complete..."
result=$("${SCRIPT_DIR}/wait-for-workflow.sh" "$OWNER" "$REPO" "$RUN_ID" "$POLL_INTERVAL" "$TIMEOUT") || {
  echo "❌ CI failed!"
  echo "$result" | jq . 2>/dev/null || echo "$result"
  exit 1
}
echo "✅ CI passed!"
echo ""

# Step 4: Integration signoff
if [ "${SKIP_SIGNOFF:-}" = "1" ]; then
  echo "⏭️  Skipping signoff (SKIP_SIGNOFF=1)"
else
  echo "🔏 Running integration signoff..."
  gh signoff integration
  echo "✅ Signoff complete!"
fi
echo ""

# Step 5: Trigger promote-to-latest
echo "🚀 Triggering promote-to-latest..."
gh workflow run promote-to-latest.yml 2>&1
echo ""

# Step 6: Find and wait for promote workflow
echo "🔍 Finding promote workflow run..."
sleep 3  # Brief pause for workflow_dispatch to register
PROMOTE_RUN_ID=$("${SCRIPT_DIR}/find-workflow-run.sh" "$OWNER" "$REPO" "$COMMIT_SHA" "promote-to-latest.yml" 12)
echo "   Run ID: ${PROMOTE_RUN_ID}"
echo "   URL: https://github.com/${OWNER}/${REPO}/actions/runs/${PROMOTE_RUN_ID}"
echo ""

echo "⏳ Waiting for promote to complete..."
result=$("${SCRIPT_DIR}/wait-for-workflow.sh" "$OWNER" "$REPO" "$PROMOTE_RUN_ID" "$POLL_INTERVAL" "$TIMEOUT") || {
  echo "❌ Promote failed!"
  echo "$result" | jq . 2>/dev/null || echo "$result"
  exit 1
}
echo ""
echo "🎉 Release complete! dev-${SHORT_SHA} promoted to latest."
