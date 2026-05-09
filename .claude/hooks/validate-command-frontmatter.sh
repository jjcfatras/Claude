#!/usr/bin/env bash
# PostToolUse Edit|Write: enforce slash-command frontmatter shape.
# Requires opening '---' on line 1 and a 'description:' field before the closing '---'.
. "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

case "$HOOK_FILE" in
  */plugins/*/commands/*.md)
    awk -v f="$HOOK_FILE" '
      NR == 1 && $0 != "---" { msg = "command frontmatter must start with ---"; done = 1; exit 1 }
      NR == 1 { next }
      /^---[[:space:]]*$/ { done = 1; if (found) exit 0; msg = "command frontmatter missing description field"; exit 1 }
      /^description:/ { found = 1 }
      END {
        if (!done && !found) msg = "command frontmatter missing description field"
        if (msg) { print f ": " msg > "/dev/stderr"; exit 1 }
      }
    ' "$HOOK_FILE"
    ;;
esac
