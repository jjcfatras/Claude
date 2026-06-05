#!/usr/bin/env bash
# PreToolUse Read|Edit|Write|Grep|Glob: block doc-audit tool calls whose
# target path falls inside an excluded directory — node_modules/ or
# .claude/worktrees/ (git-worktree copies of the repo).
#
# Exception: a session working *inside* a worktree (cwd under
# .claude/worktrees/<name>/) is allowed to read/edit that worktree's own files
# — the exclusion is meant to keep doc-audit *scans* (run from the main
# checkout) out of worktrees, not to wall off legitimate in-worktree editing.
set -euo pipefail

INPUT=$(cat)
FILE_PATH=$(jq -r '.tool_input.file_path // .tool_input.path // empty' <<< "$INPUT")
CWD=$(jq -r '.cwd // empty' <<< "$INPUT")

[ -z "$FILE_PATH" ] && exit 0

# If cwd is inside a worktree, allow edits to that same worktree's files.
if [[ "$CWD" == *"/.claude/worktrees/"* ]]; then
  wt_prefix="${CWD%%/.claude/worktrees/*}"
  wt_after="${CWD#*/.claude/worktrees/}"
  wt_root="$wt_prefix/.claude/worktrees/${wt_after%%/*}"
  case "$FILE_PATH" in
    "$wt_root" | "$wt_root"/*) exit 0 ;;
  esac
fi

case "/$FILE_PATH/" in
  */node_modules/*)
    echo "doc-audit: blocked tool call targeting node_modules path: $FILE_PATH" >&2
    exit 2
    ;;
  */.claude/worktrees/*)
    echo "doc-audit: blocked tool call targeting .claude/worktrees path: $FILE_PATH" >&2
    exit 2
    ;;
esac

exit 0
