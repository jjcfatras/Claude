---
description: Respond to inline code review comments — dismiss false positives and preexisting issues, fix valid ones
argument-hint: <pr-number> [comment-id]
allowed-tools: Bash(git *), Bash(gh *), Bash(jq *), Bash(mktemp *), Bash(base64 *), Bash(wc *), Bash(ls *), Read, Edit, Write, Grep, Glob
model: opus
effort: high
---

Respond to inline review comments on a pull request. For each comment, determine whether the flagged issue is (1) a false positive, (2) preexisting code not introduced by this PR, or (3) a valid issue. Dismiss cases 1 and 2 with an explanation reply. For case 3, implement the fix and reply confirming the change.

**Shell Command Safety:** All bash commands — yours and agents' — must follow the rules in `.claude/references/shell-safety.md`. The condensed version is included in every agent preamble below.

Follow these steps precisely:

## Step 0: Setup and fetch PR metadata

1. Run `mktemp -d /tmp/review-respond-XXXXXX` to create a temp directory. Store the path as `$TMPDIR`.
2. Parse the PR number from `$ARGUMENTS`. If a second argument is present, treat it as a specific comment ID to process.
3. Run `gh repo view --json owner,name` to get the repository owner and name.
4. Run `gh api user` and extract the `login` field — this is the current user.
5. Run `gh pr view $PR_NUMBER --json headRefName,headRefOid,baseRefName,baseRefOid` to get branch info and commit SHAs.
6. Run `gh pr diff $PR_NUMBER` and save the output to `$TMPDIR/pr.diff` using the Write tool.
7. Parse the diff to build a map of changed files and their hunk ranges (same as in code-review: `file path → list of [newStart, newStart+newCount-1]` ranges), and whether each line is added (`+`) or context (` `).

## Step 1: Fetch and filter review comments

1. Fetch all review comments: `gh api --paginate repos/OWNER/REPO/pulls/NUMBER/comments`
2. Save the raw JSON to `$TMPDIR/all-comments.json` using the Write tool.
3. Filter to **actionable comments** by excluding:
   - Comments where `in_reply_to_id` is not null (these are replies, not top-level comments)
   - Comments authored by the current user (`user.login` matches)
   - If a specific comment ID was provided in step 0, filter to only that comment
4. For each remaining comment, check if the current user has already replied by scanning all comments for entries where `in_reply_to_id` matches this comment's `id` AND `user.login` matches the current user. Exclude comments that already have a reply from us.
5. Save the filtered list to `$TMPDIR/pending-comments.json`.
6. Display the count of pending comments. If zero, report "No unaddressed review comments found" and stop.

## Step 2: Analyze each comment

For each pending comment, perform the following analysis. If there are 3 or fewer comments, analyze sequentially. If there are more than 3, launch parallel agents (up to 5) to analyze batches.

**CRITICAL — when launching parallel agents in this step, every agent prompt MUST begin with this exact text block:**

> You are a review-response analysis agent. FORBIDDEN: Never use `sed`, `awk`, `du`, or `grep` as Bash commands — they are not in the allowed tools and will trigger permission prompts that block the workflow. Use the Read tool to read files, the Grep tool to search content, and `jq`/`gh api --jq` for JSON processing. No `#` comments in bash commands. No heredocs. No multi-line bash commands. No `jq -f`/`--rawfile`/`--slurpfile`. No `$()` command substitution. No curly braces with quotes in the same command — pipe to `jq` instead of `--jq` when URLs contain braces. No output redirection (`>`, `>>`) — use the Write tool. No adjacent quote characters (e.g., `'"`, `"'`) at word start — simplify quoting or use the Write tool. No ANSI-C quoting (`$'...'`) — never place `$` immediately before a single quote. Do NOT post anything to GitHub — only the main skill posts replies in Step 5.

For each comment, extract: `id`, `path`, `line`, `original_line`, `diff_hunk`, `body`, `user.login`.

### Check 1 — Is this preexisting?

Determine whether the commented code was introduced by this PR or existed before:

1. **Parse the `diff_hunk`**: Look at the last line(s) of the `diff_hunk` (which correspond to the commented line). If the line starts with ` ` (space, context line) rather than `+` (added line), the code is **preexisting** — it was not added or modified by this PR.
2. **Confirm with git blame**: Run `git blame -L LINE,LINE -- PATH` on the file. Check the commit SHA — if it is NOT the HEAD commit of the PR branch and NOT any commit in the PR, the code predates this PR.
3. If confirmed preexisting → categorize as **preexisting** and record the blame commit SHA and date.

### Check 2 — Is this a false positive?

If the code IS from this PR (not preexisting), analyze whether the reviewer's concern is valid:

1. **Read the full file** at the commented path to understand context (surrounding functions, imports, types, error handling).
2. **Understand the reviewer's concern** by carefully parsing their comment body.
3. **Evaluate the concern** against the actual code:
   - Does the code already handle the scenario the reviewer is worried about?
   - Is there handling elsewhere in the codebase (e.g., middleware, base class, framework feature) that addresses the concern?
   - Is the reviewer misreading the code or missing context?
   - Is the concern about style/preference rather than correctness?
   - Would the suggested change actually be incorrect or unnecessary given the types/constraints?
4. Use Grep to search for relevant patterns if needed (e.g., if the reviewer asks about error handling, search for try/catch or error middleware in related files).
5. If you can **definitively** demonstrate the concern is unfounded with concrete evidence → categorize as **false positive** and record the evidence.
6. If there is any reasonable chance the reviewer is correct → categorize as **valid**.

**Important**: Err on the side of treating issues as valid. Only categorize as false positive when you have clear, concrete evidence. When in doubt, it is a valid issue.

### Output per comment

For each comment, produce:

- **Comment ID** and **reviewer** (`user.login`)
- **File:line** reference
- **Reviewer's comment** (first 200 chars)
- **Verdict**: `preexisting`, `false-positive`, or `valid`
- **Reasoning**: 2-3 sentences explaining why
- **Evidence**: For preexisting — blame info. For false positive — the specific code/pattern that addresses the concern. For valid — brief description of what needs to change.

## Step 3: Present triage results to user

Display a formatted summary of all comments and their verdicts:

```
## Review Comment Triage — PR #NUMBER

### Preexisting (N)
- **file.ts:42** (@reviewer): "Their comment..." → Code predates PR (blame: abc1234, 2025-01-15)

### False Positives (N)
- **file.ts:88** (@reviewer): "Their comment..." → Already handled by XYZ middleware

### Valid Issues (N)
- **file.ts:120** (@reviewer): "Their comment..." → Plan: add null check before accessing .property
```

Ask the user to confirm before proceeding. The user may:

- Approve all verdicts
- Override specific verdicts (e.g., "treat #3 as valid" or "skip #5")
- Cancel entirely

Wait for confirmation before continuing.

## Step 4: Implement fixes for valid issues

For each comment triaged as **valid** (and approved by the user):

1. Read the file at the commented path.
2. Understand the reviewer's suggestion and the surrounding code context.
3. Plan the minimal, targeted fix that addresses the reviewer's concern without unnecessary refactoring.
4. Apply the fix using the Edit tool.
5. After editing, re-read the modified section to verify the fix is correct and doesn't break surrounding code.
6. Record what was changed for the reply in step 5.

If multiple valid issues exist in the same file, apply them carefully to avoid conflicts. Process them from bottom-to-top (highest line number first) so line numbers remain stable.

## Step 5: Post replies to all comments

For each analyzed comment, compose and post a reply.

### Reply templates

**For preexisting issues:**

```
This code predates this PR — it was introduced in commit `COMMIT_SHA` (DATE). The change in this PR doesn't modify this line.

If you'd like this addressed, I can create a follow-up issue to track it separately.
```

**For false positives:**

```
This is actually already handled — SPECIFIC_EVIDENCE.

BRIEF_EXPLANATION of why the current code is correct.
```

**For valid issues (fixed):**

```
Good catch! Fixed — DESCRIPTION_OF_CHANGE.

BRIEF_EXPLANATION of what was changed and why it addresses the concern.
```

### Posting process

For each reply:

1. Write the reply body to a temp file using the Write tool (e.g., `$TMPDIR/reply-COMMENT_ID.md`).
2. Post using: `gh api repos/OWNER/REPO/pulls/NUMBER/comments/COMMENT_ID/replies -F body=@$TMPDIR/reply-COMMENT_ID.md`
3. Verify the response includes a valid `id` field confirming the reply was created.

After all replies are posted, display a summary:

```
## Done — PR #NUMBER

- Replied to N comments (X preexisting, Y false positives, Z fixes applied)
- Files modified: list of files changed
```
