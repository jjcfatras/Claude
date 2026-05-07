---
allowed-tools: Bash(gh pr comment:*), Bash(gh pr diff:*), Bash(gh pr view:*), Bash(gh api:*), Bash(jq:*), Bash(mktemp:*), Bash(mkdir:*), Bash(base64:*), Bash(rm:*), Bash(date:*), Bash(sleep:*), Bash(find:*), Bash(sed:*), Bash(cat:*), Bash(ls:*), Bash(code-review-helper:*), Read, Write, Grep, Glob, Monitor, Agent, TeamCreate, TeamDelete, TaskCreate, TaskList, TaskGet, TaskUpdate, TaskStop, SendMessage, mcp__*, Skill
description: Code review a pull request via a multi-specialist agent team. Spawns one custom subagent per applicable category (security, types, react, infra, errors, perf, quality, claude-md), coordinates them via a shared task list and peer DMs for cross-domain verification, and posts inline review comments. Cleans up its temp workspace (under /tmp) after posting.
argument-hint: [pr-number]
disable-model-invocation: false
model: opus
effort: xhigh
---

Provide a code review for the given pull request using a multi-specialist agent team.

**Setup:** Run `mktemp -d /tmp/pr-review-XXXXXX` to create a unique temp directory and store the path as `$REVIEW_TMPDIR`. All temp files in this review must be written under `$REVIEW_TMPDIR/`. Create a todo list for steps 1-6.

**Batching contract.** Throughout this command, any block marked `<<single-message>>` must be emitted as **one assistant message containing all of the listed tool_uses in parallel** — not one per turn. The harness runs concurrent tool_uses in a single message simultaneously; emitting them serially across N turns adds N × ~1–2 s of streaming + classifier overhead before any work begins. Anti-pattern observed in transcripts where a 6-call batch took 7 s instead of 1 s. The same rule applies to step 1's plan TaskCreates, step 1a's parallel `gh`, step 2c's specialist TaskCreates, step 2d's Agent fan-out + step 2e's safety Monitor, step 2e's `finalize_now` broadcast, and step 2g's `shutdown_request` broadcast.

Emit the six plan TaskCreates as `<<single-message>>`:

```
<<single-message>>
TaskCreate({subject: "Step 1: Prep PR diff + summary", description: "Fetch PR metadata, diff, prior reviews, CLAUDE.md, summary paragraph", activeForm: "Prepping PR data"})
TaskCreate({subject: "Step 2: Build team + run specialists", description: "Roster, spawn-context bundle, TeamCreate, spawn agents, await scan_complete DMs, teardown", activeForm: "Running multi-specialist scan"})
TaskCreate({subject: "Step 3: Filter + assemble payload", description: "Run code-review-helper finalize for dedup + gating + payload", activeForm: "Filtering findings"})
TaskCreate({subject: "Step 4: Present + confirm", description: "Show user inline + summary findings, request post permission", activeForm: "Presenting findings"})
TaskCreate({subject: "Step 5: Post review", description: "Three-tier ladder: batched create-and-submit → pending+submit → fallback comment", activeForm: "Posting review"})
TaskCreate({subject: "Step 6: Cleanup", description: "rm -rf $REVIEW_TMPDIR after prefix safety check", activeForm: "Cleaning up workspace"})
```

Update each task after the corresponding step completes.

**`/tmp` writability fallback.** Some project allowlists permit `mktemp -d /tmp/...` but block subsequent `Write` and `rm` against `/tmp/` paths. Verify writability before proceeding: use the Write tool to create `$REVIEW_TMPDIR/.writable` (any short content). If the Write is denied, fall back: run `mkdir -p $HOME/.claude/tmp` then `mktemp -d $HOME/.claude/tmp/pr-review-XXXXXX`, point `$REVIEW_TMPDIR` at the new path, and retry the writability sentinel. Do not retry against `/tmp` once it has denied a write — the denial is structural (allowlist scope), not transient.

**Pre-flight: probe team-coordination tools.** This skill's whole design (concurrent specialist scans + lead-driven finalization + peer DMs) hard-depends on `Agent`, `TeamCreate`, `TeamDelete`, `TaskCreate`, `TaskList`, `TaskGet`, `TaskUpdate`, `SendMessage`. Do not trust the tool descriptions in your system prompt — they can claim a tool exists when the runtime has actually scoped it out. **Probe by calling `TaskList`** (a no-op read on an empty task list returns an empty result; a denied call returns a runtime error). Two failure shapes you must distinguish:

- **`Agent is not available inside subagents`** (or any "not available inside subagents" / "subagent" message): the skill is being invoked from a subagent context that structurally cannot spawn its own team. **No allowlist edit will fix this.** Stop and tell the user:
  > The code-review skill cannot run inside an Agent invocation — the team-coordination primitives don't propagate into subagents. Run the skill from the main interactive session instead.
- **`<tool> exists but is not enabled in this context`** / "tool not allowed" / explicit permission denial: the runtime exposes the tool but the project allowlist denies it. Stop and tell the user:
  > The code-review skill needs the team-coordination tools to be allowlisted. Add `Agent`, `TeamCreate`, `TeamDelete`, `TaskCreate`, `TaskList`, `TaskGet`, `TaskUpdate`, `TaskStop`, `SendMessage`, `Monitor` to `permissions.allow` in `.claude/settings.json` and retry.

Either way, **do not silently fall back to a single-agent review.** The skill's confidence calibration, dedup gates, and finding format all assume independent specialist scans + peer DMs; a degraded run produces low-fidelity findings without surfacing the limitation. Cleanup `$REVIEW_TMPDIR` before exiting (per step 6's prefix safety check).

This pre-flight runs **before** step 1 deliberately — step 1 spends several minutes fetching the PR diff and building the valid-line map, and there is no reason to do that work if the team can't be built.

**Required reading for the lead (this session):**

- `${CLAUDE_PLUGIN_ROOT}/references/code-review-rubrics.md` — confidence/severity rubric, findings file schema, cross-verification protocol, false-positive list. The dedup, gating, and posting steps below all reference these. The lead also embeds the rubric verbatim into each specialist's spawn prompt (step 2d), so specialists do not need to Read it themselves.
- `${CLAUDE_PLUGIN_ROOT}/references/shell-safety.md` — seven rules covering real concerns (allowed-tools gaps, the zsh `?ref=SHA` glob bug, no piping to a shell interpreter, harness backgrounding, destructive ops, the `for x in "a" "b"` classifier-crash pattern). Heuristic-only rules retired with auto mode. Specialists rarely invoke Bash beyond `date +%s` and don't need to read this file.

**Execution model:** Step 1 does the deterministic shell + Read work inline on the lead and dispatches a single Sonnet 4.6 prep agent for the LLM-needing summary paragraph. Step 2 builds a team: each specialist is a persistent teammate (under `.claude/agents/code-review-*.md`) that DMs peers for cross-domain verification while scanning. Specialists write `findings/<role>.json`, DM `team-lead` with `scan_complete: <role>` to wake the lead, then stay idle for incoming peer DMs. The lead's turn ends after spawning; each `scan_complete` DM resumes it for one short turn to check whether all findings have landed. Once they have, the lead broadcasts `finalize_now`, which is the cue for specialists to mark their assignment task `completed`. Steps 3-5 run on the lead. Step 6 cleans up.

Design rationale (cost shape, finalization protocol, notification flow, teardown degraded state, etc.) lives in `${CLAUDE_PLUGIN_ROOT}/references/code-review-design-notes.md`. Not read at runtime.

Follow these steps precisely:

## 1. Prep (lead-inline + one Sonnet 4.6 prep agent)

Earlier revisions of this skill ran three prep agents in parallel. In practice the model often emitted only one `Agent` tool_use in the first turn and waited for it to return before launching the others (~75 s of wasted serial time). The current shape avoids that: the lead does the deterministic shell + Read work itself, and only the LLM-needing summary paragraph is dispatched as a single Sonnet 4.6 agent. One `Agent` call → no parallel-emission concern.

### 1a. Lead-inline gh + helper (parallel Bash + Write)

Capture identifiers and prep the diff. Emit the four `Bash` calls below as one assistant message (`<<single-message>>` shape — see step 1 batching note); they're independent and the harness runs them concurrently.

```
<<single-message>>
Bash({command: "gh pr view NUMBER --json headRefOid,headRepository -q '{sha: .headRefOid, owner: .headRepository.owner.login, repo: .headRepository.name}'"})
Bash({command: "gh pr view NUMBER --json url,number,title,headRefName -q '{number, title, headRefName, url}'"})
Bash({command: "gh pr diff NUMBER > $REVIEW_TMPDIR/pr-NUMBER.diff"})
Bash({command: "gh api --paginate repos/OWNER/REPO/pulls/NUMBER/reviews | jq '[.[] | select((.body // \"\") | contains(\"Generated with [Claude Code]\"))] | sort_by(.submitted_at) | last'"})
```

Substitute `NUMBER`, `OWNER`, `REPO` from the user's argument and the head-repo lookup output before emitting. Notes per call:

- `gh pr view NUMBER --json headRefOid,headRepository …` — capture the full HEAD SHA, OWNER, REPO of the **head** repo (forks differ; you still post against the base via the PR's own URL below).
- `gh pr view NUMBER --json url,number,title,headRefName …` — capture the post-target URL and head branch name. **This is the only other `gh pr view --json` call you need.** Do not invent more — `baseRepository` is **not** a valid field, and forks post to the base repo via the PR's own URL (returned by `--json url`).
- `gh pr diff NUMBER > $REVIEW_TMPDIR/pr-NUMBER.diff` — save the diff once. Specialists Read this from disk; don't refetch.
- `gh api --paginate … reviews | jq '[…contains("Generated with [Claude Code]")] | sort_by(.submitted_at) | last'` — pick the most recent prior Claude Code review (if any). Capture its `id`, `submitted_at`, `commit_id`. Then:
  - If found: `gh api --paginate repos/OWNER/REPO/pulls/NUMBER/reviews/ID/comments` and extract `path`, `line`, `start_line`, snippet (text between the first pair of triple-backtick fences in `body`), and first-line description (first line of `body` after stripping the snippet).
  - Use the Write tool to write `$REVIEW_TMPDIR/prior-issues.json`. Schema:

    ```json
    {
      "last_review_date": "...",
      "last_review_commit": "...",
      "issues": [
        {
          "path": "...",
          "line": 0,
          "start_line": 0,
          "snippet": "...",
          "description": "..."
        }
      ]
    }
    ```

    If no prior Claude Code review exists, write the file with `last_review_date` / `last_review_commit` set to `null` and an empty `issues` array.

After the diff file lands, run the helper to extract the changed-files list and valid-line map:

```
code-review-helper diff \
  --in  $REVIEW_TMPDIR/pr-NUMBER.diff \
  --out-changed-files $REVIEW_TMPDIR/changed-files.json \
  --out-valid-lines   $REVIEW_TMPDIR/valid-lines.json
```

The helper handles binary files, renames, deletions, and `+0,0` deletion-only hunks deterministically — do not parse the diff yourself.

If `code-review-helper` is missing (the plugin's `bin/` shim or its prebuilt platform binary is unavailable), abort with: "code-review-helper not on PATH. Reinstall the plugin via `/plugin install code-review@jjcfatras-tools`, or rebuild from source: `cd ${CLAUDE_PLUGIN_ROOT}/tools/code-review-helper && make release`." Don't auto-build.

### 1b. Walk CLAUDE.md (lead-inline Glob + Read)

From `$REVIEW_TMPDIR/changed-files.json`, derive the set of unique parent directories of changed files and walk each up to repo root. That parent-set is the candidate set: only `CLAUDE.md` files that are ancestors of a changed file matter. To find which candidates exist on disk, call `Glob` **once** with `pattern: "**/CLAUDE.md"` and `path` set to the repo root — it returns every `CLAUDE.md` under the repo in a single tool call. Intersect the Glob result with the candidate parent-set, then `Read` each survivor. **Do not** issue one `Read` per candidate path and treat not-found as the no-file signal — that pattern produces N-1 spurious `is_error: true` results per run, inflates the session's tool_failure_rate, and burns latency on every miss. **Do not** pre-test existence with `[ -f "$p" ]` or any `for p in ...; do ... && echo` Bash loop either — that pattern returns exit 1 when the last test fails (see transcript `58a0cb3a` line 121) and pollutes the run with `is_error: true` results from the shell side. Glob is the right tool here because it returns the existence set directly without per-miss errors. Build `$REVIEW_TMPDIR/claude-md-files.json` (Write tool) as `{ "<path>": "<contents>", … }` with verbatim contents. Write `{}` if Glob returned no matches or none of the matches intersected the candidate parent-set.

### 1c. PR Summary prep agent (Sonnet 4.6, single Agent call, foreground)

Spawn one `Agent` call with `subagent_type: "code-review-pr-summary"`, `model: "sonnet"`, `mode: "auto"` (Sonnet 4.6 is the minimum the auto-mode classifier supports — without it, simple shell forms like redirection prompt for permission and stall the agent). The `code-review-pr-summary` subagent ships with this plugin and declares `tools: Read` only — keep the spawn prompt narrow and don't add other tool surfaces inline. Prompt:

```
You are the PR Summary prep agent for PR #NUMBER in OWNER/REPO.

Read $REVIEW_TMPDIR/pr-NUMBER.diff once.

Return a single-paragraph technical summary of the change: what the PR does, which files/areas it touches, the user-visible behavior change, and any obvious test scope. No bulleted lists, no preamble — just the paragraph. Output the paragraph as your final response (no Write call needed).
```

The returned paragraph becomes the `SUMMARY` section in step 2b's spawn-context bundle.

## 2. Build the team and run the multi-specialist review

The pre-flight at the top of the skill has already confirmed the team-coordination tools are usable. If somehow you reached step 2 with any of them missing, abort here rather than degrading.

### 2a. Determine which specialists apply

Based on the changed-file list:

- **HAS_CLAUDE_MD**: true if step 1b found CLAUDE.md files (i.e. `claude-md-files.json` is non-empty).
- **HAS_TYPESCRIPT**: true if any changed file ends in `.ts` or `.tsx`.
- **HAS_FRONTEND**: true if any changed file is in a frontend dir (e.g., `src/components/`, `src/pages/`, `src/hooks/`, `app/`) or has a `.tsx`/`.jsx` extension that contains React components.
- **HAS_INFRASTRUCTURE**: true if any changed file matches migration / terraform / docker / config patterns (`*.sql`, `migrations/`, `*.tf`, `*.hcl`, `docker*`, `Dockerfile*`, infra/deploy directories, or files referencing `secret_manager_path`).

The roster always includes `security`, `quality`, `errors`, `perf`. Conditionals add `claude-md`, `typescript`, `react`, `infra` based on the flags above.

### 2b. Write the roster file and shared inputs

Build `$REVIEW_TMPDIR/roster.json` using the Write tool. Schema:

```json
{
  "team_name": "code-review-<PR_NUMBER>",
  "members": [
    {
      "role": "security",
      "name": "security-reviewer",
      "subagent_type": "code-review-security"
    },
    {
      "role": "react",
      "name": "react-reviewer",
      "subagent_type": "code-review-react"
    }
  ]
}
```

Use `<role>-reviewer` as the teammate `name` (the rubrics file's routing table refers to peers by these names — do not deviate). Include only roles that apply per 2a.

The other shared artifacts are already on disk from step 1:

- `$REVIEW_TMPDIR/changed-files.json` — written by the helper in 1a.
- `$REVIEW_TMPDIR/claude-md-files.json` — written by the lead in 1b (or `{}` if none).
- `$REVIEW_TMPDIR/prior-issues.json` — written by the lead in 1a.

Also create the directory `$REVIEW_TMPDIR/findings/` (specialists will write files into it). Use `Bash` with `mkdir -p`.

**Migration history snapshot (gated).** When `HAS_INFRASTRUCTURE` is true _and_ the changed files include any path matching `*/migrations/*` (or any path under a directory named `migrations`), build a small history index so specialists don't each rediscover the project's migration conventions independently. Without it, on a typical migration PR each of `quality`, `errors`, `typescript`, `claude-md`, and `infra` will independently `Read` 3–5 historical migration files just to learn the local idempotency / ordering convention — real cost observed in transcript `74931090` (12–18 duplicate Reads on a single migration PR).

For each unique parent directory of a changed migration file, run `ls -t <dir>` (Bash) and capture the **5 most recent files** (excluding the touched file itself). Then use the Write tool to create `$REVIEW_TMPDIR/migration-history.json`. Schema:

```json
{
  "migrations/prospect": [
    { "path": "migrations/prospect/2026-04-03.ts", "first_line": "..." },
    { "path": "migrations/prospect/2026-03-26.ts", "first_line": "..." }
  ]
}
```

Capture the first non-blank line of each historical file as `first_line` (use `Read` with `limit: 1` for very small files, or `Bash` `head -n 1`) — usually a `// migration: <name>` or comment header; that's enough to give specialists a quick "is this idempotency-pattern the same one used recently?" signal without each one issuing its own `Read`. Skip if the heuristic surfaces zero historical files (new migration directory). Inject the resulting JSON into the bundle as a new `## Migration history` section (see template below); omit the section entirely on non-infra PRs.

**Build the spawn-context bundle.** Read `${CLAUDE_PLUGIN_ROOT}/references/code-review-rubrics.md` once with the Read tool, then use the Write tool to create `$REVIEW_TMPDIR/spawn-context.md` with the structure below. Each specialist Reads this single file at startup instead of receiving an inlined copy in its spawn prompt — that's what keeps the lead's spawn message small (every additional inlined token would multiply across roster size on the lead's serial output stream).

```
# Code review spawn context (PR #<NUMBER>, <OWNER>/<REPO>)

## Per-PR
- HEAD_SHA: <full HEAD SHA>
- PR_NUMBER: <NUMBER>
- REVIEW_TMPDIR: <$REVIEW_TMPDIR>
- DIFF: $REVIEW_TMPDIR/pr-<NUMBER>.diff

## Summary
<paragraph returned by step 1c PR Summary agent>

## Changed files
<verbatim contents of changed-files.json>

## Roster (active specialists — DM peers by `name`)
<verbatim contents of roster.json>

## Prior issues (most recent prior Claude Code review on this PR; may be empty)
<verbatim contents of prior-issues.json>

## CLAUDE.md content (paths + contents from step 1b; may be empty `{}`)
<verbatim contents of claude-md-files.json>

## Migration history (only if HAS_INFRASTRUCTURE and a migration file is touched; omit otherwise)
<verbatim contents of migration-history.json>

## Rubric
<verbatim contents of ${CLAUDE_PLUGIN_ROOT}/references/code-review-rubrics.md>
```

Concatenate verbatim — don't paraphrase, don't reformat the JSON, don't strip the rubric's headings. The on-disk JSON artifacts remain as durable run artifacts; specialists should not Read them separately because the bundle already contains them.

### 2c. Create the team and assignment tasks

1. `TeamCreate({team_name: "code-review-<PR_NUMBER>", description: "Multi-specialist review for PR <NUMBER>"})`.
2. Emit one `TaskCreate` per roster member in a single `<<single-message>>` block (see top-of-file batching contract):

   ```
   <<single-message>>
   TaskCreate({subject: "Review for <role-1>", description: "Specialist task — write findings to $REVIEW_TMPDIR/findings/<role-1>.json then mark complete.", activeForm: "Reviewing <role-1>"})
   TaskCreate({subject: "Review for <role-2>", description: "Specialist task — write findings to $REVIEW_TMPDIR/findings/<role-2>.json then mark complete.", activeForm: "Reviewing <role-2>"})
   ...one TaskCreate per role in the roster...
   ```

   Capture each returned task ID; you'll pass it to the spawn prompt as `ASSIGNMENT_TASK_ID`.

3. **Persist the role → task-ID mapping to disk.** Use the Write tool to create `$REVIEW_TMPDIR/assignments.json` as a JSON object mapping each role to the assignment-task ID returned in step 2. Schema: `{"security": "7", "quality": "8", ...}`. The teardown ladder in step 2g reads this file to escalate via `TaskStop` deterministically — without it, the lead must guess task IDs from turn-text recall (a recurring failure mode; see step 2g.5).

### 2d. Spawn all applicable specialists in one message

Launch every member of the roster as a teammate via the `Agent` tool. The whole batch — every `Agent` call **and** the step-2e safety `Monitor` — must be one `<<single-message>>` block (see top-of-file batching contract). Anti-pattern observed in transcript `58a0cb3a` lines 204→221 (six specialists serialized over 14 s instead of one batch); same shape recurred in transcript `e1f2b2e5` where seven Agent calls + the Monitor were emitted across eight separate turns over ~19 s. If you find yourself about to emit a single `Agent` call when there are more in this batch, **stop** and re-batch all of them plus the Monitor.

```
<<single-message>>
Agent({team_name: "code-review-<PR_NUMBER>", name: "<role-1>-reviewer", subagent_type: "code-review-<role-1>", mode: "auto", description: "Code review specialist — <role-1>", prompt: <SPAWN_PROMPT for role-1>})
Agent({team_name: "code-review-<PR_NUMBER>", name: "<role-2>-reviewer", subagent_type: "code-review-<role-2>", mode: "auto", description: "Code review specialist — <role-2>", prompt: <SPAWN_PROMPT for role-2>})
...one Agent call per roster member...
Monitor({command: "sleep 300; echo scan_complete_timer_fired", timeout_ms: 305000, persistent: false, description: "code-review scan-complete safety timer"})
```

`mode: "auto"` is required so each specialist runs under the auto-mode classifier (auto-approves safe shell patterns without prompting; required for long unattended scans).

Keep the spawn prompt small. Every shared section (rubric, roster, prior-issues, CLAUDE.md content, changed files, summary) lives in `$REVIEW_TMPDIR/spawn-context.md` from 2b — specialists Read it once at startup. Each additional inlined token here would multiply across roster size on the lead's serial output stream; in earlier revisions a full-inline spawn message hit 18k+ output tokens and added ~150 s of streaming before any specialist could start. Template:

```
You are <role>-reviewer on the code review team for <OWNER>/<REPO> PR #<PR_NUMBER>.

ASSIGNMENT_TASK_ID: <task id captured in 2c>

Your first action is to Read $REVIEW_TMPDIR/spawn-context.md once before doing anything else. It contains every per-PR value (HEAD_SHA, REVIEW_TMPDIR, diff path, summary, changed files, roster, prior issues, CLAUDE.md content) and the rubric. Do not Read the rubric file or any of the JSON artifacts (roster, prior-issues, claude-md-files, changed-files) separately — they are inside the bundle.

After reading the bundle, follow your agent system prompt's workflow and the rubric's "Specialist workflow" section. When emitting `line` for a finding, Read the source file at HEAD to confirm the line number — never compute it from hunk-header arithmetic.
```

The spawn prompt stays small on purpose — every additional inlined token multiplies across roster size and adds serial streaming time on the lead.

When `Agent` is called with `team_name`, it returns immediately (the response includes "Spawned successfully" / "running via mailbox") rather than blocking until the agent's first turn completes. Specialists run asynchronously; you'll be woken via a `scan_complete` DM as each one's findings file lands (see 2e).

### 2e. Wait for scan_complete DMs, then broadcast finalize

Specialists DM `team-lead` with `scan_complete: <role>` once they've written `findings/<role>.json` (rubric workflow step 8). The findings file is the source of truth; the DM is the wake signal. There is no polling cadence — the lead ends its turn after spawning and each DM resumes it for one short turn.

The safety wake `Monitor({command: "sleep 300; echo scan_complete_timer_fired", timeout_ms: 305000, persistent: false, description: "code-review scan-complete safety timer"})` is part of the step-2d `<<single-message>>` block — co-emitted with every `Agent` call. Do not emit it on its own turn after the spawns. It is the single backstop in the rare case where a specialist crashes before sending any DM (its 180 s self-budget should preclude this, but the monitor keeps the skill from hanging if a specialist is structurally broken). On the happy path, every roster role DMs well before the monitor emits and you broadcast finalize without ever consulting it. (Per `${CLAUDE_PLUGIN_ROOT}/references/shell-safety.md` rule #8, `Monitor` is the wake-on-event primitive — never `Bash sleep N` to pace the lead's turn.)

After the spawn-and-timer message, **end your turn**. The next time the harness invokes you, it will be because either (a) a teammate sent a DM or (b) the safety monitor emitted its `scan_complete_timer_fired` line. On every such wakeup turn:

1. **Your only directory-listing call here is** `Glob({pattern: "findings/*.json", path: "$REVIEW_TMPDIR"})` to enumerate which `<role>.json` files have landed. **Do not invoke `Bash ls`, `Bash find`, or any other shell directory-walk on `$REVIEW_TMPDIR/findings/`** — the wake is the DM, not the listing, and `Bash` polling is wasteful (each call streams an exec turn for ~50 bytes of output) and racey against the DM-driven design. Anti-pattern observed in transcript `e1f2b2e5` where the lead ran `ls /tmp/pr-review-.../findings/` three times across three wake turns instead of one Glob per wake. (Per shell-safety rule #9, Glob is the one-shot directory-listing primitive.)
2. If every roster role has a corresponding `<role>.json`, send `finalize_now` to every roster member in one `<<single-message>>` block (see top-of-file batching contract):

   ```
   <<single-message>>
   SendMessage({to: "<role-1>-reviewer", message: "finalize_now: all peers have finished scanning; mark your task complete"})
   SendMessage({to: "<role-2>-reviewer", message: "finalize_now: all peers have finished scanning; mark your task complete"})
   ...one SendMessage per roster member...
   ```

   Then proceed to 2f. Anti-patterns observed in transcripts `58a0cb3a` (six broadcasts serialized over 4 s) and `e1f2b2e5` (seven over 4.4 s).

3. Otherwise, look at what woke you. If it was a teammate DM (or any wake before the safety monitor emitted), end the turn — another DM (or the monitor) will wake you again.
4. **Once the safety monitor has fired and any role is still missing**, send one wake-up DM to each missing role in a single message: `SendMessage({to: "<role>-reviewer", message: "lead-wakeup: your self-budget should have fired by now. Write whatever findings you have with scan_status: 'timed_out' and stay idle for finalize_now."})`. Arm one more `Monitor({command: "sleep 60; echo scan_complete_grace_fired", timeout_ms: 65000, persistent: false, description: "code-review scan-complete grace window"})` as a grace window and end the turn. Single shot — don't keep issuing wake-ups.
5. **On the grace-window wake**, if a role is still missing, treat it as unreachable: track the role name in an `unreachable_roles` list (in your turn text), broadcast `finalize_now` to every roster member, and proceed to 2f. Do **not** write a stub findings file — step 2f's missing-file branch handles consolidation correctly, and a stub races with a slow-but-live agent that may still write its real findings during teardown.

Notification-flow + safety-timer rationale: see `${CLAUDE_PLUGIN_ROOT}/references/code-review-design-notes.md`.

### 2f. Collect findings

The findings files in `$REVIEW_TMPDIR/findings/` are the input to step 3's helper invocation. There is nothing to read or merge here — the helper enumerates the directory itself, tolerates `scan_status: "timed_out"`, and reports missing roster roles via `consolidated.json`'s `missing_roles` field. The only thing this step needs is to confirm (via a Glob check) that _some_ `<role>.json` files actually landed; if the directory is empty, abort the review and surface that to the user rather than feeding an empty workspace to the helper.

**Do not pre-validate or hand-repair specialist findings files.** If you suspect a `findings/<role>.json` is malformed (e.g., you ran a sanity `jq .` on it and got a parse error), **do not** repair it with `sed`/`Edit`/heredoc rewrites. The helper's `internal/findings/load.go` already routes parse-failures to `consolidated.json`'s `unreadable_roles` field at step 3 — that is the supported recovery path. Hand-repairing the file (i) silently swallows the schema violation that produced the bad escape, (ii) loses the signal that a specialist's prompt needs a fix, and (iii) risks introducing a _worse_ malformation if the `sed` pattern is wrong. Real failure observed in transcript `74931090` where the lead `sed`-repaired a double-escaped backtick in `errors.json` instead of letting `unreadable_roles` surface it. If the user wants the malformed specialist's output recovered, they can re-run `/code-review` on that PR or DM the specialist directly — both are cheaper than a hand-repair that masks a recurring schema bug.

### 2g. Tear down the team

`TeamDelete` refuses while teammates are alive, so shut them down first. Best-effort with a hard wall-clock cap — findings are already on disk; one uncooperative specialist must not block step 3. Three attempts with widening Monitor windows (15 s → 30 s → 30 s, ~75 s worst case). Happy-path latency is unchanged: `TeamDelete` succeeds on attempt 1.

Per shell-safety rule #8, every wait window below uses `Monitor` — never `Bash sleep N` — so the harness owns the wake and the user isn't prompted on each iteration.

1. Send a shutdown request to every teammate as one `<<single-message>>` block (see top-of-file batching contract):

   ```
   <<single-message>>
   SendMessage({to: "<role-1>-reviewer", message: {"type": "shutdown_request", "reason": "review complete, team teardown"}})
   SendMessage({to: "<role-2>-reviewer", message: {"type": "shutdown_request", "reason": "review complete, team teardown"}})
   ...one SendMessage per roster member...
   ```

   Same anti-pattern as 2d/2e (transcript `e1f2b2e5` serialized seven shutdowns over ~4.8 s). If you find yourself emitting one `SendMessage` and ending the turn, stop and re-batch the rest with it.

2. Arm `Monitor({command: "sleep 15; echo teardown_wait_done", timeout_ms: 20000, persistent: false, description: "code-review teardown wait 1"})` and end the turn; the emit-line wakes you.
3. Call `TeamDelete()`. On success, proceed to step 3 of the skill.
4. **On "still active member(s)" error (attempt 1 failed)**:
   a. The slow-but-live recovery path that used to require a manual re-Read + re-merge is now implicit: any findings file a holdout writes between attempt 1 and attempt 3 will be picked up automatically when step 3's helper runs (the helper enumerates `$REVIEW_TMPDIR/findings/` once, fresh). No explicit merge step is needed here.
   b. Send one more `shutdown_request` to each named holdout, arm `Monitor({command: "sleep 30; echo teardown_wait_done", timeout_ms: 35000, persistent: false, description: "code-review teardown wait 2"})`, end the turn, then retry `TeamDelete()` on the wake.
5. **On second failure (attempt 2 failed) — escalate via `TaskStop`.** Cooperative shutdown has demonstrably failed; the holdout is producing output on each `shutdown_request` wake (a real failure mode observed in transcript `b466fe08` where `quality-reviewer` violated rubric step 11 and kept the slot active across three `TeamDelete` calls). The escalation is a deterministic two-substep sequence — **do not collapse them into one turn**, because (i) plan-task IDs (1–6 from your step-1 todo) and assignment-task IDs (created in step 2c) live in the same numeric namespace and the model will reach for the wrong one from turn-text recall (real failure observed in transcripts `9c2b43de` and `74931090`), and (ii) specialists self-complete their assignment task on `finalize_now` (rubric step 10), so any task ID captured in step 2c may already have been deleted by the time teardown runs.

   **Substep 5a — re-fetch task state.** In a single message: (i) Read `$REVIEW_TMPDIR/assignments.json` to recover the role → task-ID mapping written in step 2c.3, **and** (ii) call `TaskList()` (no arguments) to get the _current_ live assignment-task IDs. End the turn after the `TaskList` call. The wake delivers `TaskList`'s response.

   **Substep 5b — apply `TaskStop` per holdout.** For each named holdout, look up its task ID in the `assignments.json` map you read in 5a. Then check whether that ID still appears in the `TaskList` response:
   - **If the holdout's task ID is in `TaskList`'s output**: pass that exact numeric string (e.g. `"7"`) to `TaskStop({task_id: "<numeric-id>"})`. **Do not pass the agent id** — spawn results, mailbox notifications, and `shutdown_request` acks all surface a string of the form `<role>-reviewer@<team-name>` (e.g. `quality-reviewer@code-review-1337`); that is the agent id, _not_ a task id. **Do not pass plan-task IDs** (the 1–6 todos you created at the top of the skill) — they live in the same namespace as assignment IDs and `TaskStop`-ing one is a no-op against the agent slot.
   - **If the holdout's task ID is _not_ in `TaskList`'s output**: the specialist already self-completed (rubric step 10). The agent slot is hung but the task is gone — `TaskStop` will reject with `No task found`. **Skip `TaskStop` for this holdout** and rely on the third `TeamDelete` to close out the slot (the holdout's mailbox typically drains on its own within ~30 s).

   Worked example. Suppose `assignments.json` is `{"security": "7", "claude-md": "12"}` and `TaskList` returns `{"tasks": [{"id": "7", "subject": "Review for security", ...}]}` (no entry for `"12"`). For a `claude-md-reviewer` holdout, skip `TaskStop` (its task is gone). For a `security-reviewer` holdout, call `TaskStop({task_id: "7"})`.

   Then arm `Monitor({command: "sleep 30; echo teardown_wait_done", timeout_ms: 35000, persistent: false, description: "code-review teardown wait 3"})`, end the turn, then retry `TeamDelete()` on the wake (attempt 3).

6. **On third failure**, stop trying. Log one warning naming the leftover team + holdout(s) and continue to step 3 of the skill. Don't loop.

Degraded-state explanation: see `${CLAUDE_PLUGIN_ROOT}/references/code-review-design-notes.md`.

## 3. Filter — deduplication, gates, payload assembly

The deterministic pipeline (positional dedup → semantic dedup → prior-review dedup → confidence/severity gate → inline-eligibility classification → payload + fallback assembly) lives in `code-review-helper`. The contract — every rule used to live in prose here — is documented in the helper source under `${CLAUDE_PLUGIN_ROOT}/tools/code-review-helper/internal/` and exhaustively tested. Don't reimplement any of those rules in this skill.

Run:

```
code-review-helper finalize \
  --diff $REVIEW_TMPDIR/pr-<NUMBER>.diff \
  --findings-dir $REVIEW_TMPDIR/findings \
  --prior-issues $REVIEW_TMPDIR/prior-issues.json \
  --head-sha <full HEAD SHA> \
  --owner <OWNER> --repo <REPO> --pr-number <NUMBER> \
  --expected-roles <comma-separated roster role names> \
  --out-consolidated     $REVIEW_TMPDIR/consolidated.json \
  --out-payload          $REVIEW_TMPDIR/payload.json \
  --out-pending-payload  $REVIEW_TMPDIR/payload-pending.json \
  --out-body             $REVIEW_TMPDIR/payload-body.json \
  --out-fallback         $REVIEW_TMPDIR/fallback.md
```

The helper:

- Loads every `findings/<role>.json` (tolerating `scan_status: "timed_out"` and missing files; the role names supplied via `--expected-roles` are checked so the consolidated output reports which roster roles never delivered).
- Runs both dedup passes, prior-review dedup, the confidence/severity gate, and inline-eligibility snapping.
- Writes `consolidated.json` (`{inline_eligible, summary_only, dropped_prior_review, specialists_used, timed_out_roles, missing_roles, unreadable_roles, invalid_findings, last_review_date}`) — read this at step 4.
- Writes `payload.json` already shaped for `gh api ... reviews --input` (the batched create-and-submit form), `payload-pending.json` (same shape minus the `event` field, for the two-step fallback in step 5b), `payload-body.json` (just `{"body":"…"}` for the submit step of the two-step), and `fallback.md` for `gh pr comment -F` if posting fails.

Read `consolidated.json` after the call. If `inline_eligible` and `summary_only` are both empty, stop and present the empty result in step 4.

## 4. Present and confirm

Read `$REVIEW_TMPDIR/consolidated.json`. Show the user the inline-eligible + summary-only findings (severity, confidence, file:line, description) for each. If `dropped_prior_review` is non-empty, include "Skipped N issue(s) already flagged in prior review (`<last_review_date>`)." plus a brief list (file:line — description) so the user can override if needed. If `missing_roles`, `timed_out_roles`, or `unreadable_roles` is non-empty, surface those names so the user knows the review may be incomplete. If `invalid_findings` is non-empty, surface each entry as `<role> finding <id>: <reason>` — the helper dropped it because it didn't match the rubric schema (e.g. missing `line`, lowercase severity, missing required field). The user should know which specialist to re-scan or whose output is partial; do not silently swallow these.

Ask permission to post. If the user declines, skip step 5 (still run step 6 cleanup).

## 5. Post the review

Use the GitHub Reviews API via `gh api` (single API call, single notification, inline comments on relevant diff lines).

### 5a. Validate the payloads

The helper already produced four files in `$REVIEW_TMPDIR/`:

- `payload.json` — batched create-and-submit (`event: "COMMENT"`); ready for `gh api ... reviews --input`. Used in 5b step 1.
- `payload-pending.json` — same shape, **no `event` field**. Used in 5b step 2 (creates a pending review).
- `payload-body.json` — `{"body": "…"}`. Used in 5b step 2 to submit the pending review via `--input` without re-quoting JSON in shell.
- `fallback.md` — markdown body for `gh pr comment -F` (last-resort tier).

Sanity check: `jq . $REVIEW_TMPDIR/payload.json $REVIEW_TMPDIR/payload-pending.json $REVIEW_TMPDIR/payload-body.json`. If `jq` rejects any of them, the helper has a bug — surface the parse error and stop; don't try to repair the JSON by hand.

ISSUE_FORMAT, the three review-summary variants, summary-table column rules, GitHub blob-link format, and severity emojis (🔴 Critical, 🟡 Medium, 📝 Minor) are all owned by `${CLAUDE_PLUGIN_ROOT}/tools/code-review-helper/internal/render/`. If the format needs to change, edit the renderers and their golden tests — do not edit the JSON the helper produced.

### 5b. Post (three-tier ladder)

The GitHub Reviews API intermittently returns `HTTP 422 "An internal error occurred, please try again."` on the batched create-and-submit endpoint, even when the payload is structurally valid (transcript `58a0cb3a`, lines 395/408/454: three identical retries of the same payload, all 422). Retrying the same call doesn't help — the workaround is to switch to the two-step pending+submit flow that GitHub's UI uses internally.

**Tier 1 — happy path (single call):**

```
gh api repos/OWNER/REPO/pulls/NUMBER/reviews --method POST --input $REVIEW_TMPDIR/payload.json
```

(Substitute actual OWNER, REPO, NUMBER values.) On HTTP 200, the review is posted with inline comments — done, proceed to step 6.

**Tier 2 — on HTTP 422 only, fall through to two-step pending+submit.** Treat any other failure (4xx ≠ 422, 5xx, network) as terminal and skip to tier 3. **Do not retry** the tier-1 call with the same payload (the same payload will 422 again). Tier 2 is a different code path; either it works on the first try or it doesn't:

1. Create the pending review:
   ```
   REVIEW_ID=$(gh api repos/OWNER/REPO/pulls/NUMBER/reviews --method POST \
       --input $REVIEW_TMPDIR/payload-pending.json --jq .id)
   ```
   If this also 422s, skip to tier 3.
2. Submit the pending review:
   ```
   gh api repos/OWNER/REPO/pulls/NUMBER/reviews/$REVIEW_ID/events --method POST \
       --input $REVIEW_TMPDIR/payload-body.json -f event=COMMENT
   ```
   On HTTP 200, the review is posted with inline comments — done, proceed to step 6.
3. If the submit fails, the pending review is dangling on the PR. Surface the dangling review ID to the user with this one-line note (verbatim, substituting actual values):

   > A pending review (id `$REVIEW_ID`) is left on the PR. Delete it with `gh api repos/OWNER/REPO/pulls/NUMBER/reviews/$REVIEW_ID --method DELETE`.

   Then proceed to tier 3.

**Tier 3 — issue-comment fallback.** Read `$REVIEW_TMPDIR/fallback.md` and use the Edit tool to replace the literal placeholder `{API_ERROR}` with the actual error message from whichever tier failed last (include both 422 errors if tier 2 also failed), then post with `gh pr comment NUMBER -F $REVIEW_TMPDIR/fallback.md`. (`fallback.md` already lists every issue with its `**path:line**` prefix; only the error message needs to be patched in.)

## 6. Cleanup

Remove the temp workspace. Sanity check before deletion: `$REVIEW_TMPDIR` must start with one of the two writable roots created in setup — `/tmp/pr-review-` or `$HOME/.claude/tmp/pr-review-` — and must equal the path created at the start of this run. If the prefix check fails, log a warning and skip cleanup rather than risk an unintended delete.

Run `rm -rf $REVIEW_TMPDIR` only after the prefix check passes. If `rm` itself is denied by the project allowlist (some configurations grant `mktemp` but not `rm` against `/tmp/`), log a single-line warning naming the leftover path so the user can clean it manually, and continue — do not retry, do not fall back to a per-file deletion loop.

This runs even if the user declined posting in step 4 (the workspace is no longer needed) and even if the API post failed (the fallback comment is already on the PR).

## Notes

- Use `gh` for fetching PR data. Use `gh api repos/OWNER/REPO/pulls/NUMBER/reviews` for posting.
- Cite and link every issue (e.g., link CLAUDE.md when referenced).
- The confidence/severity rubric, findings schema, cross-verification protocol, and false-positive list live in `${CLAUDE_PLUGIN_ROOT}/references/code-review-rubrics.md`. Do not re-list them here — specialists and the lead both read that file.
