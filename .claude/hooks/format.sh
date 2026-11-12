#!/usr/bin/env bash
# Auto-format hook for PostToolUse Edit|Write.
# Reads tool input JSON from stdin and hands the file to the repo formatter, which routes by type.
. "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"
"$CLAUDE_PROJECT_DIR/scripts/format.sh" "$HOOK_FILE"
