---
name: revise
description: Capture learnings from the current session into the project's memory files (CLAUDE.md, CLAUDE.local.md, .claude/rules/). Reflects on commands used, conventions followed, testing approaches, environment quirks and gotchas hit this session, drops what's already documented, obvious or one-off, and proposes one-line additions as diffs per target file. Edits only what the user approves. For facts from a codebase scan use /docs:enrich-claude-md; for stale claims, /docs:audit-docs.
allowed-tools: Bash(git rev-parse:*), Bash(find:*), Bash(rg:*), Read, Edit, Write, AskUserQuestion
disable-model-invocation: true
---

Review this session for learnings about working in this codebase, and propose additions to the project's memory files that would make future Claude sessions more effective. The input is **the conversation so far**, not the codebase. To scan the codebase for undocumented facts, use `/docs:enrich-claude-md` instead.

Do not modify any file until the user approves a specific addition in Step 5.

## Step 1: Reflect

What context was missing that would have helped this session? List candidates from:

- Bash commands that were used or discovered
- Code style patterns followed
- Testing approaches that worked
- Environment/configuration quirks
- Warnings or gotchas encountered, including hooks or tools that blocked an action
- Corrections the user made to your approach

For each candidate, note its evidence from the session: the command and its output, the error text, or the user's words. If the session produced nothing durable, say so and stop.

## Step 2: Find memory files

Run `git rev-parse --show-toplevel` to get `$ROOT`. Outside a git repo, use the current working directory. Then find the memory files with one `find`:

```bash
find "$ROOT" \( -name node_modules -o -name .git -o -name dist -o -name build -o -name vendor -o -name .next -o -name target -o -name out -o -name coverage -o -path '*/.claude/worktrees' \) -prune -o \
  -type f \( -name 'CLAUDE.md' -o -name 'CLAUDE.local.md' -o -path '*/.claude/rules/*.md' \) -print
```

Read each match in full, so you never propose something already documented. For each file, note its sections and style (table or prose, heading depth, terseness).

## Step 3: Draft and filter

Keep it concise: one line per concept. Memory files are part of every prompt, so brevity matters.

Format: `<command or pattern>` - `<brief description>`. Where the target file uses another style, match it instead.

Drop a candidate when any of these holds:

- It is already stated, even loosely, in a file from Step 2.
- It is obvious from a glance at the code or filenames.
- It is generic best practice the model already knows.
- It is a one-off fix unlikely to recur.
- It can't be said in a line or two.

Route each survivor to one file:

| Target                 | What goes there                                                      |
| ---------------------- | -------------------------------------------------------------------- |
| `CLAUDE.md`            | Team-shared facts (checked into git)                                 |
| `CLAUDE.local.md`      | Personal or machine-local facts (gitignored)                         |
| `.claude/rules/<x>.md` | A topic an existing rules file already covers                        |
| `<subdir>/CLAUDE.md`   | A fact specific to one subtree that already has a nested `CLAUDE.md` |

A committed `CLAUDE.md` must never reference `CLAUDE.local.md`. That file is gitignored, so the pointer dangles for teammates. The user-global `~/.claude/CLAUDE.md` and auto-memory files are out of scope.

## Step 4: Show proposed changes

For each addition:

````markdown
### Update: ./CLAUDE.md › <section>

**Why:** <one-line reason, citing the session evidence>

```diff
+ <the addition - keep it brief>
```
````

If nothing survived Step 3, say so plainly and stop.

## Step 5: Apply with approval

Offer each addition through **AskUserQuestion**. Batch up to four single-select questions per call, one per addition, each with a short header naming the target (e.g. `CLAUDE.md › Commands`) and two options: `Add` ("Insert this line via Edit") and `Skip` ("Don't add it").

Apply each approved addition with `Edit`, in the section named in Step 4. Use `Write` only when an approved addition's target doesn't exist yet (typically `CLAUDE.local.md`), and confirm the path first. Edit only files the user approved.

After the last decision, summarize what was added and what was skipped.

## Operating notes

- **Search with `rg` through Bash.** The `Grep`/`Glob` tools are removed on native macOS/Linux builds. `rg` exits **1** on no match, so guard every search that may find nothing with `|| true`, or one empty match fails the whole `Bash` call.
- **Missed additions cost nothing; bad ones erode trust** in the memory file. When unsure whether a line earns its place, drop it.
