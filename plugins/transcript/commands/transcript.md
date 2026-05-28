---
description: Print the filepath of the current Claude Code session's .jsonl transcript, with size and line count
allowed-tools: Bash(bash:*)
model: haiku
effort: low
disable-model-invocation: true
---

Run the bash block below exactly once and print only its stdout. No preamble, no commentary, no markdown formatting around the result.

Claude Code exports `$CLAUDE_CODE_SESSION_ID` and stores each session's transcript at `$HOME/.claude/projects/<encoded-cwd>/<session-id>.jsonl`, where `<encoded-cwd>` is the absolute current working directory with every `/` and `.` replaced by `-`.

If the env-derived path exists, report it. If not, fall back to the most-recently-modified `*.jsonl` in the encoded directory and label the line `path (fallback):` so the user sees the path didn't come from the env var.

```bash
set -u
sid="${CLAUDE_CODE_SESSION_ID:-}"
if [ -z "$sid" ]; then
  echo "error: CLAUDE_CODE_SESSION_ID is not set" >&2
  exit 1
fi

encoded="$(printf '%s' "$PWD" | tr '/.' '--')"
dir="$HOME/.claude/projects/$encoded"
expected="$dir/$sid.jsonl"

label="path"
file="$expected"
if [ ! -f "$file" ]; then
  if [ -d "$dir" ]; then
    fallback="$(ls -t "$dir"/*.jsonl 2> /dev/null | head -n1)"
    if [ -n "$fallback" ]; then
      file="$fallback"
      label="path (fallback)"
    fi
  fi
fi

if [ ! -f "$file" ]; then
  echo "error: no transcript found at $expected and no fallback in $dir" >&2
  exit 1
fi

# stat -f%z is BSD/darwin; on linux use stat -c%s.
if bytes=$(stat -f%z "$file" 2> /dev/null); then :; else bytes=$(stat -c%s "$file"); fi
mb=$(awk -v b="$bytes" 'BEGIN { printf "%.2f", b/1048576 }')
lines=$(wc -l < "$file" | tr -d ' ')

echo "$label: $file"
echo "size: ${mb} MB"
echo "lines: $lines"
```
