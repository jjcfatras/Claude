---
description: Commit, push, and open a pull request
allowed-tools: Bash(git add:*), Bash(git status:*), Bash(git diff:*), Bash(git log:*), Bash(git branch:*), Bash(git checkout:*), Bash(git switch:*), Bash(git rev-parse:*), Bash(git symbolic-ref:*), Bash(git push:*), Bash(git commit:*), Bash(gh pr create:*), Bash(gh pr view:*), AskUserQuestion
model: sonnet
effort: low
---

## Context

- Current git status: !`git status`
- Current git diff (staged and unstaged changes): !`git diff HEAD`
- Current branch: !`git branch --show-current`
- Recent commits: !`git log --oneline -10`

## Your task

Commit the changes above, push them, and open a pull request. Requires the GitHub CLI (`gh`) to be installed and authenticated, and an `origin` remote.

1. **Determine the default branch.** Run `git symbolic-ref --short refs/remotes/origin/HEAD` and strip the `origin/` prefix. If that fails, treat `main`, then `master`, as the default.

2. **Protected-branch guard.** Treat the current branch as _protected_ if its name contains any of these case-sensitive substrings: `dev`, `stage`, `qa`, `prod`, `main`, `release`. If protected, do **not** silently commit onto it. Call the **AskUserQuestion** tool (`multiSelect: false`) with header `Branch`, a question naming the protected branch (e.g. "You're on protected branch `develop`. Commit here or create a new branch?"), and two options:
   - `Create new branch` (Recommended) — "Derive a short kebab-case branch name from the changes and run `git switch -c <name>` before committing."
   - `Use current branch` — "Commit directly onto the protected branch."

   On `Create new branch`, create and report the branch. On `Use current branch`, stay. If the current branch is **not** protected, stay **without prompting**.

3. **Commit.** Decide which files belong in the commit. **Never stage files that look like secrets** (`.env`, `.env.*`, `credentials.json`, `*.pem`, `id_rsa`, `*.key`, token/password files). Draft a message matching the recent commit style and append this trailer as its final line, then stage and create a single commit:

   ```
   Co-Authored-By: Claude <noreply@anthropic.com>
   ```

4. **Push.** `git push -u origin HEAD` if the branch has no upstream, otherwise `git push`. If rejected (non-fast-forward), stop and report — do not force-push.

5. **Open the PR.** Inspect **all** commits on this branch relative to the default branch (`git log <default>..HEAD --oneline`), not just the latest, so the description covers the whole branch. Create the PR with `gh pr create`, writing a body of the form:

   ```
   ## Summary
   - <1-3 bullets describing what changed across the branch>

   ## Test plan
   - [ ] <how to verify>

   🤖 Generated with [Claude Code](https://claude.com/claude-code)
   ```

6. Report the PR URL returned by `gh pr create`.
