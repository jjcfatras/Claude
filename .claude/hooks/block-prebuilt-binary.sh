#!/usr/bin/env bash
# PreToolUse Edit|Write: block direct edits to prebuilt code-review binaries.
# Regenerate via (cd plugins/code-review-AT/tools/code-review-helper && make release).
# Exit 2 blocks the tool call and surfaces this message to Claude.
. "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

case "$HOOK_FILE" in
  */plugins/code-review-AT/bin/*)
    echo "prebuilt binary: regenerate via (cd plugins/code-review-AT/tools/code-review-helper && make release)" >&2
    exit 2
    ;;
esac
