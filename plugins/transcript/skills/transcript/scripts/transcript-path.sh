#!/usr/bin/env bash
# Print the path of the current Claude Code session's .jsonl transcript.
#
# Claude Code exports $CLAUDE_CODE_SESSION_ID and stores each session's
# transcript at $HOME/.claude/projects/<encoded-cwd>/<session-id>.jsonl, where
# <encoded-cwd> is the absolute cwd with every `/` and `.` replaced by `-`.
#
# Lookup order:
#   1. The env-derived path.
#   2. <session-id>.jsonl by exact name in any project dir. $PWD has usually
#      moved since the session started: EnterWorktree relocates it while the
#      transcript stays under the original cwd's project dir.
#   3. The newest *.jsonl in the encoded dir, only if it records this session.
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
  # if it actually records this session. Lookups 1 and 2 are exact (session ids
  # are unique); this one is a guess. Unverified, it would return some other
  # session's transcript with exit 0, and callers treat this path as
  # authoritative (plugin-session-auditor feeds it straight into its parser).
  # The $sid check turns that silent wrong answer into the error below.
  # `grep -q -m1` stops at the first hit, so the cost is bounded regardless of
  # transcript size.
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
