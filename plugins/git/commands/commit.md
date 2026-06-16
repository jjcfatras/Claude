---
description: Create a git commit with an auto-generated message matching the repo's style
allowed-tools: Bash(git add:*), Bash(git status:*), Bash(git diff:*), Bash(git log:*), Bash(git branch:*), Bash(git switch:*), Bash(git commit:*), AskUserQuestion
model: sonnet
effort: low
---

## Context

- Current git status: !`git status`
- Current git diff (staged and unstaged changes): !`git diff HEAD`
- Current branch: !`git branch --show-current`
- Recent commits: !`git log --oneline -10`

## Your task

Based on the changes above, create a single git commit.

1. **Protected-branch guard.** Treat the current branch as _protected_ if its name contains any of these case-sensitive substrings: `dev`, `stage`, `qa`, `prod`, `main`, `release`. If the current branch is protected, do **not** silently commit onto it. Call the **AskUserQuestion** tool (`multiSelect: false`) with header `Branch`, a question naming the protected branch (e.g. "You're on protected branch `develop`. Commit here or create a new branch?"), and two options:
   - `Create new branch` (Recommended) — "Derive a short kebab-case branch name from the nature of the changes (e.g. `fix-auth-token-expiry`) and run `git switch -c <name>` before committing."
   - `Use current branch` — "Commit directly onto the protected branch."

   On `Create new branch` (or an equivalent answer), create the branch and report the name you created. On `Use current branch`, stay on the protected branch. If the current branch is **not** protected, stay on it **without prompting**.

2. Review the diff and decide which files belong in this commit. **Never stage files that look like secrets** — `.env`, `.env.*`, `credentials.json`, `*.pem`, `id_rsa`, `*.key`, or anything holding tokens/passwords. If the only changes are such files, stop and tell the user instead of committing.
3. Draft a commit message that matches the style of the recent commits above (e.g. Conventional Commits `type(scope): subject` if the repo uses it, otherwise a plain imperative subject). Append this trailer as the final line:

   ```
   Co-Authored-By: Claude <noreply@anthropic.com>
   ```

4. Stage the relevant files and create the commit.

Aside from the branch guard's AskUserQuestion prompt, do not use any other tools or send any other messages: once you are on the right branch, stage and commit in a single message.
