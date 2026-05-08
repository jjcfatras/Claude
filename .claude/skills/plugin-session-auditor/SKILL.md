---
name: plugin-session-auditor
description: Audit a Claude Code session transcript (.jsonl) for issues with the plugins in this repository — repetitive permission prompts, errors, failed tool calls, and orchestration inefficiencies. Spawns one specialist subagent per issue category, consolidates findings into a proposals doc with pros/cons per option, and asks before implementing fixes. Use whenever the user provides a `.jsonl` session log path and asks to audit, review, analyze, find issues in, or improve a plugin based on a session — even if they don't say "audit" explicitly. Also triggers on phrases like "look at this transcript", "what went wrong in this session", "the plugins were misbehaving in this run".
argument-hint: <jsonl-path-or-dir-or-glob>
---

# Plugin Session Auditor

Audits Claude Code jsonl session logs for plugin issues, then proposes fixes.

## When this triggers

- User passes a path like `~/.claude/projects/.../<uuid>.jsonl` and asks for a review, audit, or analysis.
- User says one of the plugins (`cherry-pick`, `merge`, `code-review`, `respond-to-review`, `test-driven-fix`, `doc-audit`) misbehaved in a recent session.
- User wants to find inefficiencies, repeated permission prompts, or orchestration mistakes from a real run.

The user may pass a single jsonl, a directory of them, or a glob — the parser accepts all three.

## What it does

1. **Parse** the jsonl(s) into structured event JSON via the bundled Go tool.
2. **Scope-detect** which plugins in `plugins/` were exercised by the session.
3. **Spawn four specialists in parallel**, one per issue category:
   - `permissions` — repetitive permission prompts, missing allowlist entries
   - `errors` — hook errors, api errors, unhandled tool result errors
   - `tool-failures` — non-permission tool errors, retry-loops, malformed tool inputs
   - `orchestration` — agent spawn waste, repeated work between subagents, sequencing errors, prompt bloat, context inefficiency
4. **Consolidate** findings into `proposals.md` — for each issue, document the problem, evidence (with timestamps + tool_use_ids), 2+ fix options with pros/cons, and a recommendation.
5. **Ask** the user which proposals to implement before touching plugin source.
6. **Implement** approved fixes against the relevant `plugins/<name>/` files. Bump `plugin.json` `version` per the repo's SemVer rules.

## Workflow

### Step 1 — Set up the run workspace

Create a timestamped dir at the repo root:

```bash
RUN_DIR="${CLAUDE_PROJECT_DIR:-$(pwd)}/plugin-session-auditor-workspace/$(date +%Y%m%dT%H%M%S)"
mkdir -p "$RUN_DIR/findings"
```

The workspace dir is gitignored scratch — safe to leave around or delete after the run.

### Step 2 — Parse the transcript

Run the parser as a single chained command. Some Claude Code harnesses reset
cwd between Bash invocations, so a separate `cd` followed by `go run` will
silently execute in the wrong directory and produce no output:

```bash
(cd "${CLAUDE_PROJECT_DIR}/.claude/skills/plugin-session-auditor/tools/session-parser" && go run . --out "$RUN_DIR/parsed.json" "<jsonl-path-or-dir>")
```

`parsed.json` contains: `session_id`, `cwd`, `git_branch`, `plugins_used`, `plugin_scope_known`, full `events` (tool_calls, tool_failures, permission_denials, agent_spawns, slash_commands, hook_events, api_errors), and aggregate `stats`.

If `plugins_used` is empty, the session never invoked one of this repo's plugins. Tell the user — they may have given the wrong file. Don't audit further unless they confirm.

See `references/jsonl-schema.md` for the schema and field semantics.

### Step 3 — Read the parsed data + relevant plugin source

Before spawning specialists, you (the lead) skim `parsed.json` to:

- Note `plugins_used` — the specialists should focus their fix proposals on these plugins.
- Note unusual signals — high `tool_failure_rate`, long `permission_denial_runs`, many repeated `slash_commands`, big `turn_duration_ms_p95`.
- Read the manifests + commands of the plugins-in-scope so you can brief the specialists with concrete file paths to investigate (e.g., "this session ran `/code-review` — point the orchestration specialist at `plugins/code-review/commands/code-review.md` and the agents under `plugins/code-review/agents/`").

### Step 4 — Run the four specialists

Preferred: single message with four parallel `Agent` tool calls (one per
category). If the `Agent` tool is unavailable in the current harness — for
example, when this skill is being evaluated from inside a subagent that does
not expose Agent — fall back to running each specialist inline in sequence:
read its instruction file, do the analysis acting as that specialist, and
write the findings file. Either path is acceptable; the four findings files
must always exist.

Each specialist gets:

- The path to `parsed.json`
- The plugin scope it should focus on (the `plugins_used` list)
- Pointers to the relevant plugin source files
- Its instruction file from `agents/`
- A target findings file path: `$RUN_DIR/findings/<category>.md`

The specialists' instructions are in:

- `agents/permissions-analyzer.md`
- `agents/errors-analyzer.md`
- `agents/tool-failures-analyzer.md`
- `agents/orchestration-analyzer.md`

Each specialist writes its findings file directly. They do not implement fixes — that is the lead's job after user approval.

Brief each specialist with:

```
Read the instruction file at <skill>/agents/<category>-analyzer.md and follow it.

Inputs:
- Parsed session JSON: $RUN_DIR/parsed.json
- Plugins in scope: <list from parsed.json plugins_used>
- Plugin source roots: plugins/<name>/ for each plugin in scope
- Repo CLAUDE.md: read for plugin-versioning rules and project structure

Output: write your findings to $RUN_DIR/findings/<category>.md.
```

When spawning each specialist via the `Agent` tool, read the YAML frontmatter at the top of its instruction file once and pass the `model:` value through to the `Agent` tool call's `model` parameter (current settings: `permissions-analyzer`, `errors-analyzer`, `tool-failures-analyzer` → `sonnet`; `orchestration-analyzer` → `opus`). If a file lacks a `model:` field, omit the parameter so the spawn inherits from the lead. The inline-fallback path (when `Agent` is unavailable) ignores frontmatter — the lead runs the specialist's instructions in its own model.

### Step 5 — Consolidate into proposals

Once all four specialists return, read the four findings files and write `$RUN_DIR/proposals.md` with this structure:

```markdown
# Audit proposals — <session_id>

Source: <jsonl path>
Plugins in scope: <list>

## Proposal 1: <short title>

**Category:** permissions | errors | tool-failures | orchestration
**Problem:** <1–3 sentences>
**Evidence:** <bullets — timestamps, tool_use_ids, file paths>

**Options:**

- **A. <option name>** — <description>
  - Pros: <bullets>
  - Cons: <bullets>
- **B. <option name>** — <description>
  - Pros: <bullets>
  - Cons: <bullets>

**Recommendation:** <A | B | C | hybrid>, because <reason>.

---
```

#### Promote vs. demote (the consolidation rubric)

A **finding becomes a full proposal** if it meets at least one of these tests:

1. **Recurrence** — the same problem is visible in 3+ events, or appears in multiple specialists' findings, or recurs across multiple plugin commands.
2. **Magnitude** — the cost is concretely large: ≥10s of wall-clock, ≥10% of session tokens, ≥3 retries, ≥2 specialists doing duplicate work.
3. **Live in HEAD** — the source pattern still exists in the plugin file at `git HEAD`; check before demoting "already-fixed" — the prior commits may have addressed only part.
4. **Cross-cutting** — affects multiple commands or agents in the same plugin, so a single change has leverage.
5. **High user-visible cost** — the user explicitly mentioned it ("felt slow", "kept asking permission"), even if individual evidence is thin.

A **finding goes to "minor observations"** if all of these are true:

- It is a one-off occurrence with no plausible repeat.
- The fix would be smaller than the proposal-writing overhead (e.g., a typo).
- It is verifiably already addressed at HEAD with no remnants in the source.
- It is out of scope for this repo (third-party hook, harness, MCP server).

Default to promoting when in doubt. Padding proposals with marginal findings is bad, but under-promoting is worse — the user's whole reason for running the auditor is to surface non-obvious orchestration cost. If you have <2 substantive proposals on a session the user said felt slow, re-read the orchestration findings and ask whether you're being too conservative.

Every option has both Pros AND Cons. If you find yourself writing only pros, you haven't thought hard enough — every change has a tradeoff (added complexity, version bump cost, behavior change for existing users).

#### Calibration check before writing the doc

Before finalizing proposals.md, sanity-check yourself:

- Did I promote at least one orchestration finding from each specialist that produced concrete evidence? If a specialist returned 3+ findings and none became proposals, ask why.
- Did I demote anything to "minor observations" only because the source already mentioned it once? Source acknowledgement is not the same as a fix landing — verify the actual line in the plugin file.
- Did I recommend "leave as-is" on every proposal? If yes, the doc is not decision-ready — either the audit found nothing real (in which case write a one-line "no actionable findings" doc instead) or the threshold is wrong.

### Step 6 — Present and ask

Show the user the proposals doc location and a one-line summary per proposal. Ask which to implement. Accept "all", a list of numbers, or "skip N".

### Step 7 — Implement approved fixes

For each approved proposal:

1. Apply the change to the relevant `plugins/<name>/` file(s).
2. Bump the plugin's `version` in `plugins/<name>/.claude-plugin/plugin.json` per the repo's SemVer rules (see root `CLAUDE.md`).
3. Note the change for the commit message.

Do **not** auto-commit. Stop after the edits are in the worktree and tell the user what changed.

## Important behaviors

- **Don't audit non-plugin sessions silently.** If `plugins_used` is empty, surface it before going further.
- **Keep proposals decision-ready.** Each option has real pros AND real cons. If a choice is obvious, say so and recommend it — don't manufacture fake alternatives.
- **Cite evidence.** Every proposal must reference specific tool_use_ids, timestamps, or counts from `parsed.json`. Vague "the session seemed slow" is not a finding.
- **Stay in the proposed plugin scope.** Don't refactor adjacent plugins just because you see room for improvement — that's scope creep and the user didn't ask for it.
- **Bump versions on apply, not on propose.** The proposal lists which plugin would bump and at what tier; the actual `plugin.json` edit happens only after the user approves.

## Output paths reference

| Artifact            | Path                                                                 |
| ------------------- | -------------------------------------------------------------------- |
| Run workspace       | `plugin-session-auditor-workspace/<timestamp>/`                      |
| Parsed events       | `<run>/parsed.json`                                                  |
| Specialist findings | `<run>/findings/{permissions,errors,tool-failures,orchestration}.md` |
| Proposals           | `<run>/proposals.md`                                                 |

## Files in this skill

- `tools/session-parser/` — Go parser (build with `go run .`, no prebuilt binary)
- `agents/<category>-analyzer.md` — instructions for each specialist
- `references/jsonl-schema.md` — what the parser emits and what each field means
