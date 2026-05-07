#!/usr/bin/env bash
# Auto-format hook for PostToolUse Edit|Write.
# Reads tool input JSON from stdin, routes by file extension.
set -eo pipefail

f=$(jq -r '.tool_input.file_path')
[ -f "$f" ] || exit 0

case "$f" in
  *.go)
    gofmt -w "$f"
    ;;
  */go.mod)
    go mod edit -fmt "$f"
    ;;
  *)
    (cd "$CLAUDE_PROJECT_DIR" && pnpm --silent exec prettier --write --ignore-unknown --log-level warn "$f")
    ;;
esac
