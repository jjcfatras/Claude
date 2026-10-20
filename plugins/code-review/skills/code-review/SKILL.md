---
name: code-review
description: Multi-specialist code review of a pull request. Spawns parallel subagents (security, quality, errors, perf, plus conditional typescript/react/infra), consolidates findings via the bundled Go helper, and posts inline review comments via gh.
argument-hint: <pr-number> [--auto|--dry-run]
disable-model-invocation: true
model: opus
effort: medium
allowed-tools: Bash, Read, Write, Grep, Glob, Agent, AskUserQuestion, ListAgents
---

# /code-review — orchestrate a multi-specialist PR review

You are the orchestrator for /code-review. Execute the numbered steps below in order. Report progress with one short line per step (e.g. `[1/5] Fetching PR #42…`). Surface every command failure verbatim and stop — do not invent workarounds.

A "stop" includes harness-side Agent rejections. All specialists are spawned together in a single message (see [3/5]), so a rejection means the harness denied one block of an already-issued batch — it is **not** a cue to issue the remaining specialists one at a time. If any `Agent` call returns a message containing `"user doesn't want to proceed"` or `"tool use was rejected"`, treat it as a fatal stop: do not retry, abandon the finalize/post phase. Report which subagent was denied, then jump to **Cleanup**.

**One carve-out: an interrupt is not a denial.** The harness emits that same `"user doesn't want to proceed"` text when the user cancels an _open_ permission prompt by typing ahead — queuing a message or a slash command while the prompt is pending — which is a request for attention, not a verdict on the tool call. Tell them apart by what arrives with it: a genuine denial stands alone, while an interrupt is accompanied by `[Request interrupted by user…]` and/or a new user turn at effectively the same instant. On an interrupt, do not fatally abort a healthy run — say which call was cancelled, answer whatever the user typed, and re-issue that one call. (On transcript `d67f09e0`, `/transcript` typed at 13:58:52.487 cancelled an open prompt and produced this exact string. It landed on a `SendMessage` and cost only a re-invocation; on an `Agent` block the rule above would have killed a working review over a keystroke.)

All agents in this plugin are namespaced under `code-review:` — use the fully-qualified form for every `subagent_type` value (`code-review:security`, `code-review:quality`, etc.). The unqualified bare names are not registered and will fail with "Agent type not found".

The user passes the PR number as `$ARGUMENTS`, optionally followed by a posting-mode flag. Parse `$ARGUMENTS` as whitespace-separated tokens:

- The first positive-integer token is `PR_NUMBER`. If no such token exists, report the error and stop.
- `POST_MODE` — `auto` if a `--auto` token is present (post all eligible findings without prompting; for CI / unattended runs), `dry-run` if `--dry-run` is present (run the full review but never post), otherwise `interactive` (the default — prompt before posting). If both flags are present, `--dry-run` wins.

## Variables to derive at startup

Resolve once and reuse:

- `PR_NUMBER` — from `$ARGUMENTS` (first positive-integer token, per the parse rule above).
- `POST_MODE` — `auto` / `dry-run` / `interactive`, per the parse rule above.
- `TMP` — scratch dir at `${TMPDIR:-/tmp}/pr-review-${PR_NUMBER}-$(date +%s)`, built inline in the [1/5] command; read it back from that call's first stdout line, `TMP=…`.
- `REPO_ROOT` — `git rev-parse --show-toplevel`, resolved inline in the same command; nothing later needs it.
- `HEAD_SHA`, `OWNER`, `REPO` — printed by that same `prepare` call; never re-derive them with a separate call.
- `HELPER` — `${CLAUDE_PLUGIN_ROOT}/bin/code-review-helper`.
- `RUBRIC` — `${CLAUDE_PLUGIN_ROOT}/references/code-review-rubrics.md`.

All subsequent paths derive from `$TMP`. No path uses cwd.

---

## [1/5]+[2/5] Fetch the PR and prepare every pre-spawn artifact — ONE helper call

Report `[1/5] Fetching PR #<N>…`, run exactly this as **one** Bash call, then report `[2/5] Prepared — roster: …`:

```bash
"$HELPER" prepare --fetch --pr "$PR_NUMBER" \
  --review-tmpdir "${TMPDIR:-/tmp}/pr-review-${PR_NUMBER}-$(date +%s)" \
  --repo-root "$(git rev-parse --show-toplevel)" \
  --rubric "$RUBRIC"
```

That is both steps. Don't run `gh`, `git fetch` or `jq` yourself around it. This used to be four model-typed Bash turns, and the three "parallel" fetches ran serially anyway: the harness only runs read-only Bash concurrently, and all three write files. On transcript `dfac0a80` that cost 27 s before `prepare` even started. `--fetch` moves all of it into the helper:

1. **Scratch dir.** Creates `$TMP/findings` and prints `TMP=…` **first**, before anything can fail, so Cleanup has `$TMP` on every exit path. The path is built by the shell above, byte-for-byte the form Cleanup's guard re-expands.
2. **Sweep the last run's corpse.** This is the other half of **Cleanup**. Cleanup is a turn _this_ orchestrator takes, so a run that dies never reaches it; having each run bury the previous one's corpse bounds the leak at one. The sweep is best-effort and silent, and deliberately conservative:
   - It removes `pr-review-*` dirs directly under the temp root that are at least 48 h old (`find -mtime +1` semantics), so it cannot touch a review started today or yesterday.
   - It deletes a `refs/code-review/pr-<n>` ref only when **no** `pr-review-<n>-*` dir survives for that PR, meaning no review of it is in flight, and never this PR's own ref.
3. **Fetch, concurrently.**
   - `gh pr view` → `pr-meta.json`
   - `gh pr diff` → `pr-<N>.diff`
   - `git fetch origin +pull/<N>/head:refs/code-review/pr-<N>`
   - Then, once `pr-meta.json` has the owner and repo, the GraphQL `reviewThreads` query → `review-threads.json`.

**What fails how:**

- **`gh pr view` or `gh pr diff` failure** — a non-zero exit carrying the command's stderr. Surface it verbatim and stop (run Cleanup; `TMP=` is already on stdout).
- **`git fetch` failure** — only a `warning:` line on stderr; continue. This is an explicit exception to the surface-and-stop rule at the top of this file. The hidden ref is what lets `git show $HEAD_SHA:<path>` and `git grep <sym> $HEAD_SHA` resolve on a stale clone or a fork PR. Without it, specialists fall back to the bundle's embedded source, and line confirmation against `$HEAD_SHA` may degrade.
- **GraphQL failure** — only a `warning:` line; no `review-threads.json` is written, `prepare` treats that as "no prior review", and prior-review dedup simply does nothing.
  - Why GraphQL: `reviewThreads` gives thread-level state (`isResolved`, `isOutdated`) and every reply, which is how a PR author's dismissal of a finding as a false positive is detected. The REST review-comments endpoint exposes none of that.
  - Its first page is 50 threads × 50 comments; a PR that exceeds it needs a cursor-paginated loop in the helper.

Then `prepare` reads `pr-meta.json`, `pr-$PR_NUMBER.diff` and `review-threads.json` from `$TMP` and writes all eight downstream artifacts: `changed-files.json`, `valid-lines.json`, `claude-md-files.json`, `roster.json`, `prior-issues.json`, `spawn-context.md`, `rubric.md`, `spawn-manifest.json`. It also creates two directories the specialists use: `head/<HEAD_SHA>/` — where a changed file too large for the bundle's caps is written verbatim, so the Source index can hand every specialist one local path instead of six `git show` calls, and where `helper show` caches anything they ask for outside the changed set — and `scratch/`, for their own working copies. Both live under `$TMP` so Cleanup already covers them; that is the point, since specialist scratch written to bare `/tmp` is unreachable by any cleanup this skill runs. It replaces six separate calls (two `jq` projections plus `diff`, `roster`, `bundle-context`, `spawn-manifest`) whose outputs it reproduces byte for byte.

It prints shell-safe `KEY=value` lines to stdout — this is where you get the values later steps need, so read them from the tool result rather than re-deriving any of them:

```
TMP=…
HEAD_SHA=…   OWNER=…   REPO=…   PR_NUMBER=…   PR_STATE=…   PR_AUTHOR=…
ROSTER=security,quality,errors,perf,…          CHANGED_FILES=…   PRIOR_ISSUES=…
```

**Eligibility guard.** `prepare` exits non-zero with `PR #N is <state> — aborting` before deriving any artifact when the PR is not `OPEN` — reviewing a merged/closed PR wastes the entire specialist budget and risks posting to a finished thread. On that error, report it and stop (run Cleanup). A second, immediately-before-posting re-check lives at the top of step [5/5] to catch a PR that changed state mid-review. (The guard fires after the fetch rather than before the GraphQL call, so a closed PR costs one wasted API call — the tradeoff for having exactly one place that decides eligibility.)

Which specialists the roster patterns select is `prepare`'s job, not yours — `internal/roster/roster.go` owns them and `ROSTER=` on stdout is the answer.

**Do not inspect the artifacts.** `ROSTER=` already tells you which specialists you'll spawn, and the only file you need to open is `spawn-manifest.json` in [3/5]. If you nevertheless must look at one, note the shapes are _not_ uniform — `changed-files.json`, `roster.json` and `claude-md-files.json` are flat arrays of strings (`jq -r '.[]'`, never `.[].path`); the rest are objects. Use `jq` or a `Read` call rather than a bare `cat <file>`. And keep a disposable inspection in its **own** Bash call: never `&&` it onto a command that writes state, or the inspection's exit code masks whether the real work succeeded. (That rule holds for every step in this file, not just this one.)

---

## [3/5] Spawn all roster specialists in ONE message

Read `$TMP/spawn-manifest.json` in a **single** `Read` call (or, if you must use the shell, one `jq -c '.[]'` invocation). Do not inspect it field-by-field across multiple Bash calls — no per-entry `jq '.[N].prompt'`, no `subagent_type` listing, no `head` preview. The manifest is forwarded verbatim, so printing individual fields first buys nothing and adds a round-trip per entry. It contains one object per roster entry, each with three pre-rendered fields: `subagent_type`, `description`, `prompt`. Emit one `Agent` `tool_use` block per entry, **all as sibling blocks in this single assistant message**. Forward each object's three fields verbatim — do not modify, truncate, summarize, or skip any entry. The manifest is the ground truth; if it has N entries your message must contain N `Agent` blocks.

**Do NOT emit one `Agent` block, wait for its result, then emit the next.** That runs the specialists sequentially and is the exact bug this step exists to prevent. Every block belongs in THIS one message — the harness then runs them concurrently. The manifest is not a to-do list to work through turn by turn; it is the complete set of sibling blocks for a single message.

Schematic — every manifest entry becomes one sibling `Agent` block inside the same message (not one block per turn):

```
<single assistant message containing one Agent block per manifest entry>
  Agent(subagent_type=…, description=…, prompt=…)   ← one manifest entry
  Agent(subagent_type=…, description=…, prompt=…)   ← another manifest entry
  … one more Agent block for every remaining entry, all in this same message …
</single assistant message>
```

On harness rejection: see top-of-file stop rule. Because the whole batch is issued at once, a denial of any one block aborts the run — any findings files already on disk are abandoned, not posted. Skip the finalize/post phase and jump to Cleanup.

### In flight, and resuming after an interruption

**These `Agent` calls are asynchronous** (as of CC 2.1.246; agents have been background-by-default since v2.1.198). Each returns the moment it is accepted, _not_ when its specialist finishes — the tool result says so verbatim ("The agent is working in the background. You will be notified automatically"). The specialists then run concurrently and report **individually**, as task notifications arriving over the following minutes. So:

- **End your turn after spawning.** Do not treat the `Agent` tool results as findings, and do not check `$TMP/findings/` in the same turn — at that instant it is empty.
- **Do not poll.** The notifications arrive on their own; a `ListAgents` loop spends a full model turn each pass to re-learn what is already coming for free.
- **Verify once every roster role has reported** — each role's findings file at `$TMP/findings/<role>.json`. Missing files are surfaced as `missing_roles` by finalize; don't retry them.

**If you find yourself resumed mid-review** — an unexpected `Continue from where you left off`, or any turn where you cannot tell what landed (the orchestrator process can die while specialists are in flight) — run exactly this check, in this order, and then proceed:

1. `ls -la "$TMP/findings/"` — **disk first.** It is the only durable record.
2. `ListAgents` — which specialists are still alive.
3. **A valid findings file on disk outranks a `status: failed` task-notification.** A process exit invalidates the harness's in-process view, not the bytes a specialist already wrote. On transcript `d67f09e0` the `claude-md` notification said "did not complete… its in-process state was lost" while `findings/claude-md.json` had been on disk, valid and complete, for 71 seconds. Treat that role as **done** and do not re-spawn it — a re-spawn races a second write against a file the rubric declares immutable once written.

A role with **no** findings file and **no** live agent is a `missing_role`, full stop. Do not nudge it back to life: no `SendMessage`, no re-spawn, no `AskUserQuestion` asking whether to retry — that is the "invent workarounds" this file's first paragraph forbids, and it burns turns the run does not have. Continue to [4/5] with whatever landed; a partial roster still produces a useful review, and `missing_roles` reports the gap honestly. Only if _nothing_ landed, stop and jump to Cleanup.

**One carve-out: finished, but its file was refused.** A specialist whose findings Write is refused follows the rubric's step 4 failure protocol. Its report starts with a `FINDINGS_NOT_WRITTEN: <reason>` line, followed by its findings JSON. That role is not dead: its scan is complete, and the result is in your context.

The refusal is still a block you must not route around on your own. Never Write that JSON unasked: the harness tells you to refuse a subagent's request to perform its denied action and to surface it to the user instead. So:

- **`interactive`** — in the turn the report arrives, call AskUserQuestion once for that role.
  - Header: `Save results`.
  - Question: "<role> finished (<N> findings) but its findings file couldn't be saved — <reason>. Save it from <role>'s report?"
  - Options: `Save` / `Treat as missing`.
  - This is not the retry question banned above. Nothing re-runs; the answer only decides whether a finished result is kept, and the decision is the user's.
  - On `Save`, Write the JSON block verbatim to `$TMP/findings/<role>.json`. That Write gets its own permission check.
  - If that Write fails too, the role is a `missing_role`. Don't ask again.
- **`auto` / `dry-run`** — don't prompt. The role is a `missing_role`. In your [4/5] report, say it "finished, not saved (<reason>)" so a clean scan doesn't read as a failed one.

(On transcript `dfac0a80` quality's Write got an auto-mode classifier "no verdict", while infra's and perf's identical writes to the same dir passed. With no path for this case, the orchestrator could only offer a copy-paste shell line, and the user abandoned the run.)

---

## [4/5] Finalize and confirm

Run the helper's finalize pipeline (dedup → gate → snap → render) as **one** Bash call — `ROSTER_CSV` is derived inline, not in a turn of its own:

```bash
ROSTER_CSV=$(jq -r 'join(",")' "$TMP/roster.json")
"$HELPER" finalize --diff "$TMP/pr-$PR_NUMBER.diff" --findings-dir "$TMP/findings" --prior-issues "$TMP/prior-issues.json" --head-sha "$HEAD_SHA" --owner "$OWNER" --repo "$REPO" --pr-number "$PR_NUMBER" --expected-roles "$ROSTER_CSV" --out-consolidated "$TMP/consolidated.json" --out-payload "$TMP/payload.json" --out-pending-payload "$TMP/payload-pending.json" --out-body "$TMP/payload-body.json" --out-fallback "$TMP/fallback.md"
```

Read `$TMP/consolidated.json` with a single Bash call (use one combined `jq` invocation — do not issue separate jq reads per field) and display the summary to the user:

```bash
jq -r '
  "=== Review summary ===",
  "  Specialists: \(.specialists_used | join(", "))",
  (if (.budget_exhausted_roles // []) | length > 0 then "  Budget exhausted (self-reported partial scan): \(.budget_exhausted_roles | join(", "))" else empty end),
  (if (.missing_roles // []) | length > 0 then "  Missing: \(.missing_roles | join(", "))" else empty end),
  (if (.unreadable_roles // []) | length > 0 then "  Unreadable: \(.unreadable_roles | join(", "))" else empty end),
  (if (.invalid_findings // []) | length > 0 then "  Invalid findings (\(.invalid_findings | length)):" else empty end),
  ((.invalid_findings // [])[] | "    [\(.role)/\(.id)] \(.reason)"),
  (if (.unknown_categories // []) | length > 0 then "  Off-vocabulary categories (\(.unknown_categories | length)) — posted, but they match nothing in category-based dedup:" else empty end),
  ((.unknown_categories // [])[] | "    [\(.role)/\(.id)] \(.category)"),
  (if (.potential_conflicts // []) | length > 0 then "  ⚠ Potential conflicts (\(.potential_conflicts | length)) — each pair anchors near the other and discusses its subject; confirm they do not give opposite advice before posting:" else empty end),
  ((.potential_conflicts // [])[] | "    [\(.id_a)] \(.file):\(.line_a) ↔ [\(.id_b)] \(.file):\(.line_b) — both concern `\(.identifier)`"),
  (if (.dropped_prior_review // []) | length > 0 then "  Dropped (prior review): \(.dropped_prior_review | length)" else empty end),
  (if (.dropped_by_gate // []) | length > 0 then "  Reconciled (below gate): \(.dropped_by_gate | length)" else empty end),
  ((.dropped_by_gate // [])[] | "    [\(.id)] \(.severity) conf=\(.confidence) \(.file):\(.line) — \(.rationale)"),
  (if (.deduped // []) | length > 0 then "  Deduped (merged into another finding): \(.deduped | length)" else empty end),
  ((.deduped // [])[] | "    [\(.id)] \(.severity) conf=\(.confidence) \(.file):\(.line) → merged into \(.merged_into) by \(.merged_by) — \(.rationale)"),
  "  Inline eligible: \(.inline_eligible | length)",
  "  Summary only: \(.summary_only | length)",
  (if .accounting.ok then "  Accounted: \(.accounting.accounted) of \(.accounting.loaded) findings"
   else "  ⚠ ACCOUNTING MISMATCH: \(.accounting.accounted) of \(.accounting.loaded) findings accounted for — orphans: \(.accounting.orphans | join(", "))" end),
  "",
  ((.inline_eligible // [])[] | "  [\(.id)] \(.severity) \(.file):\(.line) — \(.rationale)"),
  ((.summary_only // [])[]    | "  [\(.id)] \(.severity) (summary) \(.file):\(.line) — \(.rationale)")
' "$TMP/consolidated.json"
```

The Invalid-findings block lists each dropped finding's role, id, and reason so the user can see what was lost (e.g., a finding with `line: 0` that the helper rejected). The Reconciled block shows findings the confidence gate filtered out, each with the specialist's one-sentence rationale. The Deduped block shows findings that a dedup pass folded into another, naming both sides. None of the three is posted to GitHub; without them, drops are silent.

Two of the remaining lines report on findings that **are** being posted rather than dropped:

- **Off-vocabulary categories** — the finding still posts, but its `category` is outside the closed vocabulary in `references/code-review-rubrics.md` § "Category vocabulary", so it silently matches nothing in category-based dedup. A recurring value here means an agent file's guidance has drifted from the rubric.
- **Potential conflicts** — two surviving findings anchored within five lines of each other where one's prose discusses the other's subject. This is a prompt to look, not a verdict: specialists scan independently with no cross-agent verification, so two of them can reach opposite conclusions about one symbol and both pass the gate (on PR #1784 one specialist flagged a variable's missing default as the defect while two others cited that same missing default as the correct pattern — all three posted). Read the pair before answering the post prompt; if they disagree, type the finding IDs you want into the prompt's free-text field rather than posting all.

**`budget_exhausted_roles` is a self-report, not an enforced deadline.** Nothing in this plugin bounds a specialist's runtime — there is no `maxTurns` on any agent and no orchestrator wall-clock check, and since the specialists run asynchronously in the background ([3/5]) nothing forces a slow one to finish either. A role only lands in this bucket by writing a findings file that says it stopped early, which means it was alive to write it. A specialist that hangs, dies, or was still running when you reached this step produces no file at all and appears in **`missing_roles`**.

**The `Accounted:` line is the conservation check.** `finalize` requires every finding a specialist wrote to land in exactly one bucket — inline, summary-only, dropped-prior, reconciled, deduped, or invalid. When the totals match, the summary above is provably the complete account of this review. When they don't, the line reads `⚠ ACCOUNTING MISMATCH` and names the orphaned IDs: findings that entered the pipeline and left no trace. That is a helper bug, not something to work around — follow the **Accounting mismatch** procedure below.

**Given a clean `Accounted:` line, the rendered summary is the complete, authoritative account of what will and will not be posted.** Every drop carries its own rationale in the blocks above. Do **not** re-read `consolidated.json`, the raw `findings/*.json`, `valid-lines.json`, or the helper's Go source to re-derive or second-guess a drop/keep decision — the reconciliation already proves nothing went missing, so those reads produce nothing the summary didn't surface. Do **not** edit any findings file or re-run `finalize`, except via the `--include-finding-ids` subset path in step [5/5]. Proceed directly to the `POST_MODE` branch.

### Accounting mismatch

Only when the `Accounted:` line reports a mismatch. The raw `findings/*.json` files are then the **only** surviving copy of each orphaned finding, so preserving them comes before anything else.

1. Report the gap in **at most 10 lines**, in exactly this shape — no prose beyond it, no finding bodies reproduced, no teaching asides. The bound matters: this message is the user's only notice, and an unbounded explanation buries it.

   ```
   ⚠ Accounting gap: N finding(s) entered the pipeline and appear in no bucket.
     <role>-<id> | <severity> conf=<n> | <file>:<line> | <one-line rationale>
     … one line per orphan …
   Raw findings preserved at $TMP/findings/ (Cleanup skipped for this run).
   Re-run by hand or file this against the helper; remove the dir with: rm -rf "$TMP"
   ```

   Take each orphan's severity, confidence, file, line and rationale from the specialist's own `findings/<role>.json` with this exact `jq`, in one Bash call. Pass the orphan IDs the summary printed. This is the single exception to the do-not-re-read rule above, and it exists because the summary provably does _not_ have this information:

   ```bash
   for f in "$TMP/findings"/*.json; do
     jq -r --argjson orphans '["<id>","<id>"]' '
       .specialist as $role | .findings[]
       | select(($role + "-" + (.id | sub("^f-";"") | sub("^" + $role + "-";""))) as $ns | $orphans | index($ns))
       | "  \($role)-\(.id) | \(.severity) conf=\(.confidence) | \(.file):\(.line) | \(.rationale)"
     ' "$f" 2> /dev/null || echo "  (unreadable, inspect by hand: $f)"
   done
   ```

   **The loop is load-bearing — do not collapse it back to `jq … "$TMP/findings"/*.json`.** `jq` treats a file glob as one concatenated stream, so a single malformed findings file aborts the whole invocation and every file that sorts after it prints nothing at all, including valid files holding orphans. That is silent, order-dependent data loss at the exact moment these files are the only surviving copy of a lost finding — and a malformed file is not hypothetical here, since a specialist writing bad JSON is precisely what `unreadable_roles` exists to record. Per-file, a bad file costs only its own line.

   (The `sub` calls reproduce the helper's ID namespacing: a specialist numbers its findings locally as `f-1` or `<role>-1`, and the loader rewrites both to `<role>-1`.)

2. **Skip Cleanup's `rm -rf` for this run** (see Cleanup) so the raw findings survive, and make sure the preservation line above names the path. The git-ref deletion still runs. This is the one carve-out from the always-runs Cleanup invariant.
3. Continue to the `POST_MODE` branch as normal — the findings that _did_ reconcile are still valid and worth posting.

This procedure runs in **every** `POST_MODE`, including `auto` and `dry-run`. An unattended run is exactly where a silently-dropped finding would otherwise vanish without anyone seeing it.

### Posting mode

Branch on `POST_MODE` (derived at startup):

- `dry-run` → **do not post and do not prompt.** Report the would-post counts (inline-eligible + summary-only) and proceed straight to Cleanup. Do not call AskUserQuestion.
- `auto` → **post without prompting.** Skip the AskUserQuestion call and proceed to step [5/5] exactly as if the user had chosen `Post all`.
- `interactive` (default) → call the **AskUserQuestion** tool to get permission to post. Use a single question (`multiSelect: false`) with header `Post review`, a question naming the counts (e.g. "Post N inline + M summary-only finding(s) to PR #<PR_NUMBER>?"), and two options:

- `Post all` — "Post every eligible finding as inline comments plus the review summary."
- `Skip` — "Skip posting and proceed to cleanup."

Map the answer to one of three branches:

- `Post all` (or an empty/affirmative reply) → post all.
- `Skip` (or a negative reply) → skip posting, proceed to cleanup.
- A comma-separated list of finding IDs typed into the tool's free-text "Other" field (e.g. `sec-1,perf-2`) → re-run finalize with `--include-finding-ids "<csv>"`, then post that subset.

If the user supplied a finding-ID subset, re-run finalize with the same flags plus `--include-finding-ids "<csv>"` — this rewrites `payload.json`, `payload-pending.json`, `payload-body.json` to the filtered subset while `consolidated.json` keeps the pre-filter audit log (the helper handles that distinction).

---

## [5/5] Post review or skip

If the user chose `Skip` (or `POST_MODE` was `dry-run`), skip to cleanup. Otherwise, first re-confirm the PR is still open — it may have been merged or closed while specialists ran:

```bash
gh pr view "$PR_NUMBER" --json state --jq '.state'
```

If the result is not `OPEN`, report `PR #$PR_NUMBER is now <state> — skipping post` and proceed to Cleanup (do not post to a finished PR). Otherwise post via `gh api` with a three-tier fallback.

**Tier 1 — single-shot review with batched comments:**

```bash
TIER1_ERR=$(gh api "repos/$OWNER/$REPO/pulls/$PR_NUMBER/reviews" --method POST --input "$TMP/payload.json" 2>&1 > /dev/null)
TIER1_RC=$?
printf '%s' "$TIER1_ERR" > "$TMP/tier1.err"
```

`2>&1 >/dev/null` discards the success-path review JSON (tier 1 doesn't need it — success is the exit code) and captures any error text into `$TIER1_ERR`, persisted to `$TMP/tier1.err` for the tier-3 patch. **Branch on the variable, never by `cat`-ing the file** — the text is already in `$TIER1_ERR`. Run this exact block — every branch ends in a plain `echo`, so the block's own exit status is always `0` and a successful post never gets misreported as a Bash failure (do **not** end the check on a bare `[ … ] && echo`, whose exit status is `1` on the success path where `$TIER1_ERR` is empty):

```bash
if [ "$TIER1_RC" -eq 0 ]; then
  echo "posted via tier 1" # → skip to Cleanup
elif printf '%s' "$TIER1_ERR" | grep -q 'HTTP 422'; then
  echo "tier 1 hit HTTP 422 — falling to tier 2"
else
  echo "tier 1 failed (rc=$TIER1_RC): $TIER1_ERR" # → fall through to tier 3
fi
```

**Tier 2 — create pending review then submit:**

```bash
REVIEW_ID=$(gh api "repos/$OWNER/$REPO/pulls/$PR_NUMBER/reviews" --method POST --input "$TMP/payload-pending.json" --jq '.id' 2> "$TMP/tier2.err")
```

If create failed (`$REVIEW_ID` is empty), fall through to tier 3 — its stderr is captured in `$TMP/tier2.err` (consumed by the tier-3 patch; don't `cat` it). Otherwise submit:

```bash
gh api "repos/$OWNER/$REPO/pulls/$PR_NUMBER/reviews/$REVIEW_ID/events" --method POST --input "$TMP/payload-body.json" -f event=COMMENT
```

If submit succeeds, report `posted via tier 2`. If submit fails, warn the user that pending review `$REVIEW_ID` is dangling (provide the `gh api … --method DELETE` command verbatim) and fall through to tier 3.

**Tier 3 — fallback issue comment:**

Patch the `{API_ERROR}` placeholder in `$TMP/fallback.md` with the captured stderr from the last failing tier — guardrail-safe (no `sed -i`, no `cat`), writing to the `.patched` copy:

```bash
ERRFILE="$TMP/tier1.err"
[ -s "$TMP/tier2.err" ] && ERRFILE="$TMP/tier2.err"
python3 -c "import pathlib,sys; src=pathlib.Path('$TMP/fallback.md').read_text(); pathlib.Path('$TMP/fallback.md.patched').write_text(src.replace('{API_ERROR}', sys.stdin.read()))" < "$ERRFILE"
gh pr comment "$PR_NUMBER" -F "$TMP/fallback.md.patched"
```

If tier 3 also fails, surface the full error and stop. Report `posted via tier 3` on success.

---

## Cleanup

Cleanup runs on every path you reach — after step [5/5] completes normally, after the user chose `Skip` at the post-review prompt, and after any fatal stop (command failure, harness denial, missing helper output). Two deliberate exceptions: skip it if `$TMP` is unset (the stop happened before step 0 created the scratch dir), and **skip the `rm -rf` if [4/5] reported an accounting mismatch** — there the raw findings are the only surviving copy of an orphaned finding, so deleting them destroys the evidence. Still delete the git ref in that case.

**It is not, and cannot be, "always".** Cleanup is a turn _you_ take, so it is skipped by construction by every abnormal termination where no turn of yours runs at all. That includes:

- the Claude Code process exiting;
- a `kill`;
- context exhaustion;
- the machine sleeping;
- the user stopping the in-flight specialists while you sit idle between notifications.

The harness hands you no turn after that stop. On transcript `dfac0a80` the stop notice was consumed by the user's next slash command, and neither Cleanup nor a completion marker ever ran.

That gap is real and it accumulates. On 2026-08-26 four orphaned `pr-review-*` dirs (one 22 hours old) and two dangling `refs/code-review/*` were found on a single machine. The pre-flight sweep that `prepare --fetch` runs in [1/5] is the compensating half. It cannot save the run that dies, but it bounds the leak at one dir instead of letting it grow without limit.

Defensive check: only `rm -rf` a path that is a `pr-review-*` directory **directly under the temp root** — matching the full path, not just its basename, so a `pr-review-*` component anywhere else on disk can't satisfy the guard. The pattern re-expands the same `${TMPDIR:-/tmp}` used to build `$TMP` in step 0, so it matches by construction whether or not `TMPDIR` carries a trailing slash:

```bash
case "$TMP" in
  "${TMPDIR:-/tmp}"/pr-review-*) rm -rf "$TMP" ;;
  *) echo "refusing to remove $TMP (not a pr-review dir under ${TMPDIR:-/tmp})" ;;
esac
```

Also delete the hidden ref created in step [1/5] (ignore failure — it may not exist if the fetch failed or the stop happened first). This runs even when the `rm -rf` is skipped for an accounting mismatch — a dangling ref has no diagnostic value:

```bash
git update-ref -d "refs/code-review/pr-$PR_NUMBER"
```

On normal completion report `[5/5] Done.`. On a fatal stop report which step failed (e.g., `Stopped at [3/5]: code-review:security spawn denied. Cleanup complete.`) and exit.

## Completion marker (always the last line)

On **every** exit path, your final message must end with exactly one completion-marker line so the agent-view job classifier resolves this run to a definite state instead of leaving it stale at "awaiting input". The classifier reads only your latest message's text and matches a literal lowercase line-prefix — `result:` (run finished) or `failed:` (fatal stop, could not complete). `needs input:` is not used here: the post-review prompt is a tool call, so while it's pending the harness already shows awaiting-input; by the time your final text turn lands the run is either complete or fatally stopped.

- It must be the **last line** of your final message, at the start of its own line, plain text — no backticks, bold, tag, or emoji. `Done` / `✅` and the `[5/5] Done.` progress line do **not** count; only the literal token flips the badge.
- The text after the token is a **self-contained one-line headline** — readable by someone who never saw the request.
- Normal completion → `result: PR #N reviewed — <posted N inline + M summary via tier T | dry-run: would post N+M | posting skipped>`.
- Fatal stop (command failure, harness denial, missing helper output) → `failed: code-review stopped at [step] — <reason>` (e.g. `failed: code-review stopped at [3/5] — code-review:security spawn denied`).
- If a later turn would otherwise replace this message (e.g. an auto-continue prompt), **re-emit the marker** so the finished state isn't downgraded.
