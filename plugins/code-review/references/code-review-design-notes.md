# Code Review Skill — Design Notes

Maintainer-only context for `${CLAUDE_PLUGIN_ROOT}/commands/code-review.md` and `${CLAUDE_PLUGIN_ROOT}/agents/code-review-*.md`. Not read by the skill at runtime. Capture rationale here so the runtime prompts stay terse.

## Cost shape

Specialists are the dominant token sink — 4 always-on plus up to 4 conditional, each paid the same shared context. Anything you can hand the specialist via the spawn prompt (which is one model input) avoids a tool-call round-trip and keeps the specialist scanning instead of reading. Step 2d inlines the rubric, roster, prior-issues, and CLAUDE.md content for exactly this reason; only the diff (which can be large) is left as a file.

## Lead-driven finalization

Specialists never mark their own task `completed`. They write `findings/<role>.json` (the lead's "scan done" signal), then stay idle so peers who are still scanning can DM them for cross-verification. The lead waits for every specialist's findings file to land, then broadcasts `finalize_now` — that DM is the cue for specialists to mark their tasks `completed` and become available for shutdown. This guarantees a slow-scanning peer can always reach a fast peer.

A peer that finishes scanning early might still be the only specialist who can verify a finding the slow peer is about to discover. The lead therefore controls when verification stops being possible, not the individual specialist. "Task in*progress" means "available for DMs"; "task completed" means "no more DMs are coming." The scan-phase self-budget is independent: it bounds the time spent \_producing* findings, not the time spent answering peer DMs from others.

## Polling cadence rationale

The lead's poll cadence intentionally trails the specialist self-budget by ~30 s on each end. The lead spawns specialists, then waits a long first window (90 s) so the specialists' initial Read of the diff plus an early scanning pass can finish before the first findings check; the trailing 30 s recheck windows after that catch slow scans without prematurely declaring a specialist stuck.

Wall-clock budget: 90 + 30 + 30 + 30 + (0 or 15 if wake-up needed) + 15 = 195 to 210 s pre-task-check. Still comfortably under the 300 s prompt-cache TTL for the lead's own context even when the wake-up path fires.

The 90 s first sleep gives every specialist time for context ingestion, an initial diff read, a first scanning pass, and a round of cross-verification DMs. Most specialists land within this window.

## Self-budget rationale (specialist-side)

The 180 s scan-phase self-budget in the specialist workflow is a ceiling, not a target — most specialists finish well inside it. The point is to bound legitimately slow scans (large PRs, many cross-package reads) and to bound stuck specialists (a `Read` on a generated bundle, a peer DM that's never coming) without a lead-side wall-clock backstop. Why 180 s: still below the prompt-cache TTL (300 s), comfortably above realistic scan times even on large PRs (initial diff Read + several cross-file Reads + a peer DM round-trip), and twice the lead's first poll cycle (90 s) so most specialists land naturally in the lead's first check.

## Spawn-prompt inlining

The spawn prompt is the specialist's only model input besides its system prompt, so put everything they need to start scanning there. The diff is the one thing left as a file because it can be large; everything else (rubric, roster, prior issues, CLAUDE.md content, changed-file list) is small enough to inline and saves a `Read` round-trip per specialist.

## Teardown degraded state

`TeamDelete` is best-effort with a hard wall-clock cap because findings are already on disk by the time teardown starts. A single uncooperative specialist must not block the posting step. If teardown fails after two attempts, log a warning and continue — the leftover team config under `~/.claude/teams/` is harmless until GCed manually, and the lead's pre-existing task list does not return until the team is removed by the user. This is a documented degraded state, not a failure.

## Cross-verification protocol asymmetry

The Critical/Medium DM bar (`confidence < 75 + cross-domain knowledge load-bearing`) is intentionally asymmetric with the Minor bar (`confidence < 50 + sits primarily in another's domain`). A missed false positive on a Critical finding is much more expensive than the marginal latency of a peer round-trip, while a Minor style nit doesn't justify pulling another specialist's attention.

The operational test on the Medium/Critical side ("did I have to read or trust a file I didn't open?") replaced an earlier rule that specialists treated as license to skip the DM. The current phrasing removes that wiggle room.

## Cross-file omission anchoring

Findings of shape "this PR added X to file A but should have mirrored it in file B" anchor on the **PR-touched file** (file A), not the missing-mirror file (file B). The reason: the inline-eligibility check at posting time looks for the finding's `file` in the valid-line map. Anchoring to file B routes the finding to summary-only (B isn't in the diff) and loses the inline-comment value. Anchoring to A keeps the finding inline-eligible at the place a reviewer would actually act on it.

## Semantic dedup pass rationale

Positional dedup alone misses the common case where two specialists correctly identify the same cross-file omission from different angles (e.g., `quality` flags "JS generator missing X" while `claude-md` flags "CLAUDE.md says update both generators"). Both findings are real, but a reviewer reading the consolidated list shouldn't see the same defect twice. The semantic pass catches that — file-path-in-explanation match OR 60-char common-substring + related-category — without false-positiving distinct findings that happen to share words.

## Why specialists don't mark their own tasks complete

If specialists self-completed when their findings file landed, a fast specialist could shut down before a slow peer's verification DM arrived, deadlocking the slow peer. The lead-broadcast `finalize_now` decouples "I am done producing findings" (file on disk) from "I am no longer available for DMs" (task completed). The harness wakes idle specialists for incoming messages, so a specialist's idle wait between findings-write and finalize_now has no resource cost beyond the live agent slot.
