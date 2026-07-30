---
name: code-review
description: Multi-specialist code review of a pull request. Spawns parallel subagents (security, quality, errors, perf, plus conditional typescript/react/infra), consolidates findings via the bundled Go helper, and posts inline review comments via gh.
argument-hint: <pr-number> [--auto|--dry-run]
disable-model-invocation: true
model: opus
effort: medium
allowed-tools: Bash, Bash(gh:*), Bash(*/code-review-helper:*), Read, Write, Grep, Glob, Agent, AskUserQuestion
---

# /code-review — orchestrate a multi-specialist PR review

You are the orchestrator for /code-review. Execute the numbered steps below in order. Report progress with one short line per step (e.g. `[1/5] Fetching PR #42…`). Surface every command failure verbatim and stop — do not invent workarounds.

A "stop" includes harness-side Agent rejections. All specialists are spawned together in a single message (see [3/5]), so a rejection means the harness denied one block of an already-issued batch — it is **not** a cue to issue the remaining specialists one at a time. If any `Agent` call returns a message containing `"user doesn't want to proceed"` or `"tool use was rejected"`, treat it as a fatal stop: do not retry, abandon the finalize/post phase. Report which subagent was denied, then jump to **Cleanup** (which always runs, regardless of whether the workflow finished normally).

All agents in this plugin are namespaced under `code-review:` — use the fully-qualified form for every `subagent_type` value (`code-review:security`, `code-review:quality`, etc.). The unqualified bare names are not registered and will fail with "Agent type not found".

The user passes the PR number as `$ARGUMENTS`, optionally followed by a posting-mode flag. Parse `$ARGUMENTS` as whitespace-separated tokens:

- The first positive-integer token is `PR_NUMBER`. If no such token exists, report the error and stop.
- `POST_MODE` — `auto` if a `--auto` token is present (post all eligible findings without prompting; for CI / unattended runs), `dry-run` if `--dry-run` is present (run the full review but never post), otherwise `interactive` (the default — prompt before posting). If both flags are present, `--dry-run` wins.

## Variables to derive at startup

Resolve once and reuse:

- `PR_NUMBER` — from `$ARGUMENTS` (first positive-integer token, per the parse rule above).
- `POST_MODE` — `auto` / `dry-run` / `interactive`, per the parse rule above.
- `TMP` — scratch dir at `${TMPDIR:-/tmp}/pr-review-${PR_NUMBER}-$(date +%s)`; created in [1/5] Turn 1.
- `REPO_ROOT` — `git rev-parse --show-toplevel`; printed by [1/5] Turn 1.
- `HEAD_SHA`, `OWNER`, `REPO` — printed by `prepare` in [2/5]; never re-derive them with a separate call.
- `HELPER` — `${CLAUDE_PLUGIN_ROOT}/bin/code-review-helper`.
- `RUBRIC` — `${CLAUDE_PLUGIN_ROOT}/references/code-review-rubrics.md`.

All subsequent paths derive from `$TMP`. No path uses cwd.

---

## [1/5] Fetch PR metadata, diff, and prior review threads

This step is **three Bash turns**, in this order. Everything derivable from what they fetch happens in [2/5], not here — so if you find yourself running `jq` to pull a value out of a file just to pass it to the next command, you are in the wrong step.

**Turn 1 — create the scratch dir** (one call; everything downstream needs `$TMP`):

```bash
TMP="${TMPDIR:-/tmp}/pr-review-${PR_NUMBER}-$(date +%s)"
mkdir -p "$TMP/findings"
echo "TMP=$TMP"
echo "REPO_ROOT=$(git rev-parse --show-toplevel)"
```

**Turn 2 — the three fetches, ALL THREE in ONE message.** Each depends only on `PR_NUMBER` and `$TMP`, never on another's output, so they must be issued as three sibling `Bash` blocks in a single assistant message:

```bash
gh pr view "$PR_NUMBER" --json headRefOid,url,number,title,headRefName,author,state > "$TMP/pr-meta.json"
```

```bash
gh pr diff "$PR_NUMBER" > "$TMP/pr-$PR_NUMBER.diff"
```

```bash
git fetch origin "+pull/$PR_NUMBER/head:refs/code-review/pr-$PR_NUMBER" --no-tags
```

**Do NOT issue one of these, wait for its result, then issue the next.** Each Bash round-trip costs several seconds of fixed harness overhead regardless of how little work it does; running three independent fetches serially spends that price three times for nothing. This is the same failure this file warns about for specialist spawns in [3/5], and it is just as wrong here.

Schematic — the three fetches are sibling blocks inside one message, not one block per turn:

```
<single assistant message>
  Bash(gh pr view …)      ← fetch 1
  Bash(gh pr diff …)      ← fetch 2
  Bash(git fetch …)       ← fetch 3
</single assistant message>
```

The `git fetch` puts the PR head in a hidden local ref so `git show $HEAD_SHA:<path>` and `git grep <sym> $HEAD_SHA` resolve even on a stale clone or a fork PR (specialists and the bundle builder both depend on the HEAD_SHA objects being present locally). If it fails (offline, fork ref unavailable, restricted remote), print a one-line warning and continue — an explicit exception to the surface-and-stop rule at the top of this file. Specialists fall back to the bundle's embedded source; line confirmation against `$HEAD_SHA` may degrade on stale clones.

**Turn 3 — prior review threads** (one call). This needs `OWNER`/`REPO`, which come from `pr-meta.json`; derive them with inline command substitution **in this same command** rather than spending a turn to print them:

```bash
OWNER=$(jq -r '.url | capture("github\\.com/(?<o>[^/]+)/(?<r>[^/]+)/pull/").o' "$TMP/pr-meta.json")
REPO=$(jq -r '.url | capture("github\\.com/(?<o>[^/]+)/(?<r>[^/]+)/pull/").r' "$TMP/pr-meta.json")
gh api graphql -F owner="$OWNER" -F repo="$REPO" -F pr="$PR_NUMBER" -f query='query($owner:String!,$repo:String!,$pr:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$pr){reviewThreads(first:50){nodes{id isResolved isOutdated comments(first:50){nodes{databaseId author{login} body path line originalLine originalStartLine}}}}}}}' > "$TMP/review-threads.json"
```

GraphQL `reviewThreads` gives thread-level state (`isResolved`, `isOutdated`) and every reply — needed to detect when the PR author has already dismissed a finding as a false positive; the REST `pulls/{n}/reviews/{rid}/comments` endpoint exposes none of that. The first-page cap is 50 threads × 50 comments, well above any real review on this repo; if a PR ever exceeds it, this step needs a cursor-paginated loop. The GraphQL variables use `-F` (typed) for `pr` and `-f` for strings.

If this call fails, `review-threads.json` will be absent or empty — that is not fatal. [2/5] treats a missing file as "no prior review" and continues; prior-review dedup simply does nothing.

---

## [2/5] Prepare every pre-spawn artifact — ONE helper call

```bash
"$HELPER" prepare --review-tmpdir "$TMP" --repo-root "$REPO_ROOT" --rubric "$RUBRIC"
```

That is the whole step. `prepare` reads `pr-meta.json`, `pr-$PR_NUMBER.diff` and `review-threads.json` from `$TMP` and writes all eight downstream artifacts: `changed-files.json`, `valid-lines.json`, `claude-md-files.json`, `roster.json`, `prior-issues.json`, `spawn-context.md`, `rubric.md`, `spawn-manifest.json`. It replaces six separate calls (two `jq` projections plus `diff`, `roster`, `bundle-context`, `spawn-manifest`) whose outputs it reproduces byte for byte.

It prints shell-safe `KEY=value` lines to stdout — this is where you get the values later steps need, so read them from the tool result rather than re-deriving any of them:

```
HEAD_SHA=…   OWNER=…   REPO=…   PR_NUMBER=…   PR_STATE=…   PR_AUTHOR=…
ROSTER=security,quality,errors,perf,…          CHANGED_FILES=…   PRIOR_ISSUES=…
```

**Eligibility guard.** `prepare` exits non-zero with `PR #N is <state> — aborting` before writing anything when the PR is not `OPEN` — reviewing a merged/closed PR wastes the entire specialist budget and risks posting to a finished thread. On that error, report it and stop (run Cleanup). A second, immediately-before-posting re-check lives at the top of step [5/5] to catch a PR that changed state mid-review. (The guard fires here rather than before the [1/5] GraphQL call, so a closed PR costs one wasted API call — the tradeoff for having exactly one place that decides eligibility.)

Roster contents, for reference — `prepare` encodes all of these patterns:

- Always-on: `security`, `quality`, `errors`, `perf`.
- Conditional: `typescript` (`.ts/.tsx/.cts/.mts`, plus `tsconfig*.json`/`jsconfig.json` and framework/bundler configs), `react` (`.tsx/.jsx`, plus framework/bundler configs), `infra` (`.sql`, `migrations/`, `db/migrations/`, `.tf`, `.hcl`, `terraform/`, `Dockerfile`, `docker-compose`/`compose.yml`, `k8s/`, `kubernetes/`, `helm/`, `deploy/`, `infra(structure)?/`, `.github/workflows/`, `Jenkinsfile`, `.circleci/`, `.buildkite/`), `claude-md` (any `CLAUDE.md` ancestor of a changed file exists at `$REPO_ROOT`). Framework/bundler configs = `{next,vite,nuxt,webpack,rollup,esbuild,babel}.config.{js,mjs,cjs,ts,mts,cts}`, `babel.config.json`, `.babelrc` — these feed both `typescript` and `react`.

**Do not inspect the artifacts.** `ROSTER=` on stdout already tells you which specialists you'll spawn, and the only file you need to open is `spawn-manifest.json` in [3/5]. If you nevertheless need to look at one, these are the shapes — they are _not_ uniform, so don't guess:

| File                                                        | Shape                                                                 |
| ----------------------------------------------------------- | --------------------------------------------------------------------- |
| `changed-files.json`, `roster.json`, `claude-md-files.json` | flat JSON array of strings — `jq -r '.[]'`, never `.[].path`          |
| `valid-lines.json`                                          | object keyed by path → array of `[start,end]` pairs                   |
| `prior-issues.json`                                         | `{last_review_date, last_review_commit, issues: [ {path, line, …} ]}` |
| `spawn-manifest.json`                                       | array of objects — `{subagent_type, description, prompt}`             |
| `consolidated.json` ([4/5])                                 | object of named buckets — see the summary `jq` in [4/5]               |

Use `jq` (a pipeline filter — allowed) or a `Read` call for any such inspection — never bare `cat <file>`, which this repo's `enforce-builtin-tools` guardrail blocks. And keep a disposable inspection in its **own** Bash call: never `&&` it onto a command that writes state, or the inspection's exit code masks whether the real work succeeded. (That rule holds for every step in this file, not just this one.)

---

## [3/5] Spawn all roster specialists in ONE message

Read `$TMP/spawn-manifest.json` in a **single** `Read` call (or, if you must use the shell, one `jq -c '.[]'` invocation). Do not inspect it field-by-field across multiple Bash calls — no per-entry `jq '.[N].prompt'`, no `subagent_type` listing, no `head` preview. The manifest is forwarded verbatim, so printing individual fields first buys nothing and adds a round-trip per entry. It contains one object per roster entry, each with three pre-rendered fields: `subagent_type`, `description`, `prompt`. Emit one `Agent` `tool_use` block per entry, **all as sibling blocks in this single assistant message**. Forward each object's three fields verbatim — do not modify, truncate, summarize, or skip any entry. The manifest is the ground truth; if it has N entries your message must contain N `Agent` blocks.

**Do NOT emit one `Agent` block, wait for its result, then emit the next.** That runs the specialists sequentially and is the exact bug this step exists to prevent. Every block belongs in THIS one message — the harness then runs them concurrently and their results return together. The manifest is not a to-do list to work through turn by turn; it is the complete set of sibling blocks for a single message.

Schematic — every manifest entry becomes one sibling `Agent` block inside the same message (not one block per turn):

```
<single assistant message containing one Agent block per manifest entry>
  Agent(subagent_type=…, description=…, prompt=…)   ← one manifest entry
  Agent(subagent_type=…, description=…, prompt=…)   ← another manifest entry
  … one more Agent block for every remaining entry, all in this same message …
</single assistant message>
```

After all Agent calls return, verify each role's findings file exists at `$TMP/findings/<role>.json`. Missing files are surfaced as `missing_roles` by the finalize step — don't retry them.

On harness rejection: see top-of-file stop rule. Because the whole batch is issued at once, a denial of any one block aborts the run — any findings files already on disk are abandoned, not posted. Skip the finalize/post phase and jump to Cleanup.

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
  (if (.timed_out_roles // []) | length > 0 then "  Timed out: \(.timed_out_roles | join(", "))" else empty end),
  (if (.missing_roles // []) | length > 0 then "  Missing: \(.missing_roles | join(", "))" else empty end),
  (if (.unreadable_roles // []) | length > 0 then "  Unreadable: \(.unreadable_roles | join(", "))" else empty end),
  (if (.invalid_findings // []) | length > 0 then "  Invalid findings (\(.invalid_findings | length)):" else empty end),
  ((.invalid_findings // [])[] | "    [\(.role)/\(.id)] \(.reason)"),
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

   Take each orphan's severity, confidence, file, line and rationale from the specialist's own `findings/<role>.json` with this exact one-shot `jq` — one call across the whole directory, not one per file. Pass the orphan IDs the summary printed. This is the single exception to the do-not-re-read rule above, and it exists because the summary provably does _not_ have this information:

   ```bash
   jq -r --argjson orphans '["<id>","<id>"]' '
     .specialist as $role | .findings[]
     | select(($role + "-" + (.id | sub("^f-";"") | sub("^" + $role + "-";""))) as $ns | $orphans | index($ns))
     | "  \($role)-\(.id) | \(.severity) conf=\(.confidence) | \(.file):\(.line) | \(.rationale)"
   ' "$TMP/findings"/*.json
   ```

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

`2>&1 >/dev/null` discards the success-path review JSON (tier 1 doesn't need it — success is the exit code) and captures any error text into `$TIER1_ERR`, persisted to `$TMP/tier1.err` for the tier-3 patch. **Branch on the variable, never by `cat`-ing the file** (the `enforce-builtin-tools` guardrail blocks bare `cat <file>`). Run this exact block — every branch ends in a plain `echo`, so the block's own exit status is always `0` and a successful post never gets misreported as a Bash failure (do **not** end the check on a bare `[ … ] && echo`, whose exit status is `1` on the success path where `$TIER1_ERR` is empty):

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

Cleanup always runs — after step [5/5] completes normally, after the user chose `Skip` at the post-review prompt, and after any fatal stop (command failure, harness denial, missing helper output). Two exceptions, and only two: skip it if `$TMP` is unset (the stop happened before step 0 created the scratch dir), and **skip the `rm -rf` if [4/5] reported an accounting mismatch** — there the raw findings are the only surviving copy of an orphaned finding, so deleting them destroys the evidence. Still delete the git ref in that case.

Defensive check: only `rm -rf` paths whose basename starts with `pr-review-` (the prefix we created in step 0):

```bash
case "$(basename "$TMP")" in
  pr-review-*) rm -rf "$TMP" ;;
  *) echo "refusing to remove $TMP (unexpected prefix)" ;;
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
