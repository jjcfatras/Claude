---
allowed-tools: Bash(gh pr comment:*), Bash(gh pr diff:*), Bash(gh pr view:*), Bash(gh api:*), Bash(jq:*), Bash(mktemp:*), Bash(mkdir:*), Bash(base64:*), Bash(ls:*), Bash(sleep:*), Bash(rm:*), Bash(date:*), Read, Write, Grep, Glob, Agent, TeamCreate, TeamDelete, TaskCreate, TaskList, TaskGet, TaskUpdate, SendMessage, mcp__*, Skill
description: Code review a pull request via a multi-specialist agent team. Spawns one custom subagent per applicable category (security, types, react, infra, errors, perf, quality, claude-md), coordinates them via a shared task list and peer DMs for cross-domain verification, and posts inline review comments. Cleans up its temp workspace (under /tmp) after posting.
argument-hint: [pr-number]
disable-model-invocation: false
model: opus
effort: xhigh
---

Provide a code review for the given pull request using a multi-specialist agent team.

**Setup:** Run `mktemp -d /tmp/pr-review-XXXXXX` to create a unique temp directory and store the path as `$REVIEW_TMPDIR`. All temp files in this review must be written under `$REVIEW_TMPDIR/`. Create a todo list for steps 1-6. Update after each step.

**`/tmp` writability fallback.** Some project allowlists permit `mktemp -d /tmp/...` but block subsequent `Write` and `rm` against `/tmp/` paths. Verify writability before proceeding: use the Write tool to create `$REVIEW_TMPDIR/.writable` (any short content). If the Write is denied, fall back: run `mkdir -p $HOME/.claude/tmp` then `mktemp -d $HOME/.claude/tmp/pr-review-XXXXXX`, point `$REVIEW_TMPDIR` at the new path, and retry the writability sentinel. Do not retry against `/tmp` once it has denied a write — the denial is structural (allowlist scope), not transient.

**Pre-flight: probe team-coordination tools.** This skill's whole design (concurrent specialist scans + lead-driven finalization + peer DMs) hard-depends on `Agent`, `TeamCreate`, `TeamDelete`, `TaskCreate`, `TaskList`, `TaskGet`, `TaskUpdate`, `SendMessage`. Do not trust the tool descriptions in your system prompt — they can claim a tool exists when the runtime has actually scoped it out. **Probe by calling `TaskList`** (a no-op read on an empty task list returns an empty result; a denied call returns a runtime error). Two failure shapes you must distinguish:

- **`Agent is not available inside subagents`** (or any "not available inside subagents" / "subagent" message): the skill is being invoked from a subagent context that structurally cannot spawn its own team. **No allowlist edit will fix this.** Stop and tell the user:
  > The code-review skill cannot run inside an Agent invocation — the team-coordination primitives don't propagate into subagents. Run the skill from the main interactive session instead.
- **`<tool> exists but is not enabled in this context`** / "tool not allowed" / explicit permission denial: the runtime exposes the tool but the project allowlist denies it. Stop and tell the user:
  > The code-review skill needs the team-coordination tools to be allowlisted. Add `Agent`, `TeamCreate`, `TeamDelete`, `TaskCreate`, `TaskList`, `TaskGet`, `TaskUpdate`, `SendMessage` to `permissions.allow` in `.claude/settings.json` and retry.

Either way, **do not silently fall back to a single-agent review.** The skill's confidence calibration, dedup gates, and finding format all assume independent specialist scans + peer DMs; a degraded run produces low-fidelity findings without surfacing the limitation. Cleanup `$REVIEW_TMPDIR` before exiting (per step 6's prefix safety check).

This pre-flight runs **before** step 1 deliberately — step 1 spends several minutes fetching the PR diff and building the valid-line map, and there is no reason to do that work if the team can't be built.

**Required reading for the lead (this session):**

- `~/.claude/references/code-review-rubrics.md` — confidence/severity rubric, findings file schema, cross-verification protocol, false-positive list. The dedup, gating, and posting steps below all reference these. The lead also embeds the rubric verbatim into each specialist's spawn prompt (step 2d), so specialists do not need to Read it themselves.
- `~/.claude/references/shell-safety.md` — seven rules covering real concerns (allowed-tools gaps, the zsh `?ref=SHA` glob bug, no piping to a shell interpreter, harness backgrounding, destructive ops, the `for x in "a" "b"` classifier-crash pattern). Heuristic-only rules retired with auto mode. Specialists rarely invoke Bash beyond `date +%s` and don't need to read this file.

**Execution model:** Step 1 uses inline `Agent` prep agents. Step 2 builds a team: each specialist is a persistent teammate (under `.claude/agents/code-review-*.md`) that DMs peers for cross-domain verification while scanning. Specialists write `findings/<role>.json`, DM `team-lead` with `scan_complete: <role>` to wake the lead, then stay idle for incoming peer DMs. The lead's turn ends after spawning; each `scan_complete` DM resumes it for one short turn to check whether all findings have landed. Once they have, the lead broadcasts `finalize_now`, which is the cue for specialists to mark their assignment task `completed`. Steps 3-5 run on the lead. Step 6 cleans up.

Design rationale (cost shape, finalization protocol, notification flow, teardown degraded state, etc.) lives in `~/.claude/references/code-review-design-notes.md`. Not read at runtime.

Follow these steps precisely:

## 1. Prep agents (Sonnet 4.6, inline)

Launch all three prep agents in a single message (foreground). Spawn each `Agent` call with `model: "sonnet"` and `mode: "auto"` — the auto-mode classifier replaces heuristic prompts so each prep agent can use straightforward shell forms (redirection, single jq filters, etc.) without manual workarounds. Sonnet 4.6 is the minimum model the classifier supports.

a. **CLAUDE.md Agent**: Return file paths and contents of relevant CLAUDE.md files: the root CLAUDE.md (if any) and CLAUDE.md files in directories modified by the PR.

b. **PR Summary Agent**: View the pull request and:

- Run `gh pr diff NUMBER > $REVIEW_TMPDIR/pr-NUMBER.diff` to save the diff in one step.
- Extract a **valid-line map** from the diff by parsing `diff --git` lines (for file paths) and `@@ ... +newStart,newCount @@` hunk headers. The map is: `file path → list of [newStart, newStart+newCount-1]` ranges.
  - **Binary files**: Skip lines containing `Binary files ... differ`. Include the file in the changed-files list but omit it from the valid-line map.
  - **Renamed files**: Use the **new** path (the `b/` path) as the map key. Pure renames with no content changes get included in the changed-files list but omitted from the valid-line map.
- Return: (1) summary, (2) `$DIFF_FILE` path, (3) changed file list, (4) `OWNER` and `REPO`, (5) PR `NUMBER`, (6) full HEAD SHA (`gh pr view NUMBER --json headRefOid -q .headRefOid`), (7) valid-line map.

c. **Prior Reviews Agent**: Check for prior Claude Code reviews on the PR:

- Fetch all reviews: `gh api --paginate repos/OWNER/REPO/pulls/NUMBER/reviews`
- Filter for the most recent review whose `body` contains `Generated with [Claude Code]`: `gh api --paginate repos/OWNER/REPO/pulls/NUMBER/reviews | jq '[.[] | select((.body // "") | contains("Generated with [Claude Code]"))] | sort_by(.submitted_at) | last'`.
- If found, extract `id`, `submitted_at`, and `commit_id`.
- Fetch its inline comments: `gh api --paginate repos/OWNER/REPO/pulls/NUMBER/reviews/ID/comments`
- Extract `path`, `line`, `start_line`, snippet (between first triple-backtick fences in body), and first-line description.
- Write the result as JSON to `$REVIEW_TMPDIR/prior-issues.json` using the Write tool. Schema:

  ```json
  {
    "last_review_date": "...",
    "last_review_commit": "...",
    "issues": [
      {
        "path": "...",
        "line": 0,
        "start_line": 0,
        "snippet": "...",
        "description": "..."
      }
    ]
  }
  ```

- If no prior Claude Code review exists, write the file with `last_review_date` / `last_review_commit` set to `null` and an empty `issues` array.
- Return the file path and the prior-issues data.

## 2. Build the team and run the multi-specialist review

The pre-flight at the top of the skill has already confirmed the team-coordination tools are usable. If somehow you reached step 2 with any of them missing, abort here rather than degrading.

### 2a. Determine which specialists apply

Based on the changed-file list:

- **HAS_CLAUDE_MD**: true if step 1a found CLAUDE.md files.
- **HAS_TYPESCRIPT**: true if any changed file ends in `.ts` or `.tsx`.
- **HAS_FRONTEND**: true if any changed file is in a frontend dir (e.g., `src/components/`, `src/pages/`, `src/hooks/`, `app/`) or has a `.tsx`/`.jsx` extension that contains React components.
- **HAS_INFRASTRUCTURE**: true if any changed file matches migration / terraform / docker / config patterns (`*.sql`, `migrations/`, `*.tf`, `*.hcl`, `docker*`, `Dockerfile*`, infra/deploy directories, or files referencing `secret_manager_path`).

The roster always includes `security`, `quality`, `errors`, `perf`. Conditionals add `claude-md`, `typescript`, `react`, `infra` based on the flags above.

### 2b. Write the roster file and shared inputs

Build `$REVIEW_TMPDIR/roster.json` using the Write tool. Schema:

```json
{
  "team_name": "code-review-<PR_NUMBER>",
  "members": [
    {
      "role": "security",
      "name": "security-reviewer",
      "subagent_type": "code-review-security"
    },
    {
      "role": "react",
      "name": "react-reviewer",
      "subagent_type": "code-review-react"
    }
  ]
}
```

Use `<role>-reviewer` as the teammate `name` (the rubrics file's routing table refers to peers by these names — do not deviate). Include only roles that apply per 2a.

Also write:

- `$REVIEW_TMPDIR/changed-files.json` — JSON array of changed paths.
- `$REVIEW_TMPDIR/claude-md-files.json` — JSON object `{ "<path>": "<contents>", … }` from step 1a (or `{}`).
- `$REVIEW_TMPDIR/prior-issues.json` — already written by step 1c.
- Create the directory `$REVIEW_TMPDIR/findings/` (specialists will write files into it). Use `Bash` with `mkdir -p`.

Then **read `~/.claude/references/code-review-rubrics.md` once with the Read tool** and keep its content available for the spawn prompts in step 2d. Embedding the rubric in the spawn prompt is what lets specialists skip the corresponding `Read` and start scanning sooner. The roster, prior-issues, claude-md-files, and changed-files JSON you just wrote should also be inlined into each spawn prompt — the on-disk copies remain as a fallback and as durable artifacts of the run, but specialists shouldn't have to fetch them.

### 2c. Create the team and assignment tasks

1. `TeamCreate({team_name: "code-review-<PR_NUMBER>", description: "Multi-specialist review for PR <NUMBER>"})`.
2. For each member in the roster, `TaskCreate({subject: "Review for <role>", description: "Specialist task — write findings to $REVIEW_TMPDIR/findings/<role>.json then mark complete.", activeForm: "Reviewing <role>"})`. Capture each returned task ID; you'll pass it to the spawn prompt as `ASSIGNMENT_TASK_ID`.

### 2d. Spawn all applicable specialists in one message

Launch every member of the roster as a teammate via the `Agent` tool. Send all calls in **a single message** so they run concurrently. For each member:

```
Agent({
  team_name: "code-review-<PR_NUMBER>",
  name: "<role>-reviewer",
  subagent_type: "code-review-<role>",
  mode: "auto",
  description: "Code review specialist — <role>",
  prompt: <SPAWN_PROMPT>
})
```

`mode: "auto"` is required so each specialist runs under the auto-mode classifier (auto-approves safe shell patterns without prompting; required for long unattended scans).

Inline the rubric, roster, prior-issues, CLAUDE.md content, and changed-file list directly into the spawn prompt. Only the diff stays as a file (size). Specialists must not Read the inlined files. Template:

```
You are <role>-reviewer on the code review team for <OWNER>/<REPO> PR #<PR_NUMBER>.

CONTEXT VALUES
- OWNER: <OWNER>
- REPO: <REPO>
- HEAD_SHA: <full HEAD SHA>
- PR_NUMBER: <PR_NUMBER>
- REVIEW_TMPDIR: $REVIEW_TMPDIR
- ASSIGNMENT_TASK_ID: <task id captured in 2c>

SUMMARY
<one-paragraph summary from step 1b>

DIFF
The PR diff is on disk at: $REVIEW_TMPDIR/pr-<PR_NUMBER>.diff
Read it once when you start scanning. Don't refetch via `gh pr diff`.

CHANGED FILES
<JSON array of changed paths — verbatim contents of changed-files.json>

ROSTER (active specialists on this team — DM peers by `name`)
<verbatim contents of roster.json>

PRIOR ISSUES (most recent prior Claude Code review on this PR; may be empty)
<verbatim contents of prior-issues.json>

CLAUDE.MD CONTENT (paths + contents from step 1a; may be empty)
<verbatim contents of claude-md-files.json>

RUBRIC (already loaded — do not Read the file)
<verbatim contents of ~/.claude/references/code-review-rubrics.md>

GETTING STARTED
Begin by Read'ing $REVIEW_TMPDIR/pr-<PR_NUMBER>.diff, then follow your agent system prompt's workflow and the rubric's "Specialist workflow" section. The rubric, roster, prior-issues, and CLAUDE.md content above are inline — do not Read those files.
```

When `Agent` is called with `team_name`, it returns immediately (the response includes "Spawned successfully" / "running via mailbox") rather than blocking until the agent's first turn completes. Specialists run asynchronously; you'll be woken via a `scan_complete` DM as each one's findings file lands (see 2e).

### 2e. Wait for scan_complete DMs, then broadcast finalize

Specialists DM `team-lead` with `scan_complete: <role>` once they've written `findings/<role>.json` (rubric workflow step 8). The findings file is the source of truth; the DM is the wake signal. There is no polling cadence — the lead ends its turn after spawning and each DM resumes it for one short turn.

In **the same message** as the parallel `Agent` calls in 2d, also issue **one** safety timer:
`Bash({command: "sleep 300", run_in_background: true, description: "scan-complete safety timer"})`. This is the single backstop in the rare case where a specialist crashes before sending any DM (its 180 s self-budget should preclude this, but the timer keeps the skill from hanging if a specialist is structurally broken). On the happy path, every roster role DMs well before the timer fires and you broadcast finalize without ever consulting it.

After the spawn-and-timer message, **end your turn**. The next time the harness invokes you, it will be because either (a) a teammate sent a DM or (b) the safety timer fired. On every such wakeup turn:

1. List `$REVIEW_TMPDIR/findings/`.
2. If every roster role has a corresponding `<role>.json`, send `finalize_now` to every roster member in one SendMessage block (`SendMessage({to: "<role>-reviewer", message: "finalize_now: all peers have finished scanning; mark your task complete"})`) and proceed to 2f.
3. Otherwise, look at what woke you. If it was a teammate DM (or any wake before the safety timer fired), end the turn — another DM (or the timer) will wake you again.
4. **Once the safety timer has fired and any role is still missing**, send one wake-up DM to each missing role in a single message: `SendMessage({to: "<role>-reviewer", message: "lead-wakeup: your self-budget should have fired by now. Write whatever findings you have with scan_status: 'timed_out' and stay idle for finalize_now."})`. Issue one more `Bash({command: "sleep 60", run_in_background: true, description: "scan-complete grace window"})` as a grace window and end the turn. Single shot — don't keep issuing wake-ups.
5. **On the grace-window wake**, if a role is still missing, treat it as unreachable: track the role name in an `unreachable_roles` list (in your turn text), broadcast `finalize_now` to every roster member, and proceed to 2f. Do **not** write a stub findings file — step 2f's missing-file branch handles consolidation correctly, and a stub races with a slow-but-live agent that may still write its real findings during teardown.

Notification-flow + safety-timer rationale: see `~/.claude/references/code-review-design-notes.md`.

### 2f. Collect findings

Read every `$REVIEW_TMPDIR/findings/<role>.json` that exists. For each role in the roster:

- If the file exists and parses, append its `findings` array to the consolidated list.
- If the file is missing or has `scan_status: "timed_out"`, log it and continue with whatever made it through.

### 2g. Tear down the team

`TeamDelete` refuses while teammates are alive, so shut them down first. Best-effort with a hard wall-clock cap — findings are already on disk; one uncooperative specialist must not block step 3. Three attempts with widening sleep windows (15 s → 30 s → 30 s, ~75 s worst case). Happy-path latency is unchanged: `TeamDelete` succeeds on attempt 1.

1. Send a shutdown request to every teammate in a single message: `SendMessage({to: "<role>-reviewer", message: {"type": "shutdown_request", "reason": "review complete, team teardown"}})`.
2. `Bash sleep 15` with `run_in_background: true`, wait via `TaskOutput` (`block: true`).
3. Call `TeamDelete()`. On success, proceed to step 3 of the skill.
4. **On "still active member(s)" error (attempt 1 failed)**:
   a. **Re-list `$REVIEW_TMPDIR/findings/`.** If any role previously in `unreachable_roles` (or any `scan_status: "timed_out"` role) now has a real findings file (not the missing/timed-out state you saw at 2f), Read it and merge into the consolidated list. A slow-but-live agent often writes its real findings during teardown — this is the deterministic recovery path that replaces the previous lucky re-Read.
   b. Send one more `shutdown_request` to each named holdout, `sleep 30 s`, retry `TeamDelete()`.
5. **On second failure (attempt 2 failed)**: call `TaskList` to inspect holdout state. If the holdout's assignment task is already `completed`, the runtime is just slow to GC the agent slot — `sleep 30 s` and retry `TeamDelete()` once more (attempt 3). If the holdout's task is still `in_progress`, the agent is genuinely deadlocked — same `sleep 30 s` + retry, but expect the retry to fail.
6. **On third failure**, stop trying. Log one warning naming the leftover team + holdout(s) and continue to step 3 of the skill. Don't loop.

Degraded-state explanation: see `~/.claude/references/code-review-design-notes.md`.

## 3. Filter — deduplication and gates

**Pre-filter 1 — Deduplication across specialists:** Apply two passes.

_Pass 1 — Positional dedup_: Group findings by file path and line number (within ±3 lines). For each group, keep the finding with the highest confidence score. If tied, prefer the specialist whose domain best matches the issue category.

_Pass 2 — Semantic dedup_: After positional dedup, look for findings that describe the same defect at different anchors. Two findings are semantic duplicates when **either** holds:

1. One finding's `file` path appears as a path-string inside another finding's `explanation` field (case-sensitive, full path match), AND both findings have severity ≥ Medium.
2. The two findings' `explanation` fields share a 60+ character common substring AND their `category` fields are related (security ↔ errors, quality ↔ claude-md, typescript ↔ react are related pairs; everything else is unrelated).

When two findings match as semantic duplicates, keep the one with higher confidence; if tied, prefer the one whose `file` is **inside the diff** (so dedup tends to leave an inline-eligible representative). Append a note to the kept finding's `explanation`: "_This finding was also independently raised by `<other-specialist>` (confidence `<N>`) at `<other-file>:<line>`._"

**Pre-filter 2 — Prior-review deduplication:** For each surviving finding, match against `$REVIEW_TMPDIR/prior-issues.json`:

1. Same file path.
2. Line within ±5 lines of a prior issue's line OR snippet shares a 40+ character common substring with a prior issue's snippet.
3. If matched AND the line is on unchanged code (context line ` `, not added line `+` in the diff) → remove the finding (already flagged in prior review).
4. If matched BUT the code at that location has changed (`+` line in diff) → keep the finding and append to its explanation: "_Note: This issue was flagged in a prior review but the code has since changed._"

Log the count removed by this filter for reporting in step 4.

**Gate 1 — Confidence/Severity filter:**

- Confidence must be at least 50.
- If confidence is between 50-74, only include if severity is Critical or Medium.
- Confidence ≥ 75 is included regardless of severity.

If no findings meet these criteria, do not proceed to posting — present the empty result in step 4.

**Gate 2 — Diff line validation:**

Using the valid-line map from step 1b, validate that every surviving finding targets a line within a valid hunk range for that file:

- **In range**: mark as **inline-eligible**.
- **Out of range, within 5 lines of nearest valid line**: snap to nearest valid line, prepend "_Note: This comment was placed on the nearest diff line; the issue actually occurs on line {original_line}._" Mark inline-eligible.
- **Out of range, >5 lines away or file not in diff**: mark as **summary-only**.
- **Multi-line** (`startLine` + `line`): if only `startLine` is out of range but `line` is valid, drop `startLine` (single-line). If `line` is out of range, apply snapping.

Result: two lists (inline-eligible, summary-only). If both empty, stop. If only summary-only exists, skip step 5a's `comments` array.

## 4. Present and confirm

Show the user the consolidated finding list with severity, confidence, file:line, and a one-line description for each. If issues were removed by prior-review dedup, include "Skipped N issue(s) already flagged in prior review ({last_review_date})." plus a brief list (file:line — description) so the user can override if needed.

Ask permission to post. If the user declines, skip step 5 (still run step 6 cleanup).

## 5. Post the review

Use the GitHub Reviews API via `gh api` (single API call, single notification, inline comments on relevant diff lines).

### 5a. Build the JSON payload

Target structure:

```json
{
  "commit_id": "<full HEAD SHA>",
  "event": "COMMENT",
  "body": "<review summary — see formats below>",
  "comments": [
    {
      "path": "relative/file/path",
      "line": 42,
      "side": "RIGHT",
      "body": "<formatted comment per ISSUE_FORMAT>"
    },
    {
      "path": "relative/file/path",
      "start_line": 45,
      "line": 50,
      "start_side": "RIGHT",
      "side": "RIGHT",
      "body": "<formatted multi-line comment>"
    }
  ]
}
```

Approach:

1. Use the Write tool to save each inline-eligible issue's formatted body (per ISSUE_FORMAT below) to a unique temp file: `$REVIEW_TMPDIR/comment-1.md`, `$REVIEW_TMPDIR/comment-2.md`, etc. (Raw markdown — kept as a debugging aid.)
2. Use the Write tool to save the review summary body to `$REVIEW_TMPDIR/summary.md` (raw markdown — no escaping needed; `--rawfile` reads it verbatim).
3. Use the Write tool to save `$REVIEW_TMPDIR/comments-array.json`: a JSON array of objects matching the comment structure above (`path`, `line`, `side`, `body`, plus `start_line` / `start_side` for multi-line). Embed each `comment-N.md` content into its `body` field as a properly-escaped JSON string. If only summary-only issues exist, write `[]`.
4. Assemble the final payload via jq: `jq -n --arg sha "$HEAD_SHA" --rawfile summary $REVIEW_TMPDIR/summary.md --slurpfile comments $REVIEW_TMPDIR/comments-array.json '{commit_id: $sha, event: "COMMENT", body: $summary, comments: $comments[0]}' > $REVIEW_TMPDIR/payload.json`.
5. Validate: `jq . $REVIEW_TMPDIR/payload.json`.

If only summary-only issues exist, the `$comments[0]` value is an empty array and the resulting `comments` field is `[]`.

### 5b. Post

`gh api repos/OWNER/REPO/pulls/NUMBER/reviews --method POST --input $REVIEW_TMPDIR/payload.json`

(Substitute actual OWNER, REPO, and NUMBER values.)

**Fallback**: If the API call fails, write the full review summary body to `$REVIEW_TMPDIR/fallback.md`, then post with `gh pr comment NUMBER -F $REVIEW_TMPDIR/fallback.md`. Include all issues (inline-eligible and summary-only) in the body using ISSUE_FORMAT, each prefixed with `**path:line**`. Footer: "Note: Inline comments failed ({error}). All issues listed below."

### ISSUE_FORMAT (used for inline comment bodies and summary-only issues)

````
{severity_emoji} **{Severity}** (Confidence: {N}/100) - {brief description}

**Explanation:** {detailed explanation. If CLAUDE.md-triggered, quote: "CLAUDE.md says: <...>"}

**Code:**

```{language}
{problematic code from PR}
```

**Suggested fix:**

```{language}
{corrected code}
```
````

Severity emojis: 🔴 Critical, 🟡 Medium, 📝 Minor. For inline comments, omit the file path from the description (already attached to the line).

### Review summary body

All variants start with `### Code review` and end with:

```
🤖 Generated with [Claude Code](https://claude.ai/code)

<sub>If this code review was useful, please react with 👍. Otherwise, react with 👎.</sub>
```

- **Has inline issues**: Summary table (columns: #, Severity, Confidence, File, Description) + "See inline comments for full details, code examples, and suggested fixes." If summary-only issues also exist, append `#### Additional issues (could not attach inline)` with each in ISSUE_FORMAT including `path:line`.
- **Only summary-only issues**: Empty `comments` array. Header: "Found N issue(s). These could not be placed as inline comments because their line numbers fall outside the diff's visible range." List each in ISSUE_FORMAT.
- **No issues**: Empty `comments` array. Body: "No issues found. Reviewed by: <comma-separated roster roles>."

### Linking to code in inline comments

Format: `https://github.com/OWNER/REPO/blob/<full HEAD SHA>/<path>#L<start>-L<end>`

Requires the full SHA (no `$(git rev-parse HEAD)` — it won't be expanded in markdown). Repo name must match the PR's repo. Provide at least 1 line of context before and after.

## 6. Cleanup

Remove the temp workspace. Sanity check before deletion: `$REVIEW_TMPDIR` must start with one of the two writable roots created in setup — `/tmp/pr-review-` or `$HOME/.claude/tmp/pr-review-` — and must equal the path created at the start of this run. If the prefix check fails, log a warning and skip cleanup rather than risk an unintended delete.

Run `rm -rf $REVIEW_TMPDIR` only after the prefix check passes. If `rm` itself is denied by the project allowlist (some configurations grant `mktemp` but not `rm` against `/tmp/`), log a single-line warning naming the leftover path so the user can clean it manually, and continue — do not retry, do not fall back to a per-file deletion loop.

This runs even if the user declined posting in step 4 (the workspace is no longer needed) and even if the API post failed (the fallback comment is already on the PR).

## Notes

- Use `gh` for fetching PR data. Use `gh api repos/OWNER/REPO/pulls/NUMBER/reviews` for posting.
- Cite and link every issue (e.g., link CLAUDE.md when referenced).
- The confidence/severity rubric, findings schema, cross-verification protocol, and false-positive list live in `~/.claude/references/code-review-rubrics.md`. Do not re-list them here — specialists and the lead both read that file.
