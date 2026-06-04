#!/bin/bash
# ~/.claude/hooks/enforce-lsp-navigation.sh
# Push symbol searches in Go/TS-family files toward the LSP tool.
# Fires on the Grep tool AND on `grep`/`rg` run via Bash.
# Fires ONLY on a high-confidence symbol search; everything else passes through.
INPUT=$(cat)
TOOL=$(echo "$INPUT" | jq -r '.tool_name // empty')

# Go/TS-family (LSP-covered) scopes.
lsp_ext='^(go|ts|tsx|js|jsx|mts|cts|mjs|cjs)$'
lsp_type='^(go|ts|typescript|tsx|js|javascript|jsx)$'

# Symbol gate: echo the bare identifier (and return 0) if the candidate is a
# symbol; print nothing (return 1) otherwise. Strips \b anchors, one paren
# pair, and one layer of surrounding single/double quotes first.
is_symbol() {
  local core="$1"
  core=${core#\\b}
  core=${core%\\b}
  core=${core#(}
  core=${core%)}
  core=${core#\"}
  core=${core%\"}
  core=${core#\'}
  core=${core%\'}
  echo "$core" | grep -Eq '^[A-Za-z_][A-Za-z0-9_]*$' && printf '%s' "$core"
}

# ext_matches_lsp <space-separated globs/exts> -> 0 if any names an LSP ext.
ext_matches_lsp() {
  local ext
  for ext in $(echo "$1" | grep -oE '[A-Za-z0-9]+'); do
    echo "$ext" | grep -Eqi "$lsp_ext" && return 0
  done
  return 1
}

emit_deny() {
  echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"deny\",\"permissionDecisionReason\":\"$1\"}}"
}

LSP_REASON_TAIL="Use the LSP tool: workspaceSymbol to locate it, goToDefinition / findReferences / incomingCalls to navigate. Use search only for non-symbol text (string literals, comments, config keys)."

# ---------------------------------------------------------------------------
# Grep tool branch (structured inputs). Also the fallback when tool_name is
# absent, preserving the original behavior.
# ---------------------------------------------------------------------------
if [ "$TOOL" != "Bash" ]; then
  PATTERN=$(echo "$INPUT" | jq -r '.tool_input.pattern // empty')
  GLOB=$(echo "$INPUT" | jq -r '.tool_input.glob // empty')
  TYPE=$(echo "$INPUT" | jq -r '.tool_input.type // empty')
  PATH_ARG=$(echo "$INPUT" | jq -r '.tool_input.path // empty')
  [ -z "$PATTERN" ] && exit 0

  core=$(is_symbol "$PATTERN")
  [ -z "$core" ] && exit 0

  matched=0
  [ -n "$TYPE" ] && echo "$TYPE" | grep -Eqi "$lsp_type" && matched=1
  [ "$matched" -eq 0 ] && [ -n "$GLOB" ] && ext_matches_lsp "$GLOB" && matched=1
  if [ "$matched" -eq 0 ] && [ -n "$PATH_ARG" ]; then
    echo "${PATH_ARG##*.}" | grep -Eqi "$lsp_ext" && matched=1
  fi
  [ "$matched" -eq 0 ] && exit 0

  emit_deny "Symbol search '$core' in a Go/TS file. $LSP_REASON_TAIL"
  exit 0
fi

# ---------------------------------------------------------------------------
# Bash branch: detect `grep`/`rg` symbol searches inside the raw command.
# Split into segments on | ; && || (same splitter as enforce-builtin-tools.sh)
# and inspect each. Conservative: any ambiguity -> pass through.
# ---------------------------------------------------------------------------
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')
[ -z "$COMMAND" ] && exit 0

while IFS= read -r segment; do
  # Strip leading whitespace and a leading VAR=val assignment prefix.
  seg=$(echo "$segment" | sed 's/^[[:space:]]*//' | sed 's/^[A-Za-z_][A-Za-z_0-9]*=[^ ]* //')
  # Word-split (no glob/quote handling: a quoted multi-word pattern splits and
  # then fails the symbol gate, so it safely passes through).
  read -ra toks <<< "$seg"
  [ "${#toks[@]}" -eq 0 ] && continue
  base=$(basename "${toks[0]}" 2> /dev/null)
  { [ "$base" = "grep" ] || [ "$base" = "rg" ]; } || continue

  pattern=""
  gtype=""
  gglob=""
  paths=()
  i=1
  n=${#toks[@]}
  while [ "$i" -lt "$n" ]; do
    tok="${toks[$i]}"
    case "$tok" in
      # Value-taking flags: skip the flag and its following value token.
      -e | --regexp | -f | --file | -m | --max-count | -A | -B | -C | --context)
        i=$((i + 1))
        ;;
      # rg type filter.
      -t | --type)
        i=$((i + 1))
        gtype="${toks[$i]}"
        ;;
      --type=*) gtype="${tok#*=}" ;;
      # rg glob / grep include filter.
      -g | --glob | --include)
        i=$((i + 1))
        gglob="$gglob ${toks[$i]}"
        ;;
      --glob=* | --include=*) gglob="$gglob ${tok#*=}" ;;
      # Any other flag: plain, consumes no value.
      -*) : ;;
      # First bare token is the pattern; the rest are path targets.
      *) if [ -z "$pattern" ]; then pattern="$tok"; else paths+=("$tok"); fi ;;
    esac
    i=$((i + 1))
  done

  [ -z "$pattern" ] && continue
  core=$(is_symbol "$pattern")
  [ -z "$core" ] && continue

  matched=0
  [ -n "$gtype" ] && echo "$gtype" | grep -Eqi "$lsp_type" && matched=1
  [ "$matched" -eq 0 ] && [ -n "$gglob" ] && ext_matches_lsp "$gglob" && matched=1
  if [ "$matched" -eq 0 ] && [ "${#paths[@]}" -gt 0 ]; then
    for p in "${paths[@]}"; do
      case "$p" in
        *.*) echo "${p##*.}" | grep -Eqi "$lsp_ext" && {
          matched=1
          break
        } ;;
      esac
    done
  fi
  # No type/glob/path narrows to an LSP file -> ambiguous, pass through.
  [ "$matched" -eq 0 ] && continue

  emit_deny "Symbol search '$core' via \`$base\` in Go/TS scope. $LSP_REASON_TAIL"
  exit 0
done < <(echo "$COMMAND" | tr '|' '\n' | sed 's/[;&]\{1,2\}/\n/g')
exit 0
