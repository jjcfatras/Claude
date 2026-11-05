---
description: Respond to every flagged issue on a PR — inline comments and review-body findings alike, including reviews you posted yourself (e.g. via `/code-review --comment`) — dismissing false positives and preexisting issues, fixing valid ones in a throwaway worktree on the PR head
argument-hint: <pr-number> [comment-id]
allowed-tools: Bash(git *), Bash(gh *), Bash(jq *), Bash(rg *), Bash(echo *), Bash(npx nx *), Bash(mktemp *), Bash(base64 *), Bash(wc *), Bash(ls *), Read, Edit, Write, AskUserQuestion, Agent, Workflow, EnterWorktree, ExitWorktree, mcp__*
model: opus
effort: high
---

Respond to every flagged issue on a pull request — both **inline review comments** (line-attached) and **review-body findings** (issues listed in a review's summary body that couldn't attach to a specific line). For each flagged issue, determine whether it is (1) a false positive, (2) preexisting code not introduced by this PR, or (3) a valid issue. Dismiss cases 1 and 2 with an explanation reply. For case 3, implement the fix, **commit and push it before posting any reply**, refresh the PR description to match the branch as it now stands, and reply confirming the change — so reviewers never see a "Fixed" reply that points at code still sitting in the local working tree, or a PR description that predates the fixes.

All of it runs in a throwaway worktree at the PR head — created after Step 1, removed after the push — so the checkout you invoke the command from (its branch, uncommitted changes, untracked files) is never touched, and the command works from any checkout of the repo.

**Shell Command Safety:** Skills run under [auto permission mode](https://code.claude.com/docs/en/permission-modes), which replaces the old static-analysis prompts with a classifier. The rules that still matter — allowed-tools gaps, no piping to a shell interpreter, no destructive ops without confirmation — are condensed into the agent preamble in Step 2.

**Tool discipline:** Six rules apply across every step. They are not stylistic — they keep the command working under stricter permission modes and cut wall time:

- **Search with `Bash(rg *)`.** The `Grep`/`Glob` tools are removed on native macOS/Linux builds (CC 2.1.117+), where `rg` is that same embedded ripgrep; `Bash(rg *)` is in `allowed-tools` so it runs unprompted. Two gotchas: `rg` exits **1** on no match, so append `|| true` to any probe allowed to find nothing, or it fails the whole Bash call; and always quote patterns and globs (`rg 'foo' -g '*.ts'`) to sidestep the zsh "no matches found" glob bug.
- **Use `jq` for JSON filtering, never `node -e` / `python -c`.** `Bash(jq *)` is in `allowed-tools`; `node` and `python` are not. `jq` handles all of Step 1's GitHub-API JSON parsing. Pass **absolute** `$REVIEW_DIR/...` paths — never `cd "$REVIEW_DIR" && jq ...`. Prefix filters match the literal command string, so a leading `cd` is what gets matched and the `Bash(jq *)` grant never fires.
- **Fetch with `Read` anything you intend to `Edit`.** `Edit` refuses a file the harness has no record of reading, so a range pulled with `sed -n`/`cat` does not satisfy it and you pay for the same bytes twice. Use `rg`/`sed` to **locate** (cheap, no precondition), then `Read` the range you will actually edit.
- **Batch independent tool calls in parallel.** When a step says "for each X do Y" and the Y's have no ordering dependency (different file paths, different comment IDs, different verification targets), emit them as multiple tool calls in a single assistant message. Steps 1, 4, and 5 call this out explicitly where it matters.
- **`$REVIEW_DIR` is a placeholder, not a shell variable.** Bash state does not persist between tool calls, so write the literal absolute path from Step 0 into every command — never a `REVIEW_DIR=…;` prefix, which turns every call into a compound command (see Worktree setup step 3). Never substitute `$TMPDIR`: macOS already exports it as the system temp dir, so `"$TMPDIR/pr.diff"` silently lands somewhere else.
- **Background the slow commands, then wait.** Run Worktree setup's install and Step 5's commit with `run_in_background: true`, capturing output as `<cmd> > "$REVIEW_DIR/<name>.log" 2>&1; echo "exit=$?"`. A repo's pre-commit hooks can run for many minutes — past the 120 s default Bash timeout and close to the 600 s ceiling. Keep triaging while the install runs and confirm its notification before Step 4. After launching the commit, end your turn and wait for its notification — never `sleep` or poll the output file — then confirm `exit=0` in the log before pushing.

Follow these steps precisely:

## Step 0: Setup and fetch PR metadata

1. Run `mktemp -d /tmp/review-respond-XXXXXX` to create a temp directory. Store the path as `$REVIEW_DIR`.
2. Parse the PR number from `$ARGUMENTS`. If a second argument is present, treat it as a specific inline-comment ID to target; in that case, skip review-body processing entirely (the user is scoping to one inline thread).
3. Run `gh repo view --json owner,name` to get the repository owner and name.
4. Run `gh api user` and extract the `login` field — this is the current user. It is used **only** for dedup — spotting threads we already replied to (Step 1a) and findings we already answered (Step 1d) — never to exclude items. Reviews and comments you authored yourself are in scope.
5. Run `gh pr view $PR_NUMBER --json headRefName,headRefOid,baseRefName,baseRefOid,commits,isCrossRepository` to get branch info and commit SHAs. Record `commits[].oid` as the PR commit list — the triage procedure's Check 1 compares blame SHAs against it, and Step 2 passes it to the workflow. If `isCrossRepository` is `true`, stop: fixes are pushed to `origin/<headRefName>`, which for a fork PR would create a stray branch on the base repo instead of updating the PR.
6. Save the PR diff with a shell redirect: `gh pr diff $PR_NUMBER > "$REVIEW_DIR/pr.diff"`. Redirect every raw `gh` fetch in Steps 0–1 the same way — the Write tool would make you re-emit every byte as output tokens. Keep Write for content you compose (reply bodies, the PR description).
7. Parse the diff to build a map of changed files and their hunk ranges (`file path → list of [newStart, newStart+newCount-1]` ranges), and whether each line is added (`+`) or context (` `).

## Step 1: Fetch and filter flagged items

"Flagged items" covers two sources: **inline comments** (line-attached) and **review-body findings** (issues listed in a review's summary body that couldn't attach to a specific line). Both flow through the same downstream steps. Produce a unified `$REVIEW_DIR/pending-items.json` whose entries have a common shape plus a `source_type` of `inline-comment` or `review-body-finding`.

**Filter the fetched JSON with `jq`.** All three downstream filters (1a inline-comment dedup, 1b review filtering, 1d marker dedup) operate on the JSON files saved in Step 0. Use `jq` — not `node -e` / `python -c` — per the Tool discipline rules above. The three filters are independent (different files, no shared state); issue them as parallel Bash tool calls in a single assistant message rather than serializing.

### 1a — Inline comments

1. Fetch all review comments: `gh api --paginate repos/OWNER/REPO/pulls/NUMBER/comments`
2. Redirect the raw JSON to `$REVIEW_DIR/all-comments.json`.
3. Filter to **actionable comments** by excluding:
   - Comments where `in_reply_to_id` is not null (these are replies, not top-level comments)
   - If a specific comment ID was provided in step 0, filter to only that comment

   Do **not** filter on author. Top-level comments you wrote yourself are in scope — a review posted by `/code-review --comment` lands under your own login, and those findings are exactly what this command exists to triage.

4. For each remaining comment, check if the current user has already replied by scanning all comments for entries where `in_reply_to_id` matches this comment's `id` AND `user.login` matches the current user. Exclude comments that already have a reply from us. With self-authored comments in scope, this is the **only** guard against answering the same thread twice — never skip it.

### 1b — Reviews

If a specific inline comment ID was provided in step 0, skip 1b–1d entirely — the user is scoping the run to one thread.

1. Fetch all reviews: `gh api --paginate repos/OWNER/REPO/pulls/NUMBER/reviews`. Redirect the raw JSON to `$REVIEW_DIR/reviews.json`.
2. Filter the reviews to entries that meet **all** of the following. Note the absence of an author filter — self-authored reviews are in scope, same as self-authored inline comments:
   - `body` is non-empty after trimming whitespace,
   - `body` is not a trivial acknowledgement — exclude bodies whose trimmed text is, case-insensitive, one of `lgtm`, `looks good`, `approved`, `:+1:`, `👍`, or is shorter than roughly 20 characters of prose (a bare "thanks!" or "great work" has no findings to triage),
   - `body` does not report a clean result — exclude bodies whose only substantive claim is that nothing was found (e.g. containing `No issues found`), so an all-clear automated review doesn't spawn a parsing agent for nothing.

### 1c — Parse review bodies into discrete findings

Review bodies are free-form markdown. Findings can appear as bullet points, numbered items, `###`-delimited subsections, or a single prose paragraph. Parse each surviving review body into a list of findings:

- For review bodies with fewer than ~4 filtered reviews total, parse inline (read the body yourself and split it into findings).
- For 4 or more, launch one Sonnet 4.6 agent per review in parallel (`model: "sonnet"`, `mode: "auto"`), each bound by the **agent preamble** from Step 2 (the exact text starting "You are a review-response analysis agent…"), with the per-agent role adjusted to "You are a review-body parsing agent." The agent's only job is to split a single review body into findings and return structured JSON — it must not hit the network beyond what it was given and must not post anything.

For each finding, emit:

- `id`: synthetic stable string `review-<reviewId>-finding-<N>` where N is the zero-based index within that review's body. Stability matters — the same input must produce the same IDs across runs so the dedup check in 1d works.
- `text`: the finding's markdown as written (don't paraphrase; downstream analysis needs the reviewer's original wording).
- `path`, `line`: optional. Only populate when the finding explicitly names a file (`src/foo.ts`, `` `src/foo.ts` ``) and/or a line (`line 42`, `:42`). Use a simple regex — don't guess from context.
- `reviewer`: `review.user.login`.
- `review_id`: the review's `id`.

Discard "findings" that are purely summary, approval, or praise (e.g., "Overall this looks great", "Nice refactor"). The downstream steps only handle concerns.

### 1d — Dedup review-body findings against prior replies

When the skill posts a reply for a review-body finding (Step 6), it embeds a stable HTML-comment marker of the form `<!-- respond-to-review:review-<reviewId>:finding-<N> -->`. Use this to detect previously-handled findings and skip them:

1. Fetch PR-level issue comments: `gh api --paginate repos/OWNER/REPO/issues/NUMBER/comments`. Redirect it to `$REVIEW_DIR/issue-comments.json`.
2. Filter the list to comments authored by the current user.
3. For each review-body finding from 1c, check `$REVIEW_DIR/issue-comments.json` for its marker string with `jq` — the file is JSON, so `jq` reads it structurally rather than by text match: `jq -e --arg m "<marker>" 'any(.[]; .body | contains($m))' "$REVIEW_DIR/issue-comments.json"`. If the marker appears in any of our prior PR comments, drop the finding.

### 1e — Merge into pending items

Combine the filtered inline comments from 1a and the surviving review-body findings from 1c/1d into a single list.

**Drop review-body findings that merely point at an inline comment already in the list.** An automated review body — `/code-review`'s in particular — leads with an index table of its own inline findings (one row per finding: severity, confidence, `file:line` link) followed by "See inline comments for full details". Those rows are pointers, not separate concerns, and answering them would double-reply to every finding. Drop a review-body finding when either holds:

- its `path` and `line` match a surviving inline comment from 1a, or
- it carries no concern of its own — a bare table row, a `file:line` link with no described problem, or text deferring to the inline comments.

Findings under an "Additional issues (could not attach inline)" heading are **not** pointers — they exist nowhere else and must survive.

Each entry in `$REVIEW_DIR/pending-items.json` must contain:

- `id`
- `source_type`: `inline-comment` or `review-body-finding`
- `reviewer` (`user.login` for inline, `review.user.login` for review-body)
- `body` (the comment text for inline, `text` for review-body)
- `path`, `line`: populated when available; review-body findings may have neither
- `diff_hunk`: for inline comments only
- `review_id`: for review-body findings only

Display the count of pending items. If zero, report "No unaddressed review items found" and stop.

## Worktree setup

Triage reads and every fix happen in a worktree at the PR head, never in the checkout the command was invoked from — that one may be on another branch entirely, or carry uncommitted work.

1. Run `git rev-parse --show-toplevel` and store it as `$REPO_ROOT`. The worktree path is `$WORKTREE` = `$REPO_ROOT/.claude/worktrees/pr-<NUMBER>`.
2. Fetch the branch and add a **detached** worktree at the exact head SHA from Step 0:

   ```bash
   git fetch origin <headRefName>
   git worktree add --detach "$WORKTREE" <headRefOid>
   ```

   Detached, not on the local branch: `git worktree add` refuses a branch already checked out elsewhere — often the user's main checkout. Pinned to `headRefOid`, not `origin/<headRefName>`: triage then reads exactly the code Step 0's diff describes. If the path already exists, a previous run kept it holding unpushed work (see Worktree teardown) — stop and report it; never remove it. If either command fails, stop.

3. Call **EnterWorktree** with `path: $WORKTREE`. Bash now runs there. From here until teardown the harness refuses any Bash call it cannot prove keeps git inside the worktree. Run each `git` command as its own plain call — no `;`/`&&`/pipe chain around it (a trailing `> log 2>&1; echo "exit=$?"` capture is fine) — and never hand generated text to `bash`/`sh`/`zsh` inside a compound command: write it to a file and run `bash -n <file>` as its own call. If a call is refused, split it; don't rephrase the chain.
4. Install dependencies from the lockfile, if there is one, in the background (see Tool discipline): `pnpm-lock.yaml` → `pnpm install --frozen-lockfile`, `package-lock.json` → `npm ci`, `yarn.lock` → `yarn install --immutable`. A fresh worktree has no `node_modules`, so triage probes, Step 4's checks, and the repo's pre-commit hooks (husky regenerates its hook shims on install) would otherwise fail or silently skip. Other toolchains (Go modules, cargo) fetch on first build — skip them.
5. Gitignored files (`.env*`, local config) are not copied. If a Step 4 check fails only because one is missing, copy it from `$REPO_ROOT` rather than "fixing" code.

**From here until Worktree teardown, work only in `$WORKTREE`.** Read and Edit `$WORKTREE/<repo-relative path>`, never `$REPO_ROOT/<path>`.

## Step 2: Analyze each item

Every pending item gets a **verdict object** — `preexisting`, `false-positive`, or `valid` — produced by the procedure in `${CLAUDE_PLUGIN_ROOT}/references/triage-checks.md` (Check 1: preexisting? Check 2: false positive? plus the verdict-object fields and the reproduce-before-you-record rule). Read that file once now. It is the single source of truth for both paths below, so the inline and workflow paths return the same shape and Step 3 never re-parses prose.

**3 or fewer items** — analyze inline: apply the procedure yourself to each item, including its reproduce-before-you-record rule (re-run the blame / re-`Read` the cited lines before recording any dismissal). Record one verdict object per item.

**More than 3 items** — call the **Workflow** tool. The user invoking this command _is_ the explicit opt-in Workflow requires, so don't ask again. Pass the shipped script, never a hand-written one:

- `script`: the exact contents of `${CLAUDE_PLUGIN_ROOT}/references/triage-workflow.js` — Read it and pass it verbatim, no edits or abridging. Don't use `scriptPath`: Workflow only loads paths under the working directory or an added directory, never the plugin root, even after you've Read the file.
- `args` (a real JSON object, not a string): `{ pr, headSha, baseSha, prCommits, reviewDir, worktree, checksPath, items }` — `pr` is the PR number; `headSha`, `baseSha`, and `prCommits` come from Step 0; `reviewDir` is `$REVIEW_DIR`; `worktree` is `$WORKTREE`; `checksPath` is `${CLAUDE_PLUGIN_ROOT}/references/triage-checks.md`; `items` is the parsed array from `$REVIEW_DIR/pending-items.json` (Read the file and pass its contents as an array — a stringified list makes the script throw on purpose).

The script pipelines the items through two stages: one analysis agent per item, forced by `schema` to return a verdict object, then — for every `preexisting` or `false-positive` verdict only — an independent verifier agent whose job is to _refute_ the dismissal by reproducing its cited evidence (re-running the blame at the head SHA, `Read`ing the quoted lines, re-querying Context7). A dismissal the verifier cannot reproduce comes back as `valid` with `overturned: "<original verdict>"` and the verifier's reason; one it can reproduce carries `verified: true`. `valid` verdicts are not re-verified — they are already the safe default, and the user gates them in Step 3.

The workflow runs in the background and the tool returns immediately. **Wait for its completion notification** — do not start Step 3 on partial results. The notification carries the script's return value, `{ verdicts }`, where `verdicts[i]` corresponds to `items[i]`. If the value is truncated, Read `journal.jsonl` in the workflow's transcript directory instead. Then:

- For any entry that is `null` or has `verdict: null` (its analysis agent was skipped or died), analyze that item inline with the same procedure. **Never drop an item** — N items in, N verdicts out is the point of the pipeline.
- Keep `verified` and `overturned` on the objects; Step 3 displays them.

**Agent preamble.** The script's `PREAMBLE` constant is this exact text; when editing either copy, edit both (the same rule the code-review agents' shared `## Environment` block follows). Step 1c's parsing agents also reuse it with the role line adjusted, which is why it stays in this file:

> You are a review-response analysis agent. Use the Read/Write tools and `rg` for searching, rather than `sed`/`awk`/`du` — those aren't in this skill's `allowed-tools`, so they prompt outside auto permission mode. Don't pipe untrusted data into `sh`/`bash`. Don't run `rm -rf` or destructive git operations without confirmation. Do NOT post anything to GitHub — only the main skill posts replies in Step 6.
>
> **Verify library claims with Context7.** When evaluating a reviewer's concern that hinges on a specific library, framework, or external API (React hooks, Prisma, Next.js routing, AWS SDK, etc.), verify the claim against current docs before deciding: call `mcp__plugin_context7_context7__resolve-library-id`, then `mcp__plugin_context7_context7__query-docs` with the returned ID. Use this to confirm a **false-positive** verdict (the library really does handle the concern) or a **valid** verdict (the library really does behave as the reviewer says). Skip Context7 for general programming patterns, project-internal logic, or anything verifiable from the diff alone — don't burn calls on claims that don't depend on external library behavior.

The checks themselves, the verdict-object fields, and the reproduce-before-you-record rule live in `${CLAUDE_PLUGIN_ROOT}/references/triage-checks.md` — not repeated here so the two paths cannot drift apart.

## Step 3: Present triage results to user

**Emit the table and the `AskUserQuestion` call in the SAME assistant message.** Rendering the summary is not the step — the step is the question. If your message ends after the table, the run stalls and the user has to prod you; a forward reference in your own prose ("asking below", "sending the prompt now") does not survive the message boundary. The shape is:

```text
[assistant message N]
  text:      the triage table below
  tool_use:  AskUserQuestion   ← same message, not the next one
```

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
    - **file.ts:7** (@reviewer): "Their comment..." → verifier could not reproduce the false-positive evidence: cited guard is not at handler.ts:88 → Plan: add the guard
  Review-body findings (@reviewer on review #12345):
    - "Their finding..." → Plan: add error boundary around the X call site
```

Omit a sub-section when it is empty (don't show an "Inline comments:" header with nothing under it). Omit the entire verdict category if its count is zero.

Mark each dismissal the workflow's verifier upheld (`verified: true`) with a `(verified)` suffix. A verdict the verifier overturned (`overturned` set) lands under **Valid Issues** with the note `→ verifier could not reproduce the <original verdict> evidence: <reason>`, so the user sees the flip and can use `Override some` to restore the original if they disagree. Inline-path dismissals carry no suffix — you reproduced them yourself, so there was no second reader.

In that same message, call the **AskUserQuestion** tool (`multiSelect: false`, header `Triage`) to confirm before proceeding, with three options:

- `Approve all` — "Proceed with every verdict as shown."
- `Override some` — "Change specific verdicts first — I'll ask which."
- `Cancel` — "Stop without replying to or fixing anything."

On `Approve all`, continue to Step 4. On `Override some` (or the user's Other-field reply), collect the overrides as free-text — e.g. "treat #3 as valid", "skip #5" — apply them to the verdict set, then continue. On `Cancel`, run Worktree teardown, then stop. Wait for the response before continuing.

## Step 4: Implement fixes for valid issues

For each item triaged as **valid** (and approved by the user):

1. Read `$WORKTREE/<path>` for the item's path. For review-body findings without an explicit path, use the verdict object's `evidence.file` and `fix_plan`.
2. Understand the reviewer's suggestion and the surrounding code context.
3. Plan the minimal, targeted fix that addresses the reviewer's concern without unnecessary refactoring.
4. Apply the fix using the Edit tool.
5. After editing, re-read the modified section to verify the fix is correct and doesn't break surrounding code.
6. Record what was changed (the file paths and a one-line description) for use in Step 5 (commit message) and Step 6 (reply body).

If multiple valid issues exist in the same file, apply them carefully to avoid conflicts. `Edit` matches on strings rather than line numbers, so edit order does not shift later matches — but if one fix rewrites text a later fix targets, re-`Read` that range before editing it again.

**Verification — after the last Edit, before Step 5:** Run the project's affected-project type-check, lint, and test (or equivalents) to confirm the fixes compile and pass. The specific commands depend on the project's tooling — pick the right targets for what you changed (e.g., `npx nx run-many -t check-types -p <projects>` for Nx, `pnpm typecheck && pnpm lint && pnpm test` for plain pnpm, `go test ./...` for Go). Issue **all three** as parallel Bash tool calls in a single assistant message — independent targets with no ordering dependency. If any fails, fix it before committing in Step 5; a commit pushed to the PR branch that turns out to break CI is worse than a slower run.

**Read the outcome from the exit status, not from a grep.** Capture each run to a file and branch on its exit code — `npx nx run-many -t lint -p <projects> > "$REVIEW_DIR/lint.log" 2>&1; echo "exit=$?"` — then `rg` the saved log for detail **only** when the exit code is nonzero. Do not pipe the command straight into `rg … | head -N`: in a pipeline the exit status you see belongs to `rg`, not the build, and the summary line ("`NX Successfully ran…`", "`Tests: N passed`") is routinely pushed past a `head` cutoff by warnings or by the app-under-test's own log output — at which point a passing run looks indeterminate and you re-run the whole target to find out. The saved log costs nothing and keeps the full output available if you do need it.

Only `Bash(npx nx *)` is pre-approved; other runners (`pnpm`, `go`, `make`, `cargo`) — and Worktree setup's install command — will prompt outside auto permission mode. That is expected — no static prefix list can cover every project's toolchain.

**Vague valid findings** — some review-body findings are valid concerns but too abstract to fix mechanically (e.g., "consider whether this module is doing too much"). For any item whose verdict object has `vague: true`, do **not** attempt a speculative edit. Mark it as `noted` (distinct from `fixed`) and surface it in the Step 6 reply as "noted — will address in a follow-up PR". Silently dropping these leaves the reviewer in the dark; an explicit acknowledgement keeps the conversation honest.

## Step 5: Commit and push fixes

If Step 4 applied zero edits (all items were preexisting / false-positive / vague-noted), **skip this step entirely** — there is nothing to publish — and proceed to Worktree teardown.

Otherwise, every "Fixed — …" reply in Step 6 must point at code that is already on the remote PR branch. Commit and push _before_ posting any reply. A reviewer who reads "Good catch! Fixed in commit `abc1234`" must be able to see that commit on the PR.

1. **Stage only the files Step 4 modified.** Use the file-path list recorded in Step 4 (not `git add -A` / `git add .`), so unrelated dirty working-tree changes are not swept into this commit. Pass each path explicitly: `git add -- PATH1 PATH2 …`.
2. **Detect the repo's commit-message style** once: `git log --pretty=%s -n 10`. If the recent subjects look like Conventional Commits (`type(scope): …`), match that style; otherwise use a plain imperative subject.
3. **Compose the commit message** via heredoc (so newlines survive). Default template (adapt the subject to the detected style):

   ```
   fix: address review feedback on PR #<NUMBER>

   - <one-line per fix, mirroring the Step 4 record (file:line — what changed)>

   Reviewers: @<reviewer1>, @<reviewer2>
   ```

   Do not skip pre-commit hooks (no `--no-verify`). Do not amend a previous commit — make a new commit, per the project's git rules.

4. **Commit:** `git commit -m "$(cat <<'EOF' … EOF)"`, in the background with output captured to `$REVIEW_DIR/commit.log` (see Tool discipline). If the commit fails because a pre-commit hook rewrote files (e.g., a formatter), re-`git add` the same paths and create a _new_ commit — never `--amend` to silence the hook.
5. **Push:** `git push origin HEAD:refs/heads/<headRefName>`. The worktree is on a detached HEAD, so there is no tracking branch for a bare `git push` — name the remote ref explicitly. If push fails with non-fast-forward, **stop and surface** — do not force-push, do not rebase silently. The user must resolve the divergence before the skill continues.
6. **Verify the push landed:** local `git rev-parse HEAD` must match `gh api repos/OWNER/REPO/git/refs/heads/<headRefName> --jq .object.sha`. If they differ, stop and surface — do not proceed to Step 6.
7. **Capture the short SHA** with `git rev-parse --short HEAD`. Step 6's "Fixed" template embeds this SHA so each reply points at a specific commit on the PR.
8. **Refresh the PR description.** Run `gh pr view $PR_NUMBER --json state,url,body,commits` — one call returns both the existing body to preserve and the branch's full commit list (`commits[].messageHeadline`), which now includes the fix commit just pushed. If `state` is not `OPEN`, skip this sub-step.
   - Regenerate **only** the `## Summary` section, and base it on **all** commits on the branch — not just the fix commit — so the description reflects the whole branch as it now stands. Preserve every other part of the body verbatim: the `## Test plan` section, any manually-added sections or notes, and the `🤖 Generated with [Claude Code](https://claude.com/claude-code)` footer. If the body has no `## Summary` heading, add one at the top without disturbing the rest.
   - Write the updated body to `$REVIEW_DIR/pr-body.md` with the Write tool, then apply it with `gh pr edit $PR_NUMBER --body-file $REVIEW_DIR/pr-body.md` — never inline `--body`, so multi-line markdown isn't mangled by shell quoting.
   - **This sub-step is non-fatal**, unlike every other stop in Step 5. A failed `gh pr view` or `gh pr edit` must **not** block Step 6: the fixes are already on the remote, so every reply is still truthful and a stale description is cosmetic. Record the failure for the Step 6 summary and continue.

Then run Worktree teardown.

## Worktree teardown

Runs after Step 5 (or in its place when Step 5 is skipped) and on the Step 3 `Cancel` exit.

1. Call **ExitWorktree** with `action: "keep"`. It never removes a worktree entered by `path`; this only returns the session to `$REPO_ROOT`.
2. Run `git worktree remove "$WORKTREE"` with `run_in_background: true` and go straight on to Step 6 — deleting the worktree's fresh `node_modules` takes ~20 s and nothing in Step 6 needs it gone. Read the removal's notification before writing Step 6's summary (on the `Cancel` exit, before stopping). If it refused because the tree is dirty, do not `--force`: show `git -C "$WORKTREE" status --short`, keep the worktree, and note its path in the summary. This is non-fatal — the fixes are already pushed.

**On any stop after Step 4's first Edit** (a check or hook failure you can't fix, a non-fast-forward push, a push-verify mismatch), the worktree holds unpublished work: do sub-step 1 only, never remove it, and report `$WORKTREE` plus `git -C "$WORKTREE" rev-parse --short HEAD` so the user can finish by hand. Removing a detached worktree discards its unpushed commits along with its reflog.

## Step 6: Post replies to all items

Inline comments get their own per-comment reply via the standard inline-reply endpoint. Review-body findings cannot be "replied to" in place — GitHub has no per-finding reply endpoint for review bodies — so they are **grouped by `review_id` and answered with one aggregated PR-level comment per review**. This matches the user intent of replying "in place where possible" (one response per external review) while keeping the PR conversation readable.

### Reply templates (per-item body)

Each of these templates produces the body for a single item. Inline replies post this directly. Aggregated review-body replies embed one of these under a per-finding heading (see "Review-body findings" below).

**Fill every placeholder from the verdict object, never from memory.** `COMMIT_SHA` and `DATE` are `evidence.blame_sha` and `evidence.blame_date`; `SPECIFIC_EVIDENCE` is `evidence.file:line` plus `evidence.quote`; `DESCRIPTION_OF_CHANGE` is the Step 4 record. A reply may not cite anything the verdict's evidence does not contain — Step 2 verified exactly those fields, and embellishing here would throw that guarantee away.

**When the item's reviewer is the current user** (a self-authored review), keep the same structure but drop the second-person flourishes: write `Fixed in commit …` instead of `Good catch! Fixed in commit …`, and open the aggregated body with `Re: my earlier review summary — addressing the findings below.` rather than @-mentioning yourself. Everything else, including the mandatory HTML-comment markers, is unchanged.

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
Good catch! Fixed in commit `<SHORT_SHA>` — DESCRIPTION_OF_CHANGE.

BRIEF_EXPLANATION of what was changed and why it addresses the concern.
```

`<SHORT_SHA>` is the value captured at the end of Step 5 (`git rev-parse --short HEAD` after the push). If Step 5 was skipped (no edits), this template is not used.

**For valid issues (noted, not fixed — vague finding from Step 4):**

```
Noted — this is a valid concern but broader than what I'll resolve inside this PR. I'll address it in a follow-up.

BRIEF_ACKNOWLEDGEMENT of the core concern so the reviewer knows we heard them.
```

### Batching the Write and POST phases

By this point in Step 6, every reply body is composed. There is no ordering dependency between writing one reply file and another, or between posting one reply and another (each `gh api ... replies` POST targets a distinct comment-reply resource; the aggregated review-body POST targets the issue-comments endpoint). To avoid serializing the wall-clock:

1. **Write all reply files in one parallel assistant message** — emit every `Write` tool call simultaneously (one assistant turn containing as many `Write` blocks as there are reply files: one per inline-comment reply, plus one per review-body aggregated reply).
2. **Then post all replies in one parallel assistant message** — emit every `gh api ... /comments/<id>/replies` POST and every `gh api ... /issues/<n>/comments` POST simultaneously. The per-call `--jq .id` check still localizes failures: a missing `id` in any one result identifies the failing call.

Do NOT interleave Write and POST calls in a single message — every Write must complete before the POST that reads its file. Two assistant messages: all Writes, then all POSTs.

### Posting process — inline comments

For each inline comment:

1. Write the reply body to a temp file using the Write tool (e.g., `$REVIEW_DIR/reply-COMMENT_ID.md`).
2. Post using: `gh api repos/OWNER/REPO/pulls/NUMBER/comments/COMMENT_ID/replies -F body=@$REVIEW_DIR/reply-COMMENT_ID.md`
3. Verify the response includes a valid `id` field confirming the reply was created.

### Posting process — review-body findings

Group all non-dropped review-body findings by `review_id`. For each group with at least one finding:

1. Compose one aggregated reply body in a temp file `$REVIEW_DIR/reply-review-<REVIEW_ID>.md` using the Write tool. Structure:

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

2. Post using: `gh api repos/OWNER/REPO/issues/NUMBER/comments -F body=@$REVIEW_DIR/reply-review-<REVIEW_ID>.md`
   (Note: `issues`, not `pulls` — PR-level comments live on the issues endpoint. This is correct and intentional.)
3. Verify the response includes a valid `id` field confirming the comment was created.

### Summary

After all replies are posted, display a summary:

```
## Done — PR #NUMBER

- Replied to N inline comments + M review-body findings across K reviews
  (X preexisting, Y false positives, Z fixes applied, W noted for follow-up)
- Files modified: list of files changed
- PR description: Summary section refreshed | not refreshed (<reason>)
```

Include the PR-description line only when Step 5 ran (it is skipped wholesale when there were no edits). State `not refreshed` with the reason when Step 5.8 skipped or failed — a closed/merged PR, or a `gh` error. Add `- Worktree: kept at <path> (<reason>)` only when Worktree teardown refused to remove it.

## Step 7: Emit the completion marker (always the last line)

On **every** exit path, your final message must end with exactly one completion-marker line so the agent-view job classifier resolves this run to a definite state instead of leaving it stale at "awaiting input". The classifier reads only your latest message's text and matches a literal lowercase line-prefix — `result:` (run finished) or `failed:` (fatal stop, could not complete).

- It must be the **last line** of your final message, at the start of its own line, plain text — no backticks, bold, tag, or emoji. `Done` / `✅` do **not** count; only the literal token flips the badge.
- The text after the token is a **self-contained one-line headline** — readable by someone who never saw the request. Good: `result: PR #1551 — replied to 1 inline comment; fix 45a30c0 pushed`. Weak: `result: done`.
- Covers the early exits too: "No unaddressed review items found" (Step 1e) → `result: PR #N — no unaddressed review items`; `Cancel` at triage (Step 3) → `result: PR #N — triage cancelled, nothing posted`; a fork PR (Step 0) / Worktree setup failure / non-fast-forward push / push-verify-mismatch stop (Step 5) → `failed: PR #N — <what blocked it>`, naming the kept worktree path when Worktree teardown left one behind.
- If a later turn would otherwise replace this message (e.g. an auto-continue prompt), **re-emit the marker** so the finished state isn't downgraded.
