#!/usr/bin/env bash
set -euo pipefail

echo "=== Setting up gh-signoff ==="

# Check prerequisites
if ! command -v gh &> /dev/null; then
  echo "Error: gh CLI is required. Install from https://cli.github.com"
  exit 1
fi

# Install gh-signoff extension
echo "Installing gh-signoff extension..."
if gh extension list | grep -q "basecamp/gh-signoff"; then
  echo "  Already installed, upgrading..."
  gh extension upgrade basecamp/gh-signoff
else
  gh extension install basecamp/gh-signoff
fi
echo "  ✓ gh-signoff installed"

# Show version
echo ""
gh signoff version

# Show usage
echo ""
echo "=== Usage ==="
echo ""
echo "After running integration tests locally:"
echo "  ./scripts/integration-test.sh"
echo "  gh signoff integration"
echo ""
echo "Check status:"
echo "  gh signoff status"
echo ""
echo "Optional: require signoff for branch protection:"
echo "  gh signoff install --branch main integration"
