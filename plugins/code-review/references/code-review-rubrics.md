# Code Review Specialist Rubrics

Shared by every `code-review-*` specialist agent. Defines the scoring rubric, the findings file schema, the cross-verification protocol, and the false-positive list. The skill (`.claude/commands/code-review.md`) references this from each agent and from the posting step.

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

CLAUDE.md-flagged issues: cap confidence at 60 unless the rule is quoted verbatim in CLAUDE.md AND the finding is also a clear functional bug.

## Severity scale

- 🔴 **Critical** — Security vulnerabilities, authorization bypasses, data loss risks, crashes in common paths
- 🟡 **Medium** — Missing validations, incorrect behavior in edge cases, documentation gaps for new APIs, migration issues
- 📝 **Minor** — Code duplication, style inconsistencies, minor improvements, nitpicks

## Specialist workflow

Every specialist follows this lifecycle. The lead's coordination depends on the contract — don't deviate.

1. Mark your assignment task `in_progress` (`TaskUpdate({taskId: ASSIGNMENT_TASK_ID, status: "in_progress"})`).
2. **Record your scan-start wall-clock**: run `date +%s` and remember the value as `SCAN_START`. You will use this to self-bound the scan phase (step 6).
3. **Scan the diff** for issues in your domain. Use `Read` for surrounding context (function signatures, imports, call sites) when the diff alone isn't enough. **Also use `Read` to confirm every `line` value before emitting a finding** — see the `line` field rule under "Findings file schema". Hunk-math drift is the single most common reason findings get demoted to summary-only. Don't refetch via `gh pr diff` — the diff path is given in the spawn prompt's `DIFF` section.
4. **Send outgoing verification DMs** as you find uncertain cross-domain issues (see "Cross-verification protocol"). Continue scanning while replies are in flight.
5. **Settle outgoing verifications.** Apply replies per the rubric. Anything still unanswered after the timeout becomes `peer_timeout`.
6. **Self-budget the scan phase.** Before each new `Read`, before each new outgoing DM, and before any other multi-second tool call during scanning, run `date +%s` and compute `elapsed = now - SCAN_START`. If `elapsed > 180` (seconds), stop scanning immediately and proceed to step 7 with whatever findings you have. Do not start new tool calls past the budget — finish the one in flight, then write findings. The 180 s budget is a ceiling, not a target — most specialists finish well inside it. The point is to bound legitimately slow scans (large PRs, many cross-package reads) and to bound stuck specialists (a `Read` on a generated bundle, a peer DM that's never coming) without a lead-side wall-clock backstop. Why 180 s: still below the prompt-cache TTL (300 s), comfortably above realistic scan times even on large PRs (initial diff Read + several cross-file Reads + a peer DM round-trip), and twice the lead's first poll cycle (90 s) so most specialists land naturally in the lead's first check.
7. **Write `$REVIEW_TMPDIR/findings/<role>.json`** per the schema below, using the Write tool. The presence of this file is the lead's signal that you've finished scanning. Treat it as immutable once written — incoming peer DMs after this point are for helping peers verify _their_ findings, not for revising yours.
   - If you reached this step naturally (scan complete, all DMs settled): set `scan_status: "complete"`.
   - If you reached this step because the self-budget in step 6 fired: set `scan_status: "timed_out"` and include whatever findings you accumulated. Do not omit partial findings — incomplete signal is more useful than no signal.
8. **DM `team-lead` with `scan_complete: <role>`.** Send `SendMessage({to: "team-lead", message: "scan_complete: <role>"})` immediately after the Write in step 7 lands. This is the lead's wake signal — without it, the lead idles its 300 s safety-monitor budget every run waiting for someone to hold up the contract (real failure observed in transcript `74931090`: ~109 s of pure post-Write idle on a run where every specialist had already written findings). The DM is small, sent once per specialist, and is the only thing that lets the lead exit the safety-monitor early on the happy path.
9. **Stay idle. Do _not_ mark your task complete on your own.** You remain available to answer incoming `VERIFICATION_REQUEST` DMs from peers who are still scanning. The harness wakes you for incoming messages — you don't need to poll. The self-budget in step 6 bounds your scan phase only; idle-listening for peer DMs has no time cap. Marking your task `completed` here disengages you from the mailbox, which is why step 10 below gates that operation behind `finalize_now` (real failure observed in transcripts `74931090` where two specialists self-completed early and then ignored `shutdown_request` DMs, forcing the full 3-attempt teardown ladder).
10. **On `finalize_now` DM from the lead**: this signals every peer has finished scanning, so no more cross-verifications can arrive. Mark your task `completed` (`TaskUpdate({taskId: ASSIGNMENT_TASK_ID, status: "completed"})`). If `finalize_now` arrived before you reached step 7 (rare with self-budgeting; would mean either your `date +%s` calls didn't fire often enough, or the lead's wall-clock fired exceptionally early), write `findings/<role>.json` first with `scan_status: "timed_out"` and whatever findings you have, then mark complete.
11. **On `shutdown_request` DM**: approve and terminate per the standard team protocol.

Why this shape: a peer that finishes scanning early might still be the only specialist who can verify a finding the slow peer is about to discover. The lead therefore controls when verification stops being possible (step 10), not the individual specialist. "Task in*progress" means "available for DMs"; "task completed" means "no more DMs are coming." The scan-phase self-budget (step 6) is independent: it bounds the time spent \_producing* findings, not the time spent answering peer DMs from others.

### Post-scan idle contract

Restating step 8–10 in the form most likely to bind under load:

- **After Write**: send the `scan_complete: <role>` DM to `team-lead` (step 8). Do nothing else.
- **Do not** call `TaskUpdate({status: "completed"})` here. Step 10's `finalize_now` DM is the only authorisation for that.
- **Do not** send any other proactive `SendMessage` — only reply to incoming `VERIFICATION_REQUEST` and `shutdown_request` DMs.
- The harness wakes you on incoming DMs. You don't poll, you don't `Bash sleep`, you don't busy-loop.

Specialists that violate the contract (most often by pre-emptively `TaskUpdate=completed` right after Write) disengage from their mailbox; subsequent `shutdown_request` DMs go unread, and the lead's teardown ladder degrades into a 75 s wall-clock waste. The contract is short on purpose: Write → DM `scan_complete` → idle → on `finalize_now`, `TaskUpdate=completed` → on `shutdown_request`, terminate.

## Findings file schema

Every specialist writes its findings to `$REVIEW_TMPDIR/findings/<specialist>.json` using the Write tool. The Go helper validates strictly — invalid findings are dropped with a warning, but the run continues. To get your findings posted, **match the example below field-for-field**.

**Required top-level keys**: `specialist` (your role, lowercase, no `-reviewer` suffix), `scan_status` (`"complete"` or `"timed_out"`), `findings` (array; may be empty).

**Required per-finding fields**: `id` (non-empty string), `category` (free-form string), `file` (relative path, non-empty), `line` (positive integer — see field rules), `confidence` (0–100 integer), `severity` (exactly `"Critical"` / `"Medium"` / `"Minor"` — title-case, **not** `"critical"`), `rationale`, `explanation`, `code`, `language`.

**Optional per-finding fields**: `startLine` (positive integer ≤ `line`, or null/omit), `suggested_fix` (code snippet string or null — see field rules), `verifications` (array; empty if none).

**Do NOT**:

- Invent top-level keys like `reviewer`, `pr`, `non_findings`, `summary`, or put a `scan_complete` key inside the JSON body. They are silently ignored at parse time but signal you are not following the schema. (The `scan_complete: <role>` signal is sent as a `SendMessage` DM to `team-lead` per workflow step 8 — it is not a JSON field.)
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
      "line": 42,
      "startLine": null,
      "confidence": 75,
      "severity": "Critical",
      "rationale": "One-sentence justification for confidence and severity.",
      "explanation": "Middleware skips the req.user.role check before next(). Any authenticated user can hit admin endpoints.",
      "code": "const user = req.body.user as User;\nreturn db.users.update(user);",
      "suggested_fix": "const parsed = UserSchema.safeParse(req.body.user);\nif (!parsed.success) return res.status(400).json(parsed.error);\nreturn db.users.update(parsed.data);",
      "language": "ts",
      "verifications": [
        {
          "asked": "typescript",
          "verdict": "confirmed",
          "note": "TS reviewer agrees: assertion bypasses narrowing.",
          "applied_adjustment": 25
        }
      ]
    }
  ]
}
```

Field rules:

- `file` — relative path from repo root.
  - **Default**: a path in the PR diff (the line you're flagging is on a `+` or modified line).
  - **Cross-file omission findings** (e.g., "this PR added X to file A but should have mirrored it in file B"): set `file` to the **PR-touched file** (file A in the example), so the inline-eligibility check at step 3 has somewhere to anchor. Put the actually-missing path in the `explanation` body and in `out_of_diff_reason` if applicable. Anchoring to file B (which by definition isn't in the diff) routes the finding to summary-only and loses the inline-comment value.
- `line` — line **in the new version of the file**, as it would appear when checked out at HEAD_SHA. Not the position within a hunk, not the diff-line offset.

  **Don't compute this from the diff alone.** Hunk math is error-prone: multi-hunk files shift offsets, deletions invalidate naive counting, `\ No newline at end of file` markers and rename headers throw off line indices. Specialists routinely emit lines that drift by 1–10+ from the truth, which then either snap noisily or demote to summary-only. The fix is direct: the working tree at scan time IS the HEAD*SHA checkout for this PR, so a `Read` of the file shows you exactly the line numbers GitHub will see. Read the file once, locate the issue visually, and copy that line number into `line`. The diff is for finding \_what* to flag; the file at HEAD is for confirming _where_ it lives. For multi-line issues, use Read to set both `startLine` and `line`.

  Concrete example: if a hunk header reads `@@ -28,3 +286,16 @@` and your finding is on the third added line, do not write `line: 288` based on hunk arithmetic — Read the file, find the issue, and copy the actual line number. (In this example it would be 288, but only confirm by Read.)

  Must correspond to `+`-prefix or modified lines in the diff (the inline-eligibility check at finalize time enforces this; lines in unchanged regions get demoted to summary-only and lose their inline anchor).

- `explanation` — **≤3 short sentences** covering (1) what is wrong on the flagged line and (2) what can happen if it is ignored. Don't restate the severity rating — `rationale` already does that. Don't describe the fix in prose — that's `suggested_fix`. For CLAUDE.md citations, the verbatim quote counts as one of the three sentences. The renderer labels this section "Issue & impact:" — the field's contents should read as exactly that.
- `language` — for fenced code blocks at posting time (`ts`, `tsx`, `py`, `sql`, `tf`, `yaml`, etc.).
- `suggested_fix` — the corrected code itself, not prose. The renderer wraps the value in a fenced block tagged with `language`, so write only code (or a minimal patch excerpt with surrounding context if it wouldn't read standalone) — no backticks, headings, or prose prefixes like "Fix:". Put reasoning in `explanation`. Use `null` only when no code-level change applies.
- `verifications` — empty array if you didn't DM anyone. One entry per peer asked.
- `scan_status` — `"complete"` if you wrote this file normally at workflow step 7 after a clean scan; `"timed_out"` if either the scan-phase self-budget in workflow step 6 fired with the scan still incomplete, or the lead's `finalize_now` interrupted you before you reached step 7 (rare with self-budgeting).

### JSON string escaping (read this if you embed code or quotes)

The Write tool serialises whatever string you pass to it; the helper then loads each `findings/<role>.json` with `encoding/json`. Either layer rejects malformed escapes, dropping the file into `unreadable_roles` (real failure observed in transcript `74931090`: `errors-reviewer` produced an `Invalid escape at line 29, column 958` on a finding that quoted inline code).

Fields most likely to contain problematic characters: `explanation`, `rationale`, `code`, `suggested_fix`. Rules:

- **Backticks are literal in JSON strings.** Do **not** escape them as `` \` `` (that's a Markdown rule, not a JSON rule). Write them verbatim: `"explanation": "Use the `await` keyword..."` is fine; `"explanation": "Use the \`await\` keyword..."` is malformed.
- The only escapes JSON requires inside a string are: `\"` (double-quote), `\\` (backslash), `\/` (optional, for forward slash), `\b`, `\f`, `\n`, `\r`, `\t`, `\uXXXX` (for control chars and non-BMP escapes).
- **Newlines in `code` / `suggested_fix`**: write `\n` (a two-character escape inside the JSON string), not a literal newline in the source. The Write tool handles this correctly when you pass a string with embedded `\n` sequences.
- **Prefer the `code` / `suggested_fix` fields over inlining code in `explanation`.** Code fences in `explanation` (` ``` `) work fine but are easy to mis-escape on accident. The renderer always wraps `code` and `suggested_fix` in a fenced block tagged with `language` — that's what those fields are for.
- **Don't embed actual quotes around verbatim source quotes inside `explanation`.** If you need a verbatim quote of CLAUDE.md or another file, set `code` (with `language` matching the source) and reference the file:line in `explanation` prose — that avoids both nested-quote escaping and Markdown-vs-JSON-quoting confusion.

If you suspect your output may have a malformed escape, the cheapest sanity check is to run your finding through `JSON.stringify({...})` in your head — if the string would need extra backslashes to round-trip, you have a bug.

## Cross-verification protocol

Whether to DM a peer depends on the severity of the finding. The asymmetry is intentional: a missed false positive on a Critical finding is much more expensive than the marginal latency of a peer round-trip, while a Minor style nit doesn't justify pulling another specialist's attention.

**For Critical or Medium findings**, send a peer DM whenever **both** of these are true:

1. Your tentative confidence is < 75.
2. Cross-domain knowledge is load-bearing for the finding — i.e., a specialist in another domain could raise or lower confidence with information you don't have direct authority on. Don't gate this on whether you _think_ you can answer it yourself; if a peer's expertise materially affects the finding, ask.

   Operational test for criterion 2: ask yourself "to score this finding, did I have to read or trust a file I didn't open?" If yes — a related-PR-touched file in another package, the contents of a CLAUDE.md section that lives in someone else's domain, a runtime contract you're inferring rather than verifying — DM the specialist whose domain owns that file (per the routing table). Examples that should fire a DM:
   - You're flagging "the JS generator should mirror this TS generator change" but you didn't open the JS generator file: DM `quality-reviewer` or `typescript-reviewer`.
   - You're flagging an authz bypass that depends on whether middleware upstream covers it: DM `security-reviewer`.
   - You're flagging a CLAUDE.md compliance issue but the rule's quote requires verifying TS types: DM `typescript-reviewer`.

   The previous rule was correct in spirit but specialists treated "I think I can reason about it" as license to skip the DM. The operational test removes that wiggle room: if your evidence rests on a file you haven't opened, ask.

**For Minor findings**, send a peer DM only if **all** of these are true:

1. Your tentative confidence is < 50.
2. The finding sits primarily in another specialist's domain (use the routing table below) and you genuinely cannot reason about it well from your own perspective.

**For both severities**: if the target specialist isn't on the roster (`$REVIEW_TMPDIR/roster.json`), skip verification, keep your original confidence, and add a `verifications` entry with `verdict: "peer_unavailable"`. Do not chain verifications (one round only — see "One round only" below).

The rationale: at low severity, the calling specialist can usually reason about adjacent-domain knowledge well enough on their own and DMs add noise. At Medium/Critical the calling specialist's secondhand understanding is exactly what we don't want to trust — a single peer round-trip is cheap insurance against landing the wrong call on a real bug.

**Send DMs as you find uncertain issues — don't batch.** Continue scanning while you wait. Replies arrive as new turns; process each one as it comes in, then keep going.

### Routing table

| Finding pattern                                                                       | DM to                 |
| ------------------------------------------------------------------------------------- | --------------------- |
| Type assertions, `any`/`unknown` narrowing, generic constraints, discriminated unions | `typescript-reviewer` |
| Hook deps, re-render, memoization, accessibility, effect cleanup                      | `react-reviewer`      |
| Migration safety, terraform, docker, secrets, infra config                            | `infra-reviewer`      |
| Auth, authorization, input validation, SQL injection, ownership checks                | `security-reviewer`   |
| try/catch, error propagation, async races, unhandled rejections                       | `errors-reviewer`     |
| N+1 queries, loop inefficiency, bundle size, leaks                                    | `perf-reviewer`       |
| CLAUDE.md citation accuracy or scope                                                  | `claude-md-reviewer`  |
| Code duplication, convention/style adherence                                          | `quality-reviewer`    |

### Request format

Send via `SendMessage`. Use a 5–10 word `summary`. Message body — one block per request:

```
VERIFICATION_REQUEST
finding_id: f-3
file: src/auth/handler.ts
line: 42
snippet: const userId = req.body.userId as string;
my_concern: Cast bypasses runtime validation; if userId is undefined, downstream lookup fails open.
my_tentative_confidence: 35
```

### Response format

When you receive a `VERIFICATION_REQUEST`, reply within your next turn:

```
VERIFICATION_RESPONSE
finding_id: f-3
verdict: confirmed
note: Yes — there is no narrowing on this line; the only validation is at line 38 and it doesn't cover this path.
```

`verdict` is exactly one of:

- `confirmed` — caller raises confidence by 25 (capped at 100).
- `false_positive` — caller drops the finding.
- `out_of_scope` — caller keeps original confidence (e.g., the question wasn't really in your domain).

Always include a one-sentence `note` so the caller can quote it in `verification_notes`.

### Timeout

If a peer doesn't reply within **30 seconds** of your `VERIFICATION_REQUEST`, treat the verification as `peer_timeout` and proceed:

- Keep original confidence.
- Add a `verifications` entry with `verdict: "peer_timeout"` and a note `"verifier did not respond within 30s"`.

You don't need to literally count seconds. Heuristics:

- Send DMs early and continue scanning.
- After your scan is complete, wait one more idle cycle to collect late responses.
- When you reach step 7 of the workflow (writing findings), any still-pending outgoing DMs become `peer_timeout`. Don't hold up findings for a slow peer — your incoming-DM availability afterward (step 9) is unrelated to your outgoing verifications.

### One round only

Do not chain verifications. If a peer DMs you and you're unsure, give your best `out_of_scope` reply rather than DMing a third agent. Chained verification is forbidden — it can deadlock the team.

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

- **Don't post to GitHub.** The lead handles all posting. Your output is `$REVIEW_TMPDIR/findings/<role>.json` and your DM replies — nothing else.
- **Bash usage is rare.** You almost never need Bash beyond `date +%s` for self-budget timestamps. When you do shell out, follow `${CLAUDE_PLUGIN_ROOT}/references/shell-safety.md` — auto mode handles common patterns; the surviving rules cover real concerns (allowed-tools gaps, destructive ops, RCE).
- **Build the findings JSON with the Write tool**, not `echo`/heredoc/redirection. Write is more reliable for embedding code snippets that contain quotes, backticks, and newlines (a quoting-fidelity concern, not a permission concern).
- **Spawn-prompt context is authoritative.** The lead inlines the rubric, roster, prior-review issues, CLAUDE.md content, and changed-file list directly into your spawn prompt. Don't Read those files — they're already in your context.

## Verifying claims with Context7

When a finding's validity hinges on a specific library, framework, or external API (React hooks, Prisma, Next.js routing, AWS SDK, etc.), verify the claim against current docs before finalizing. Call `mcp__plugin_context7_context7__resolve-library-id`, then `mcp__plugin_context7_context7__query-docs`. If docs contradict or don't support the flagged behavior, drop the finding or score it low.

Skip Context7 for general programming patterns, project-internal logic, or anything verifiable from the diff alone — don't burn calls on claims that don't depend on external library behavior.
