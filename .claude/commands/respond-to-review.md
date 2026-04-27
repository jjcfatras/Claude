---
description: Respond to every flagged issue on a PR — inline comments and review-body findings alike — dismissing false positives and preexisting issues, fixing valid ones
argument-hint: <pr-number> [comment-id]
allowed-tools: Bash(git *), Bash(gh *), Bash(jq *), Bash(mktemp *), Bash(base64 *), Bash(wc *), Bash(ls *), Read, Edit, Write, Grep, Glob, mcp__*
model: opus
effort: high
---

Respond to every flagged issue on a pull request — both **inline review comments** (line-attached) and **review-body findings** (issues listed in a review's summary body that couldn't attach to a specific line). For each flagged issue, determine whether it is (1) a false positive, (2) preexisting code not introduced by this PR, or (3) a valid issue. Dismiss cases 1 and 2 with an explanation reply. For case 3, implement the fix and reply confirming the change.

**Shell Command Safety:** All bash commands — yours and agents' — must follow the rules in `~/.claude/references/shell-safety.md`. The condensed version is included in every agent preamble below.

Follow these steps precisely:

## Step 0: Setup and fetch PR metadata

1. Run `mktemp -d /tmp/review-respond-XXXXXX` to create a temp directory. Store the path as `$TMPDIR`.
2. Parse the PR number from `$ARGUMENTS`. If a second argument is present, treat it as a specific inline-comment ID to target; in that case, skip review-body processing entirely (the user is scoping to one inline thread).
3. Run `gh repo view --json owner,name` to get the repository owner and name.
4. Run `gh api user` and extract the `login` field — this is the current user.
5. Run `gh pr view $PR_NUMBER --json headRefName,headRefOid,baseRefName,baseRefOid` to get branch info and commit SHAs.
6. Run `gh pr diff $PR_NUMBER` and save the output to `$TMPDIR/pr.diff` using the Write tool.
7. Parse the diff to build a map of changed files and their hunk ranges (same as in code-review: `file path → list of [newStart, newStart+newCount-1]` ranges), and whether each line is added (`+`) or context (` `).

## Step 1: Fetch and filter flagged items

"Flagged items" covers two sources: **inline comments** (line-attached) and **review-body findings** (issues listed in a review's summary body that couldn't attach to a specific line). Both flow through the same downstream steps. Produce a unified `$TMPDIR/pending-items.json` whose entries have a common shape plus a `source_type` of `inline-comment` or `review-body-finding`.

### 1a — Inline comments

1. Fetch all review comments: `gh api --paginate repos/OWNER/REPO/pulls/NUMBER/comments`
2. Save the raw JSON to `$TMPDIR/all-comments.json` using the Write tool.
3. Filter to **actionable comments** by excluding:
   - Comments where `in_reply_to_id` is not null (these are replies, not top-level comments)
   - Comments authored by the current user (`user.login` matches)
   - If a specific comment ID was provided in step 0, filter to only that comment
4. For each remaining comment, check if the current user has already replied by scanning all comments for entries where `in_reply_to_id` matches this comment's `id` AND `user.login` matches the current user. Exclude comments that already have a reply from us.

### 1b — Reviews

If a specific inline comment ID was provided in step 0, skip 1b–1d entirely — the user is scoping the run to one thread.

1. Fetch all reviews: `gh api --paginate repos/OWNER/REPO/pulls/NUMBER/reviews`. Save the raw JSON to `$TMPDIR/reviews.json` using the Write tool.
2. Filter the reviews to entries that meet **all** of the following:
   - `user.login` is not the current user,
   - `body` is non-empty after trimming whitespace,
   - `body` is not a trivial acknowledgement — exclude bodies whose trimmed text is, case-insensitive, one of `lgtm`, `looks good`, `approved`, `:+1:`, `👍`, or is shorter than roughly 20 characters of prose (a bare "thanks!" or "great work" has no findings to triage).

### 1c — Parse review bodies into discrete findings

Review bodies are free-form markdown. Findings can appear as bullet points, numbered items, `###`-delimited subsections, or a single prose paragraph. Parse each surviving review body into a list of findings:

- For review bodies with fewer than ~4 filtered reviews total, parse inline (read the body yourself and split it into findings).
- For 4 or more, launch one Haiku agent per review in parallel, each bound by the **forbidden-bash preamble** from Step 2 (the exact text starting "You are a review-response analysis agent…"), with the per-agent role adjusted to "You are a review-body parsing agent." The agent's only job is to split a single review body into findings and return structured JSON — it must not hit the network beyond what it was given and must not post anything.

For each finding, emit:

- `id`: synthetic stable string `review-<reviewId>-finding-<N>` where N is the zero-based index within that review's body. Stability matters — the same input must produce the same IDs across runs so the dedup check in 1d works.
- `text`: the finding's markdown as written (don't paraphrase; downstream analysis needs the reviewer's original wording).
- `path`, `line`: optional. Only populate when the finding explicitly names a file (`src/foo.ts`, `` `src/foo.ts` ``) and/or a line (`line 42`, `:42`). Use a simple regex — don't guess from context.
- `reviewer`: `review.user.login`.
- `review_id`: the review's `id`.

Discard "findings" that are purely summary, approval, or praise (e.g., "Overall this looks great", "Nice refactor"). The downstream steps only handle concerns.

### 1d — Dedup review-body findings against prior replies

When the skill posts a reply for a review-body finding (Step 5), it embeds a stable HTML-comment marker of the form `<!-- respond-to-review:review-<reviewId>:finding-<N> -->`. Use this to detect previously-handled findings and skip them:

1. Fetch PR-level issue comments: `gh api --paginate repos/OWNER/REPO/issues/NUMBER/comments`. Save to `$TMPDIR/issue-comments.json` using the Write tool.
2. Filter the list to comments authored by the current user.
3. For each review-body finding from 1c, use the **Grep tool** (not the `grep` shell command — rule 4 of shell-safety) on `$TMPDIR/issue-comments.json` to check for its marker string. If the marker appears in any of our prior PR comments, drop the finding.

### 1e — Merge into pending items

Combine the filtered inline comments from 1a and the surviving review-body findings from 1c/1d into a single list. Each entry in `$TMPDIR/pending-items.json` must contain:

- `id`
- `source_type`: `inline-comment` or `review-body-finding`
- `reviewer` (`user.login` for inline, `review.user.login` for review-body)
- `body` (the comment text for inline, `text` for review-body)
- `path`, `line`: populated when available; review-body findings may have neither
- `diff_hunk`: for inline comments only
- `review_id`: for review-body findings only

Display the count of pending items. If zero, report "No unaddressed review items found" and stop.

## Step 2: Analyze each item

For each pending item in `$TMPDIR/pending-items.json`, perform the following analysis. If there are 3 or fewer items total, analyze sequentially. If there are more than 3, launch parallel agents (up to 5) to analyze batches.

**CRITICAL — when launching parallel agents in this step, every agent prompt MUST begin with this exact text block:**

> You are a review-response analysis agent. FORBIDDEN: Never use `sed`, `awk`, `du`, or `grep` as Bash commands — they are not in the allowed tools and will trigger permission prompts that block the workflow. Use the Read tool to read files, the Grep tool to search content (ripgrep-backed — e.g. `pattern: "try\\s*\\{"`, `path: "src/"`, `output_mode: "files_with_matches"`; see `~/.claude/references/shell-safety.md` rule 4), and `jq`/`gh api --jq` for JSON processing. No `#` comments in bash commands. No heredocs. No multi-line bash commands. No `jq -f`/`--rawfile`/`--slurpfile`. No `$()` command substitution. No curly braces with quotes in the same command — pipe to `jq` instead of `--jq` when URLs contain braces. No output redirection (`>`, `>>`) — use the Write tool. No adjacent quote characters (e.g., `'"`, `"'`) at word start — simplify quoting or use the Write tool. No ANSI-C quoting (`$'...'`) — never place `$` immediately before a single quote. Do NOT post anything to GitHub — only the main skill posts replies in Step 5.
>
> **Verify library claims with Context7.** When evaluating a reviewer's concern that hinges on a specific library, framework, or external API (React hooks, Prisma, Next.js routing, AWS SDK, etc.), verify the claim against current docs before deciding: call `mcp__plugin_context7_context7__resolve-library-id`, then `mcp__plugin_context7_context7__query-docs` with the returned ID. Use this to confirm a **false-positive** verdict (the library really does handle the concern) or a **valid** verdict (the library really does behave as the reviewer says). Skip Context7 for general programming patterns, project-internal logic, or anything verifiable from the diff alone — don't burn calls on claims that don't depend on external library behavior.

For each item, extract the fields relevant to its `source_type`:

- **Inline comment** (`source_type: inline-comment`): `id`, `path`, `line`, `original_line`, `diff_hunk`, `body`, `reviewer`.
- **Review-body finding** (`source_type: review-body-finding`): `id`, `review_id`, `reviewer`, `body` (the finding text), and optional `path` / `line` when the finding named a location.

### Check 1 — Is this preexisting?

Determine whether the code the item flags was introduced by this PR or existed before. Branch on `source_type`:

**Inline comments:**

1. **Parse the `diff_hunk`**: Look at the last line(s) of the `diff_hunk` (which correspond to the commented line). If the line starts with ` ` (space, context line) rather than `+` (added line), the code is **preexisting** — it was not added or modified by this PR.
2. **Confirm with git blame**: Run `git blame -L LINE,LINE -- PATH` on the file. Check the commit SHA — if it is NOT the HEAD commit of the PR branch and NOT any commit in the PR, the code predates this PR.
3. If confirmed preexisting → categorize as **preexisting** and record the blame commit SHA and date.

**Review-body findings:**

1. If the finding has both a `path` and a `line`, run the same `git blame -L LINE,LINE -- PATH` check and apply the same preexisting rule.
2. If the finding has only a `path` (no line), skim `$TMPDIR/pr.diff` for that path. If the finding clearly describes code that isn't in the PR's added lines for that file (e.g., the reviewer says "the existing handler in `foo.ts` does X" and the diff shows no touching of that handler), categorize as **preexisting** and record the reasoning.
3. If the finding has neither a path nor a line, or you cannot confidently localize it in the diff, **skip the preexisting verdict** and proceed to Check 2. This is a feature, not a gap — vague architectural concerns typically aren't about preexisting code.

### Check 2 — Is this a false positive?

If the item is not preexisting, analyze whether the reviewer's concern is valid. The check works the same way for both source types — the only difference is what context you read.

1. **Read the relevant file(s)** to understand context (surrounding functions, imports, types, error handling). For inline comments, that's the commented path. For review-body findings, follow any file references in the finding text; if the finding is cross-cutting, read the most likely affected files based on the PR diff.
2. **Understand the reviewer's concern** by carefully parsing the comment / finding text.
3. **Evaluate the concern** against the actual code:
   - Does the code already handle the scenario the reviewer is worried about?
   - Is there handling elsewhere in the codebase (e.g., middleware, base class, framework feature) that addresses the concern?
   - Is the reviewer misreading the code or missing context?
   - Is the concern about style/preference rather than correctness?
   - Would the suggested change actually be incorrect or unnecessary given the types/constraints?
4. Use Grep to search for relevant patterns if needed (e.g., if the reviewer asks about error handling, search for try/catch or error middleware in related files). If the reviewer's concern hinges on external library behavior (React hooks, Prisma, Next.js, AWS SDK, etc.), verify the claim with Context7 (`mcp__plugin_context7_context7__resolve-library-id` then `mcp__plugin_context7_context7__query-docs`) before deciding the verdict.
5. If you can **definitively** demonstrate the concern is unfounded with concrete evidence → categorize as **false positive** and record the evidence.
6. If there is any reasonable chance the reviewer is correct → categorize as **valid**.

**Important**: Err on the side of treating issues as valid. Only categorize as false positive when you have clear, concrete evidence. When in doubt, it is a valid issue.

### Output per item

For each item, produce:

- **Item ID** (inline comment ID or synthetic `review-<reviewId>-finding-<N>`) and **reviewer**
- **Source type**: `inline-comment` or `review-body-finding`
- **File:line** reference when available (may be absent for review-body findings)
- **Review ID** (review-body findings only)
- **Original text** (first 200 chars of the comment or finding)
- **Verdict**: `preexisting`, `false-positive`, or `valid`
- **Reasoning**: 2-3 sentences explaining why
- **Evidence**: For preexisting — blame info or diff reasoning. For false positive — the specific code/pattern that addresses the concern. For valid — brief description of what needs to change. If the finding is too vague to act on mechanically, note that here; Step 4 will respect it.

## Step 3: Present triage results to user

Display a formatted summary of all items and their verdicts, split by source within each verdict category so the user can see at a glance where each concern came from:

```
## Review Item Triage — PR #NUMBER

### Preexisting (N)
  Inline comments:
    - **file.ts:42** (@reviewer): "Their comment..." → Code predates PR (blame: abc1234, 2025-01-15)
  Review-body findings (@reviewer on review #12345):
    - "Their finding..." → Targets existing handler not modified by this PR

### False Positives (N)
  Inline comments:
    - **file.ts:88** (@reviewer): "Their comment..." → Already handled by XYZ middleware
  Review-body findings (@reviewer on review #12345):
    - "Their finding..." → Current architecture already addresses this via EVIDENCE

### Valid Issues (N)
  Inline comments:
    - **file.ts:120** (@reviewer): "Their comment..." → Plan: add null check before accessing .property
  Review-body findings (@reviewer on review #12345):
    - "Their finding..." → Plan: add error boundary around the X call site
```

Omit a sub-section when it is empty (don't show an "Inline comments:" header with nothing under it). Omit the entire verdict category if its count is zero.

Ask the user to confirm before proceeding. The user may:

- Approve all verdicts
- Override specific verdicts (e.g., "treat #3 as valid" or "skip #5")
- Cancel entirely

Wait for confirmation before continuing.

## Step 4: Implement fixes for valid issues

For each item triaged as **valid** (and approved by the user):

1. Read the file at the item's path. For review-body findings without an explicit path, use the paths identified during Step 2's Check 2 analysis.
2. Understand the reviewer's suggestion and the surrounding code context.
3. Plan the minimal, targeted fix that addresses the reviewer's concern without unnecessary refactoring.
4. Apply the fix using the Edit tool.
5. After editing, re-read the modified section to verify the fix is correct and doesn't break surrounding code.
6. Record what was changed (the file paths and a one-line description) for the reply in Step 5.

If multiple valid issues exist in the same file, apply them carefully to avoid conflicts. Process them from bottom-to-top (highest line number first) so line numbers remain stable.

**Vague valid findings** — some review-body findings are valid concerns but too abstract to fix mechanically (e.g., "consider whether this module is doing too much"). For any item whose Step 2 evidence field was marked as vague/non-mechanical, do **not** attempt a speculative edit. Mark it as `noted` (distinct from `fixed`) and surface it in the Step 5 reply as "noted — will address in a follow-up PR". Silently dropping these leaves the reviewer in the dark; an explicit acknowledgement keeps the conversation honest.

## Step 5: Post replies to all items

Inline comments get their own per-comment reply via the standard inline-reply endpoint. Review-body findings cannot be "replied to" in place — GitHub has no per-finding reply endpoint for review bodies — so they are **grouped by `review_id` and answered with one aggregated PR-level comment per review**. This matches the user intent of replying "in place where possible" (one response per external review) while keeping the PR conversation readable.

### Reply templates (per-item body)

Each of these templates produces the body for a single item. Inline replies post this directly. Aggregated review-body replies embed one of these under a per-finding heading (see "Review-body findings" below).

**For preexisting items:**

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

**For valid issues (noted, not fixed — vague finding from Step 4):**

```
Noted — this is a valid concern but broader than what I'll resolve inside this PR. I'll address it in a follow-up.

BRIEF_ACKNOWLEDGEMENT of the core concern so the reviewer knows we heard them.
```

### Posting process — inline comments

For each inline comment:

1. Write the reply body to a temp file using the Write tool (e.g., `$TMPDIR/reply-COMMENT_ID.md`).
2. Post using: `gh api repos/OWNER/REPO/pulls/NUMBER/comments/COMMENT_ID/replies -F body=@$TMPDIR/reply-COMMENT_ID.md`
3. Verify the response includes a valid `id` field confirming the reply was created.

### Posting process — review-body findings

Group all non-dropped review-body findings by `review_id`. For each group with at least one finding:

1. Compose one aggregated reply body in a temp file `$TMPDIR/reply-review-<REVIEW_ID>.md` using the Write tool. Structure:

   ```
   @REVIEWER re: your review summary — addressing your findings below.

   ### 1. <short handle for finding, e.g., first ~60 chars of the finding text>

   > Quoted snippet of the original finding (blockquote the reviewer's words)

   <per-item template body from the list above, matching this finding's verdict>

   ### 2. <short handle for next finding>

   > Quoted snippet

   <per-item template body>

   ...

   <!-- respond-to-review:review-<REVIEW_ID>:finding-0 -->
   <!-- respond-to-review:review-<REVIEW_ID>:finding-1 -->
   ```

   The trailing HTML-comment markers — one per finding included in this reply — are mandatory. Step 1d relies on these exact strings to skip the finding on future runs. The marker format must match `respond-to-review:review-<REVIEW_ID>:finding-<N>` where N is the zero-based index assigned in Step 1c. Do not abbreviate, rename, or omit them.

2. Post using: `gh api repos/OWNER/REPO/issues/NUMBER/comments -F body=@$TMPDIR/reply-review-<REVIEW_ID>.md`
   (Note: `issues`, not `pulls` — PR-level comments live on the issues endpoint. This is correct and intentional.)
3. Verify the response includes a valid `id` field confirming the comment was created.

### Summary

After all replies are posted, display a summary:

```
## Done — PR #NUMBER

- Replied to N inline comments + M review-body findings across K reviews
  (X preexisting, Y false positives, Z fixes applied, W noted for follow-up)
- Files modified: list of files changed
```
