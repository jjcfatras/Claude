#!/bin/bash
# ~/.claude/hooks/toolsearch-grep-glob.sh
# Grep/Glob are currently inoperable (Claude Code bugs #52121, #61845): stripped
# from the session and absent from the ToolSearch deferred catalog. This hook
# intercepts ToolSearch attempts to load them and redirects to the Bash fallback
# (rg / find / ls). Revert the reason text once the tools are restored.
INPUT=$(cat)
QUERY=$(echo "$INPUT" | jq -r '.tool_input.query // empty')
[ -z "$QUERY" ] && exit 0

# Only act on `select:` loads that name Grep/Glob (avoid free-text false positives)
echo "$QUERY" | grep -qiE 'select:' || exit 0

hits=""
echo "$QUERY" | grep -qiwE 'Grep' && hits="${hits}Grep "
echo "$QUERY" | grep -qiwE 'Glob' && hits="${hits}Glob "
[ -z "$hits" ] && exit 0

# Bash alternative hint (enforce-builtin-tools.sh-style "Do not X. Use Y")
alt=""
case "$hits" in *Grep*) alt="${alt}rg <pattern> for Grep. " ;; esac
case "$hits" in *Glob*) alt="${alt}find . -name '<pat>' / ls for Glob. " ;; esac

reason="${hits}unavailable — Claude Code bug #52121/#61845 strips them from the session and the ToolSearch catalog (not loadable). Use Bash: ${alt}"
echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"deny\",\"permissionDecisionReason\":\"${reason}\"}}"
exit 0
