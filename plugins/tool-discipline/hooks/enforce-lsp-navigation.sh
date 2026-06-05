#!/bin/bash
# ~/.claude/hooks/enforce-lsp-navigation.sh
# Push symbol searches in Go/TS-family files toward the LSP tool.
# Fires on the Grep tool AND on `grep`/`rg` run via Bash.
# Fires ONLY on a high-confidence symbol search; everything else passes through.
#
# Fail-open / opt-out (all checked before any deny):
#   TOOL_DISCIPLINE_BLOCK_LSP=0  -> disable this hook entirely.
#   ENABLE_LSP_TOOL not "1"      -> the steered-to tool is off; do nothing.
#   backing server binary absent -> no LSP to use for that scope; pass through.
#   TOOL_DISCIPLINE_LSP_MODE=ask -> soft prompt instead of a hard deny.
INPUT=$(cat)
TOOL=$(echo "$INPUT" | jq -r '.tool_name // empty')

# Opt-out switch (mirrors enforce-builtin-tools.sh TOOL_DISCIPLINE_BLOCK_GREP).
[ "${TOOL_DISCIPLINE_BLOCK_LSP:-1}" = "0" ] && exit 0
# The hook only makes sense when the LSP tool it steers toward is enabled.
[ "${ENABLE_LSP_TOOL:-}" = "1" ] || exit 0

# Decision mode: deny (default, hard block) or ask (soft permission prompt).
LSP_MODE="${TOOL_DISCIPLINE_LSP_MODE:-deny}"
[ "$LSP_MODE" = "ask" ] || LSP_MODE="deny"

# Go/TS-family (LSP-covered) type filter.
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

# Symbol-shape gate (deny decision only): a validated identifier looks like a
# code symbol when it carries an uppercase letter or an underscore
# (CamelCase / PascalCase / snake_case / SCREAMING_CASE). A bare all-lowercase
# single word (enabled, error, status) is far more likely a string-literal or
# config-key content search the LSP cannot serve, so it passes through.
# \b anchoring is deliberately NOT treated as symbol intent — it is stripped by
# is_symbol, then the inner token is judged purely by shape.
is_symbol_shaped() {
  # LC_ALL=C: under some locales (notably /bin/bash 3.2 + UTF-8) the [A-Z] glob
  # collates to include lowercase, which would mis-classify plain words as
  # shaped. Force byte ordering so [A-Z] means exactly A-Z.
  local LC_ALL=C
  case "$1" in
    *[A-Z]* | *_*) return 0 ;;
    *) return 1 ;;
  esac
}

# lsp_lang_for_globs <space-separated globs/exts> -> echo "go" or "ts" for the
# first LSP-covered extension found; echo nothing (return 1) otherwise.
lsp_lang_for_globs() {
  local ext
  for ext in $(echo "$1" | grep -oE '[A-Za-z0-9]+'); do
    case "$(echo "$ext" | tr 'A-Z' 'a-z')" in
      go)
        echo go
        return 0
        ;;
      ts | tsx | js | jsx | mts | cts | mjs | cjs)
        echo ts
        return 0
        ;;
    esac
  done
  return 1
}

# lsp_lang_for_type <rg/grep type> -> echo "go" or "ts" (caller has already
# confirmed the type matches lsp_type).
lsp_lang_for_type() {
  case "$(echo "$1" | tr 'A-Z' 'a-z')" in
    go) echo go ;;
    *) echo ts ;;
  esac
}

# lsp_lang_for_path <filename/path> -> echo "go"/"ts" by extension; else nothing.
lsp_lang_for_path() {
  case "$1" in
    *.go) echo go ;;
    *.ts | *.tsx | *.js | *.jsx | *.mts | *.cts | *.mjs | *.cjs) echo ts ;;
  esac
}

# lsp_server_available <lang> -> 0 if the backing LSP server binary is present.
# gopls commonly installs to ~/go/bin (or $GOBIN / $GOPATH/bin), which is often
# absent from the hook's PATH, so check those locations explicitly too.
lsp_server_available() {
  case "$1" in
    go)
      command -v gopls > /dev/null 2>&1 \
        || [ -x "${GOBIN:-${GOPATH:-$HOME/go}/bin}/gopls" ] \
        || [ -x "$HOME/go/bin/gopls" ]
      ;;
    ts) command -v typescript-language-server > /dev/null 2>&1 ;;
    *) return 1 ;;
  esac
}

emit_decision() {
  echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"$1\",\"permissionDecisionReason\":\"$2\"}}"
}

LSP_REASON_TAIL="Use the LSP tool. If you already know a file that references this symbol, prefer goToDefinition / hover there (most reliable; loads the project without needing the global index). Use workspaceSymbol for a blind global search — if it returns empty, the server may still be indexing, so retry once before concluding the symbol is absent. If this is a string-literal / comment / config-key search rather than a symbol, search a non-LSP scope, or set TOOL_DISCIPLINE_BLOCK_LSP=0 to bypass."

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
  is_symbol_shaped "$core" || exit 0

  lang=""
  [ -n "$TYPE" ] && echo "$TYPE" | grep -Eqi "$lsp_type" && lang=$(lsp_lang_for_type "$TYPE")
  [ -z "$lang" ] && [ -n "$GLOB" ] && lang=$(lsp_lang_for_globs "$GLOB")
  [ -z "$lang" ] && [ -n "$PATH_ARG" ] && lang=$(lsp_lang_for_path "$PATH_ARG")
  [ -z "$lang" ] && exit 0
  lsp_server_available "$lang" || exit 0

  emit_decision "$LSP_MODE" "Symbol search '$core' in a Go/TS file. $LSP_REASON_TAIL"
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
  # printf, not echo: echo interprets backslash escapes (e.g. \b -> backspace)
  # in some shells, corrupting word-boundary patterns before is_symbol can strip
  # the \b anchors. printf '%s' passes the bytes through verbatim.
  seg=$(printf '%s' "$segment" | sed 's/^[[:space:]]*//; s/^[A-Za-z_][A-Za-z_0-9]*=[^ ]* //')
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
  is_symbol_shaped "$core" || continue

  lang=""
  [ -n "$gtype" ] && echo "$gtype" | grep -Eqi "$lsp_type" && lang=$(lsp_lang_for_type "$gtype")
  [ -z "$lang" ] && [ -n "$gglob" ] && lang=$(lsp_lang_for_globs "$gglob")
  if [ -z "$lang" ] && [ "${#paths[@]}" -gt 0 ]; then
    for p in "${paths[@]}"; do
      lang=$(lsp_lang_for_path "$p")
      [ -n "$lang" ] && break
    done
  fi
  # No type/glob/path narrows to an LSP file -> ambiguous, pass through.
  [ -z "$lang" ] && continue
  lsp_server_available "$lang" || continue

  emit_decision "$LSP_MODE" "Symbol search '$core' via \`$base\` in Go/TS scope. $LSP_REASON_TAIL"
  exit 0
done < <(printf '%s\n' "$COMMAND" | tr '|' '\n' | sed 's/[;&]\{1,2\}/\n/g')
exit 0
