#!/usr/bin/env bash
# PostToolUse Edit|Write: validate plugin.json / marketplace.json after writes.
. "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

case "$HOOK_FILE" in
  */.claude-plugin/plugin.json)
    if ! jq -e '.name and .version' "$HOOK_FILE" > /dev/null 2>&1; then
      echo "invalid plugin.json: missing parse, .name, or .version: $HOOK_FILE" >&2
      exit 1
    fi
    name=$(jq -r '.name' "$HOOK_FILE")
    if ! jq -e --arg n "$name" 'any(.plugins[]; .name == $n)' "$CLAUDE_PROJECT_DIR/.claude-plugin/marketplace.json" > /dev/null 2>&1; then
      echo "plugin $name has no entry in .claude-plugin/marketplace.json — add one" >&2
      exit 2
    fi
    ;;
  */.claude-plugin/marketplace.json)
    if ! jq -e '.name and .plugins' "$HOOK_FILE" > /dev/null 2>&1; then
      echo "invalid marketplace.json: missing parse, .name, or .plugins: $HOOK_FILE" >&2
      exit 1
    fi
    ;;
esac
