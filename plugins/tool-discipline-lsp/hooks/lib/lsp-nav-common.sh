# shellcheck shell=bash
# lib/lsp-nav-common.sh — shared engine for the per-language LSP-steering hooks.
#
# SOURCED, never executed directly. enforce-lsp-navigation-go.sh and
# enforce-lsp-navigation-ts.sh each source this file, declare their language
# contract, then call run_lsp_nav. All language-agnostic logic lives here:
# input read, opt-out/mode handling, symbol gates, the Grep-tool input parser,
# the Bash grep/rg command parser, and the decision emitter.
#
# Language contract a caller must provide before calling run_lsp_nav:
#   LSP_LANG_LABEL        - human label for the reason string ("Go", "TS").
#   LSP_TYPE_NAMES        - space-separated rg/grep `-t` type names for this lang.
#   LSP_EXTENSIONS        - space-separated bare extensions (no dot) for this lang.
#   lsp_server_available  - function returning 0 when the backing LSP binary exists.
#
# Decision/exit behavior is unchanged from the original single-script hook;
# splitting only relocates the language data. CC runs both per-language hooks in
# parallel and merges PreToolUse decisions most-restrictive-wins, and a command
# resolves to at most one language scope, so at most one hook ever denies.

LSP_REASON_TAIL="Use the LSP tool. If you already know a file that references this symbol, prefer goToDefinition / hover there (most reliable; loads the project without needing the global index). Use workspaceSymbol for a blind global search — if it returns empty, the server may still be indexing, so retry once before concluding the symbol is absent. If this is a string-literal / comment / config-key search rather than a symbol, search a non-LSP scope, or set TOOL_DISCIPLINE_BLOCK_LSP=0 to bypass."

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

# _in_list NEEDLE "space-separated list" -> 0 if NEEDLE is an element.
_in_list() {
  local needle="$1" item
  for item in $2; do
    [ "$item" = "$needle" ] && return 0
  done
  return 1
}

# _match_type <rg/grep type> -> 0 if the type name belongs to this language
# (case-insensitive), per LSP_TYPE_NAMES.
_match_type() {
  _in_list "$(echo "$1" | tr 'A-Z' 'a-z')" "$LSP_TYPE_NAMES"
}

# _match_globs <space-separated globs/exts> -> 0 if any extension token belongs
# to this language, per LSP_EXTENSIONS. Tokenizes on non-alphanumerics so a glob
# like *.ts yields the token "ts".
_match_globs() {
  local ext
  for ext in $(echo "$1" | grep -oE '[A-Za-z0-9]+'); do
    _in_list "$(echo "$ext" | tr 'A-Z' 'a-z')" "$LSP_EXTENSIONS" && return 0
  done
  return 1
}

# _match_path <filename/path> -> 0 if the file extension belongs to this language.
_match_path() {
  case "$1" in
    *.*) _in_list "$(echo "${1##*.}" | tr 'A-Z' 'a-z')" "$LSP_EXTENSIONS" ;;
    *) return 1 ;;
  esac
}

emit_decision() {
  echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"$1\",\"permissionDecisionReason\":\"$2\"}}"
}

# run_lsp_nav — main driver. Reads the tool input from stdin and, for a
# high-confidence symbol search scoped to this language, emits a deny/ask
# decision. Everything else passes through (exit 0, no output).
run_lsp_nav() {
  local INPUT TOOL LSP_MODE
  INPUT=$(cat)

  # Opt-out switch (mirrors enforce-builtin-tools.sh TOOL_DISCIPLINE_BLOCK_GREP).
  [ "${TOOL_DISCIPLINE_BLOCK_LSP:-1}" = "0" ] && exit 0
  # The hook only makes sense when the LSP tool it steers toward is enabled.
  [ "${ENABLE_LSP_TOOL:-}" = "1" ] || exit 0

  # Decision mode: deny (default, hard block) or ask (soft permission prompt).
  LSP_MODE="${TOOL_DISCIPLINE_LSP_MODE:-deny}"
  [ "$LSP_MODE" = "ask" ] || LSP_MODE="deny"

  TOOL=$(echo "$INPUT" | jq -r '.tool_name // empty')

  # -------------------------------------------------------------------------
  # Grep tool branch (structured inputs). Also the fallback when tool_name is
  # absent, preserving the original behavior.
  # -------------------------------------------------------------------------
  if [ "$TOOL" != "Bash" ]; then
    local PATTERN GLOB TYPE PATH_ARG core claimed
    PATTERN=$(echo "$INPUT" | jq -r '.tool_input.pattern // empty')
    GLOB=$(echo "$INPUT" | jq -r '.tool_input.glob // empty')
    TYPE=$(echo "$INPUT" | jq -r '.tool_input.type // empty')
    PATH_ARG=$(echo "$INPUT" | jq -r '.tool_input.path // empty')
    [ -z "$PATTERN" ] && exit 0

    core=$(is_symbol "$PATTERN")
    [ -z "$core" ] && exit 0
    is_symbol_shaped "$core" || exit 0

    claimed=0
    [ -n "$TYPE" ] && _match_type "$TYPE" && claimed=1
    [ "$claimed" -eq 0 ] && [ -n "$GLOB" ] && _match_globs "$GLOB" && claimed=1
    [ "$claimed" -eq 0 ] && [ -n "$PATH_ARG" ] && _match_path "$PATH_ARG" && claimed=1
    [ "$claimed" -eq 0 ] && exit 0
    lsp_server_available || exit 0

    emit_decision "$LSP_MODE" "Symbol search '$core' in a $LSP_LANG_LABEL file. $LSP_REASON_TAIL"
    exit 0
  fi

  # -------------------------------------------------------------------------
  # Bash branch: detect `grep`/`rg` symbol searches inside the raw command.
  # Split into segments on | ; && || (same splitter as enforce-builtin-tools.sh)
  # and inspect each. Conservative: any ambiguity -> pass through.
  # -------------------------------------------------------------------------
  local COMMAND segment seg base pattern gtype gglob tok core p claimed i n
  local -a toks paths
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

    claimed=0
    [ -n "$gtype" ] && _match_type "$gtype" && claimed=1
    [ "$claimed" -eq 0 ] && [ -n "$gglob" ] && _match_globs "$gglob" && claimed=1
    if [ "$claimed" -eq 0 ] && [ "${#paths[@]}" -gt 0 ]; then
      for p in "${paths[@]}"; do
        _match_path "$p" && {
          claimed=1
          break
        }
      done
    fi
    # No type/glob/path narrows to an LSP file -> ambiguous, pass through.
    [ "$claimed" -eq 0 ] && continue
    lsp_server_available || continue

    emit_decision "$LSP_MODE" "Symbol search '$core' via \`$base\` in $LSP_LANG_LABEL scope. $LSP_REASON_TAIL"
    exit 0
  done < <(printf '%s\n' "$COMMAND" | tr '|' '\n' | sed 's/[;&]\{1,2\}/\n/g')
  exit 0
}
