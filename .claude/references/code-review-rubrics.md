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
2. **Scan the diff** for issues in your domain. Use `Read` for surrounding context (function signatures, imports, call sites) when the diff alone isn't enough. Don't refetch via `gh pr diff` — the diff is already on disk at `DIFF_FILE`.
3. **Send outgoing verification DMs** as you find uncertain cross-domain issues (see "Cross-verification protocol"). Continue scanning while replies are in flight.
4. **Settle outgoing verifications.** Apply replies per the rubric. Anything still unanswered after the timeout becomes `peer_timeout`.
5. **Write `$REVIEW_TMPDIR/findings/<role>.json`** per the schema below, using the Write tool. The presence of this file is the lead's signal that you've finished scanning. Treat it as immutable once written — incoming peer DMs after this point are for helping peers verify _their_ findings, not for revising yours.
6. **Stay idle. Do _not_ mark your task complete on your own.** You remain available to answer incoming `VERIFICATION_REQUEST` DMs from peers who are still scanning. The harness wakes you for incoming messages — you don't need to poll.
7. **On `finalize_now` DM from the lead**: this signals every peer has finished scanning, so no more cross-verifications can arrive. Mark your task `completed` (`TaskUpdate({taskId: ASSIGNMENT_TASK_ID, status: "completed"})`). If `finalize_now` arrived before you reached step 5 (the lead's wall-clock backstop fired while you were still scanning), write `findings/<role>.json` first with `scan_status: "timed_out"` and whatever findings you have, then mark complete.
8. **On `shutdown_request` DM**: approve and terminate per the standard team protocol.

Why this shape: a peer that finishes scanning early might still be the only specialist who can verify a finding the slow peer is about to discover. The lead therefore controls when verification stops being possible (step 7), not the individual specialist. "Task in_progress" means "available for DMs"; "task completed" means "no more DMs are coming."

## Findings file schema

Every specialist writes its findings to `$REVIEW_TMPDIR/findings/<specialist>.json` using the Write tool. Schema:

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
      "explanation": "Detailed explanation of why this is an issue and its impact. If CLAUDE.md-triggered, quote the relevant section.",
      "code": "the problematic code from the PR (multi-line allowed)",
      "suggested_fix": "example of how to fix it (or null if not applicable)",
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
- `line` — line **in the new version of the file**, as it would appear when checked out at HEAD_SHA. Not the position within a hunk, not the diff-line offset. Concrete example: if a hunk header reads `@@ -28,3 +286,16 @@` and your finding is on the third added line, `line: 288`, not `line: 3`. For multi-line issues, set `startLine` and use `line` as the end. Must correspond to `+`-prefix or modified lines in the diff.
- `language` — for fenced code blocks at posting time (`ts`, `tsx`, `py`, `sql`, `tf`, `yaml`, etc.).
- `verifications` — empty array if you didn't DM anyone. One entry per peer asked.
- `scan_status` — `"complete"` if you wrote this file normally at workflow step 5; `"timed_out"` if the lead's `finalize_now` interrupted you mid-scan and you're committing partial results per workflow step 7.

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
- When you reach step 5 of the workflow (writing findings), any still-pending outgoing DMs become `peer_timeout`. Don't hold up findings for a slow peer — your incoming-DM availability afterward (step 6) is unrelated to your outgoing verifications.

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

## Verifying claims with Context7

When a finding's validity hinges on a specific library, framework, or external API (React hooks, Prisma, Next.js routing, AWS SDK, etc.), verify the claim against current docs before finalizing. Call `mcp__plugin_context7_context7__resolve-library-id`, then `mcp__plugin_context7_context7__query-docs`. If docs contradict or don't support the flagged behavior, drop the finding or score it low.

Skip Context7 for general programming patterns, project-internal logic, or anything verifiable from the diff alone — don't burn calls on claims that don't depend on external library behavior.
