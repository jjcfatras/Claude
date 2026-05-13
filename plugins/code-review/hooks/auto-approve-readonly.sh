#!/bin/bash
# Auto-approve Grep and Glob so code-review specialists do not stall on
# permission prompts. Both tools are read-only.
set -euo pipefail

TOOL_NAME=$(jq -r '.tool_name // empty')

case "$TOOL_NAME" in
  Grep | Glob)
    cat << 'JSON'
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "allow",
    "permissionDecisionReason": "code-review plugin: read-only tool auto-approved"
  }
}
JSON
    ;;
  *)
    exit 0
    ;;
esac
