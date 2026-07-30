---
name: commit-push-pr
description: Commit, push, and open a pull request. Use when the user asks to "open a PR", "commit and create a pull request", or wants changes committed, pushed, and a new PR opened.
allowed-tools: Skill, Bash(git add:*), Bash(git status:*), Bash(git diff:*), Bash(git log:*), Bash(git branch:*), Bash(git checkout:*), Bash(git switch:*), Bash(git rev-parse:*), Bash(git symbolic-ref:*), Bash(git push:*), Bash(git commit:*), Bash(gh pr create:*), Bash(gh pr view:*), Bash(gh pr edit:*), Bash(mktemp:*), AskUserQuestion
model: sonnet
effort: low
---

## Your task

Commit the current changes, push them, and open a pull request. Requires the GitHub CLI (`gh`) to be installed and authenticated, and an `origin` remote.

This skill **composes** the `git:commit` and `git:push` skills rather than reimplementing them. Each injects its own git context and owns its own safety rules — the protected-branch guard and secret-file screening live in `git:commit`, the upstream handling and the no-force-push rule live in `git:push`. Do **not** duplicate that logic here.

1. **Determine the default branch.** Run `git symbolic-ref --short refs/remotes/origin/HEAD` and strip the `origin/` prefix. If that fails, treat `main`, then `master`, as the default. Step 4 needs this; `git:push` computes it independently and does not hand it back.

2. **Commit.** Invoke the **`git:commit`** skill via the `Skill` tool. It owns the protected-branch guard (including the `AskUserQuestion` prompt and any new branch it creates), secret-file screening, drafting a message in the repo's style, the `Co-Authored-By` trailer, and staging. Its closing instruction to "not use any other tools or send any other messages" scopes to the commit step only — it does **not** end this workflow. Once it reports the commit, continue to step 3. If it stops _without_ committing (e.g. the only changes were secret-looking files), stop here and report why — do not push or open a PR.

3. **Push.** Invoke the **`git:push`** skill via the `Skill` tool. It sets the upstream when the branch has none and refuses to force-push if the push is rejected. Its PR-refresh step is expected to find nothing and no-op here, since a new branch has no PR yet. If the push was rejected, stop and report — do not continue to step 4.

4. **Open the PR.** If `git:push` reported that it refreshed an **existing** open PR, this branch already has one: report that URL and stop — do not call `gh pr create`, it will fail. Otherwise inspect **all** commits on this branch relative to the default branch (`git log <default>..HEAD --oneline`), not just the latest, so the description covers the whole branch. Create the PR with `gh pr create`, writing a body of the form:

   ```
   ## Summary
   - <1-3 bullets describing what changed across the branch>

   ## Test plan
   - [ ] <how to verify>

   🤖 Generated with [Claude Code](https://claude.com/claude-code)
   ```

5. Give **one** combined report: the branch committed to, the remote pushed to, and the PR URL returned by `gh pr create`. The delegated skills' own reports are internal to this workflow — don't repeat them verbatim.
