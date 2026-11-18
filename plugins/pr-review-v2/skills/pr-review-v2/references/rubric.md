# Code Review Specialist Rubric

Shared by every `pr-review-v2:*` specialist agent. Defines the scoring rubric, the findings schema, and the false-positive list. The `/pr-review-v2` workflow passes this file's path in every specialist's prompt; Read it once at startup.

## Confidence scale (0–100)

Calibrate every finding by working through these questions. They drive the score, not whether to keep the finding — report everything with confidence > 0.

- **Pre-existing?** Look at diff prefixes. Issue only on context lines (` `) and the PR doesn't change usage or impact → score low. PR amplifies it (new call sites, broader exposure, moves into shared code) → score on amplified impact.
- **Handled elsewhere?** Use `git grep` at `<HEAD_SHA>` to search for related patterns in the diff. If another path covers it, reduce confidence.
- **Safer alternative already in the file?** If the same file (or a nearby sibling) already imports/uses the safe counterpart of the pattern you're flagging — `timingSafeEqual` beside a plain `===` credential compare, parameterized queries beside string-built SQL, `.safeParse()` beside a bare `.parse()` — the author demonstrably knew the safe pattern and applied it inconsistently: treat that as clear evidence and score high (75–90). Don't misread the in-file safe usage as "handled elsewhere" unless it actually covers the flagged input. "Nearby sibling" means a peer per the next bullet's definition — one distant look-alike is not evidence the author knew better.
- **Repo convention?** Before treating a construct as a defect (a cast, a wrapper pattern, an error-handling or spec-structure shape), `git grep`/`Read` 2–3 sibling files or methods that use the same construct. If siblings use it identically, it's an established convention, not a defect — score low. Exception: a demonstrably exploitable bug or vulnerability is not excused by repetition; score that on impact. **A sibling is a peer by role, not by syntax:** same base class or interface, same directory or layer, same job — the other DALs, the other route handlers, the other migrations. A file that merely contains similar-looking syntax is not a sibling; `requiredFields: (keyof Credentials)[]` in a vendor service is not precedent for a DAL's column list. Enumerate the peer set **before** citing any precedent, and apply it symmetrically: if the closest peers all do what the diff does, the diff _is_ the convention — score 0, however many look-alikes exist elsewhere in the repo.
- **Intentional?** If the change is directly related to the PR's stated purpose, score low.
- **CI would catch it?** If a standard linter / type-checker would flag it reliably, score low. Don't run build steps — assume CI runs.
- **False positive per the list below?** Score low.
- **Already flagged by prior review?** Match against the prior-issues file named in your prompt (same file, ±5 lines, or 40+ char snippet overlap). If matched on **unchanged** code, score low. If matched but the line is now `+` in the diff, keep but note the prior flag in the explanation.

| Score | Meaning                                               |
| ----: | ----------------------------------------------------- |
|     0 | False positive, clearly intentional, or CI-catchable  |
|    25 | Plausible but unverified                              |
|    50 | Verified but ambiguous (another path might handle it) |
|    60 | Verified, leaning real                                |
|    75 | Verified and confirmed in practice, little ambiguity  |
|    80 | Strong signal, narrow ambiguity                       |
|    90 | Near-certain, clear evidence                          |
|   100 | Certain, unambiguous evidence                         |

When unsure between two levels, pick the lower one.

> **Gate note:** the workflow's gate keeps everything with confidence ≥ 75, keeps confidence 50–74 only when severity is Critical or Medium, and drops everything below 50. A **Minor** finding scored 50–74 is therefore dropped before posting — don't spend budget emitting Minor findings below 75; they never reach the reviewer.

## Severity scale

- 🔴 **Critical** — Security vulnerabilities, authorization bypasses, data loss risks, crashes in common paths
- 🟡 **Medium** — Missing validations, incorrect behavior in edge cases, documentation gaps for new APIs, migration issues
- 📝 **Minor** — Code duplication, style inconsistencies, minor improvements, nitpicks

## Specialist workflow

1. **Read this rubric once.** Your prompt carries every other shared input: the PR identifiers, `HEAD_SHA`, `REPO_ROOT`, `REVIEW_TMPDIR`, the changed-file list, and the paths of the diff and the prior-issues file.
2. **Scan the diff** for issues in your domain. Don't refetch it via `gh pr diff`. Use `Read` for surrounding context (function signatures, imports, call sites) when the diff alone isn't enough. **Use `Read` to confirm every `line` value before emitting a finding** — see the `line` field rule below. Hunk-math drift is the single most common reason findings get demoted to summary-only.

   **Verify the claim itself, not just the line.** An "X is missing" finding requires reading the file and confirming X really is absent — never trust a grep match, the diff hunk alone, or a remembered pattern. For high-impact claims (auth bypass, injection, IDOR, race, data leak), trace the runtime path from the wired-up entry point (route registration, event handler, exported API) that actually triggers it. A finding whose triggering path you cannot construct is "plausible but unverified": score it ≤50 and cap severity at what you actually demonstrated — the gate handles the rest.

   **Read the callee before trusting a call site.** When the diff adds a call to an in-house wrapper or a configurable library API (data-access helper, queue/worker factory, client wrapper), Read the callee's implementation or docs — silent defaults (implicit result limits, fallbacks, retries, truncation) and mandatory pairings are invisible at the call site. If you can't verify, lower confidence.

3. **Return your findings as the structured output**, matching the schema below.
   - On a normal scan: `scan_status: "complete"`.
   - If you cannot complete the scan (tool budget exhausted, repeated read failures, etc.): `scan_status: "budget_exhausted"`, with whatever findings you accumulated. Partial signal is more useful than no signal. Nothing cuts you off — no deadline is enforced — so this status only ever comes from a specialist that chose to stop.

Specialists never see each other's output. After you return, every finding that passes the gate is re-checked by a verifier that can only refute what it can reproduce at `HEAD_SHA`, and a consolidator merges duplicates across specialists. Neither replaces your own verification: when a finding depends on cross-domain knowledge you can't verify from the diff, lower the confidence rather than asserting it.

## Findings schema

The workflow enforces this schema on your structured output. A finding that still fails validation (for example `startLine` greater than `line`) is reported as invalid and never posted.

**Top level**: `scan_status` (`"complete"` or `"budget_exhausted"`), `files_reviewed` (repo-relative paths of the changed files whose hunks or HEAD copy you actually read, plus guidance files you read; grep hits don't count) and `findings` (array; may be empty). The terminal summary compares `files_reviewed` with your domain's changed files and flags a thin scan, so an honest short list is better than a padded one.

**Required per-finding fields**: `id` (non-empty string), `category` (one of the 15 values in § "Category vocabulary"), `file` (relative path, non-empty), `line` (positive integer — see field rules), `confidence` (0–100 integer), `severity` (exactly `"Critical"` / `"Medium"` / `"Minor"`), `rationale`, `explanation`, `code`, `language`, `suggested_fix` (string or `null`), `startLine` (positive integer ≤ `line`, or `null`).

**Do NOT**:

- Invent per-finding fields like `recommendation`, `risk`, `title`, `location`. Put that material in `explanation`.
- Use prefixed IDs like `PERF-001`. Use `f-1`, `f-2`, …; IDs only need to be unique within your output.
- Pad `explanation` past three sentences. The reader already sees `severity`, `rationale`, `code`, and `suggested_fix` — `explanation` is for what's wrong + impact, not a recap. See the `explanation` field rule below.
- Emit a `suggested_fix` identical to `code`. The two render as side-by-side fenced blocks, so an identical (or near-identical) fix reads as "no change." Use `null` when there's no concrete change, and keep both fields tight so a real change is visible — see the `suggested_fix` field rule below.

Example output:

```json
{
  "scan_status": "complete",
  "files_reviewed": ["src/auth/handler.ts"],
  "findings": [
    {
      "id": "f-1",
      "category": "security",
      "file": "src/auth/handler.ts",
      "line": 44,
      "startLine": 42,
      "confidence": 75,
      "severity": "Critical",
      "rationale": "One-sentence justification for confidence and severity.",
      "explanation": "Middleware skips the req.user.role check before next(). Any authenticated user can hit admin endpoints.",
      "code": "const user = req.body.user as User;\nreturn db.users.update(user);",
      "suggested_fix": "const parsed = UserSchema.safeParse(req.body.user);\nif (!parsed.success) return res.status(400).json(parsed.error);\nreturn db.users.update(parsed.data);",
      "language": "ts"
    },
    {
      "id": "f-2",
      "category": "security",
      "file": "src/auth/handler.ts",
      "line": 12,
      "startLine": null,
      "confidence": 80,
      "severity": "Medium",
      "rationale": "Single-line bypass; mechanical fix.",
      "explanation": "Comparison uses == not === — coerces 0/'' to true and lets the role guard pass for empty-string roles.",
      "code": "if (req.user.role == 'admin') {",
      "suggested_fix": "if (req.user.role === 'admin') {",
      "language": "ts"
    },
    {
      "id": "f-3",
      "category": "design",
      "file": "src/auth/handler.ts",
      "line": 88,
      "startLine": null,
      "confidence": 65,
      "severity": "Minor",
      "rationale": "Structural concern with no single mechanical fix.",
      "explanation": "Auth, validation, and persistence all live in this handler. Recommended split is per-concern — too many valid factorings to pin to a single snippet.",
      "code": "await db.users.update(user);",
      "suggested_fix": null,
      "language": "ts"
    }
  ]
}
```

The first finding shows a multi-line replacement anchored over lines 42–44 (`startLine: 42`, `line: 44`). The second is a single-line mechanical fix (`startLine: null`). The third is a structural finding with no single mechanical replacement (`suggested_fix: null`).

Field rules:

- `file` — relative path from repo root.
  - **Default**: a path in the PR diff (the line you're flagging is on a `+` or modified line).
  - **Cross-file omission findings** (e.g., "this PR added X to file A but should have mirrored it in file B"): set `file` to the **PR-touched file** (file A in the example), so the inline-eligibility check has somewhere to anchor. Put the actually-missing path in the `explanation` body. Anchoring to file B (which by definition isn't in the diff) routes the finding to summary-only and loses the inline-comment value.
- `line` — line **in the new version of the file**, as it would appear when checked out at `HEAD_SHA`. Not the position within a hunk, not the diff-line offset.

  **Don't compute this from the diff alone.** Hunk math is error-prone: multi-hunk files shift offsets, deletions invalidate naive counting, `\ No newline at end of file` markers and rename headers throw off line indices. Specialists routinely emit lines that drift by 1–10+ from the truth, which then either snap noisily or demote to summary-only. The fix is direct: locate the issue in a scratch copy of `git show <HEAD_SHA>:<path>` (the working tree is not guaranteed to be at `HEAD_SHA`) and copy that line number into `line`. The diff is for finding _what_ to flag; the file at HEAD is for confirming _where_ it lives. For multi-line issues, use the HEAD content to set both `startLine` and `line`.

  Concrete example: if a hunk header reads `@@ -28,3 +286,16 @@` and your finding is on the third added line, do not write `line: 288` based on hunk arithmetic — Read the file, find the issue, and copy the actual line number.

  Must correspond to `+`-prefix or modified lines in the diff (the inline-eligibility check enforces this; lines in unchanged regions get demoted to summary-only and lose their inline anchor).

- `explanation` — **≤3 short sentences** covering (1) what is wrong on the flagged line and (2) what can happen if it is ignored. Don't restate the severity rating — `rationale` already does that. Don't describe the fix in prose — that's `suggested_fix`. For CLAUDE.md citations, the verbatim quote counts as one of the three sentences. The renderer labels this section "Issue & impact:" — the field's contents should read as exactly that.
- `language` — for fenced code blocks at posting time (`ts`, `tsx`, `py`, `sql`, `tf`, `yaml`, etc.).
- `suggested_fix` — **a string when the finding has a concrete code-level fix** — i.e., a self-contained replacement that goes on the cited line(s). `null` only when no single-snippet replacement applies: architectural findings (rename a module, restructure a folder), findings that span many non-contiguous sites, or findings where picking between several valid fixes needs human judgment. The renderer wraps the value in a fenced block tagged with `language`, so write only code (or a minimal patch excerpt with surrounding context if it wouldn't read standalone) — no backticks, headings, or prose prefixes like "Fix:". Put reasoning in `explanation`. **Scope tightly:** keep both `code` and `suggested_fix` to the line(s) you're changing plus the minimum context needed to read standalone. **Must differ:** if your replacement would be byte-identical to the cited `code`, set `suggested_fix` to `null`. (The workflow drops a fix equal to the code as a backstop, but don't rely on it.)
- `startLine` — **required when `suggested_fix` spans more than one line.** Set to the first line of the replaced range; `line` remains the last line of the range. Both must reference right-side (added/context) lines inside the diff hunk. For single-line fixes, set it to `null`.

### Category vocabulary

`category` is a **closed vocabulary**; the schema rejects any other value. Pick exactly one of these fifteen:

| Category               | Use for                                                                           |
| ---------------------- | --------------------------------------------------------------------------------- |
| `accessibility`        | a11y: semantics, labels, focus management, contrast, keyboard reachability        |
| `api-contract`         | request/response shape, breaking changes, versioning, serialization compatibility |
| `concurrency`          | races, await sequencing, locking, ordering assumptions, idempotency under retry   |
| `config`               | environment/deployment parameterization: defaults, secrets wiring, feature flags  |
| `convention-adherence` | project-documented patterns, CLAUDE.md rules, established naming/structure        |
| `correctness`          | logic that produces a wrong result or wrong state                                 |
| `data-integrity`       | migrations, schema, constraints, persistence and backfill correctness             |
| `design`               | SOLID, coupling, layering, abstraction and responsibility boundaries              |
| `duplication`          | duplicated code or duplicated knowledge (DRY)                                     |
| `error-handling`       | try/catch shape, propagation, transaction boundaries, failure recovery            |
| `observability`        | logging, metrics, alerting, whether a failure ever reaches a human                |
| `performance`          | complexity, N+1, pagination, memory lifecycle, bundle size                        |
| `security`             | authn/authz, injection, SSRF/CSRF, path traversal, crypto misuse, secret exposure |
| `testing`              | missing, wrong, or misleading test coverage                                       |
| `type-safety`          | narrowing, `any`/`unknown`, assertions, generic constraints, exhaustiveness       |

**Categorize the defect, not yourself.** The category answers "what kind of problem is this?", never "which specialist am I?". Several roles legitimately emit `config` or `correctness`; only a security finding is `security`.

This matters mechanically, not just for tidiness. When two specialists flag the same defect from different angles, a shared `category` is the strongest same-defect signal the dedup step has — it is what collapses their findings into one PR comment instead of two.

## False-positive list — do not flag

- Pre-existing issues on lines the PR did not modify, unless the PR significantly amplifies the issue.
- Pedantic nitpicks that a senior engineer wouldn't call out.
- Issues a linter/typechecker/compiler would catch (missing imports, type errors, formatting). Assume CI handles these.
- General code-quality concerns (test coverage, documentation) unless explicitly required in CLAUDE.md. This covers _missing_ tests and docs — defects in tests the diff itself adds or modifies, and docs the PR itself modifies that cite symbols absent from the codebase, are in scope.
- Issues called out in CLAUDE.md but explicitly silenced in code (e.g., a lint-ignore comment).
- Functional changes that are clearly intentional or directly related to the broader change.
- Issues identical to those already flagged in a prior Claude Code review on unchanged code (use the prior-issues file).

## Specialist boundary rules

These apply to every `pr-review-v2:*` specialist in addition to the workflow above.

- **Don't post to GitHub, and don't edit the repository.** The orchestrator handles all posting. Your output is the structured output — nothing else.
- **Bash usage is rare.** When you do shell out: auto mode handles common patterns; the rules that still matter cover real concerns (allowed-tools gaps, destructive ops, RCE). Prefer `Read` for file contents and `jq` for JSON over ad-hoc shell parsing, keep each command a separate Bash call (no `&&` chaining, no leading `cd`), and never pipe to a shell interpreter.
- **Your prompt is authoritative.** Don't re-fetch what it already gives you.

## Verifying claims with Context7

Context7 is granted only to the `security`, `perf`, `typescript`, and `react` specialists — the roles whose findings most often hinge on external library behavior. If you are one of those and a finding's validity hinges on a specific library, framework, or external API (React hooks, Prisma, Next.js routing, AWS SDK, etc.), verify the claim against current docs before finalizing. Call `mcp__plugin_context7_context7__resolve-library-id`, then `mcp__plugin_context7_context7__query-docs`. If docs contradict or don't support the flagged behavior, drop the finding or score it low. If you are a specialist **without** the Context7 tools and a finding hinges on library behavior you can't otherwise verify, lower its confidence rather than asserting — don't attempt the call.

**"Which version is installed" is one of these questions, not a prerequisite to one.** `resolve-library-id` accepts a library name (optionally a version) directly, so there is nothing to look up first. Tracing a pin through `pnpm-workspace.yaml` → `node_modules/.pnpm` → a vendored `.d.ts`/`.d.cts` spends several Bash round-trips — each one a possible permission prompt — to arrive at weaker evidence, since a shipped type declaration proves an API's _type_ shape, not the documented runtime behavior your finding rests on. Reach for `node_modules` only when Context7 has no entry for the library at all.

Skip Context7 for general programming patterns, project-internal logic, or anything verifiable from the diff alone — don't burn calls on claims that don't depend on external library behavior.
