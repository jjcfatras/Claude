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

Fetch prior Claude-Code review threads on this PR (used by the helper's prior-review dedup pass). Uses GraphQL `reviewThreads` so we get thread-level state (`isResolved`, `isOutdated`) and every reply — needed to detect when the PR author has already dismissed a finding as a false positive. The REST `pulls/{n}/reviews/{rid}/comments` endpoint does not expose any of that.

First capture the PR author login (used below to mark "author replied" threads):

```bash
gh pr view "$PR_NUMBER" --json author --jq '.author.login' > "$TMP/pr-author.txt"
```

Then fetch all review threads in one GraphQL call. The first-page cap is 50 threads × 50 comments, which is well above any real review on this repo; if a PR ever exceeds it, this step needs a cursor-paginated loop. We embed the GraphQL variables via `-F` (typed) for `pr` and `-f` for strings:

```bash
gh api graphql -F owner="$OWNER" -F repo="$REPO" -F pr="$PR_NUMBER" -f query='query($owner:String!,$repo:String!,$pr:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$pr){reviewThreads(first:50){nodes{id isResolved isOutdated comments(first:50){nodes{databaseId author{login} body path line originalLine originalStartLine}}}}}}}' > "$TMP/review-threads.json"
```

Then project to the helper's `PriorIssuesFile` shape — only threads whose first comment is a `/code-review` finding count. The filter keys off the per-finding header format `... (Confidence: NN/100) - ...` emitted by `internal/render/issue.go` (which is unique to this plugin's output); the older "Claude Code" / "Generated with Claude" marker lives only on the review _summary_, not on inline-comment bodies. `author_dismissed` is true when any reply (comments after the first) is authored by the PR author. `line` falls back to `originalLine` when GitHub couldn't re-anchor:

```bash
jq --arg pr_author "$(cat "$TMP/pr-author.txt")" '{issues: [.data.repository.pullRequest.reviewThreads.nodes[] | . as $t | ($t.comments.nodes[0]) as $first | select(($first.body // "") | test("\\(Confidence: [0-9]+/100\\)")) | {path: ($first.path // ""), line: ($first.line // $first.originalLine // 0), start_line: ($first.originalStartLine // 0), snippet: "", description: $first.body, is_resolved: $t.isResolved, is_outdated: $t.isOutdated, author_dismissed: any($t.comments.nodes[1:][]?; .author.login == $pr_author)}]}' "$TMP/review-threads.json" > "$TMP/prior-issues.json"
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
- Conditional: `typescript` (`.ts/.tsx/.cts/.mts`), `react` (`.tsx/.jsx`), `infra` (`.sql`, `migrations/`, `db/migrations/`, `.tf`, `.hcl`, `terraform/`, `Dockerfile`, `docker-compose`, `k8s/`, `kubernetes/`, `helm/`, `deploy/`, `infra(structure)?/`), `claude-md` (any `CLAUDE.md` ancestor of a changed file exists at `$REPO_ROOT`).

Read `$TMP/roster.json` to know which specialists you'll spawn.

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

Read `$TMP/spawn-manifest.json`. It contains one object per roster entry, each with three pre-rendered fields: `subagent_type`, `description`, `prompt`. Emit one `Agent` `tool_use` block per entry, **all as sibling blocks in this single assistant message**. Forward each object's three fields verbatim — do not modify, truncate, summarize, or skip any entry. The manifest is the ground truth; if it has N entries your message must contain N `Agent` blocks.

Schematic — `N` here equals the manifest length, which equals the roster length:

```
<single assistant message containing N tool_use blocks>
  Agent(subagent_type=manifest[0].subagent_type, description=manifest[0].description, prompt=manifest[0].prompt)
  Agent(subagent_type=manifest[1].subagent_type, description=manifest[1].description, prompt=manifest[1].prompt)
  … one Agent block per remaining manifest entry …
</single assistant message>
```

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

On normal completion report `[5/5] Done.`. On a fatal stop report which step failed (e.g., `Stopped at [3b/5]: code-review:security spawn denied. Cleanup complete.`) and exit.
