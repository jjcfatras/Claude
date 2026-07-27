---
name: push
description: Push already-committed work to the current branch's origin; refresh an open PR's description if one exists. Does not commit — use commit-push for that.
when_to_use: Use when the user asks to "push", "just push", "push without committing", or wants the remote updated with existing commits.
allowed-tools: Bash(git status:*), Bash(git log:*), Bash(git branch:*), Bash(git rev-parse:*), Bash(git symbolic-ref:*), Bash(git push:*), Bash(gh pr view:*), Bash(gh pr edit:*), Bash(mktemp:*)
model: sonnet
effort: low
---

## Context

- Current git status: !`git status`
- Current branch: !`git branch --show-current`
- Recent commits: !`git log --oneline -10`

## Your task

Push the current branch's existing commits to `origin`. Do **not** create a commit — if there are uncommitted changes, leave them alone and mention them in the final report.

1. **Determine the default branch.** Run `git symbolic-ref --short refs/remotes/origin/HEAD` and strip the `origin/` prefix. If that fails (no `origin/HEAD`), treat `main`, then `master`, as the default.

2. **Push.** If the branch already has an upstream, run `git push`. If it does not (it has never been pushed), run `git push -u origin HEAD` to set the upstream. If the status above shows the branch is already up to date with its upstream and there is nothing to push, report that and stop — skip the PR refresh. If the push is rejected (non-fast-forward), **stop and report** — do not force-push.

3. **Refresh an open PR (if one exists).** Check whether the current branch has an open PR: run `gh pr view --json number,state,url,body` (with no argument it targets the current branch). If the command errors or `state` is not `OPEN`, there is no open PR — skip to the final report and do **not** create one. If an open PR is found:
   - Inspect **all** commits on the branch relative to the default branch (`git log <default>..HEAD --oneline`), not just the latest, so the description reflects the whole branch as it now stands.
   - Take the existing `body` returned above and regenerate **only** the `## Summary` section to match the current branch state. Preserve every other part of the body verbatim — the `## Test plan` section, any manually-added sections or notes, and the `🤖 Generated with [Claude Code](https://claude.com/claude-code)` footer. If the body has no `## Summary` heading, add one at the top without disturbing the rest.
   - Write the updated body to a temp file (`mktemp`) and apply it with `gh pr edit --body-file <file>` — avoid inline `--body` so multi-line markdown isn't mangled by shell quoting.

4. Report the branch name, the remote it was pushed to, whether uncommitted changes were left behind, and — if a PR was refreshed — its URL. If no open PR existed, say so.
