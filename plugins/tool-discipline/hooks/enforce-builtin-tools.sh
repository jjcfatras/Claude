#!/bin/bash
# ~/.claude/hooks/enforce-builtin-tools.sh
# PreToolUse deny hook: redirect file READS to Read and file EDITS to Edit by
# blocking cat/head/tail/sed when they operate on a FILE. Tools used as pure
# stdin pipeline filters (downstream of `|`) are allowed — they have no
# Read/Edit equivalent.
#
# Pure-bash matching ([[ =~ ]] + parameter expansion) — no cat/head/tail/sed/
# awk/tr/basename, so the hook never uses the tool class it bans and adds no
# per-call spawns (mirrors no-cd-chaining.sh). jq is used only for safe JSON
# parse/encode.
#
# LIMITATION (intentional): blocked tools nested inside command/process
# substitution ($(cat f), <(cat f), `cat f`) or wrapped in `bash -c '...'` are
# NOT inspected. Out of scope to avoid false positives on an every-call hook.

INPUT=$(< /dev/stdin)
COMMAND=$(jq -r '.tool_input.command // empty' <<< "$INPUT")
[ -z "$COMMAND" ] && exit 0

# grep/rg stays OFF by default: CC #52121/#61845 keep the built-in Grep tool
# inoperable, so bash grep/rg is the required fallback. Re-enable via env once
# fixed (machine-discoverable toggle, replacing the old bare comment).
BLOCK_GREP="${TOOL_DISCIPLINE_BLOCK_GREP:-0}"

# split STR on top-level whitespace into the WORDS array, quote-aware
# ('...' and "..." keep their inner spaces). Used to find positional args.
split_words() {
  WORDS=()
  local s=$1 w="" insq=0 indq=0 k=0 len=${#1} ch
  while [ "$k" -lt "$len" ]; do
    ch=${s:k:1}
    if [ "$insq" -eq 1 ]; then
      [ "$ch" = "'" ] && insq=0
      w+=$ch
      k=$((k + 1))
      continue
    fi
    if [ "$indq" -eq 1 ]; then
      [ "$ch" = '"' ] && indq=0
      w+=$ch
      k=$((k + 1))
      continue
    fi
    case "$ch" in
      "'") insq=1 w+=$ch ;;
      '"') indq=1 w+=$ch ;;
      ' ' | $'\t')
        [ -n "$w" ] && {
          WORDS+=("$w")
          w=""
        }
        ;;
      *) w+=$ch ;;
    esac
    k=$((k + 1))
  done
  [ -n "$w" ] && WORDS+=("$w")
}

# has_file_arg base rest -> 0 (true) if the command reads/writes a named file.
# Heuristics per tool; pure-bash, quote-aware via split_words.
has_file_arg() {
  local base=$1
  split_words "$2"
  local positional=0 t skip_next=0
  case "$base" in
    cat)
      for t in "${WORDS[@]}"; do
        [[ $t == -* ]] || return 0 # any non-flag token is a filename
      done
      return 1
      ;;
    head | tail)
      for t in "${WORDS[@]}"; do
        if [ "$skip_next" -eq 1 ]; then
          skip_next=0
          continue
        fi
        case "$t" in
          -n | -c | --lines | --bytes) skip_next=1 ;; # flag takes a separate value
          -*) : ;;                                    # -n5, -5, --lines=, other flags
          *) return 0 ;;                              # bare token = filename
        esac
      done
      return 1
      ;;
    sed)
      for t in "${WORDS[@]}"; do
        if [ "$skip_next" -eq 1 ]; then
          skip_next=0
          continue
        fi
        case "$t" in
          -e | -f | --expression | --file) skip_next=1 ;; # consumes the script/script-file
          -*) : ;;
          *)
            positional=$((positional + 1))
            [ "$positional" -ge 2 ] && return 0 # 1st positional = script, 2nd = data file
            ;;
        esac
      done
      return 1
      ;;
  esac
  return 1
}

# --- quote-aware split into segments, tracking the preceding separator ---
# seps[i]: "" first stmt | "pipe" after `|` | "stmt" after &&/||/;/&
segs=()
seps=()
cur=""
prevsep=""
insq=0
indq=0
i=0
n=${#COMMAND}
while [ "$i" -lt "$n" ]; do
  c=${COMMAND:i:1}
  nx=${COMMAND:i+1:1}
  if [ "$insq" -eq 1 ]; then
    [ "$c" = "'" ] && insq=0
    cur+=$c
    i=$((i + 1))
    continue
  fi
  if [ "$indq" -eq 1 ]; then
    [ "$c" = '"' ] && indq=0
    cur+=$c
    i=$((i + 1))
    continue
  fi
  case "$c" in
    "'")
      insq=1
      cur+=$c
      i=$((i + 1))
      ;;
    '"')
      indq=1
      cur+=$c
      i=$((i + 1))
      ;;
    '|')
      segs+=("$cur")
      seps+=("$prevsep")
      cur=""
      if [ "$nx" = "|" ]; then
        prevsep="stmt"
        i=$((i + 2))
      else
        prevsep="pipe"
        i=$((i + 1))
      fi
      ;;
    '&')
      segs+=("$cur")
      seps+=("$prevsep")
      cur=""
      prevsep="stmt"
      if [ "$nx" = "&" ]; then i=$((i + 2)); else i=$((i + 1)); fi
      ;;
    ';')
      segs+=("$cur")
      seps+=("$prevsep")
      cur=""
      prevsep="stmt"
      i=$((i + 1))
      ;;
    *)
      cur+=$c
      i=$((i + 1))
      ;;
  esac
done
segs+=("$cur")
seps+=("$prevsep")

deny() { # $1 = reason text
  local rj
  rj=$(jq -Rn --arg m "$1" '$m')
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":%s},"systemMessage":%s}\n' "$rj" "$rj"
  exit 0
}

for idx in "${!segs[@]}"; do
  seg=${segs[$idx]}
  pre=${seps[$idx]}
  seg="${seg#"${seg%%[![:space:]]*}"}" # ltrim
  while [[ $seg =~ ^[A-Za-z_][A-Za-z_0-9]*=[^[:space:]]*[[:space:]]+ ]]; do
    seg="${seg#"${BASH_REMATCH[0]}"}" # strip ALL leading VAR=val
  done
  cmd="${seg%%[[:space:]]*}"
  base="${cmd##*/}" # first word + basename
  rest="${seg#"$cmd"}"
  rest="${rest#"${rest%%[![:space:]]*}"}"

  case "$base" in
    cat | head | tail | sed) : ;;
    grep | rg)
      [ "$BLOCK_GREP" = "1" ] && deny "Use the Grep tool instead of \`$base\`. (Segment: $seg)"
      continue
      ;;
    *) continue ;;
  esac

  # sed -i / --in-place always writes a file → block regardless of pipe position
  if [ "$base" = "sed" ] && [[ $rest =~ (^|[[:space:]])(-i|--in-place) ]]; then
    deny "Use Edit for in-place file edits, not \`sed -i\`. (Segment: $seg)"
  fi

  # downstream of a pipe → stdin filter, no Read/Edit equivalent → allow
  [ "$pre" = "pipe" ] && continue

  # first position: block only when a FILE argument is present (heuristic)
  if has_file_arg "$base" "$rest"; then
    case "$base" in
      cat) deny "Use Read to read files, or Write to create them — not \`cat <file>\`. (Segment: $seg)" ;;
      head | tail) deny "Use Read with offset/limit, not \`$base <file>\`. (Segment: $seg)" ;;
      sed) deny "Use Edit for modifications, Read for line ranges — not \`sed … <file>\`. (Segment: $seg)" ;;
    esac
  fi
done
exit 0
