---
description: Multi-specialist code review of a pull request. Spawns parallel subagents (security, quality, errors, perf, plus conditional typescript/react/infra), consolidates findings via the bundled Go helper, and posts inline review comments via gh.
argument-hint: [pr-number] [--auto|--dry-run]
disable-model-invocation: false
model: opus
effort: medium
allowed-tools: Bash, Bash(gh:*), Bash(*/code-review-helper:*), Read, Write, Grep, Glob, Agent, AskUserQuestion
---

# /code-review — orchestrate a multi-specialist PR review

You are the orchestrator for /code-review. Execute the numbered steps below in order. Report progress with one short line per step (e.g. `[1/5] Fetching PR #42…`). Surface every command failure verbatim and stop — do not invent workarounds.

A "stop" includes harness-side Agent rejections. All specialists are spawned together in a single message (see [3b/5]), so a rejection means the harness denied one block of an already-issued batch — it is **not** a cue to issue the remaining specialists one at a time. If any `Agent` call returns a message containing `"user doesn't want to proceed"` or `"tool use was rejected"`, treat it as a fatal stop: do not retry, abandon the finalize/post phase. Report which subagent was denied, then jump to **Cleanup** (which always runs, regardless of whether the workflow finished normally).

All agents in this plugin are namespaced under `code-review:` — use the fully-qualified form for every `subagent_type` value (`code-review:security`, `code-review:quality`, etc.). The unqualified bare names are not registered and will fail with "Agent type not found".

The user passes the PR number as `$ARGUMENTS`, optionally followed by a posting-mode flag. Parse `$ARGUMENTS` as whitespace-separated tokens:

- The first positive-integer token is `PR_NUMBER`. If no such token exists, report the error and stop.
- `POST_MODE` — `auto` if a `--auto` token is present (post all eligible findings without prompting; for CI / unattended runs), `dry-run` if `--dry-run` is present (run the full review but never post), otherwise `interactive` (the default — prompt before posting). If both flags are present, `--dry-run` wins.

## Variables to derive at startup

Resolve once and reuse:

- `PR_NUMBER` — from `$ARGUMENTS` (first positive-integer token, per the parse rule above).
- `POST_MODE` — `auto` / `dry-run` / `interactive`, per the parse rule above.
- `EPOCH` — `date +%s`.
- `TMP` — scratch dir at `${TMPDIR:-/tmp}/pr-review-${PR_NUMBER}-${EPOCH}`. Create with `mkdir -p "$TMP/findings"`.
- `REPO_ROOT` — `git rev-parse --show-toplevel`.
- `HELPER` — `${CLAUDE_PLUGIN_ROOT}/bin/code-review-helper`.
- `RUBRIC` — `${CLAUDE_PLUGIN_ROOT}/references/code-review-rubrics.md`.

All subsequent paths derive from `$TMP`. No path uses cwd.

---

## [1/5] Fetch PR metadata, diff, and prior issues

Run sequentially (each as a separate Bash call — don't chain with `&&`). Independent calls (e.g., `gh pr view` and `gh pr diff` — both depend only on `PR_NUMBER` and `TMP`, not on each other's output) may be emitted in the same model turn; calls that read variables from a previous result (e.g., the jq `OWNER`/`REPO` extraction below, which needs `pr-meta.json` already on disk) must wait for the earlier call to complete:

```bash
gh pr view "$PR_NUMBER" --json headRefOid,url,number,title,headRefName,author,state > "$TMP/pr-meta.json"
```

```bash
gh pr diff "$PR_NUMBER" > "$TMP/pr-$PR_NUMBER.diff"
```

Fetch the PR head into a hidden local ref so `git show $HEAD_SHA:<path>` and `git grep <sym> $HEAD_SHA` resolve even on a stale clone or a fork PR (specialists and the bundle builder both depend on the HEAD_SHA objects being present locally). This call depends only on `PR_NUMBER`, so it may be emitted in the same turn as the two fetches above:

```bash
git fetch origin "+pull/$PR_NUMBER/head:refs/code-review/pr-$PR_NUMBER" --no-tags
```

If this fetch fails (offline, fork ref unavailable, restricted remote), print a one-line warning and continue — an explicit exception to the surface-and-stop rule at the top of this file. Specialists fall back to the bundle's embedded source; line confirmation against `$HEAD_SHA` may degrade on stale clones.

Extract `HEAD_SHA`, `OWNER`, `REPO`, and the PR author login — all derived from the single `pr-meta.json` fetch above (no second `gh pr view` call). The author login is used further down to mark "author replied" threads in the prior-issues filter:

```bash
HEAD_SHA=$(jq -r '.headRefOid' "$TMP/pr-meta.json")
OWNER=$(jq -r '.url | capture("github\\.com/(?<o>[^/]+)/(?<r>[^/]+)/pull/").o' "$TMP/pr-meta.json")
REPO=$(jq -r '.url | capture("github\\.com/(?<o>[^/]+)/(?<r>[^/]+)/pull/").r' "$TMP/pr-meta.json")
jq -r '.author.login' "$TMP/pr-meta.json" > "$TMP/pr-author.txt"
```

**Eligibility guard.** Abort early if the PR is not open — reviewing a merged/closed PR wastes specialist budget and risks posting to a finished thread. This guard reuses the fetch above (no extra API call); a second, immediately-before-posting re-check lives at the top of step [5/5] to catch a PR that changed state mid-review:

```bash
PR_STATE=$(jq -r '.state' "$TMP/pr-meta.json")
```

If `$PR_STATE` is not `OPEN`, report `PR #$PR_NUMBER is $PR_STATE — aborting` and stop (run Cleanup).

Fetch prior Claude-Code review threads on this PR (used by the helper's prior-review dedup pass). Uses GraphQL `reviewThreads` so we get thread-level state (`isResolved`, `isOutdated`) and every reply — needed to detect when the PR author has already dismissed a finding as a false positive. The REST `pulls/{n}/reviews/{rid}/comments` endpoint does not expose any of that.

Fetch all review threads in one GraphQL call. The first-page cap is 50 threads × 50 comments, which is well above any real review on this repo; if a PR ever exceeds it, this step needs a cursor-paginated loop. We embed the GraphQL variables via `-F` (typed) for `pr` and `-f` for strings:

```bash
gh api graphql -F owner="$OWNER" -F repo="$REPO" -F pr="$PR_NUMBER" -f query='query($owner:String!,$repo:String!,$pr:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$pr){reviewThreads(first:50){nodes{id isResolved isOutdated comments(first:50){nodes{databaseId author{login} body path line originalLine originalStartLine}}}}}}}' > "$TMP/review-threads.json"
```

Then project to the helper's `PriorIssuesFile` shape — only threads whose first comment is a `/code-review` finding count. The filter keys off the hidden `<!-- cr-finding id="…" -->` marker that `internal/render/issue.go` embeds in every finding body — a stable machine identity that survives any change to the rendered header wording. (The previous filter keyed on the human `(Confidence: NN/100)` header, which silently broke dedup whenever that text changed.) It extracts the `fingerprint` (for the helper's exact-match dedup arm) and a base64 `snippet`, decoded for the snippet-overlap arm. `author_dismissed` is true when any reply (comments after the first) is authored by the PR author. `line` falls back to `originalLine` when GitHub couldn't re-anchor:

```bash
jq --arg pr_author "$(cat "$TMP/pr-author.txt")" '{issues: [.data.repository.pullRequest.reviewThreads.nodes[] | . as $t | ($t.comments.nodes[0]) as $first | ($first.body // "") as $body | select($body | test("<!-- cr-finding id=")) | {path: ($first.path // ""), line: ($first.line // $first.originalLine // 0), start_line: ($first.originalStartLine // 0), fingerprint: (($body | capture("<!-- cr-finding id=\"(?<fp>[a-f0-9]+)\"") // {fp:""}) | .fp), snippet: (($body | capture("snippet64=\"(?<s>[A-Za-z0-9+/=]*)\"") // {s:""}) | .s | if . == "" then "" else (. | @base64d) end), description: $body, is_resolved: $t.isResolved, is_outdated: $t.isOutdated, author_dismissed: any($t.comments.nodes[1:][]?; .author.login == $pr_author)}]}' "$TMP/review-threads.json" > "$TMP/prior-issues.json"
```

If the PR has no prior Claude-Code reviews, the `select(...)` filter yields zero rows and the jq still emits `{"issues": []}` — no special-case branch needed.

---

## [2/5] Parse diff and build the roster

Parse the diff into changed-files + valid-lines maps:

```bash
"$HELPER" diff --in "$TMP/pr-$PR_NUMBER.diff" --out-changed-files "$TMP/changed-files.json" --out-valid-lines "$TMP/valid-lines.json"
```

Compute the CLAUDE.md ancestor walk and the specialist roster. The helper encodes all conditional-role patterns and writes both files in one call:

```bash
"$HELPER" roster --changed-files "$TMP/changed-files.json" --repo-root "$REPO_ROOT" --out-claude-md-files "$TMP/claude-md-files.json" --out-roster "$TMP/roster.json"
```

Roster contents:

- Always-on: `security`, `quality`, `errors`, `perf`.
- Conditional: `typescript` (`.ts/.tsx/.cts/.mts`, plus `tsconfig*.json`/`jsconfig.json` and framework/bundler configs), `react` (`.tsx/.jsx`, plus framework/bundler configs), `infra` (`.sql`, `migrations/`, `db/migrations/`, `.tf`, `.hcl`, `terraform/`, `Dockerfile`, `docker-compose`/`compose.yml`, `k8s/`, `kubernetes/`, `helm/`, `deploy/`, `infra(structure)?/`, `.github/workflows/`, `Jenkinsfile`, `.circleci/`, `.buildkite/`), `claude-md` (any `CLAUDE.md` ancestor of a changed file exists at `$REPO_ROOT`). Framework/bundler configs = `{next,vite,nuxt,webpack,rollup,esbuild,babel}.config.{js,mjs,cjs,ts,mts,cts}`, `babel.config.json`, `.babelrc` — these feed both `typescript` and `react`.

Read `$TMP/roster.json` to know which specialists you'll spawn.

To inspect a helper-written JSON file inline, use `jq` (a pipeline filter — allowed) or a separate `Read` call — never bare `cat <file>`, which this repo's `enforce-builtin-tools` guardrail blocks.

---

## [3a/5] Build spawn-context bundle and spawn manifest

Two independent helper calls — both depend only on already-on-disk inputs (`roster.json`, `changed-files.json`, the rubric source). Emit them in the same model turn:

```bash
"$HELPER" bundle-context --review-tmpdir "$TMP" --head-sha "$HEAD_SHA" --pr-number "$PR_NUMBER" --owner "$OWNER" --repo "$REPO" --repo-root "$REPO_ROOT" --rubric "$RUBRIC" --rubric-out "$TMP/rubric.md"
```

```bash
"$HELPER" spawn-manifest --roster "$TMP/roster.json" --review-tmpdir "$TMP" --head-sha "$HEAD_SHA" --pr-number "$PR_NUMBER" --owner "$OWNER" --repo "$REPO" --repo-root "$REPO_ROOT" --out "$TMP/spawn-manifest.json"
```

`bundle-context` synthesizes the `## Summary` section deterministically from `changed-files.json` (file count + top directories) and writes `$TMP/spawn-context.md` plus `$TMP/rubric.md`. `spawn-manifest` reads `roster.json` and writes one fully-rendered Agent payload per role to `$TMP/spawn-manifest.json` — the orchestrator does no per-role string-building in [3b/5].

All three files must exist before specialists run.

---

## [3b/5] Spawn all roster specialists in ONE message

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

Run the helper's finalize pipeline (dedup → gate → snap → render):

```bash
ROSTER_CSV=$(jq -r 'join(",")' "$TMP/roster.json")
```

```bash
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
  "  Inline eligible: \(.inline_eligible | length)",
  "  Summary only: \(.summary_only | length)",
  "",
  ((.inline_eligible // [])[] | "  [\(.id)] \(.severity) \(.file):\(.line) — \(.rationale)"),
  ((.summary_only // [])[]    | "  [\(.id)] \(.severity) (summary) \(.file):\(.line) — \(.rationale)")
' "$TMP/consolidated.json"
```

The Invalid-findings block lists each dropped finding's role, id, and reason so the user can see what was lost (e.g., a finding with `line: 0` that the helper rejected). The Reconciled block shows findings the confidence gate filtered out, each with the specialist's one-sentence rationale — the reasoning trail for what was investigated and dismissed. Neither is posted to GitHub; without them, drops are silent.

Then branch on `POST_MODE` (derived at startup):

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

If the result is not `OPEN`, report `PR #$PR_NUMBER is now <state> — skipping post` and proceed to Cleanup (do not post to a finished PR). Otherwise post via `gh api` with a three-tier fallback (the same pattern `src/helpers/post-review.ts` implemented in code-review-AT).

**Tier 1 — single-shot review with batched comments:**

```bash
TIER1_ERR=$(gh api "repos/$OWNER/$REPO/pulls/$PR_NUMBER/reviews" --method POST --input "$TMP/payload.json" 2>&1 > /dev/null)
TIER1_RC=$?
printf '%s' "$TIER1_ERR" > "$TMP/tier1.err"
```

`2>&1 >/dev/null` discards the success-path review JSON (tier 1 doesn't need it — success is the exit code) and captures any error text into `$TIER1_ERR`, persisted to `$TMP/tier1.err` for the tier-3 patch. **Branch on the variable, never by `cat`-ing the file** (the `enforce-builtin-tools` guardrail blocks bare `cat <file>`):

- `$TIER1_RC` is `0` → report `posted via tier 1` and skip to cleanup.
- non-zero **and** `$TIER1_ERR` contains `HTTP 422` → fall to tier 2.
- any other non-zero → surface it verbatim with `echo "$TIER1_ERR"` and fall through to tier 3.

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

Cleanup always runs — after step [5/5] completes normally, after the user chose `Skip` at the post-review prompt, and after any fatal stop (command failure, harness denial, missing helper output). Skip it only if `$TMP` is unset (the stop happened before step 0 created the scratch dir).

Defensive check: only `rm -rf` paths whose basename starts with `pr-review-` (the prefix we created in step 0):

```bash
case "$(basename "$TMP")" in
  pr-review-*) rm -rf "$TMP" ;;
  *) echo "refusing to remove $TMP (unexpected prefix)" ;;
esac
```

Also delete the hidden ref created in step [1/5] (ignore failure — it may not exist if the fetch failed or the stop happened first):

```bash
git update-ref -d "refs/code-review/pr-$PR_NUMBER"
```

On normal completion report `[5/5] Done.`. On a fatal stop report which step failed (e.g., `Stopped at [3b/5]: code-review:security spawn denied. Cleanup complete.`) and exit.

## Completion marker (always the last line)

On **every** exit path, your final message must end with exactly one completion-marker line so the agent-view job classifier resolves this run to a definite state instead of leaving it stale at "awaiting input". The classifier reads only your latest message's text and matches a literal lowercase line-prefix — `result:` (run finished) or `failed:` (fatal stop, could not complete). `needs input:` is not used here: the post-review prompt is a tool call, so while it's pending the harness already shows awaiting-input; by the time your final text turn lands the run is either complete or fatally stopped.

- It must be the **last line** of your final message, at the start of its own line, plain text — no backticks, bold, tag, or emoji. `Done` / `✅` and the `[5/5] Done.` progress line do **not** count; only the literal token flips the badge.
- The text after the token is a **self-contained one-line headline** — readable by someone who never saw the request.
- Normal completion → `result: PR #N reviewed — <posted N inline + M summary via tier T | dry-run: would post N+M | posting skipped>`.
- Fatal stop (command failure, harness denial, missing helper output) → `failed: code-review stopped at [step] — <reason>` (e.g. `failed: code-review stopped at [3b/5] — code-review:security spawn denied`).
- If a later turn would otherwise replace this message (e.g. an auto-continue prompt), **re-emit the marker** so the finished state isn't downgraded.
