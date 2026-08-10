---
name: pr
description: Open a pull request for the current branch's already-pushed commits. Does not commit or push — use commit-push-pr for that.
when_to_use: Use when the user asks to "open a PR", "create a pull request", or "PR this" for work that is already committed and pushed.
allowed-tools: Bash(git status:*), Bash(git log:*), Bash(git branch:*), Bash(git rev-parse:*), Bash(git symbolic-ref:*), Bash(gh pr view:*), Bash(gh pr create:*), mcp__claude_ai_Atlassian_Rovo__getAccessibleAtlassianResources
model: sonnet
effort: low
---

## Context

- Current git status: !`git status`
- Current branch: !`git branch --show-current`
- Recent commits: !`git log --oneline -10`

## Your task

Open a pull request for the current branch. Requires the GitHub CLI (`gh`) installed and authenticated, and an `origin` remote. Do **not** commit and do **not** push — if there are uncommitted changes, leave them alone and mention them in the final report.

1. **Determine the default branch.** Run `git symbolic-ref --short refs/remotes/origin/HEAD` and strip the `origin/` prefix. If that fails, treat `main`, then `master`, as the default.

2. **Guard: not on the default branch.** If the current branch _is_ the default branch, stop and report — there is no branch to open a PR from.

3. **Guard: branch is pushed and up to date.** The branch must have an upstream and no unpushed commits. Check the upstream with `git rev-parse --abbrev-ref --symbolic-full-name @{u}`; the status above reports any commits ahead of it. If there is no upstream, or the branch is ahead of its upstream, **stop and report** — tell the user to run `/git:push` first. Do not push.

4. **Guard: no open PR already.** Run `gh pr view --json number,state,url` (with no argument it targets the current branch). If it returns a PR whose `state` is `OPEN`, report that URL and stop — do not create a second PR and do not modify the existing one. To refresh an existing PR's description, `/git:push` does that.

5. **Look for a JIRA key in the branch name.** Scan the current branch name for keys matching `[A-Z][A-Z0-9]+-[0-9]+` — **uppercase only** — and dedupe them, preserving the order they appear. So `fix/BAU-1892-request-log-retention` yields `BAU-1892`, while `fix/issue-42` and `chore/v2-1` yield nothing. This is intentionally strict: a fabricated ticket link is worse than no link.
   - **No match** → skip straight to step 6 and leave the body as it was. Do **not** ask the user for a key.
   - **Match** → call `getAccessibleAtlassianResources` to get the Atlassian site `url`. If exactly one site comes back, use it. If more than one does, use the first and name the site you picked in the final report so a wrong guess is visible and easy to correct. If the call fails or returns nothing, omit the ticket section and say so in the final report — **never block the PR on this step.**
   - Build one link per key as `<site-url>/browse/<KEY>`.

6. **Open the PR.** Inspect **all** commits on this branch relative to the default branch (`git log <default>..HEAD --oneline`), not just the latest, so the description covers the whole branch. Create the PR with `gh pr create`, writing a body of the form:

   ```
   ## Ticket(s)

   - <ticket link, one bullet per key from step 5>

   ## Summary
   - <1-3 bullets describing what changed across the branch>

   ## Test plan
   - [ ] <how to verify>

   🤖 Generated with [Claude Code](https://claude.com/claude-code)
   ```

   Omit the `## Ticket(s)` section entirely — heading included — when step 5 produced no links.

7. Report the PR URL returned by `gh pr create`, which ticket keys were linked (or that the branch name contained none), and whether uncommitted changes were left behind.
