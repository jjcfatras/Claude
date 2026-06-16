---
description: Commit changes and push to the current branch's origin; refresh an open PR's description if one exists
allowed-tools: Bash(git add:*), Bash(git status:*), Bash(git diff:*), Bash(git log:*), Bash(git branch:*), Bash(git checkout:*), Bash(git switch:*), Bash(git rev-parse:*), Bash(git symbolic-ref:*), Bash(git push:*), Bash(git commit:*), Bash(gh pr view:*), Bash(gh pr edit:*), Bash(mktemp:*), AskUserQuestion
model: sonnet
effort: low
---

## Context

- Current git status: !`git status`
- Current git diff (staged and unstaged changes): !`git diff HEAD`
- Current branch: !`git branch --show-current`
- Recent commits: !`git log --oneline -10`

## Your task

Commit the changes above and push them to the current branch's `origin`.

1. **Determine the default branch.** Run `git symbolic-ref --short refs/remotes/origin/HEAD` and strip the `origin/` prefix. If that fails (no `origin/HEAD`), treat `main`, then `master`, as the default.

2. **Protected-branch guard.** Treat the current branch as _protected_ if its name contains any of these case-sensitive substrings: `dev`, `stage`, `qa`, `prod`, `main`, `release`. If the current branch is protected, do **not** silently commit onto it. Call the **AskUserQuestion** tool (`multiSelect: false`) with header `Branch`, a question naming the protected branch (e.g. "You're on protected branch `develop`. Commit here or create a new branch?"), and two options:
   - `Create new branch` (Recommended) — "Derive a short kebab-case branch name from the nature of the changes (e.g. `fix-auth-token-expiry`) and run `git switch -c <name>` before committing."
   - `Use current branch` — "Commit directly onto the protected branch."

   On `Create new branch` (or an equivalent answer), create the branch and report the name you created. On `Use current branch`, stay on the protected branch. If the current branch is **not** protected, stay on it **without prompting**.

3. **Commit.** Review the diff and decide which files belong in the commit. **Never stage files that look like secrets** — `.env`, `.env.*`, `credentials.json`, `*.pem`, `id_rsa`, `*.key`, or anything holding tokens/passwords. Draft a message matching the style of the recent commits above, append the trailer below as its final line, then stage the relevant files and create a single commit:

   ```
   Co-Authored-By: Claude <noreply@anthropic.com>
   ```

4. **Push.** If the branch already has an upstream, run `git push`. If it does not (you just created it, or it has never been pushed), run `git push -u origin HEAD` to set the upstream. If the push is rejected (non-fast-forward), **stop and report** — do not force-push.

5. **Refresh an open PR (if one exists).** Check whether the current branch has an open PR: run `gh pr view --json number,state,url,body` (with no argument it targets the current branch). If the command errors or `state` is not `OPEN`, there is no open PR — skip to the final report and do **not** create one. If an open PR is found:
   - Inspect **all** commits on the branch relative to the default branch (`git log <default>..HEAD --oneline`), not just the latest, so the description reflects the whole branch as it now stands.
   - Take the existing `body` returned above and regenerate **only** the `## Summary` section to match the current branch state. Preserve every other part of the body verbatim — the `## Test plan` section, any manually-added sections or notes, and the `🤖 Generated with [Claude Code](https://claude.com/claude-code)` footer. If the body has no `## Summary` heading, add one at the top without disturbing the rest.
   - Write the updated body to a temp file (`mktemp`) and apply it with `gh pr edit --body-file <file>` — avoid inline `--body` so multi-line markdown isn't mangled by shell quoting.

6. Report the final branch name, the remote it was pushed to, and — if a PR was refreshed — its URL. If no open PR existed, say so.
