#!/bin/bash
# ~/.claude/hooks/enforce-builtin-tools.sh
INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')
[ -z "$COMMAND" ] && exit 0

# Check all segments of piped/chained commands
while IFS= read -r segment; do
  cmd=$(echo "$segment" | sed 's/^[[:space:]]*//' | sed 's/^[A-Za-z_][A-Za-z_0-9]*=[^ ]* //')
  base=$(basename "$(echo "$cmd" | awk '{print $1}')" 2> /dev/null)
  case "$base" in
    cat) msg="Use Read to read files, or Write to create them" ;;
    head | tail) msg="Use Read with offset/limit parameters" ;;
    sed) msg="Use Edit for modifications, Read for line ranges" ;;
    # grep|rg)   msg="Use the built-in Grep tool" ;;
    #            ^ DISABLED ON PURPOSE: the built-in Grep tool is currently inoperable
    #              (CC bug #52121/#61845), so bash grep/rg is the required fallback.
    #              Do NOT re-enable this line until Grep/Glob are restored.
    *) continue ;;
  esac
  echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"deny\",\"permissionDecisionReason\":\"Do not use \`$base\`. $msg\"}}"
  exit 0
done < <(echo "$COMMAND" | tr '|' '\n' | sed 's/[;&]\{1,2\}/\n/g')
exit 0
