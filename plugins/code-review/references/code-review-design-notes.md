# Code Review Skill — Design Notes

Maintainer-only context for `${CLAUDE_PLUGIN_ROOT}/commands/code-review.md` and `${CLAUDE_PLUGIN_ROOT}/agents/code-review-*.md`. Not read by the skill at runtime. Capture rationale here so the runtime prompts stay terse.

## Cost shape

Two cost surfaces matter, and they do not point the same way:

1. **Per-specialist input tokens.** 4 always-on specialists plus up to 4 conditional, each paid the same shared context (rubric + roster + prior-issues + claude-md-files + changed-files). On its own, this argues for handing the specialist everything in the spawn prompt so it skips a Read round-trip and starts scanning sooner.
2. **Lead serial output streaming.** Whatever the lead inlines into the specialist spawn prompt gets _generated_ by the lead `roster_size ×` (once per specialist) on a single serial output stream. With 6 specialists × a ~5K-token rubric, that is ~30K extra output tokens, which at ~80–100 tok/s adds ~150 s of wall-clock streaming on the lead before any specialist can start. The lead is the bottleneck because no specialist runs until the lead's spawn message finishes streaming.

Surface (2) dominates above ~3 specialists and grows linearly with roster size. The current design (step 2b writes a single `$REVIEW_TMPDIR/spawn-context.md` bundle that every specialist Reads once at startup) trades a per-specialist Read (~1–2 s, in parallel) for the avoided lead-side serial streaming. Concretely: a real run on 6 specialists hit 18,353 output tokens in the spawn message (`610967f5-…` transcript, May 2026) which is what motivated the disk-bundle move; reverting to inline would put that ~150 s back on the critical path.

Specialists remain the dominant _input_ token sink, but lead output is the dominant _latency_ sink.

## Lead-driven finalization

Specialists never mark their own task `completed`. They write `findings/<role>.json` (the lead's "scan done" signal), then stay idle so peers who are still scanning can DM them for cross-verification. The lead waits for every specialist's findings file to land, then broadcasts `finalize_now` — that DM is the cue for specialists to mark their tasks `completed` and become available for shutdown. This guarantees a slow-scanning peer can always reach a fast peer.

A peer that finishes scanning early might still be the only specialist who can verify a finding the slow peer is about to discover. The lead therefore controls when verification stops being possible, not the individual specialist. "Task in\*progress" means "available for DMs"; "task completed" means "no more DMs are coming." Scan-time bounding lives on the lead (step 2d's workload-scaled safety `Monitor`, 240 s floor → 540 s ceiling); specialists do not self-budget.

## DM-driven scan completion (no polling)

This skill no longer polls. Earlier revisions had the lead arm a 90 + 30 + 30 + 30 sequence of `Bash sleep N` waits and `ls findings/` checks; that design was retired in favor of DM-driven wakeups. The current shape:

1. Lead spawns specialists + arms one workload-scaled safety `Monitor({command: "sleep <N>; echo scan_complete_timer_fired"})` (formula below) in the same `<<single-message>>` block, then ends its turn.
2. Each specialist DMs `team-lead` with `scan_complete: <role>` immediately after writing `findings/<role>.json` (rubric step 6). The DM is the lead's wake signal.
3. On every wake-turn the lead counts received `scan_complete` DMs against the roster and either broadcasts `finalize_now` (count == roster) or ends the turn (more DMs incoming). No directory enumeration on the wake-turn — the DMs are the source of truth, and step 3's helper tolerates any stragglers.
4. The safety `Monitor` is a backstop for the rare case where a specialist crashes before any DM. On the happy path it never fires; the lead `TaskStop`s it after broadcasting `finalize_now`.
5. If the safety Monitor fires with roles still missing, the lead arms one 60 s grace `Monitor` after wake-up DMs, then proceeds with whatever landed.

Why DM-driven instead of polled: the harness already wakes the lead on inbound DMs at zero cost; a fixed-cadence sleep both wastes wall-clock on the happy path (every specialist done well before the next poll) and burns cache (sleeps past the 300 s prompt-cache TTL force a cache miss on the next turn). The DM design also collapses the lead's per-run wall-clock from ~210 s pre-task-check (under the old polling design) to whatever the slowest specialist takes — the lead never sleeps unless the safety timer fires.

Wall-clock impact: on a clean run, lead's post-spawn idle is just the specialists' max scan duration (typically 60–180 s). The safety Monitor is dead code on every healthy small-PR run; transcripts `b5a8dd9d` / `c9fa54fb` (May 2026) confirmed every specialist landed well inside it. On large PRs the safety budget extends — see "Why the safety timer scales with changed-files count" below.

## Why scan-time bounding lives on the lead

Earlier revisions pushed a 180 s self-budget into the rubric: each specialist captured `SCAN_START = date +%s` at startup and re-checked elapsed time every few tool calls. Across multiple transcripts (`b5a8dd9d`, `c9fa54fb`, May 2026) the recheck cadence was too coarse to ever fire — every healthy specialist finished well before 180 s, and the budget never caught a real timeout. Per-specialist budgets only added ~15 redundant `date +%s` calls per run plus ~30 lines of rubric prose. The team-level safety `Monitor` (workload-scaled, lead-side) and the `lead-wakeup` DM cover the same failure modes (specialist crashes, runs forever, peer DM never arrives) without per-specialist instrumentation.

## Why the safety timer scales with changed-files count

Prior to May 2026 the safety budget was a flat 240 s sized to sit inside the 300 s prompt-cache TTL — the wake-turn after a fired monitor stayed cache-warm at zero extra cost. That held on small PRs. On a 176-file PR (transcript `6739c9db`, May 2026) none of seven specialists DM'd `scan_complete` inside 240 s; the slowest (`perf`) took 600 s. The safety timer fired before any specialist could even signal completion, forcing the lead-wakeup → grace-window → 3-attempt teardown cascade and — worse — `quality` and `perf` finished after the lead had already run `code-review-helper finalize`, so their findings were silently dropped from the consolidated output.

The current shape (`spawnbatch.computeScanBudget`): `min(240 + 2 × max(0, files - 50), 540)`. Floor is 240 s for ≤50-file PRs (preserves cache behavior). Each additional changed file above the shoulder adds 2 s. Ceiling is 540 s — well past the 300 s TTL, so the wake-turn pays one cache miss, but the wake-turn itself is short (just DM accounting + finalize broadcast) so the cost is bounded to one cache-rebuild. The 540 s ceiling is reached at 295 changed files; PRs above that get the same budget — past a certain point even a workload-scaled timer can't outrun a runaway specialist, and the right move is to abort cleanly.

The tradeoff: small PRs see no change; large PRs trade one cache miss on the wake-turn for not amputating slow specialists' findings. The cache miss costs ~3-4 s on the wake-turn; dropping a specialist's findings can cost the entire utility of the run. The exchange is heavily in favor of the larger budget.

## Why `TaskStop` is no longer part of the teardown ladder

Earlier shapes of step 2g had a substep 5b that called `TaskStop({task_id: <numeric-id>})` on plan-task IDs returned by `TaskList`. The pre-flight section at the top of the command file explicitly warned that `TaskStop` operates on the background-shell namespace, not plan tasks created by `TaskCreate`. Transcript `6739c9db` (May 2026) confirmed the warning: three `TaskStop` calls on live in_progress plan tasks all returned `No task found`. The escalation was dead code that consumed a two-turn handoff (substep 5a `TaskList` → substep 5b `TaskStop`) and produced three `is_error: true` results per degraded teardown. The current shape drops `TaskStop` entirely and replaces the two-substep sequence with one longer drain window (45 s) followed by `TeamDelete` attempt 3. Worst-case wall-clock budget is unchanged (~75-90 s); happy path is `TeamDelete` succeeds on attempt 1 with no waiting at all.

## Why specialist git-show guidance went from advisory to prohibitive

The line-14 sentence in every specialist agent doc used to advise "search [the bundle's source section] before reaching for `git show` or `Read`." Telemetry on transcript `6739c9db` (May 2026) showed specialists ignoring that advisory 56% of the time — 164 of 225 `git show` calls were against files the bundle had already embedded (`lender-referral.service.ts` was re-fetched by all 7 specialists; `aof.service.ts` by 6 of 7). The bundle's embedding payoff (the whole rationale for the per-file + aggregate caps in `bundle.go`) was being eaten by re-fetches. The current line-14 + line-16 wording is prohibitive: scan the `## Source index` block FIRST, and only `git show` files that are demonstrably NOT in the changed-files list. Files marked `_omitted: …_` (over the per-file or aggregate cap) route to paginated `Read`, not `git show` — paginated `Read` against the worktree path costs less than `git show <HEAD_SHA>:<path>` for large files. The advisory→prohibitive shift is a pure prose change; the bundle helper's behavior is unchanged.

## Why one authorized post-posting `TeamDelete` retry

Step 2g caps teardown at 3 attempts in ~90 s with explicit "don't loop" guidance. By that point, if a specialist still holds its slot, the cheapest way to drain it is the natural 30-120 s window of posting the review — the holdout's outgoing-DM queue typically clears in that span. Transcript `6739c9db` (May 2026) showed the lead ignoring the "don't loop" rule and successfully closing the team config with a 4th unauthorized `TeamDelete` ~4 min after attempt 3. One authorized post-posting attempt in step 6 (after `rm -rf` cleanup) captures that real-world pattern without normalizing the loop. It is non-blocking — either it succeeds or it doesn't; no further `TeamDelete` or `Monitor` calls follow. Past that point the leftover team config under `~/.claude/teams/` is harmless until manually GCed.

## Spawn-prompt inlining (and why the bundle is on disk)

Earlier revisions inlined everything (rubric + roster + prior-issues + CLAUDE.md content + changed-file list) into each specialist spawn prompt to save a `Read` round-trip per specialist. That accounting was incomplete — see "Cost shape" above. The full math: inlining costs `roster_size ×` lead output tokens of duplicated content on a single serial stream; one shared on-disk bundle costs each specialist a single Read in parallel. For roster ≥4, the bundle wins by tens of seconds and grows linearly worse for inline as roster size goes up.

Current shape: step 2b builds `$REVIEW_TMPDIR/spawn-context.md` once (rubric verbatim + per-PR sections + verbatim JSON artifacts) and the spawn prompt in 2d points at it. The diff stays as a separate on-disk file because it can be large and is consumed per-specialist anyway.

If the rubric/roster/etc. ever shrinks dramatically (say, < ~500 tokens combined) the math flips back. Until then, keep the bundle on disk and keep the spawn prompt small.

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
