---
description: Create a git commit with an auto-generated message matching the repo's style
allowed-tools: Bash(git add:*), Bash(git status:*), Bash(git diff:*), Bash(git log:*), Bash(git branch:*), Bash(git commit:*)
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

1. Review the diff and decide which files belong in this commit. **Never stage files that look like secrets** — `.env`, `.env.*`, `credentials.json`, `*.pem`, `id_rsa`, `*.key`, or anything holding tokens/passwords. If the only changes are such files, stop and tell the user instead of committing.
2. Draft a commit message that matches the style of the recent commits above (e.g. Conventional Commits `type(scope): subject` if the repo uses it, otherwise a plain imperative subject). Append this trailer as the final line:

   ```
   Co-Authored-By: Claude <noreply@anthropic.com>
   ```

3. Stage the relevant files and create the commit.

You have the capability to call multiple tools in a single response. Stage and commit in a single message. Do not use any other tools or do anything else. Do not send any other text or messages besides these tool calls.
