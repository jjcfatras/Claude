---
allowed-tools: Bash(gh pr comment:*), Bash(gh pr diff:*), Bash(gh pr view:*), Bash(gh api:*), Bash(jq:*), Bash(mktemp:*), Bash(mkdir:*), Bash(base64:*), Bash(rm:*), Bash(date:*), Bash(sleep:*), Bash(find:*), Bash(sed:*), Bash(cat:*), Bash(git:*), Bash(code-review-helper:*), Read, Write, Grep, Glob, Monitor, Agent, TeamCreate, TeamDelete, TaskCreate, TaskList, TaskGet, TaskUpdate, TaskStop, SendMessage, mcp__*, Skill
description: Code review a pull request via a multi-specialist agent team. Spawns one custom subagent per applicable category (security, types, react, infra, errors, perf, quality, claude-md), coordinates them via a shared task list and peer DMs for cross-domain verification, and posts inline review comments. Cleans up its temp workspace (under /tmp) after posting.
argument-hint: [pr-number]
disable-model-invocation: false
model: opus
effort: xhigh
---

Provide a code review for the given pull request using a multi-specialist agent team.

**Setup:** Run `mktemp -d /tmp/pr-review-XXXXXX` to create a unique temp directory and store the path as `$REVIEW_TMPDIR`. All temp files in this review must be written under `$REVIEW_TMPDIR/`.

**Plan:** 1. Prep PR data, 2. Build team and run specialists, 3. Filter findings, 4. Present and confirm, 5. Post review, 6. Cleanup.

**Batching contract.** The four roster-driven fan-out sites (step 2c specialist TaskCreates, step 2d Agent fan-out + safety Monitor, step 2e `finalize_now` broadcast, step 2g `shutdown_request` broadcast) are now emitted by `code-review-helper spawn-batch` and Read-then-echoed by the lead — see the per-step instructions for the exact invocation. The lead's job at each site is to (1) Bash the helper to produce `$REVIEW_TMPDIR/<kind>-batch.md`, (2) Read that file, and (3) echo every line from the `<!-- echo block below verbatim ... -->` marker to EOF as the body of the next assistant message, with no prose inserted between or within the tool-call lines. Two residual prose-only `<<single-message>>` sites remain — the pre-flight probe (`TaskList` + `TaskCreate`) and step 1a's three `gh` Bash calls — emit each as one assistant message containing every listed tool_use; if you find yourself about to emit fewer, stop and re-batch.

**Substep 0a — `/tmp` writability sentinel (mandatory; do not skip).** Some project allowlists permit `Bash(mktemp:*)` but scope the `Write` tool away from `/tmp/` paths. The `mktemp -d` above will succeed against such allowlists; subsequent specialist `Write`s into `$REVIEW_TMPDIR/findings/` will silently fail mid-run after the team has done expensive work.

Before any other step, exercise the agent's `Write` tool against the temp dir:

```
Write({file_path: "$REVIEW_TMPDIR/.writable", content: "ok"})
```

This is **not** a shell redirect or `Bash echo > …` — those ride `Bash`-tool permissions, not `Write`-tool permissions, and don't probe the right code path. **Do not proceed to the pre-flight or step 1 until this sentinel succeeds (or the fallback below resolves).**

If the `Write` is denied, fall back: run `mkdir -p $HOME/.claude/tmp`, then `mktemp -d $HOME/.claude/tmp/pr-review-XXXXXX`, point `$REVIEW_TMPDIR` at the new path, and retry the sentinel. Do not retry against `/tmp` once it has denied a write — the denial is structural (allowlist scope), not transient. If the fallback path also denies the sentinel, abort and tell the user to grant `Write` to either `/tmp/pr-review-*` or `$HOME/.claude/tmp/pr-review-*` in `.claude/settings.json` `permissions.allow`.

**Pre-flight: probe team-coordination tools.** This skill's whole design (concurrent specialist scans + lead-driven finalization + peer DMs) hard-depends on `Agent`, `TeamCreate`, `TeamDelete`, `TaskCreate`, `TaskList`, `TaskGet`, `TaskUpdate`, `SendMessage`. Do not trust the tool descriptions in your system prompt — they can claim a tool exists when the runtime has actually scoped it out. **Probe in this order, as a single `<<single-message>>` batch:**

1. `TaskList()` — a no-op read on an empty task list returns an empty result; a denied call returns a runtime error.
2. `TaskCreate({subject: "preflight-probe", description: "schema probe — discarded immediately", activeForm: "Probing"})` — exercises `TaskCreate`'s real parameter schema (not just availability). **Use exactly these three fields; do not pass `team_name`.** A successful return also plants a fresh in-context example of the correct call shape, which materially reduces the chance the model later adds extra params under priming from `TeamCreate({team_name})` or `Agent({team_name})` in step 2.

Capture the probe task's returned ID and immediately delete it with `TaskUpdate({taskId: <returned-id>, status: "deleted"})` to avoid leaving a stray task in the plan list. (`TaskStop` is for the background-shell namespace, not plan tasks — calling it here returns `No task found` even when the plan task is alive, leaving the probe sitting in the list for the rest of the run.) If the `TaskCreate` probe fails with `InputValidationError` or a similar schema error, abort the run and surface the error verbatim — the runtime's `TaskCreate` shape has drifted from what this skill expects, and the abort is preferable to spending 2–3 minutes on step 1 prep that will be wasted at step 2c. If `TaskUpdate` itself errors, tolerate quietly and continue.

Two pre-flight failure shapes you must distinguish:

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

Capture identifiers and prep the diff. The post target is the PR's `url` field — never look up the base repo as a separate call. Emit the three `Bash` calls below as one assistant message (`<<single-message>>` shape — see step 1 batching note); they're independent and the harness runs them concurrently.

```
<<single-message>>
# These three are the ONLY gh pr view / gh pr diff / gh api calls in step 1a.
# Do NOT add a 4th — in particular, `gh pr view NUMBER --json baseRepository` is
# rejected by gh ("Unknown JSON field: baseRepository"). Derive the base repo
# from the PR's `url` field. See "head vs. base" note below.
#
# Note the head owner field name: `.headRepositoryOwner.login`. There is NO
# `.headRepository.owner.login` — that path silently evaluates to null (the
# call exits 0 with `owner: null` in the JSON). `headRepository` and
# `headRepositoryOwner` are siblings in `gh pr view --json`, not nested.
Bash({command: "gh pr view NUMBER --json headRefOid,headRepository,headRepositoryOwner,url,number,title,headRefName -q '{sha: .headRefOid, owner: .headRepositoryOwner.login, repo: .headRepository.name, number, title, headRefName, url}'"})
Bash({command: "gh pr diff NUMBER > $REVIEW_TMPDIR/pr-NUMBER.diff"})
Bash({command: "gh api --paginate repos/OWNER/REPO/pulls/NUMBER/reviews | jq '[.[] | select((.body // \"\") | contains(\"Generated with [Claude Code]\"))] | sort_by(.submitted_at) | last'"})
```

Substitute `NUMBER` from the user's argument. **`OWNER`/`REPO` substitution is head-vs-base sensitive:**

- The merged `gh pr view` call's `-q` extracts the **head** repo from `headRepositoryOwner.login` + `headRepository.name` (used later for SHA-based source reads at the head ref). It also returns the post-target `url`, head branch name, and PR title in one round-trip.
- The `gh api .../pulls/NUMBER/reviews` call uses the **base** repo (where reviews are posted). For non-fork PRs, base == head, so use the same `OWNER/REPO`. For fork PRs, derive base owner/repo from the PR's `url` field (e.g. `https://github.com/<base-owner>/<base-repo>/pull/<n>` → `<base-owner>/<base-repo>`).
- **Do not** call `gh pr view NUMBER --json baseRepository` to look the base up — `baseRepository` is not a valid `--json` field on this `gh` version and the call exits 1.

Notes per call:

- The merged `gh pr view NUMBER --json …` call is the only `gh pr view --json` call you need. It returns the full HEAD SHA, head OWNER, head REPO, post-target URL, head branch name, and PR title in one HTTP round-trip. **Do not split this back into two calls** — the previous shape (separate `headRefOid,headRepository` and `url,number,title,headRefName` calls) was a relic of an older `-q` path bug that's now fixed.
- `gh pr diff NUMBER > $REVIEW_TMPDIR/pr-NUMBER.diff` — save the diff once. Specialists Read this from disk; don't refetch. **The lead must not Read the diff itself** — the prep agent in step 1c reads it for the summary, and specialists each Read it once via the spawn-context's `DIFF` pointer. Holding the diff in the lead's working set adds 50–200 KB on a large PR for no purpose.
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

**Build the spawn-context bundle via the helper.** Earlier revisions of this skill had the lead Read the rubric and Write the bundle inline — observed cost was ~4 minutes of pure model-output streaming on every run, since the bundle is mostly verbatim concatenation of on-disk JSON + the static rubric (transcript `b5a8dd9d`, May 2026). The work is mechanical, so it lives in `code-review-helper bundle-context`.

Resolve the repo working-tree root once (specialists need it for HEAD-pinned `git show` / `git grep`; the lead's cwd may be a worktree that isn't checked out to HEAD):

```
Bash({command: "git -C <head-checkout-or-cwd> rev-parse --show-toplevel"})
```

Capture stdout as `<REPO_ROOT>` for the helper invocation below.

Pipe the prep agent's summary paragraph directly into the helper via `--summary-paragraph -` (stdin) — **do not** `Write` the paragraph to disk first. A common third-party `PreToolUse:Write` hook (e.g. the `security-guidance` plugin's `security_reminder_hook.py`) substring-matches sensitive-API tokens in any `Write` payload; a PR summary that legitimately _describes_ a workflow using those APIs will trip it, fail the `Write`, and cascade into a `bundle-context: read summary paragraph: open …: no such file or directory` (transcript `65606fdb`, May 2026). Bash heredoc rides `Bash`-tool permissions, not `Write`-tool, and bypasses the matcher cleanly:

```
Bash({command: "code-review-helper bundle-context \\
  --review-tmpdir $REVIEW_TMPDIR \\
  --head-sha <full HEAD SHA> \\
  --pr-number <NUMBER> \\
  --owner <OWNER> --repo <REPO> \\
  --repo-root <REPO_ROOT> \\
  --summary-paragraph - \\
  --rubric ${CLAUDE_PLUGIN_ROOT}/references/code-review-rubrics.md \\
  --rubric-out $REVIEW_TMPDIR/rubric.md \\
  --git-workdir <REPO_ROOT> \\
  --out $REVIEW_TMPDIR/spawn-context.md <<'PARA_EOF'
<paragraph from step 1c verbatim>
PARA_EOF
"})
```

`--rubric-out` writes the rubric verbatim to `$REVIEW_TMPDIR/rubric.md` and replaces the inline `## Rubric` section in the bundle with a `RUBRIC_PATH:` pointer. This keeps `spawn-context.md` under the 25k-token Read cap on every realistic PR (the previous all-in-one bundle hit 30k+ tokens on PR #1337, forcing every specialist to paginate). Specialists Read the rubric path once after the bundle. `--repo-root` is emitted as `REPO_ROOT:` in the per-PR header so specialists never synthesize paths from their cwd. The default `--max-source-bytes` is 32 KB — large enough that medium changed files (10–30 KB) embed in the bundle and specialists don't each `git show` them separately (transcript `c9fa54fb`, May 2026: 25× duplicate `git show` of `docusign-vendor.service.ts` because the previous 12 KB default rendered it as `_omitted`). Keeping the rubric in a separate file (above) recovered the bundle headroom that earlier motivated the 12 KB cap. Do not override unless you've measured the bundle hitting the 25k-token Read cap on a real PR.

The helper enumerates `$REVIEW_TMPDIR/changed-files.json`, `roster.json`, `prior-issues.json`, `claude-md-files.json`, and (when present) `migration-history.json`, concatenates them verbatim under named sections, and copies the rubric to `--rubric-out`. With `--max-source-bytes > 0` it also embeds the contents of every changed file at HEAD that fits within the cap (via `git show HEAD_SHA:<path>`); larger files render as a placeholder pointing back at `git show`. This means specialists working on the same small file don't each pay a separate `git show` round-trip.

**Don't Read the rubric or Write the bundle yourself.** Both are owned by the helper. The bundle + the rubric file are the specialists' two Reads at startup; the on-disk JSON artifacts remain as durable run artifacts; specialists should not Read them separately because the bundle already contains them.

If `code-review-helper bundle-context` is missing (the prebuilt platform binary is unavailable or pre-dates this subcommand), abort with: "code-review-helper bundle-context not available — reinstall the plugin or rebuild the helper via `cd ${CLAUDE_PLUGIN_ROOT}/tools/code-review-helper && make release`." Don't fall back to inline assembly — the latency cost is the whole point of moving it out.

### 2c. Create the team and assignment tasks

1. `TeamCreate({team_name: "code-review-<PR_NUMBER>", description: "Multi-specialist review for PR <NUMBER>"})`.
2. Render the per-role TaskCreate batch via the helper, then echo it:

   ```
   Bash({command: "code-review-helper spawn-batch --kind tasks --roster $REVIEW_TMPDIR/roster.json --out $REVIEW_TMPDIR/tasks-batch.md"})
   Read({file_path: "$REVIEW_TMPDIR/tasks-batch.md"})
   ```

   On the next assistant message, echo the contents of `tasks-batch.md` from the `<!-- echo block below verbatim ... -->` marker through EOF as the body of the message — no prose between or within the lines, no whitespace edits, no summarization. Each `TaskCreate({...})` line becomes a real tool_use call. The helper guarantees the schema is correct (`subject`, `description`, `activeForm` only — never `team_name`); do not edit the line shapes.

   Capture each returned task ID; you'll persist them in step 2c.3 and the helper bakes them into the spawn prompts in step 2d.

3. **Persist the role → task-ID mapping to disk.** Use the Write tool to create `$REVIEW_TMPDIR/assignments.json` as a JSON object mapping each role to the assignment-task ID returned in step 2. Schema: `{"security": "7", "quality": "8", ...}`. The step-2d helper invocation reads this file to bake `ASSIGNMENT_TASK_ID` into each spawn prompt; the teardown ladder in step 2g also reads it to escalate via `TaskStop` deterministically.

### 2d. Spawn all applicable specialists in one message

Render the per-role Agent batch + safety Monitor via the helper, then echo it:

```
Bash({command: "code-review-helper spawn-batch --kind agents --roster $REVIEW_TMPDIR/roster.json --assignments-file $REVIEW_TMPDIR/assignments.json --review-tmpdir $REVIEW_TMPDIR --owner <OWNER> --repo <REPO> --pr-number <NUMBER> --out $REVIEW_TMPDIR/agents-batch.md"})
Read({file_path: "$REVIEW_TMPDIR/agents-batch.md"})
```

On the next assistant message, echo the contents of `agents-batch.md` from the `<!-- echo block below verbatim ... -->` marker through EOF as the body of the message — every `Agent({...})` line becomes a real tool_use call, and the trailing `Monitor({...})` line is the step-2e safety wake. No prose between or within the lines; do not summarize, narrate between blocks, or change whitespace. The helper bakes `ASSIGNMENT_TASK_ID` per role from `assignments.json` and emits `mode: "auto"` (required for long unattended scans).

The per-role spawn prompt body lives in `${CLAUDE_PLUGIN_ROOT}/tools/code-review-helper/internal/spawnbatch/templates/spawn-prompt.tmpl` — edit that file to change what specialists read at startup. The bundle (`spawn-context.md` from 2b) and rubric (`rubric.md`) are the specialists' two startup Reads; the prompt template itself stays short on purpose so the lead's echo of the batch file remains under the 25k-token Read cap.

When `Agent` is called with `team_name`, it returns immediately (the response includes "Spawned successfully" / "running via mailbox") rather than blocking until the agent's first turn completes. Specialists run asynchronously; you'll be woken via a `scan_complete` DM as each one's findings file lands (see 2e).

### 2e. Wait for scan_complete DMs, then broadcast finalize

Specialists DM `team-lead` with `scan_complete: <role>` once they've written `findings/<role>.json` (rubric workflow step 6). The findings file is the source of truth; the DM is the wake signal. There is no polling cadence — the lead ends its turn after spawning and each DM resumes it for one short turn.

The safety wake `Monitor({command: "sleep 240; echo scan_complete_timer_fired", timeout_ms: 245000, persistent: false, description: "code-review scan-complete safety timer"})` is the trailing line of the helper-emitted `agents-batch.md` (step 2d) — co-emitted with every `Agent` call in one assistant message. Do not emit it on its own turn after the spawns; the helper guarantees it lands in the same message. It is the single backstop bounding the team-level scan window in the rare case where a specialist crashes before sending any DM. The 240 s ceiling sits inside the 300 s prompt-cache TTL so the wake-turn is still cache-warm. On the happy path, every roster role DMs well before the monitor emits and you broadcast finalize without ever consulting it. (Per `${CLAUDE_PLUGIN_ROOT}/references/shell-safety.md` rule #8, `Monitor` is the wake-on-event primitive — never `Bash sleep N` to pace the lead's turn.)

After the spawn-and-timer message, **end your turn**. The next time the harness invokes you, it will be because either (a) a teammate sent a DM or (b) the safety monitor emitted its `scan_complete_timer_fired` line.

**Wake-turn protocol — DM count is the source of truth.** Track the set of `scan_complete: <role>` DMs you have received. Do **not** enumerate the findings directory on a wake-turn — no `Glob`, no `Bash ls`, no `Bash find`. The DMs are sent immediately after each specialist's `Write` lands (rubric step 6), and step 3's `code-review-helper finalize` tolerates any stragglers. On every wake-turn:

1. If the count of `scan_complete` DMs received equals the roster size, broadcast `finalize_now` to every roster member via the helper, then proceed to 2f:

   ```
   Bash({command: "code-review-helper spawn-batch --kind finalize --roster $REVIEW_TMPDIR/roster.json --out $REVIEW_TMPDIR/finalize-batch.md"})
   Read({file_path: "$REVIEW_TMPDIR/finalize-batch.md"})
   ```

   On the next assistant message, echo the contents of `finalize-batch.md` from the marker through EOF — every `SendMessage({...})` line becomes a real tool_use. No prose between or within the lines.

2. Otherwise, look at what woke you. If it was a teammate DM (or any wake before the safety monitor emitted), end the turn — another DM (or the monitor) will wake you again.

3. **Once the safety monitor has fired and any role is still missing**, send one wake-up DM to each missing role in a single message: `SendMessage({to: "<role>-reviewer", message: "lead-wakeup: the team safety monitor has fired. Write whatever findings you have with scan_status: 'timed_out' and stay idle for finalize_now."})`. Arm one more `Monitor({command: "sleep 60; echo scan_complete_grace_fired", timeout_ms: 65000, persistent: false, description: "code-review scan-complete grace window"})` as a grace window and end the turn. Single shot — don't keep issuing wake-ups.

4. **On the grace-window wake**, if a role is still missing, treat it as unreachable: track the role name in an `unreachable_roles` list (in your turn text), broadcast `finalize_now` to every roster member, and proceed to 2f. Do **not** write a stub findings file — step 2f's missing-file branch handles consolidation correctly, and a stub races with a slow-but-live agent that may still write its real findings during teardown.

(Real failures observed across three transcripts of the previous Glob-on-wake protocol: `b5a8dd9d` (May 2026), `65606fdb` (May 2026), and `c9fa54fb` (May 2026). Even with the contract calling out `Bash ls` as a violation, the lead reached for `Bash ls /findings/` on every wake-turn because most user allowlists permit it broadly. Removing the directory-enumeration step entirely eliminates the failure surface — the file confirmation was redundant with the DM signal anyway.)

Notification-flow + safety-timer rationale: see `${CLAUDE_PLUGIN_ROOT}/references/code-review-design-notes.md`.

### 2f. Collect findings

The findings files in `$REVIEW_TMPDIR/findings/` are the input to step 3's helper invocation. There is nothing to read or merge here — the helper enumerates the directory itself, tolerates `scan_status: "timed_out"`, and reports missing roster roles via `consolidated.json`'s `missing_roles` field. The only thing this step needs is to confirm (via a Glob check) that _some_ `<role>.json` files actually landed; if the directory is empty, abort the review and surface that to the user rather than feeding an empty workspace to the helper.

**Do not pre-validate or hand-repair specialist findings files.** If you suspect a `findings/<role>.json` is malformed (e.g., you ran a sanity `jq .` on it and got a parse error), **do not** repair it with `sed`/`Edit`/heredoc rewrites. The helper's `internal/findings/load.go` already routes parse-failures to `consolidated.json`'s `unreadable_roles` field at step 3 — that is the supported recovery path. Hand-repairing the file (i) silently swallows the schema violation that produced the bad escape, (ii) loses the signal that a specialist's prompt needs a fix, and (iii) risks introducing a _worse_ malformation if the `sed` pattern is wrong. Real failure observed in transcript `74931090` where the lead `sed`-repaired a double-escaped backtick in `errors.json` instead of letting `unreadable_roles` surface it. If the user wants the malformed specialist's output recovered, they can re-run `/code-review` on that PR or DM the specialist directly — both are cheaper than a hand-repair that masks a recurring schema bug.

### 2g. Tear down the team

`TeamDelete` refuses while teammates are alive, so shut them down first. Best-effort with a hard wall-clock cap — findings are already on disk; one uncooperative specialist must not block step 3. Three attempts with widening Monitor windows (15 s → 30 s → 30 s, ~75 s worst case). Happy-path latency is unchanged: `TeamDelete` succeeds on attempt 1.

Per shell-safety rule #8, every wait window below uses `Monitor` — never `Bash sleep N` — so the harness owns the wake and the user isn't prompted on each iteration.

1. Render the per-role shutdown_request batch via the helper, then echo it:

   ```
   Bash({command: "code-review-helper spawn-batch --kind shutdown --roster $REVIEW_TMPDIR/roster.json --out $REVIEW_TMPDIR/shutdown-batch.md"})
   Read({file_path: "$REVIEW_TMPDIR/shutdown-batch.md"})
   ```

   On the next assistant message, echo the contents of `shutdown-batch.md` from the marker through EOF as the body of the message — every `SendMessage({...})` line becomes a real tool_use. No prose between or within the lines.

2. Arm `Monitor({command: "sleep 15; echo teardown_wait_done", timeout_ms: 20000, persistent: false, description: "code-review teardown wait 1"})` and end the turn; the emit-line wakes you.
3. Call `TeamDelete()`. On success, proceed to step 3 of the skill.
4. **On "still active member(s)" error (attempt 1 failed)**:
   a. The slow-but-live recovery path that used to require a manual re-Read + re-merge is now implicit: any findings file a holdout writes between attempt 1 and attempt 3 will be picked up automatically when step 3's helper runs (the helper enumerates `$REVIEW_TMPDIR/findings/` once, fresh). No explicit merge step is needed here.
   b. Send one more `shutdown_request` to each named holdout, arm `Monitor({command: "sleep 30; echo teardown_wait_done", timeout_ms: 35000, persistent: false, description: "code-review teardown wait 2"})`, end the turn, then retry `TeamDelete()` on the wake.
5. **On second failure (attempt 2 failed) — escalate via `TaskStop`.** Cooperative shutdown has failed; the holdout's slot is still alive. The escalation is a deterministic two-substep sequence — keep the substeps in separate turns. The reason for the split: specialists self-complete their assignment task on `finalize_now` (rubric step 8), so any task ID captured in step 2c may already have been deleted by the time teardown runs. Re-fetch authoritative state before stopping anything.

   **Substep 5a — re-fetch task state.** In a single message: (i) Read `$REVIEW_TMPDIR/assignments.json` to recover the role → task-ID mapping written in step 2c.3, **and** (ii) call `TaskList()` (no arguments) to get the _current_ live assignment-task IDs. End the turn after the `TaskList` call. The wake delivers `TaskList`'s response.

   **Substep 5b — apply `TaskStop` per holdout.** For each named holdout, look up its task ID in the `assignments.json` map you read in 5a. Then check whether that ID still appears in the `TaskList` response:
   - **If the holdout's task ID is in `TaskList`'s output**: pass that exact numeric string (e.g. `"7"`) to `TaskStop({task_id: "<numeric-id>"})`. **Do not pass the agent id** — spawn results, mailbox notifications, and `shutdown_request` acks all surface a string of the form `<role>-reviewer@<team-name>` (e.g. `quality-reviewer@code-review-1337`); that is the agent id, _not_ a task id.
   - **If the holdout's task ID is _not_ in `TaskList`'s output**: the specialist already self-completed (rubric step 8). The agent slot is hung but the task is gone — `TaskStop` will reject with `No task found`. **Skip `TaskStop` for this holdout** and rely on the third `TeamDelete` to close out the slot (the holdout's mailbox typically drains on its own within ~30 s).

   Worked example. Suppose `assignments.json` is `{"security": "7", "claude-md": "12"}` and `TaskList` returns `{"tasks": [{"id": "7", "subject": "Review for security", ...}]}` (no entry for `"12"`). For a `claude-md-reviewer` holdout, skip `TaskStop` (its task is gone). For a `security-reviewer` holdout, call `TaskStop({task_id: "7"})`.

   Then arm `Monitor({command: "sleep 30; echo teardown_wait_done", timeout_ms: 35000, persistent: false, description: "code-review teardown wait 3"})`, end the turn, then retry `TeamDelete()` on the wake (attempt 3).

6. **On third failure**, stop trying. Log one warning naming the leftover team + holdout(s) and continue to step 3 of the skill. Don't loop.

Degraded-state explanation: see `${CLAUDE_PLUGIN_ROOT}/references/code-review-design-notes.md`.

## 3. Filter — deduplication, gates, payload assembly

The deterministic pipeline (positional dedup → semantic dedup → prior-review dedup → confidence/severity gate → inline-eligibility classification → payload + fallback assembly) lives in `code-review-helper`. The contract — every rule used to live in prose here — is documented in the helper source under `${CLAUDE_PLUGIN_ROOT}/tools/code-review-helper/internal/` and exhaustively tested. Don't reimplement any of those rules in this skill.

**Trust the helper. Do not investigate why findings dropped.** `consolidated.json`'s per-specialist counts will routinely exceed `len(inline_eligible)` because the helper deduplicates (positional + semantic + prior-review) and gates by confidence/severity — that is the design, not a bug. Once `code-review-helper finalize` exits 0 and `consolidated.json` is on disk, proceed directly to step 4. Specifically: do not Read files under `${CLAUDE_PLUGIN_ROOT}/tools/code-review-helper/internal/`, do not write a one-off Go program to reproduce the pipeline, do not re-run `finalize` "just to see," and do not hand-correlate per-specialist findings against `inline_eligible`. If the helper's behavior looks wrong, the recourse is to file an issue against `code-review-helper` after the run completes; do not block step 4 to investigate. (Same shape as the step-2f guard against hand-repairing specialist findings — the determinism is the feature.)

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
