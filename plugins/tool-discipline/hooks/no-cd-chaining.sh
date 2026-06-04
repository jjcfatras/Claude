#!/bin/bash
# ~/.claude/hooks/no-cd-chaining.sh
# PreToolUse deny hook: block cd-chaining and common bypass forms in agent Bash
# commands so the agent runs directly with absolute paths instead.
#
# Pure-bash matching ([[ =~ ]] + BASH_REMATCH) — no cat/grep/sed, so it stays
# consistent with enforce-builtin-tools.sh and adds no per-call spawns. jq is
# kept only for safe JSON parse/encode (regex JSON parsing is fragile).

INPUT=$(< /dev/stdin)
COMMAND=$(jq -r '.tool_input.command // empty' <<< "$INPUT")
[ -z "$COMMAND" ] && exit 0

# --- ERE fragments (no literal spaces; safe to use unquoted in [[ =~ ]]) ---
path='("[^"]*"|'\''[^'\'']*'\''|[^ &;|]+)' # "..." | '...' | bare token
sep='(&&|;|\|\|)'                          # && | ; | ||  (|| is literal)

# cd/pushd chaining, optionally wrapped in a subshell `(` or brace group `{`
cd_re="^[[:space:]]*[({]?[[:space:]]*(cd|pushd)[[:space:]]+${path}[[:space:]]*${sep}"
# env -C DIR / env --chdir[=]DIR  (requires `env ` prefix → `grep -C`, `environment` do not match)
env_re="^[[:space:]]*[({]?[[:space:]]*env[[:space:]]+(-C[[:space:]]|--chdir[[:space:]=])"

matched_cd=0
[[ $COMMAND =~ $cd_re ]] && matched_cd=1
matched_env=0
[[ $COMMAND =~ $env_re ]] && matched_env=1
[ "$matched_cd" -eq 0 ] && [ "$matched_env" -eq 0 ] && exit 0

GENERIC="Do not prefix with cd/pushd or env -C. Run the command directly with an absolute path."
REAL_CMD="$COMMAND"

if [ "$matched_cd" -eq 1 ]; then
  # Strip every leading `cd/pushd <path> <sep>` segment (handles cd a && cd b && cmd).
  strip="^[[:space:]]*[({]?[[:space:]]*(cd|pushd)[[:space:]]+${path}[[:space:]]*${sep}[[:space:]]*"
  while [[ $REAL_CMD =~ $strip ]]; do
    REAL_CMD="${REAL_CMD#"${BASH_REMATCH[0]}"}"
  done
elif [ "$matched_env" -eq 1 ]; then
  estrip="^[[:space:]]*[({]?[[:space:]]*env[[:space:]]+(-C[[:space:]]+|--chdir[[:space:]=]+)${path}[[:space:]]+"
  [[ $REAL_CMD =~ $estrip ]] && REAL_CMD="${REAL_CMD#"${BASH_REMATCH[0]}"}"
fi

# Trim trailing subshell/group/separator junk (e.g. the `)` left by `(cd /p && cmd)`).
trim='^(.*[^[:space:])};&|])[[:space:])};&|]*$'
[[ $REAL_CMD =~ $trim ]] && REAL_CMD="${BASH_REMATCH[1]}"

# Only offer a suggestion when extraction is clean (changed and still a real command);
# otherwise give generic guidance rather than a garbled hint.
if [ "$REAL_CMD" != "$COMMAND" ] && [[ $REAL_CMD =~ [[:alnum:]] ]]; then
  REASON="$GENERIC Run directly: $REAL_CMD"
else
  REASON="$GENERIC"
fi

# Encode the reason with jq so quotes/backslashes in the command can't break the JSON.
reason_json=$(jq -Rn --arg m "$REASON" '$m')
printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":%s}}\n' "$reason_json"
exit 0
