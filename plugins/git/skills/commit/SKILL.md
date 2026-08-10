---
name: commit
description: Create a git commit with an auto-generated message matching the repo's style. Commits only — does not push.
when_to_use: Use when the user asks to "commit", "commit this", "make a commit", or wants current changes committed with a well-formed message. Pass `--staged` when the user asks to "commit staged", "commit only what I staged", "commit the index", or otherwise wants only the already-staged changes committed.
argument-hint: [--staged]
allowed-tools: Bash(git add:*), Bash(git status:*), Bash(git diff:*), Bash(git log:*), Bash(git branch:*), Bash(git switch:*), Bash(git commit:*), AskUserQuestion
model: sonnet
effort: low
---

## Context

- Current git status: !`git status`
- Current git diff (staged and unstaged changes): !`git diff HEAD`
- Staged changes only: !`git diff --cached`
- Current branch: !`git branch --show-current`
- Recent commits: !`git log --oneline -10`

## Your task

Based on the changes above, create a single git commit.

**Mode.** If `$ARGUMENTS` contains `--staged`, run in **staged-only** mode: commit exactly what is already in the index — never run `git add`, and use the **Staged changes only** diff above (not the full diff) to decide the message. If that diff is empty, stop and tell the user nothing is staged instead of committing the working tree. Otherwise run in the default mode, where you choose the files yourself.

1. **Protected-branch guard.** Treat the current branch as _protected_ if its name contains any of these case-sensitive substrings: `dev`, `stage`, `qa`, `prod`, `main`, `release`. If the current branch is protected, do **not** silently commit onto it. Call the **AskUserQuestion** tool (`multiSelect: false`) with header `Branch`, a question naming the protected branch (e.g. "You're on protected branch `develop`. Commit here or create a new branch?"), and two options:
   - `Create new branch` (Recommended) — "Derive a short kebab-case branch name from the nature of the changes (e.g. `fix-auth-token-expiry`) and run `git switch -c <name>` before committing."
   - `Use current branch` — "Commit directly onto the protected branch."

   On `Create new branch` (or an equivalent answer), create the branch and report the name you created. On `Use current branch`, stay on the protected branch. If the current branch is **not** protected, stay on it **without prompting**.

2. Review the diff and decide which files belong in this commit. **Never stage files that look like secrets** — `.env`, `.env.*`, `credentials.json`, `*.pem`, `id_rsa`, `*.key`, or anything holding tokens/passwords. If the only changes are such files, stop and tell the user instead of committing. In staged-only mode there is nothing to decide — the index _is_ the selection — but still apply the secrets check: if the staged set contains such a file, stop and report it rather than committing. Do not `git reset` or otherwise alter the user's index.
3. Draft a commit message that matches the style of the recent commits above (e.g. Conventional Commits `type(scope): subject` if the repo uses it, otherwise a plain imperative subject). Append this trailer as the final line:

   ```
   Co-Authored-By: Claude <noreply@anthropic.com>
   ```

4. Stage the relevant files and create the commit. In staged-only mode, skip staging entirely and commit with a bare `git commit -m …` (no `git add`, no `-a`) so the unstaged changes stay unstaged.

Aside from the branch guard's AskUserQuestion prompt, do not use any other tools or send any other messages: once you are on the right branch, stage (unless `--staged`) and commit in a single message.
