---
description: Propose targeted simplifications to recently modified code and apply on approval
argument-hint: "[path|--staged|--since=<ref>]"
allowed-tools: Bash(git *), Read, Edit, Grep, Glob
model: opus
effort: xhigh
---

Propose targeted simplifications to recently modified code. Behavior preservation is the absolute hard constraint — show diffs first, apply only on user approval.

This command recreates the pre-2.1.147 `/simplify` workflow as a propose-then-apply pass.

## Step 0: Determine scope

Pick the file set in this order:

1. **`$1` is `--staged`**: run `git diff --name-only --cached` and use that file set.
2. **`$1` starts with `--since=`**: extract the ref `R`, then use the union of `git diff --name-only R...HEAD` and `git diff --name-only` (working tree). This catches both committed-since-R and uncommitted changes.
3. **`$1` is any other non-empty value**: treat it as a path or glob. If it is a directory, walk it via `Glob`. If it is a glob, expand it.
4. **`$1` is empty**: use the union of (a) files modified in the current session and (b) the output of `git status --porcelain` (working tree + staged, excluding untracked unless they are clearly source files).

After collecting candidates, drop anything that is obviously out of scope:

- Generated files (e.g., `dist/`, `bin/`, `node_modules/`, lockfiles, `.min.*`).
- Binary files.
- Files whose only changes are whitespace or formatter churn — the project formatter hook handles those.

If no candidates remain, stop and tell the user "no candidate files in scope" — do not invent work.

Otherwise list the candidate files and ask the user to confirm before continuing (one prompt for the whole set, not per file).

## Step 1: Load standards

Before reading any candidate file:

1. Read the repo root `CLAUDE.md` if present.
2. For each candidate, read the nearest `CLAUDE.md` ancestor that is not the root (project-specific conventions override defaults).
3. Read `${CLAUDE_PLUGIN_ROOT}/references/standards.md` for the four-pillar default rubric.

Carry these standards as the active rubric for the rest of the run.

## Step 2: Analyze each file

For every candidate file:

1. Read the file with `Read`.
2. Identify simplification candidates per the active rubric — see `standards.md` for what qualifies and what does not.
3. For each candidate, build a hunk:
   - A unified diff fragment (3 lines of context on each side).
   - One sentence of rationale citing the pillar it satisfies ("flatten else-if chain — enhance clarity").
4. Apply hard guardrails — drop any hunk that violates them:
   - **No behavior changes.** If you cannot prove to yourself the change is a pure refactor, drop it.
   - **No new public API and no removed public API.** Renames of exported symbols are out.
   - **No formatter-only churn.** The `PostToolUse` `prettier --write` / `gofmt -w` hook handles formatting on save. If a hunk would be reverted by the formatter, do not propose it.
   - **No comment-only edits unless removing comments that restate code.**
   - **No "while I'm here" fixes** — bug fixes, dead-code removal beyond what is in scope, dependency bumps, etc. all belong in separate commits.

If a file has no surviving hunks after guardrails, skip it silently.

## Step 3: Present diffs

For each file that has surviving hunks, in alphabetical order:

1. Print the file path as a header.
2. Print each hunk's unified diff followed by its one-sentence rationale.
3. Ask the user to choose: `apply all` / `apply some` / `skip file` / `edit and apply`.
   - **`apply all`**: every hunk in this file is approved.
   - **`apply some`**: ask which hunk numbers to apply.
   - **`skip file`**: drop every hunk for this file.
   - **`edit and apply`**: let the user paste back an edited version of any hunk's new-side text.

Do not coalesce prompts across files — one prompt per file keeps the review focused.

## Step 4: Apply approved hunks

For each approved hunk:

1. Use the `Edit` tool with the exact `old_string` and `new_string`. Match the existing indentation byte-for-byte.
2. After editing, do not run formatters manually — the project's `PostToolUse` hook handles that. If the hook reformats your edit, that is expected.
3. If `Edit` fails because `old_string` is not unique, expand the context until it is unique. Do not fall back to `Write`.

If a hunk fails to apply (e.g., the file changed mid-run), report it and continue with the rest.

## Step 5: Summary

Print a final summary:

- Files touched.
- Hunks applied vs hunks declined, grouped by file.
- Any hunks that failed to apply and why.
- A single-line reminder to run the project's test suite before committing — this command does not run tests.

## Notes

- Single-thread by design. No subagents, no parallel file analysis. Refactoring without behavior changes is the kind of task where one careful pass beats five hasty ones.
- `--auto` (skip the approval prompt) is intentionally not in this version. The propose-then-apply contract is the whole point of this command's existence after v2.1.147.
- This command never commits. The user does that.
