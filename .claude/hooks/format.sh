#!/usr/bin/env bash
# Auto-format hook for PostToolUse Edit|Write.
# Reads tool input JSON from stdin, routes by file extension.
. "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"
[ -f "$HOOK_FILE" ] || exit 0

case "$HOOK_FILE" in
  *.go)
    gofmt -w "$HOOK_FILE"
    ;;
  */go.mod)
    go mod edit -fmt "$HOOK_FILE"
    ;;
  *)
    (cd "$CLAUDE_PROJECT_DIR" && pnpm --silent exec prettier --write --ignore-unknown --log-level warn "$HOOK_FILE")
    ;;
esac
