---
name: pr-review-v2
description: Multi-specialist code review of a pull request, run as a native Claude Code Workflow. Parallel specialists (security, quality, errors, perf, plus conditional typescript/react/infra/claude-md) scan the diff, a skeptic agent tries to refute each finding, a consolidator merges duplicates, and the result posts as inline review comments via gh.
argument-hint: <pr-number> [--auto|--dry-run]
disable-model-invocation: true
allowed-tools: Bash, Read, Workflow, AskUserQuestion
---

# /pr-review-v2 — orchestrate a workflow-driven PR review

You are the orchestrator for /pr-review-v2. Execute the numbered steps below in order. Report progress with one short line per step (e.g. `[1/5] Fetching PR #42…`). Surface every command failure verbatim and stop — do not invent workarounds.

The review itself runs as the plugin's Workflow script: it picks the specialists, runs them, verifies and consolidates their findings, and renders the GitHub review. Your job is the I/O around it: fetch, launch, show the summary, ask, post, clean up.

A rejected `Workflow` call (`"user doesn't want to proceed"` or `"tool use was rejected"`) is a fatal stop: report it and jump to **Cleanup**. One exception: when a new user message or slash command arrives at the same instant, the user typed ahead and cancelled the open permission prompt. That is a request for attention, not a denial. Answer what they typed, then re-issue the same call.

Parse `$ARGUMENTS` as whitespace-separated tokens:

- `PR_NUMBER`: the first positive-integer token. If there is none, report the error and stop.
- `POST_MODE`: `auto` if a `--auto` token is present (post without prompting; for CI and unattended runs), `dry-run` if `--dry-run` is present (review but never post), otherwise `interactive`. If both flags are present, `--dry-run` wins.

---

## [1/5] Fetch the PR and prepare the workflow args: one Bash call

Report `[1/5] Fetching PR #<N>…` and run exactly this:

```bash
bash "${CLAUDE_SKILL_DIR}/scripts/prepare.sh" "$PR_NUMBER" "${CLAUDE_SKILL_DIR}/references/rubric.md"
```

It creates the scratch dir and prints `TMP=…` first, so Cleanup has it on every exit path. It also sweeps leftovers of earlier runs that are at least 48 hours old. Then it runs `gh pr view`, `gh pr diff`, `git fetch` of the PR head into `refs/pr-review-v2/pr-<N>`, and the GraphQL review-threads query concurrently, and prints shell-safe lines:

```
TMP=…   HEAD_SHA=…   OWNER=…   REPO=…   CHANGED_FILES=…   PRIOR_ISSUES=…
ARGS={…}
```

Read every value you need later from this output; never re-derive one with a separate call. Don't run `gh`, `git` or `jq` around it, and don't inspect the files under `$TMP`.

- **Non-zero exit**: surface stderr verbatim and stop (run Cleanup when `TMP=` was printed). This includes `PR #N is <state> — aborting` for a PR that is not open.
- **`warning:` lines on stderr**: mention them in one line each and continue. A failed `git fetch` means specialists can read the PR head only if this clone already has it. Unavailable review threads mean prior-review dedup does nothing. A diff over GitHub's 20,000-line limit is rebuilt locally from the fetched ref.

## [2/5] Launch the review workflow

Report `[2/5] Reviewing PR #<N> (<CHANGED_FILES> changed files)…` and call the **Workflow** tool. Invoking this skill is the explicit opt-in that Workflow requires, so don't ask for confirmation again.

- `name`: `"pr-review-v2:review"`, the workflow this plugin ships. If the tool doesn't know that name (an older Claude Code build), pass `script` instead: the exact contents of `${CLAUDE_PLUGIN_ROOT}/workflows/review.js`, Read and passed verbatim. Never `scriptPath`: Workflow only loads paths under the working directory or an added directory.
- `args`: the JSON after `ARGS=`, copied exactly, as a JSON object, not a string.

Then post one line, `Reviewing PR #<N> in the background; watch or stop it in /workflows.`, and **end the turn**. The workflow runs every agent itself and notifies you when it finishes; don't poll it.

## [3/5] Show the summary

Wait for the completion notification. If the workflow failed or was stopped (including a `self-check failed` or `needs args` error), report the error in one line, run Cleanup, and end with a `failed:` marker.

Otherwise run, as one Bash call, with the notification's `<output-file>` path:

```bash
bash "${CLAUDE_SKILL_DIR}/scripts/payload.sh" "<output-file>" "$TMP"
```

It writes `payload.json`, `payload-pending.json`, `payload-body.json` and `fallback.md` under `$TMP`, then prints the review summary. **Paste that summary verbatim into your reply, in a code block, before anything else**: Bash output is not reliably shown to the user. Don't Read or parse the output file any other way, and don't describe the findings from the notification's return value, which may be truncated.

What the summary's blocks mean (none of them is posted to GitHub):

- **Missing**: a specialist that died, was skipped in /workflows, or failed in a way the workflow couldn't recover. Its domain went unreviewed.
- **Invalid findings**: findings that failed validation (for example `startLine` past `line`), with the reason.
- **Reconciled (below gate)**: findings under the confidence gate, with the specialist's rationale.
- **Refuted by verifier**: findings a skeptic agent disproved against the PR head, with its evidence.
- **Deduped**: findings folded into another, naming the survivor and the step that merged them (`positional` or `consolidator`).
- **Potential conflicts**: two findings that give opposite advice about the same code. Both still post. Read the pair before answering the post prompt.
- **Verified**: how the surviving findings fared: `uncertain` means the skeptic could neither confirm nor refute, and `unverified` means it didn't run.

**The `Accounted:` line is the conservation check**: every finding a specialist returned lands in exactly one bucket. Given a clean line, the summary is the complete account of what will and won't be posted. Don't re-derive or second-guess any drop. If it reads `⚠ ACCOUNTING MISMATCH`, that is a bug in `workflows/review.js`: say so in one line, naming the orphan IDs it lists, and that each specialist's raw output stays in the workflow's journal (open the run in /workflows). Then continue: the findings that did reconcile are still valid.

## [4/5] Confirm

Branch on `POST_MODE`:

- `dry-run`: don't post and don't prompt. Report the would-post counts (inline + summary-only) and go to Cleanup.
- `auto`: post without prompting; go to [5/5] as if the user chose `Post all`.
- `interactive`: call **AskUserQuestion**. While the dialog is open it may be all the user sees, so the question must stand on its own: a counts line (`Post N inline + M summary-only finding(s) to PR #<PR_NUMBER>?`), then one line per inline finding copied from the summary (`<id> <severity> <file>:<line> — <description>`), at most 5, then `… and K more` past that. If the summary lists potential conflicts, add a line naming each pair. Use one question (`multiSelect: false`) with header `Post review` and two options:
  - `Post all`: "Post every eligible finding as inline comments plus the review summary."
  - `Skip`: "Skip posting and proceed to cleanup."

Map the answer:

- `Post all`, or an empty or affirmative reply → [5/5].
- `Skip`, a negative reply, or a dismissed or rejected question → skip posting and go to Cleanup. This is not a fatal stop: the run ends with `result: … posting skipped`.
- A comma-separated list of finding IDs in the free-text field (e.g. `security-1,perf-2`) → rerun `payload.sh` with that list as a third argument, which rewrites the four payload files for just that subset. Then go to [5/5]. If it reports an unknown ID, show its message (it lists the valid IDs) and ask once more.

## [5/5] Post

First re-confirm the PR is still open, since it may have been merged or closed while the workflow ran:

```bash
gh pr view "$PR_NUMBER" --json state --jq '.state'
```

If it is not `OPEN`, report `PR #<N> is now <state> — skipping post` and go to Cleanup. Otherwise post through a three-tier fallback.

**Tier 1 — single-shot review with batched comments.** Run this exact block as **one** Bash call. Shell variables don't survive between Bash calls, so a split check would read an empty `$TIER1_RC` and misreport a successful post as a failure:

```bash
TIER1_ERR=$(gh api "repos/$OWNER/$REPO/pulls/$PR_NUMBER/reviews" --method POST --input "$TMP/payload.json" 2>&1 > /dev/null)
TIER1_RC=$?
printf '%s' "$TIER1_ERR" > "$TMP/tier1.err"
if [ "$TIER1_RC" -eq 0 ]; then
  echo "posted via tier 1"
elif printf '%s' "$TIER1_ERR" | grep -q 'HTTP 422'; then
  echo "tier 1 hit HTTP 422 — falling to tier 2"
else
  echo "tier 1 failed (rc=$TIER1_RC): $TIER1_ERR"
fi
```

Branch on the echoed line, never by reading `tier1.err`. `posted via tier 1` → Cleanup. Otherwise Read [references/posting-fallback.md](references/posting-fallback.md) and continue there: at tier 2 after `HTTP 422`, at tier 3 after any other failure.

---

## Cleanup

Run Cleanup on every path that reaches it: after [5/5], after `Skip`, after a dry run, and after any fatal stop. Skip it only when `$TMP` is unset, because the stop came before [1/5] printed it. Run both commands as one Bash call; the guard removes only a `pr-review-v2-*` directory directly under the temp root, and the ref deletion may fail harmlessly when the fetch never ran:

```bash
case "$TMP" in
  "${TMPDIR:-/tmp}"/pr-review-v2-*) rm -rf "$TMP" ;;
  *) echo "refusing to remove $TMP (not a pr-review-v2 dir under ${TMPDIR:-/tmp})" ;;
esac
git update-ref -d "refs/pr-review-v2/pr-$PR_NUMBER" 2> /dev/null || true
```

Cleanup is a turn you take, so a run that dies without giving you one (Claude Code exiting, context exhaustion, the machine sleeping) leaves its dir and ref behind. The next run's `prepare.sh` sweep reaps them once they are 48 hours old.

On normal completion report `[5/5] Done.`. On a fatal stop report which step failed (e.g. `Stopped at [2/5]: Workflow call rejected. Cleanup complete.`).

## Completion marker (always the last line)

On every exit path that gives you a turn, your final message must end with exactly one completion-marker line, so the agent-view job classifier resolves the run to a definite state. Put it at the start of its own line, in plain text with no backticks, bold or emoji. `[5/5] Done.` does not count. Make the text after the token a self-contained one-line headline:

- Normal completion: `result: PR #N reviewed — <posted N inline + M summary via tier T | dry-run: would post N+M | posting skipped>`
- Fatal stop: `failed: pr-review-v2 stopped at [step] — <reason>`

The [2/5] turn ends with its one-line status and no marker: the run isn't finished, and the completion notification brings you back. If a later turn would replace your final message, emit the marker again.
