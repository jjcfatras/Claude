#!/usr/bin/env bash
# PreToolUse Read|Edit|Write|Grep|Glob: block doc-audit tool calls whose
# target path falls inside any node_modules/ directory.
set -euo pipefail

INPUT=$(cat)
FILE_PATH=$(jq -r '.tool_input.file_path // .tool_input.path // empty' <<< "$INPUT")

[ -z "$FILE_PATH" ] && exit 0

case "/$FILE_PATH/" in
  */node_modules/*)
    echo "doc-audit: blocked tool call targeting node_modules path: $FILE_PATH" >&2
    exit 2
    ;;
esac

exit 0
