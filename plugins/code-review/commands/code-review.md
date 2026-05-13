---
description: Multi-specialist code review of a pull request. Spawns parallel subagents (security, quality, errors, perf, plus conditional typescript/react/infra), consolidates findings via the bundled Go helper, and posts inline review comments via gh.
argument-hint: [pr-number]
disable-model-invocation: false
model: sonnet
effort: high
allowed-tools: Bash, Read, Write, Grep, Glob, Agent
---

# /code-review — orchestrate a multi-specialist PR review

You are the orchestrator for /code-review. Execute the numbered steps below in order. Report progress with one short line per step (e.g. `[1/6] Fetching PR #42…`). Surface every command failure verbatim and stop — do not invent workarounds.

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

## [1/6] Fetch PR metadata, diff, and prior issues

Run sequentially (each as a separate Bash call — don't chain with `&&`, per `references/shell-safety.md`):

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

## [2/6] Parse diff and build the roster

Parse the diff into changed-files + valid-lines maps:

```bash
"$HELPER" diff --in "$TMP/pr-$PR_NUMBER.diff" --out-changed-files "$TMP/changed-files.json" --out-valid-lines "$TMP/valid-lines.json"
```

Build the roster from `changed-files.json`. Always-on roles: `security`, `quality`, `errors`, `perf`. Conditional roles activate when any changed file matches:

- `typescript` — `\.(ts|tsx|cts|mts)$`
- `react` — `\.(tsx|jsx)$` OR `(^|/)(components|pages|app|src/app|src/components|src/pages)/`
- `infra` — `\.sql$` OR `(^|/)migrations/` OR `(^|/)db/migrations/` OR `\.tf$` OR `\.hcl$` OR `(^|/)terraform/` OR `(^|/)Dockerfile` OR `(^|/)docker-compose` OR `(^|/)k8s/` OR `(^|/)kubernetes/` OR `(^|/)helm/` OR `(^|/)deploy/` OR `(^|/)infra(structure)?/`

Write the roster as a flat JSON array of role strings. Do it via jq so the patterns are visible and the JSON is byte-clean:

```bash
jq -c '
  ["security","quality","errors","perf"]
  + ( if any(test("\\.(ts|tsx|cts|mts)$"; "i")) then ["typescript"] else [] end )
  + ( if any(test("\\.(tsx|jsx)$"; "i") or test("(^|/)(components|pages|app|src/app|src/components|src/pages)/"; "i")) then ["react"] else [] end )
  + ( if any(test("\\.sql$"; "i") or test("(^|/)migrations/"; "i") or test("(^|/)db/migrations/"; "i") or test("\\.tf$"; "i") or test("\\.hcl$"; "i") or test("(^|/)terraform/"; "i") or test("(^|/)Dockerfile"; "i") or test("(^|/)docker-compose"; "i") or test("(^|/)k8s/"; "i") or test("(^|/)kubernetes/"; "i") or test("(^|/)helm/"; "i") or test("(^|/)deploy/"; "i") or test("(^|/)infra(structure)?/"; "i")) then ["infra"] else [] end )
' "$TMP/changed-files.json" > "$TMP/roster.json"
```

Read `$TMP/roster.json` to know which specialists you'll spawn.

The helper's `bundle-context` step also needs an empty `claude-md-files.json`. The bundle no longer carries CLAUDE.md guidance (the claude-md specialist was dropped from this plugin), but the helper still reads the file. Write an empty object:

Use the Write tool to create `$TMP/claude-md-files.json` with content `{}`.

---

## [3/6] PR summary pre-pass

Spawn the pr-summary agent once. It returns a single technical paragraph that goes into the spawn-context bundle.

Invoke via the Agent tool:

- `subagent_type: "pr-summary"`
- `description: "PR summary paragraph"`
- `prompt: "Read $TMP/pr-$PR_NUMBER.diff and return a single-paragraph technical summary of PR #$PR_NUMBER in $OWNER/$REPO. Use the actual substituted paths — the diff is at the path you receive verbatim."` (substitute `$TMP`, `$PR_NUMBER`, `$OWNER`, `$REPO` before sending)

Capture the agent's final assistant message verbatim as `SUMMARY`. Write it to a file (the helper reads it from disk to avoid PreToolUse:Write hook collisions on sensitive-API substrings):

Use the Write tool to write `SUMMARY` content to `$TMP/summary.txt`.

---

## [4/6] Build spawn-context bundle, spawn specialists in parallel

Bundle the spawn context. The helper assembles every shared input into one markdown file plus a separate rubric copy:

```bash
"$HELPER" bundle-context --review-tmpdir "$TMP" --head-sha "$HEAD_SHA" --pr-number "$PR_NUMBER" --owner "$OWNER" --repo "$REPO" --repo-root "$REPO_ROOT" --rubric "$RUBRIC" --rubric-out "$TMP/rubric.md" --summary-paragraph "$TMP/summary.txt"
```

The helper writes `$TMP/spawn-context.md` and `$TMP/rubric.md`. Both must exist before specialists run.

Now spawn every roster role **in parallel** — emit one single message that contains one `Agent` tool call per role. Read `$TMP/roster.json` to know the role list. For each role:

- `subagent_type: "<role>"` (one of: `security`, `quality`, `errors`, `perf`, `typescript`, `react`, `infra`)
- `description: "<role> specialist scan"`
- `prompt`:

  ```
  Read $TMP/spawn-context.md and $TMP/rubric.md once at startup. Scan $TMP/pr-$PR_NUMBER.diff for issues in your domain, then Write findings JSON to $TMP/findings/<role>.json per the rubric schema.

  HEAD_SHA: <HEAD_SHA>
  REPO_ROOT: <REPO_ROOT>
  REVIEW_TMPDIR: <TMP>
  PR: #<PR_NUMBER> in <OWNER>/<REPO>
  ```

  Substitute every `<placeholder>` with the actual value before issuing the call. Substitute `<role>` with the role string for that call.

After all Agent calls return, verify each role's findings file exists at `$TMP/findings/<role>.json`. Missing files are surfaced as `missing_roles` by the finalize step — don't retry them.

---

## [5/6] Finalize and confirm

Run the helper's finalize pipeline (dedup → gate → snap → render):

```bash
ROSTER_CSV=$(jq -r 'join(",")' "$TMP/roster.json")
```

```bash
"$HELPER" finalize --diff "$TMP/pr-$PR_NUMBER.diff" --findings-dir "$TMP/findings" --prior-issues "$TMP/prior-issues.json" --head-sha "$HEAD_SHA" --owner "$OWNER" --repo "$REPO" --pr-number "$PR_NUMBER" --expected-roles "$ROSTER_CSV" --out-consolidated "$TMP/consolidated.json" --out-payload "$TMP/payload.json" --out-pending-payload "$TMP/payload-pending.json" --out-body "$TMP/payload-body.json" --out-fallback "$TMP/fallback.md"
```

Read `$TMP/consolidated.json` and display the summary to the user:

```
=== Review summary ===
  Specialists: <specialists_used joined by ", ">
  (If non-empty) Timed out: <timed_out_roles joined by ", ">
  (If non-empty) Missing: <missing_roles joined by ", ">
  (If non-empty) Unreadable: <unreadable_roles joined by ", ">
  (If non-empty) Invalid findings: <invalid_findings.length>
  (If non-empty) Dropped (prior review): <dropped_prior_review.length>
  Inline eligible: <inline_eligible.length>
  Summary only: <summary_only.length>

For each finding in inline_eligible:
  [<id>] <severity> <file>:<line> — <rationale>
For each finding in summary_only:
  [<id>] <severity> (summary) <file>:<line> — <rationale>
```

Then ask the user: `Post review? [Y]es/[n]o/[i]ds <csv>`.

- Empty or `y`/`yes` → post all.
- `n`/`no` → skip posting, proceed to cleanup.
- `ids <csv>` (e.g. `ids sec-1,perf-2`) → re-run finalize with `--include-finding-ids "<csv>"`, then post.

If the user chose `ids`, re-run finalize with the same flags plus `--include-finding-ids "<csv>"` — this rewrites `payload.json`, `payload-pending.json`, `payload-body.json` to the filtered subset while `consolidated.json` keeps the pre-filter audit log (the helper handles that distinction).

---

## [6/6] Post review or skip

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

After posting (or skip), remove the scratch dir. Defensive check: only `rm -rf` paths whose basename starts with `pr-review-` (the prefix we created in step 0):

```bash
case "$(basename "$TMP")" in
  pr-review-*) rm -rf "$TMP" ;;
  *) echo "refusing to remove $TMP (unexpected prefix)" ;;
esac
```

Report `[6/6] Done.` and stop.
