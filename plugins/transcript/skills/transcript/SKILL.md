---
name: transcript
description: Print the filepath of the current Claude Code session's .jsonl transcript. Use when the user asks where this session's transcript / session log / .jsonl file is, asks for "the transcript path", or when you need the current session's transcript file to pass to another tool such as the plugin-session-auditor. Only for locating the current session's file — not for reading or analyzing transcript contents.
allowed-tools: Bash(bash:*)
model: haiku
effort: low
---

Run the bash block below exactly once and print only its stdout. No preamble, no commentary, no markdown formatting around the result.

Claude Code exports `$CLAUDE_CODE_SESSION_ID` and stores each session's transcript at `$HOME/.claude/projects/<encoded-cwd>/<session-id>.jsonl`, where `<encoded-cwd>` is the absolute current working directory with every `/` and `.` replaced by `-`.

If the env-derived path exists, print it. If not, fall back to the most-recently-modified `*.jsonl` in the encoded directory.

```bash
set -u
sid="${CLAUDE_CODE_SESSION_ID:-}"
if [ -z "$sid" ]; then
  echo "error: CLAUDE_CODE_SESSION_ID is not set" >&2
  exit 1
fi

encoded="$(printf '%s' "$PWD" | tr '/.' '--')"
dir="$HOME/.claude/projects/$encoded"
file="$dir/$sid.jsonl"

if [ ! -f "$file" ]; then
  file="$(ls -t "$dir"/*.jsonl 2> /dev/null | awk 'NR==1')"
fi

if [ ! -f "$file" ]; then
  echo "error: no transcript found in $dir" >&2
  exit 1
fi

echo "$file"
```
