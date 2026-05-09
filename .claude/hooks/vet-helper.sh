#!/usr/bin/env bash
# PostToolUse Edit|Write: run `go vet` on the code-review helper after Go edits.
. "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

case "$HOOK_FILE" in
  */plugins/code-review/tools/code-review-helper/*.go)
    (cd "$CLAUDE_PROJECT_DIR/plugins/code-review/tools/code-review-helper" && go vet ./...) >&2
    ;;
esac
