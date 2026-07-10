---
description: Propose lossless distillation of verbose prose — inline text, recently modified prose files, or the whole project when no scope is given — and apply on approval
argument-hint: "[text|path|--staged|--since=<ref>]"
allowed-tools: Bash(git *), Read, Edit, Grep, Glob, AskUserQuestion
model: opus
effort: medium
---

Distill overly verbose prose to its concentrated essence. Lossless compression is the absolute hard constraint — every fact, qualification, and nuance in the original survives. Show diffs first, apply only on user approval.

This command is the prose counterpart of `/simplify`: the same propose-then-apply contract, applied to natural language instead of code.

## Step 0: Determine scope

Classify `$1` first:

1. **`$1` is inline prose** (multi-word text that is not a flag and does not resolve to an existing path or glob): skip the file workflow. Load the rubric (Step 1), distill the text, print the distilled version followed by the stats block from Step 5. Done.
2. **`$1` is `--staged`**: run `git diff --name-only --cached` and use that file set.
3. **`$1` starts with `--since=`**: extract the ref `R`, then use the union of `git diff --name-only R...HEAD` and `git diff --name-only` (working tree). This catches both committed-since-R and uncommitted changes.
4. **`$1` is any other path or glob**: if it is a directory, walk it via `Glob`. If it is a glob, expand it.
5. **`$1` is empty**: audit the whole project. Use the union of `git ls-files` and `git ls-files --others --exclude-standard` (tracked plus untracked-but-not-ignored files); the prose-file filter below narrows the set.

For file scopes (cases 2–5), keep only prose files: `.md`, `.mdx`, `.markdown`, `.txt`, `.rst`. Drop generated files (`dist/`, `bin/`, `node_modules/`, lockfiles), binary files, and files whose only changes are whitespace or formatter churn.

If no candidates remain, stop and tell the user "no candidate files in scope" — do not invent work.

Otherwise list the candidate files, then call the **AskUserQuestion** tool (`multiSelect: false`, header `Proceed`) — one prompt for the whole set, not per file. The question names the count (e.g. "Distill 3 candidate file(s)?") with two options:

- `Proceed` — "Analyze the listed files for distillation."
- `Cancel` — "Stop without analyzing anything."

If the user selects `Cancel` (or answers with their own equivalent), stop without reading any file.

## Step 1: Load standards

Before reading any candidate file:

1. Read `${CLAUDE_PLUGIN_ROOT}/references/standards.md` for the distillation rubric — the hierarchy of cuts and preservation rules.
2. Read the repo root `CLAUDE.md` if present, and for each candidate the nearest non-root `CLAUDE.md` ancestor. Project writing conventions override the rubric's defaults.

Carry these standards as the active rubric for the rest of the run.

## Step 2: Analyze each file

For every candidate file:

1. Read the file with `Read`.
2. Identify distillation candidates per the active rubric, applying the hierarchy of cuts in order.
3. For each candidate, build a hunk:
   - A unified diff fragment (3 lines of context on each side).
   - One sentence of rationale naming the cut level ("remove throat-clearing", "nominalization → verb").
4. Apply hard guardrails — drop any hunk that violates them:
   - **Lossless only.** No fact, qualification, or nuance removed. Genuine hedges ("may", "in most cases") stay; only empty hedging goes.
   - **Preserve voice, technical terms, numbers, names, references, and links.**
   - **Never touch fenced code blocks, YAML frontmatter, table data, or URLs.**
   - **No restructuring.** Section moves and merges are out of scope. No tone or register changes.
   - **No formatter-only churn.** The `PostToolUse` `prettier --write` hook handles wrapping on save. If a hunk would be reverted by the formatter, do not propose it.

If a file has no surviving hunks after guardrails, skip it silently.

## Step 3: Present diffs

For each file that has surviving hunks, in alphabetical order:

1. Print the file path as a header.
2. Print each hunk's unified diff followed by its one-sentence rationale.
3. Call the **AskUserQuestion** tool (`multiSelect: false`, header `Hunks`) with a question naming the file and its surviving-hunk count (e.g. "`docs/intro.md` — 3 hunk(s). How should I apply them?") and four options:
   - **`Apply all`** — every hunk in this file is approved.
   - **`Apply some`** — only some hunks; follow up (free-text) for which hunk numbers.
   - **`Skip file`** — drop every hunk for this file.
   - **`Edit and apply`** — the user edits before applying; follow up (free-text) for the edited new-side text.

   Act on the selected option (or the user's Other-field equivalent). The follow-up for `Apply some` (hunk numbers like "1 3 5") and `Edit and apply` (pasted text) stays free-text — those answers are unbounded and don't fit predefined options.

Do not coalesce prompts across files — one prompt per file keeps the review focused.

## Step 4: Apply approved hunks

For each approved hunk:

1. Use the `Edit` tool with the exact `old_string` and `new_string`.
2. After editing, do not run formatters manually — the project's `PostToolUse` hook handles that. If the hook rewraps your edit, that is expected.
3. If `Edit` fails because `old_string` is not unique, expand the context until it is unique. Do not fall back to `Write`.

If a hunk fails to apply (e.g., the file changed mid-run), report it and continue with the rest.

## Step 5: Summary

Print a final summary:

- Per file (or for the inline text): `Original ~N words → Distilled ~N words (−N%)`.
- Hunks applied vs hunks declined, grouped by file.
- Any hunks that failed to apply and why.
- If any cut required interpreting ambiguous meaning, note it.
- This command never commits. The user does that.

## Notes

- Single-threaded by design. No subagents, no parallel file analysis — one careful editorial pass beats five hasty ones.
- `--auto` (skip the approval prompt) is intentionally absent. The propose-then-apply contract is the point.
- Distillation is lossless compression, not summarizing. If shortening a passage would require dropping a fact, don't propose the hunk.
