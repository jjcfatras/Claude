---
description: Multi-specialist code review of a pull request. Spawns parallel subagents (security, quality, errors, perf, plus conditional typescript/react/infra), consolidates findings via the bundled Go helper, and posts inline review comments via gh.
argument-hint: [pr-number]
disable-model-invocation: false
model: sonnet
effort: high
allowed-tools: Bash, Read, Write, Grep, Glob, Agent
---

# /code-review — orchestrate a multi-specialist PR review

You are the orchestrator for /code-review. Execute the numbered steps below in order. Report progress with one short line per step (e.g. `[1/5] Fetching PR #42…`). Surface every command failure verbatim and stop — do not invent workarounds.

A "stop" includes harness-side Agent rejections. If any `Agent` call returns a message containing `"user doesn't want to proceed"` or `"tool use was rejected"`, treat it as a fatal stop: do not retry, do not continue spawning further specialists. Report which subagent was denied, then jump to **Cleanup** (which always runs, regardless of whether the workflow finished normally).

All agents in this plugin are namespaced under `code-review:` — use the fully-qualified form for every `subagent_type` value (`code-review:security`, `code-review:quality`, etc.). The unqualified bare names are not registered and will fail with "Agent type not found".

The user passes the PR number as `$ARGUMENTS`. If it is empty or not a positive integer, report the error and stop.

## Variables to derive at startup

Resolve once and reuse:

- `PR_NUMBER` — from `$ARGUMENTS`.
- `EPOCH` — `date +%s`.
- `TMP` — scratch dir at `${TMPDIR:-/tmp}/pr-review-${PR_NUMBER}-${EPOCH}`. Create with `mkdir -p "$TMP/findings"`.
- `REPO_ROOT` — `git rev-parse --show-toplevel`.
- `HELPER` — `${CLAUDE_PLUGIN_ROOT}/bin/code-review-helper`.
- `RUBRIC` — `${CLAUDE_PLUGIN_ROOT}/references/code-review-rubrics.md`.

All subsequent paths derive from `$TMP`. No path uses cwd.

---

## [1/5] Fetch PR metadata, diff, and prior issues

Run sequentially (each as a separate Bash call — don't chain with `&&`, per `references/shell-safety.md`). Independent calls (e.g., `gh pr view` and `gh pr diff` — both depend only on `PR_NUMBER` and `TMP`, not on each other's output) may be emitted in the same model turn; calls that read variables from a previous result (e.g., the jq `OWNER`/`REPO` extraction below, which needs `pr-meta.json` already on disk) must wait for the earlier call to complete:

```bash
gh pr view "$PR_NUMBER" --json headRefOid,url,number,title,headRefName > "$TMP/pr-meta.json"
```

```bash
gh pr diff "$PR_NUMBER" > "$TMP/pr-$PR_NUMBER.diff"
```

Extract `HEAD_SHA`, `OWNER`, `REPO`:

```bash
HEAD_SHA=$(jq -r '.headRefOid' "$TMP/pr-meta.json")
OWNER=$(jq -r '.url | capture("github\\.com/(?<o>[^/]+)/(?<r>[^/]+)/pull/").o' "$TMP/pr-meta.json")
REPO=$(jq -r '.url | capture("github\\.com/(?<o>[^/]+)/(?<r>[^/]+)/pull/").r' "$TMP/pr-meta.json")
```

Fetch prior Claude-Code reviews on this PR (used by the helper's prior-review dedup pass). Most recent Claude-Code review is the only one consulted; if none, write `{"comments": []}`:

```bash
gh api --paginate "repos/$OWNER/$REPO/pulls/$PR_NUMBER/reviews" > "$TMP/reviews.json"
```

Then in a single jq pass, extract the most recent Claude-Code review and fetch its comments. If none, write the empty file:

```bash
LATEST_CLAUDE_REVIEW_ID=$(jq -r '[.[] | select((.body // "") | test("Claude Code|claude-code|Generated with Claude"))] | (.[-1].id // empty)' "$TMP/reviews.json")
```

If `LATEST_CLAUDE_REVIEW_ID` is empty, write `{"comments": []}` to `$TMP/prior-issues.json` via the Write tool. Otherwise:

```bash
gh api --paginate "repos/$OWNER/$REPO/pulls/$PR_NUMBER/reviews/$LATEST_CLAUDE_REVIEW_ID/comments" > "$TMP/prior-comments.json"
```

Then build the prior-issues file in the helper's expected shape (via jq into a file — do not echo inline JSON):

```bash
jq --argjson rid "$LATEST_CLAUDE_REVIEW_ID" '{review_id: $rid, comments: [.[] | {id, path, line: (.line // null), body}]}' "$TMP/prior-comments.json" > "$TMP/prior-issues.json"
```

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
- Conditional: `typescript` (`.ts/.tsx/.cts/.mts`), `react` (`.tsx/.jsx`), `infra` (`.sql`, `migrations/`, `db/migrations/`, `.tf`, `.hcl`, `terraform/`, `Dockerfile`, `docker-compose`, `k8s/`, `kubernetes/`, `helm/`, `deploy/`, `infra(structure)?/`), `claude-md` (any `CLAUDE.md` ancestor of a changed file exists at `$REPO_ROOT`).

Read `$TMP/roster.json` to know which specialists you'll spawn.

---

## [3/5] Build spawn-context bundle, spawn specialists in parallel

Bundle the spawn context. The helper assembles every shared input into one markdown file plus a separate rubric copy:

```bash
"$HELPER" bundle-context --review-tmpdir "$TMP" --head-sha "$HEAD_SHA" --pr-number "$PR_NUMBER" --owner "$OWNER" --repo "$REPO" --repo-root "$REPO_ROOT" --rubric "$RUBRIC" --rubric-out "$TMP/rubric.md"
```

The helper synthesizes the `## Summary` section deterministically from `changed-files.json` (file count + top directories). No inline summary pre-pass is required from the orchestrator.

The helper writes `$TMP/spawn-context.md` and `$TMP/rubric.md`. Both must exist before specialists run.

Now spawn every roster role **in parallel** — emit one single message that contains one `Agent` tool call per role. Read `$TMP/roster.json` to know the role list. For each role:

- `subagent_type: "code-review:<role>"` — see top-of-file namespace rule.
- `description: "<role> specialist scan"`
- `prompt`:

  ```
  Read $TMP/spawn-context.md once at startup (use offset:0, limit:200 and paginate — the bundle may exceed the 25,000-token Read cap on large PRs) and Read $TMP/rubric.md once. Scan $TMP/pr-$PR_NUMBER.diff for issues in your domain (the raw diff is often >256 KB — use the `## Diff map` section in spawn-context.md to pick a targeted offset+limit, or `Bash: grep -n "^diff --git"` to enumerate file sections; do not bare-Read the diff). Populate `suggested_fix` whenever the fix is a concrete code replacement; use `null` only for structural findings where no single-snippet replacement applies, and set `startLine` when the replacement spans more than one line. Then Write findings JSON to $TMP/findings/<role>.json per the rubric schema.

  HEAD_SHA: <HEAD_SHA>
  REPO_ROOT: <REPO_ROOT>
  REVIEW_TMPDIR: <TMP>
  PR: #<PR_NUMBER> in <OWNER>/<REPO>
  ```

  Substitute every `<placeholder>` with the actual value before issuing the call. Substitute `<role>` with the bare role string for that call (the description and the findings filename use the bare name; only `subagent_type` is prefixed).

After all Agent calls return, verify each role's findings file exists at `$TMP/findings/<role>.json`. Missing files are surfaced as `missing_roles` by the finalize step — don't retry them.

On harness rejection: see top-of-file stop rule. Specialists whose findings files were written before the denial remain on disk but should not drive a post — abandon the finalize/post phase.

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
  "  Inline eligible: \(.inline_eligible | length)",
  "  Summary only: \(.summary_only | length)",
  "",
  ((.inline_eligible // [])[] | "  [\(.id)] \(.severity) \(.file):\(.line) — \(.rationale)"),
  ((.summary_only // [])[]    | "  [\(.id)] \(.severity) (summary) \(.file):\(.line) — \(.rationale)")
' "$TMP/consolidated.json"
```

The Invalid-findings block lists each dropped finding's role, id, and reason so the user can see what was lost (e.g., a finding with `line: 0` that the helper rejected). Without this, drops are silent.

Then ask the user: `Post review? [Y]es/[n]o/[i]ds <csv>`.

- Empty or `y`/`yes` → post all.
- `n`/`no` → skip posting, proceed to cleanup.
- `ids <csv>` (e.g. `ids sec-1,perf-2`) → re-run finalize with `--include-finding-ids "<csv>"`, then post.

If the user chose `ids`, re-run finalize with the same flags plus `--include-finding-ids "<csv>"` — this rewrites `payload.json`, `payload-pending.json`, `payload-body.json` to the filtered subset while `consolidated.json` keeps the pre-filter audit log (the helper handles that distinction).

---

## [5/5] Post review or skip

If the user chose `no`, skip to cleanup. Otherwise post via `gh api` with a three-tier fallback (the same pattern `src/helpers/post-review.ts` implemented in code-review-AT).

**Tier 1 — single-shot review with batched comments:**

```bash
gh api "repos/$OWNER/$REPO/pulls/$PR_NUMBER/reviews" --method POST --input "$TMP/payload.json"
```

If tier 1 succeeds (`gh` exit 0), report `posted via tier 1` and skip to cleanup.

If tier 1 fails with HTTP 422 in stderr, fall to tier 2. Any other failure → fall through to tier 3.

**Tier 2 — create pending review then submit:**

```bash
REVIEW_ID=$(gh api "repos/$OWNER/$REPO/pulls/$PR_NUMBER/reviews" --method POST --input "$TMP/payload-pending.json" --jq '.id')
```

If create failed, fall through to tier 3. Otherwise submit:

```bash
gh api "repos/$OWNER/$REPO/pulls/$PR_NUMBER/reviews/$REVIEW_ID/events" --method POST --input "$TMP/payload-body.json" -f event=COMMENT
```

If submit succeeds, report `posted via tier 2`. If submit fails, warn the user that pending review `$REVIEW_ID` is dangling (provide the `gh api … --method DELETE` command verbatim) and fall through to tier 3.

**Tier 3 — fallback issue comment:**

Patch the `{API_ERROR}` placeholder in `$TMP/fallback.md` with the captured tier-1/tier-2 stderr, then:

```bash
gh pr comment "$PR_NUMBER" -F "$TMP/fallback.md.patched"
```

If tier 3 also fails, surface the full error and stop. Report `posted via tier 3` on success.

---

## Cleanup

Cleanup always runs — after step [5/5] completes normally, after the user chose `no` at the post-review prompt, and after any fatal stop (command failure, harness denial, missing helper output). Skip it only if `$TMP` is unset (the stop happened before step 0 created the scratch dir).

Defensive check: only `rm -rf` paths whose basename starts with `pr-review-` (the prefix we created in step 0):

```bash
case "$(basename "$TMP")" in
  pr-review-*) rm -rf "$TMP" ;;
  *) echo "refusing to remove $TMP (unexpected prefix)" ;;
esac
```

On normal completion report `[5/5] Done.`. On a fatal stop report which step failed (e.g., `Stopped at [3/5]: code-review:security spawn denied. Cleanup complete.`) and exit.
