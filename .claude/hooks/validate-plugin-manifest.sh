#!/usr/bin/env bash
# PostToolUse Edit|Write: validate plugin.json / marketplace.json after writes.
. "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

case "$HOOK_FILE" in
  */.claude-plugin/plugin.json)
    if ! jq -e '.name and .version' "$HOOK_FILE" > /dev/null 2>&1; then
      echo "invalid plugin.json: missing parse, .name, or .version: $HOOK_FILE" >&2
      exit 1
    fi
    ;;
  */.claude-plugin/marketplace.json)
    if ! jq -e '.name and .plugins' "$HOOK_FILE" > /dev/null 2>&1; then
      echo "invalid marketplace.json: missing parse, .name, or .plugins: $HOOK_FILE" >&2
      exit 1
    fi
    ;;
esac
