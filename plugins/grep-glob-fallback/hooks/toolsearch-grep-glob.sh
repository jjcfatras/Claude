#!/bin/bash
# plugins/grep-glob-fallback/hooks/toolsearch-grep-glob.sh
# Since Claude Code 2.1.117, native macOS/Linux builds remove the Grep/Glob
# tools by design: their engines are embedded in the claude multi-call binary
# and exposed in the Bash tool as shell functions — rg → embedded ripgrep,
# grep → embedded gitignore-aware ugrep, find → embedded bfs. ToolSearch still
# cannot load Grep/Glob on these builds (#52121/#61845 track the catalog gap),
# so a `select:` attempt fails confusingly. This hook denies those attempts and
# redirects to the embedded Bash search. On builds that still ship Grep/Glob
# (npm, Windows) the probe below finds no embedded ripgrep and the hook allows
# the load. Launch-time escape hatch: `claude --tools Grep,Glob` (2.1.162+)
# restores the dedicated tools on native builds.
INPUT=$(cat)
QUERY=$(echo "$INPUT" | jq -r '.tool_input.query // empty')
[ -z "$QUERY" ] && exit 0

# Only act on `select:` loads that name Grep/Glob (avoid free-text false positives)
echo "$QUERY" | grep -qiE 'select:' || exit 0

hits=""
echo "$QUERY" | grep -qiwE 'Grep' && hits="${hits}Grep "
echo "$QUERY" | grep -qiwE 'Glob' && hits="${hits}Glob "
[ -z "$hits" ] && exit 0

# Self-disable on builds that still ship Grep/Glob: only deny when the claude
# binary embeds ripgrep (native multi-call build). exec -a sets argv[0]; the
# command substitution is already a subshell, so exec doesn't kill the script.
cc_bin="${CLAUDE_CODE_EXECPATH:-$(command -v claude)}"
[ -x "$cc_bin" ] || exit 0
probe=$(exec -a rg "$cc_bin" --version 2> /dev/null)
case "$probe" in ripgrep*) ;; *) exit 0 ;; esac

# Bash alternative hint (enforce-builtin-tools.sh-style "Do not X. Use Y")
alt=""
case "$hits" in *Grep*) alt="${alt}rg '<pattern>' for Grep — embedded ripgrep, same engine the Grep tool used (-g '<glob>' filter, -t <type>, -i -n -A/-B/-C, -l, --files, -U multiline); grep = embedded gitignore-aware ugrep (-z searches archives). " ;; esac
case "$hits" in *Glob*) alt="${alt}find <path> -name '<pat>' for Glob — embedded bfs; or rg --files -g '<pat>' for a gitignore-aware listing. " ;; esac

reason="${hits}removed by design on native builds (Claude Code 2.1.117+): their engines are embedded in the claude binary and exposed as Bash shell functions; ToolSearch cannot load them (#52121/#61845). Use Bash: ${alt}"
echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"deny\",\"permissionDecisionReason\":\"${reason}\"}}"
exit 0
