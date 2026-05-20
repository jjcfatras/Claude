# Code Review Specialist Rubrics

Shared by every `code-review-*` specialist agent. Defines the scoring rubric, the findings file schema, and the false-positive list. The command (`${CLAUDE_PLUGIN_ROOT}/commands/code-review.md`) references this from each agent and from the posting step.

The orchestrator invokes `code-review-helper bundle-context --rubric-out $REVIEW_TMPDIR/rubric.md` so this file is copied verbatim to a per-run path that specialists Read once on startup; it is not concatenated into `spawn-context.md` (the all-in-one bundle exceeded the Read tool's 256 KB byte cap on non-trivial PRs). The bundle's Per-PR header carries a `RUBRIC_PATH:` pointer so specialists know where to find the file.

## Confidence scale (0–100)

Calibrate every finding by working through these questions. They drive the score, not whether to keep the finding — report everything with confidence > 0.

- **Pre-existing?** Look at diff prefixes. Issue only on context lines (` `) and the PR doesn't change usage or impact → score low. PR amplifies it (new call sites, broader exposure, moves into shared code) → score on amplified impact.
- **Handled elsewhere?** Use the Grep tool to search for related patterns in the diff. If another path covers it, reduce confidence.
- **Intentional?** If the change is directly related to the PR's stated purpose, score low.
- **CI would catch it?** If a standard linter / type-checker would flag it reliably, score low. Don't run build steps — assume CI runs.
- **False positive per the list below?** Score low.
- **Already flagged by prior review?** Match against `prior_issues` (same file, ±5 lines, or 40+ char snippet overlap). If matched on **unchanged** code, score low. If matched but the line is now `+` in the diff, keep but note the prior flag in the explanation.

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

## Severity scale

- 🔴 **Critical** — Security vulnerabilities, authorization bypasses, data loss risks, crashes in common paths
- 🟡 **Medium** — Missing validations, incorrect behavior in edge cases, documentation gaps for new APIs, migration issues
- 📝 **Minor** — Code duplication, style inconsistencies, minor improvements, nitpicks

## Specialist workflow

Every specialist follows this lifecycle. Don't deviate — the orchestrator's finalize step depends on each specialist landing a findings file at the right path.

1. **Read the spawn-context bundle once** (path supplied in the user prompt; the bundle's Per-PR header points at `RUBRIC_PATH`). The bundle contains every shared input — PR identifiers, the diff path, summary (a deterministic stub unless the orchestrator supplied a prose paragraph), changed files, prior issues, CLAUDE.md content, embedded source at HEAD where applicable.
2. **Read the rubric once** at `RUBRIC_PATH` (this file). It is your source of truth for confidence/severity calibration, the findings schema, the false-positive list, and the boundary rules.
3. **Scan the diff** for issues in your domain. The diff path is the bundle's `DIFF` value — don't refetch via `gh pr diff`. Use `Read` for surrounding context (function signatures, imports, call sites) when the diff alone isn't enough. **Use `Read` to confirm every `line` value before emitting a finding** — see the `line` field rule under "Findings file schema". Hunk-math drift is the single most common reason findings get demoted to summary-only.
4. **Write `$REVIEW_TMPDIR/findings/<role>.json`** per the schema below, using the Write tool. The orchestrator pre-creates `$REVIEW_TMPDIR/findings/` — do not `mkdir -p` or pre-test it before your Write. Treat the file as immutable once written.
   - On a normal scan: set `scan_status: "complete"`.
   - If you cannot complete the scan (tool budget exhausted, repeated read failures, etc.): set `scan_status: "timed_out"` and include whatever findings you accumulated. Partial signal is more useful than no signal.
5. **Return.** Your final assistant message can be a short status line ("Wrote 4 findings, scan complete"). The orchestrator captures the file at `$REVIEW_TMPDIR/findings/<role>.json`, not your message text.

There is no cross-specialist messaging in this plugin — each specialist scans independently and emits a single findings file. Calibrate cautiously when a finding depends on cross-domain knowledge you can't verify from the diff alone; lower the confidence rather than asserting it.

## Findings file schema

Every specialist writes its findings to `$REVIEW_TMPDIR/findings/<specialist>.json` using the Write tool. The Go helper validates strictly — invalid findings are dropped with a warning, but the run continues. To get your findings posted, **match the example below field-for-field**.

**Required top-level keys**: `specialist` (your role, lowercase, no `-reviewer` suffix), `scan_status` (`"complete"` or `"timed_out"`), `findings` (array; may be empty).

**Required per-finding fields**: `id` (non-empty string), `category` (free-form string), `file` (relative path, non-empty), `line` (positive integer — see field rules), `confidence` (0–100 integer), `severity` (exactly `"Critical"` / `"Medium"` / `"Minor"` — title-case, **not** `"critical"`), `rationale`, `explanation`, `code`, `language`.

**Conditionally required per-finding fields**: `suggested_fix` (string with the replacement code when the finding has a concrete code-level fix; `null` only for structural/conceptual findings where no single-snippet replacement applies — see field rules), `startLine` (positive integer ≤ `line`; required when `suggested_fix` spans more than one line; otherwise omit or set to `null` — see field rules).

**Do NOT**:

- Invent top-level keys like `reviewer`, `pr`, `non_findings`, or `summary`. They are silently ignored at parse time but signal you are not following the schema.
- Invent per-finding fields like `recommendation`, `risk`, `title`, `location`, `worst_case_complexity`, `budget_breakdown`. Put that material in `explanation`. Extra fields are ignored, but the surface area drift indicates you may have skipped the required fields.
- Use lowercase severities (`"critical"`, `"medium"`). The helper rejects unknown severities, dropping the finding.
- Omit `line` (the helper treats Go zero-value `0` as invalid; the finding is dropped).
- Use multi-digit prefixed IDs like `PERF-001`. Use the rubric-style `f-1`, `f-2`, … (or `<role>-1` if you prefer); the dedup pipeline only requires uniqueness within your file.
- Pad `explanation` past three sentences. The reader already sees `severity`, `rationale`, `code`, and `suggested_fix` — `explanation` is for what's wrong + impact, not a recap. See the `explanation` field rule below.

Schema:

```json
{
  "specialist": "security",
  "scan_status": "complete",
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
      "category": "architecture",
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

The first finding shows a multi-line replacement anchored over lines 42–44 (`startLine: 42`, `line: 44`). The second is a single-line mechanical fix (`startLine: null`). The third is a structural finding with no single mechanical replacement (`suggested_fix: null`). Match this shape — a single-snippet fix gets a string, a structural finding gets `null`.

Field rules:

- `file` — relative path from repo root.
  - **Default**: a path in the PR diff (the line you're flagging is on a `+` or modified line).
  - **Cross-file omission findings** (e.g., "this PR added X to file A but should have mirrored it in file B"): set `file` to the **PR-touched file** (file A in the example), so the inline-eligibility check has somewhere to anchor. Put the actually-missing path in the `explanation` body. Anchoring to file B (which by definition isn't in the diff) routes the finding to summary-only and loses the inline-comment value.
- `line` — line **in the new version of the file**, as it would appear when checked out at HEAD_SHA. Not the position within a hunk, not the diff-line offset.

  **Don't compute this from the diff alone.** Hunk math is error-prone: multi-hunk files shift offsets, deletions invalidate naive counting, `\ No newline at end of file` markers and rename headers throw off line indices. Specialists routinely emit lines that drift by 1–10+ from the truth, which then either snap noisily or demote to summary-only. The fix is direct: the working tree at scan time IS the HEAD*SHA checkout for this PR, so a `Read` of the file shows you exactly the line numbers GitHub will see. Read the file once, locate the issue visually, and copy that line number into `line`. The diff is for finding \_what* to flag; the file at HEAD is for confirming _where_ it lives. For multi-line issues, use Read to set both `startLine` and `line`.

  Concrete example: if a hunk header reads `@@ -28,3 +286,16 @@` and your finding is on the third added line, do not write `line: 288` based on hunk arithmetic — Read the file, find the issue, and copy the actual line number.

  Must correspond to `+`-prefix or modified lines in the diff (the inline-eligibility check at finalize time enforces this; lines in unchanged regions get demoted to summary-only and lose their inline anchor).

- `explanation` — **≤3 short sentences** covering (1) what is wrong on the flagged line and (2) what can happen if it is ignored. Don't restate the severity rating — `rationale` already does that. Don't describe the fix in prose — that's `suggested_fix`. For CLAUDE.md citations, the verbatim quote counts as one of the three sentences. The renderer labels this section "Issue & impact:" — the field's contents should read as exactly that.
- `language` — for fenced code blocks at posting time (`ts`, `tsx`, `py`, `sql`, `tf`, `yaml`, etc.).
- `suggested_fix` — **required when the finding has a concrete code-level fix** — i.e., a self-contained replacement that goes on the cited line(s). Set to `null` only when no single-snippet replacement applies: architectural findings (rename a module, restructure a folder), findings that span many non-contiguous sites, or findings where picking between several valid fixes needs human judgment. Single-block code change → string. Architectural/conceptual finding → `null`. The renderer wraps the value in a fenced block tagged with `language`, so write only code (or a minimal patch excerpt with surrounding context if it wouldn't read standalone) — no backticks, headings, or prose prefixes like "Fix:". Put reasoning in `explanation`.
- `startLine` — **required when `suggested_fix` spans more than one line.** Set to the first line of the replaced range; `line` remains the last line of the range. Both must reference right-side (added/context) lines inside the diff hunk. For single-line fixes, omit `startLine` or set it to `null`. Specialists routinely forget this on multi-line replacements, which then anchor over only the final line and either misrender or get demoted to summary-only.
- `scan_status` — `"complete"` if you wrote this file normally after a clean scan; `"timed_out"` if you ran out of tool budget or hit repeated tool failures before reaching the end of the diff.

### JSON string escaping (read this if you embed code or quotes)

The Write tool serialises whatever string you pass to it; the helper then loads each `findings/<role>.json` with `encoding/json`. Either layer rejects malformed escapes, dropping the file into `unreadable_roles`.

Fields most likely to contain problematic characters: `explanation`, `rationale`, `code`, `suggested_fix`. Rules:

- **Backticks are literal in JSON strings.** Do **not** escape them as `` \` `` (that's a Markdown rule, not a JSON rule). Write them verbatim: `"explanation": "Use the `await` keyword..."` is fine; `"explanation": "Use the \`await\` keyword..."` is malformed.
- The only escapes JSON requires inside a string are: `\"` (double-quote), `\\` (backslash), `\/` (optional, for forward slash), `\b`, `\f`, `\n`, `\r`, `\t`, `\uXXXX` (for control chars and non-BMP escapes).
- **Newlines in `code` / `suggested_fix`**: write `\n` (a two-character escape inside the JSON string), not a literal newline in the source. The Write tool handles this correctly when you pass a string with embedded `\n` sequences.
- **Prefer the `code` / `suggested_fix` fields over inlining code in `explanation`.** Code fences in `explanation` (` ``` `) work fine but are easy to mis-escape on accident. The renderer always wraps `code` and `suggested_fix` in a fenced block tagged with `language` — that's what those fields are for.
- **Don't embed actual quotes around verbatim source quotes inside `explanation`.** If you need a verbatim quote of CLAUDE.md or another file, set `code` (with `language` matching the source) and reference the file:line in `explanation` prose — that avoids both nested-quote escaping and Markdown-vs-JSON-quoting confusion.

If you suspect your output may have a malformed escape, the cheapest sanity check is to run your finding through `JSON.stringify({...})` in your head — if the string would need extra backslashes to round-trip, you have a bug.

## False-positive list — do not flag

- Pre-existing issues on lines the PR did not modify, unless the PR significantly amplifies the issue.
- Pedantic nitpicks that a senior engineer wouldn't call out.
- Issues a linter/typechecker/compiler would catch (missing imports, type errors, formatting). Assume CI handles these.
- General code-quality concerns (test coverage, documentation) unless explicitly required in CLAUDE.md.
- Issues called out in CLAUDE.md but explicitly silenced in code (e.g., a lint-ignore comment).
- Functional changes that are clearly intentional or directly related to the broader change.
- Issues identical to those already flagged in a prior Claude Code review on unchanged code (use the prior-issues file).

## Specialist boundary rules

These apply to every `code-review-*` specialist in addition to the workflow above.

- **Don't post to GitHub.** The orchestrator handles all posting. Your output is `$REVIEW_TMPDIR/findings/<role>.json` — nothing else.
- **Bash usage is rare.** When you do shell out, follow `${CLAUDE_PLUGIN_ROOT}/references/shell-safety.md` — auto mode handles common patterns; the surviving rules cover real concerns (allowed-tools gaps, destructive ops, RCE).
- **Build the findings JSON with the Write tool**, not `echo`/heredoc/redirection. Write is more reliable for embedding code snippets that contain quotes, backticks, and newlines (a quoting-fidelity concern, not a permission concern).
- **The spawn-context bundle is authoritative.** Read it once at startup. Don't re-fetch what it already gives you.

## Verifying claims with Context7

When a finding's validity hinges on a specific library, framework, or external API (React hooks, Prisma, Next.js routing, AWS SDK, etc.), verify the claim against current docs before finalizing. Call `mcp__plugin_context7_context7__resolve-library-id`, then `mcp__plugin_context7_context7__query-docs`. If docs contradict or don't support the flagged behavior, drop the finding or score it low.

Skip Context7 for general programming patterns, project-internal logic, or anything verifiable from the diff alone — don't burn calls on claims that don't depend on external library behavior.
