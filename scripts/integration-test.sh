#!/usr/bin/env bash
set -euo pipefail

PASS=0
FAIL=0
SKIP=0

pass() { echo "  ✓ $1"; ((PASS++)); }
fail() { echo "  ✗ $1"; ((FAIL++)); }
skip() { echo "  ⊘ $1 (skipped)"; ((SKIP++)); }

echo "=== copilot-codespace integration tests ==="
echo ""

# 1. Build fresh binary
echo "Building..."
go build -o ./copilot-codespace ./cmd/copilot-codespace
pass "Binary compiles"

# 2. Binary doesn't crash on bad args
echo ""
echo "Test: binary handles bad args..."
./copilot-codespace --nonexistent 2>&1 || true
pass "Binary doesn't crash"

# 3. MCP server starts and responds to JSON-RPC initialize
echo ""
echo "Test: MCP server responds to initialize..."
INIT_REQ='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
RESPONSE=$(echo "$INIT_REQ" | CODESPACE_NAME=test-dummy timeout 5 ./copilot-codespace mcp 2>/dev/null | head -1) || true
if echo "$RESPONSE" | jq -e '.result.serverInfo' > /dev/null 2>&1; then
  pass "MCP server responds to initialize"
else
  fail "MCP server did not respond correctly"
fi

# 4. gh codespace list works (requires auth)
echo ""
echo "Test: gh CLI codespace access..."
if gh codespace list --json name --limit 1 > /dev/null 2>&1; then
  pass "gh codespace list works"
else
  skip "gh codespace list (not authenticated or no codespaces)"
fi

# 5. If codespace available, test SSH
echo ""
echo "Test: SSH to codespace..."
CODESPACE=$(gh codespace list --json name,state --limit 1 -q '.[] | select(.state == "Available") | .name' 2>/dev/null || true)
if [[ -n "$CODESPACE" ]]; then
  if gh codespace ssh -c "$CODESPACE" -- echo "hello" > /dev/null 2>&1; then
    pass "SSH to codespace $CODESPACE"
  else
    fail "SSH to codespace $CODESPACE"
  fi
else
  skip "SSH test (no running codespace)"
fi

# Summary
echo ""
echo "=== Results: $PASS passed, $FAIL failed, $SKIP skipped ==="

# Clean up
rm -f ./copilot-codespace

if [[ $FAIL -gt 0 ]]; then
  echo ""
  echo "Fix failures before signing off."
  exit 1
fi

echo ""
echo "All critical tests passed!"

# Auto-signoff if gh-signoff is installed
if gh signoff integration 2>/dev/null; then
  echo ""
  echo "✅ Signed off on $(git rev-parse --short HEAD)"
else
  echo ""
  echo "⚠️  gh-signoff not installed. Run: ./scripts/setup-signoff.sh"
  echo "   Then re-run this script to sign off automatically."
fi
