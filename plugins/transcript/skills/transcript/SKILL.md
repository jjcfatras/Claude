---
name: transcript
description: Print the filepath of the current Claude Code session's .jsonl transcript. Use when the user asks where this session's transcript / session log / .jsonl file is, asks for "the transcript path", or when you need the current session's transcript file to pass to another tool such as the plugin-session-auditor. Only for locating the current session's file — not for reading or analyzing transcript contents.
# Bare `Bash`, not a prefix filter: the block below starts `set -u`, and Bash
# filters match the literal command string, so `Bash(bash:*)` never matched the
# one command this skill runs. A filter that cannot fire is worse than none —
# it reads as least-privilege while granting nothing.
allowed-tools: Bash
model: haiku
effort: low
---

Run the bash block below exactly once and print only its stdout. No preamble, no commentary, no markdown formatting around the result.

Claude Code exports `$CLAUDE_CODE_SESSION_ID` and stores each session's transcript at `$HOME/.claude/projects/<encoded-cwd>/<session-id>.jsonl`, where `<encoded-cwd>` is the absolute current working directory with every `/` and `.` replaced by `-`.

If the env-derived path exists, print it. If not, `$PWD` has usually moved since the session started — `EnterWorktree` relocates it while the transcript stays under the original cwd's project dir — so search every project dir for `<session-id>.jsonl` by exact name. Only if that also misses, fall back to the most-recently-modified `*.jsonl` in the encoded directory — but only if that file belongs to this session.

That last condition matters. The first two lookups are exact: session ids are unique, so `<session-id>.jsonl` can only match the right file. The fallback is a guess, and without a check it fails silently in the worst possible way — returning some _other_ session's transcript with exit 0 and no warning. Callers treat this path as authoritative (the `plugin-session-auditor` skill feeds it straight into its parser), so a wrong-but-plausible answer means auditing the wrong session and never knowing. Verifying that the candidate contains `$sid` converts that into the loud error the script already has two lines below.

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
  # $PWD moved since the session started (e.g. EnterWorktree).
  file="$(find "$HOME/.claude/projects" -maxdepth 2 -name "$sid.jsonl" -print -quit 2> /dev/null)"
fi

if [ -z "$file" ]; then
  # Best-effort recovery: newest transcript in this project dir, accepted only
  # if it actually records this session. `grep -q -m1` stops at the first hit,
  # so the cost is bounded regardless of transcript size.
  cand="$(ls -t "$dir"/*.jsonl 2> /dev/null | awk 'NR==1')"
  if [ -n "$cand" ] && grep -q -m1 -F "\"$sid\"" "$cand" 2> /dev/null; then
    file="$cand"
  fi
fi

if [ -z "$file" ] || [ ! -f "$file" ]; then
  echo "error: no transcript for session $sid under $HOME/.claude/projects" >&2
  exit 1
fi

echo "$file"
```
