# Component frontmatter reference

Every YAML frontmatter field available on the three authorable Claude Code component types:
**skills** (`SKILL.md`), **slash commands** (`plugins/<name>/commands/*.md`), and **agents**
(`plugins/<name>/agents/*.md`). Skills and slash commands were unified upstream — a command file
accepts the same frontmatter as a `SKILL.md` — so they share one reference here (§1); agents have
their own set (§2).

Sources: §1 (skills & slash commands) is verified against the official skills docs
(`code.claude.com/docs/en/skills#configure-skills`, checked 2026-07-08) and the slash-commands docs
(`code.claude.com/docs/en/slash-commands`, checked 2026-07-10) — which confirm the unification above;
§2 (agents) against the official subagents docs
(`code.claude.com/docs/en/sub-agents#supported-frontmatter-fields`, checked 2026-07-09). This file
defines the repo-local tier semantics (root `CLAUDE.md` links here); "in practice" notes from this
repo's component files.

Plugin manifests (`plugins/<name>/.claude-plugin/plugin.json`) are separate config, not component
frontmatter — out of scope here. Use `${CLAUDE_PLUGIN_ROOT}` inside any component to resolve
plugin-relative paths at runtime.

---

## 1. Skills & slash commands

Files in `.claude/skills/<name>/SKILL.md`, `.claude/commands/*.md`, and plugin `commands/*.md` all
create a `/command`; `SKILL.md` is the recommended authoring format and plain command files remain
operational. All fields are optional; only `description` is recommended, so Claude knows when to
load the skill/command.

Every field below is **accepted** on both, but a few carry a `(skills only)` tag — _the field drives
automatic or background-knowledge behavior that an explicitly-invoked `/command` never uses._

| Field                            | Purpose                                                                                                                                                                                                                                                                                         | Values / example                                      | Required?   |
| -------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------- | ----------- |
| `name`                           | Display name in skill listings; defaults to the directory name. For a slash command the name derives from the filename, so this is inert on `commands/*.md`; only a plugin-root `SKILL.md` uses it as the command name.                                                                         | `plugin-session-auditor`                              | No          |
| `description`                    | The triggering field — decides when Claude applies the skill/command. Falls back to the first body paragraph if omitted. Combined with `when_to_use`, truncated at **1,536 chars** in the listing, so put the key use case first. Write third-person and pack it with concrete trigger phrases. | see below                                             | Recommended |
| `when_to_use` `(skills only)`    | Extra trigger context (phrases / example requests). Appended to `description`; counts toward the 1,536-char cap. Feeds auto-trigger ranking, so it does nothing for an explicitly-invoked `/command`.                                                                                           | trigger phrases                                       | No          |
| `argument-hint`                  | Autocomplete hint for expected args.                                                                                                                                                                                                                                                            | `[issue-number]`, `[filename] [format]`               | No          |
| `arguments`                      | Named positional args for `$name` substitution. Space-separated string or YAML list; names map to positions in order.                                                                                                                                                                           | `[issue, branch]`                                     | No          |
| `disable-model-invocation`       | `true` = user-only (`/name`); Claude won't auto-load it, it's dropped from Claude's context, and it's not preloaded into subagents.                                                                                                                                                             | `true` \| `false` (default `false`)                   | No          |
| `user-invocable` `(skills only)` | `false` = hide from the `/` menu (Claude-only background knowledge). Antithetical to a slash command, whose purpose is `/`-invocation.                                                                                                                                                          | `true` (default) \| `false`                           | No          |
| `allowed-tools`                  | Tools pre-approved (no prompt) while the skill/command is active. Does **not** restrict the pool. Space/comma string or YAML list.                                                                                                                                                              | `Bash(git add *) Bash(git commit *)`                  | No          |
| `disallowed-tools`               | Tools removed from the pool while active; clears on your next message.                                                                                                                                                                                                                          | `AskUserQuestion`                                     | No          |
| `model`                          | Model while the skill/command is active (rest of the turn; not saved to settings). Same values as `/model`, or `inherit`.                                                                                                                                                                       | `haiku` \| `sonnet` \| `opus` \| `fable` \| `inherit` | No          |
| `effort`                         | Effort level while active; overrides the session effort.                                                                                                                                                                                                                                        | `low` \| `medium` \| `high` \| `xhigh` \| `max`       | No          |
| `context`                        | `fork` runs the skill/command in a forked subagent — the file body becomes the prompt, with no conversation history. Applies to both (per the changelog: "Skills and slash commands can now be executed within a forked sub-agent context").                                                    | `fork`                                                | No          |
| `agent`                          | Subagent type when `context: fork` is set. Built-in (`Explore` / `Plan` / `general-purpose`) or a custom agent; defaults to `general-purpose`.                                                                                                                                                  | `Explore`                                             | No          |
| `hooks`                          | Hooks scoped to the skill/command's lifecycle.                                                                                                                                                                                                                                                  | see hooks docs                                        | No          |
| `paths` `(skills only)`          | Globs that gate auto-activation to matching files. Comma string or YAML list. A `/command` is invoked explicitly, never path-gated.                                                                                                                                                             | `src/**/*.ts`                                         | No          |
| `shell`                          | Shell for `` !`cmd` `` injection. `powershell` needs `CLAUDE_CODE_USE_POWERSHELL_TOOL=1`.                                                                                                                                                                                                       | `bash` (default) \| `powershell`                      | No          |

**Not a Claude Code skill field:** `version` comes from the [Agent Skills](https://agentskills.io)
open standard and appears in some upstream examples, but it is absent from the Claude Code skills
frontmatter reference and ignored — none of this repo's `SKILL.md` files use it.

```yaml
---
name: skill-name # optional — defaults to the directory name
description: This skill should be used when the user asks to "specific phrase 1", "specific phrase 2", "specific phrase 3". Include exact phrases users would say. Be concrete and specific.
---
```

### In practice

- `.claude/skills/plugin-session-auditor/SKILL.md` carries `argument-hint`
  (`<jsonl-path-or-dir-or-glob>`) — a documented skill field used as intended.
- `plugins/transcript/skills/transcript/SKILL.md` is the repo's first plugin-shipped skill
  (model-invocable), using the command-style subset — `description`, `allowed-tools`,
  `model: haiku`, `effort: low`.
- `plugins/docs/skills/audit-docs/SKILL.md` and `plugins/docs/skills/enrich-claude-md/SKILL.md`
  are plugin-shipped skills using the fuller command-style subset — `description`,
  `argument-hint`, `model: opus`, `effort: high`, `allowed-tools` — and are model-invocable (no
  `disable-model-invocation`).
- The seven `plugins/git/skills/*/SKILL.md` skills (`commit`, `commit-push`, `commit-push-pr`,
  `push`, `clean_gone`, `cherry-pick`, `merge`) use the command-style subset — `description`,
  `argument-hint` (cherry-pick/merge only), `allowed-tools` (all but clean_gone), `model`,
  `effort` — and are model-invocable. Six migrated from `commands/*.md`; `push` (push +
  PR-description refresh, no commit) was authored as a skill directly. `commit` and `push` also
  carry `when_to_use` (their trigger phrases extracted out of `description`) — the repo's first
  use of a `(skills only)` field.
- The two `plugins/simplify/skills/*/SKILL.md` skills (`simplify-code`, `simplify-prose`) migrated from
  `commands/*.md` keeping the command-style subset — `description`, `argument-hint`,
  `allowed-tools`, `model`, `effort` — and are model-invocable.
- The three `plugins/jira/skills/*/SKILL.md` skills (`create-ticket`, `implement-ticket`,
  `create-tests`) migrated from `commands/*.md` keeping the command-style subset — `description`,
  `argument-hint`, `allowed-tools`, `model`, `effort` — plus `disable-model-invocation: true`:
  the repo's first user-only skills, invocable via `/jira:*` but never auto-loaded by Claude.
- `plugins/code-review/skills/code-review/SKILL.md` migrated from `commands/code-review.md`
  keeping the command-style subset — `description`, `argument-hint`, `model: opus`,
  `effort: medium`, `allowed-tools` — plus an explicit `name: code-review` and
  `disable-model-invocation: true`: user-only like the jira skills, invocable as `/code-review`
  (unchanged, since the skill directory name matches the old command filename) but never
  auto-loaded by Claude.
- Every plain `commands/*.md` slash command in this repo uses only the common subset —
  `description`, `argument-hint`, `disable-model-invocation`, `model`, `effort`, `allowed-tools` —
  and none of the `(skills only)` fields; among skills, only the git `commit`/`push`
  `when_to_use` breaks that pattern.

### `model` tier semantics

These govern the `model` row above for both skills and slash commands.

- `model` — which Claude model executes the command. Four values (if unset, inherits the session model; omit unless a command has a specific need):
  - `haiku` — fastest and cheapest, smallest reasoning budget. For simple, mechanical, deterministic commands or skills (e.g. the `transcript` skill). Typically pairs with `effort: low`.
  - `sonnet` — balanced cost vs. capability; the practical default. For standard single-agent workflows and routine commands (e.g. `commit`, `jira:create-ticket`). Typically pairs with `effort: low`/`medium`.
  - `opus` — most capable and most expensive of the standard tiers. Reserve for genuinely complex multi-agent orchestration and the hardest reasoning / design judgment (e.g. `debate`, `jira:implement-ticket`, `simplify`). Typically pairs with `effort: high`/`xhigh`.
  - `fable` — Mythos-class tier above Opus (Claude Fable 5); the most capable model, for the hardest, longest-running tasks. Never a default. Cybersecurity/bio content trips its input classifiers and falls back to Opus automatically, and Fable's documented code-review gains explicitly exclude security analysis for the same reason — so it is the wrong pin for security-domain work despite being the top tier. Unused in this repo (the code-review `security` agent ran on it until 2026-07-28, now `opus`).

### `effort` tier semantics

These govern the `effort` row above for both skills and slash commands.

- `effort` — how thoroughly the model reasons through the command. Five levels (availability varies by model; an unsupported level falls back to the nearest supported one; if unset, inherits the session effort):
  - `low` — minimal thinking, fastest, biggest token savings. For mechanical / deterministic commands.
  - `medium` — moderate thinking; balances cost/latency vs. depth. For light reasoning without deep multi-step planning (e.g. `/code-review`, `/jira:create-ticket`).
  - `high` — deep reasoning; the practical default for substantive commands. For multi-step workflows, conflict resolution, design judgment.
  - `xhigh` — extended reasoning; Opus-only (4.7+), falls back to `high` elsewhere, so pair with `model: opus`. For the hardest analysis / refactor judgment.
  - `max` — maximum reasoning budget; highest cost/latency. Reserve for the most demanding tasks. (Unused in this repo.)

### Slash-command specifics

**Repo enforcement:** the `validate-command-frontmatter.sh` PostToolUse hook requires every
`plugins/*/commands/*.md`, `plugins/*/skills/*/SKILL.md`, and `.claude/skills/*/SKILL.md` to start
with `---` and include a `description:` field.

**`allowed-tools` formats** — single, comma-separated, YAML array, `Bash(...)` command filters, or
the (discouraged) all-tools wildcard:

```yaml
allowed-tools: Read
allowed-tools: Read, Write, Edit
allowed-tools:
  - Read
  - Write
  - Bash(git:*)
allowed-tools: Bash(git:*)   # only git; also Bash(npm:*), Bash(docker:*)
allowed-tools: "*"            # all tools — avoid
```

Example (from `plugins/git/skills/commit/SKILL.md`):

```yaml
---
name: commit
description: Create a git commit with an auto-generated message matching the repo's style. Commits only — does not push.
when_to_use: Use when the user asks to "commit", "commit this", "make a commit", or wants current changes committed with a well-formed message.
allowed-tools: Bash(git add:*), Bash(git status:*), Bash(git diff:*), Bash(git log:*), Bash(git branch:*), Bash(git switch:*), Bash(git commit:*), AskUserQuestion
model: sonnet
effort: low
---
```

---

## 2. Agents — `agents/*.md`

Only `name` and `description` are required.

| Field             | Purpose                                                                                                                                                                                                                           | Values / example                                                                                                                          | Required? |
| ----------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | --------- |
| `name`            | Unique identifier using lowercase letters and hyphens. Hooks receive this value as `agent_type`. The filename doesn't have to match.                                                                                              | `code-reviewer`                                                                                                                           | **Yes**   |
| `description`     | The dispatch-decision field — when Claude should delegate to this agent. State triggering conditions ("Use this agent when…"); conventionally include `<example>`/`<commentary>` blocks; "use proactively" encourages delegation. | see below                                                                                                                                 | **Yes**   |
| `tools`           | Allowlist of tools (least privilege). Inherits all tools if omitted. Comma-separated string; MCP tool names allowed. To preload skills, use `skills` rather than listing `Skill` here.                                            | `Read, Grep, Glob`                                                                                                                        | No        |
| `disallowedTools` | Denylist — removed from the inherited or specified list. Applied first; `tools` resolves against the remaining pool. Accepts `mcp__<server>` / `mcp__*` patterns.                                                                 | `Write, Edit`                                                                                                                             | No        |
| `model`           | Which model the agent uses. `CLAUDE_CODE_SUBAGENT_MODEL` env var overrides all of these; a per-invocation `model` parameter beats the frontmatter.                                                                                | `sonnet` \| `opus` \| `haiku` \| `fable` \| full model ID \| `inherit` (default)                                                          | No        |
| `permissionMode`  | Permission mode for the subagent. Parent `bypassPermissions`/`acceptEdits`/auto mode takes precedence. Ignored for plugin subagents (see note).                                                                                   | `default` \| `acceptEdits` \| `auto` \| `dontAsk` \| `bypassPermissions` \| `plan` (+ `manual` alias for `default`, ≥2.1.200)             | No        |
| `maxTurns`        | Maximum number of agentic turns before the subagent stops.                                                                                                                                                                        | `30`                                                                                                                                      | No        |
| `skills`          | Skills preloaded into the subagent's context at startup — full content injected, not just descriptions. Unlisted skills stay invocable via the Skill tool.                                                                        | YAML list of skill names                                                                                                                  | No        |
| `mcpServers`      | MCP servers for this subagent: string references to already-configured servers, or inline definitions scoped to the subagent. Ignored for plugin subagents (see note).                                                            | server-name refs or inline defs (`stdio`/`http`/`sse`/`ws`)                                                                               | No        |
| `hooks`           | Lifecycle hooks scoped to this subagent; frontmatter `Stop` converts to `SubagentStop` at runtime. Ignored for plugin subagents (see note).                                                                                       | see hooks docs                                                                                                                            | No        |
| `memory`          | Persistent memory directory that survives across conversations; auto-enables Read/Write/Edit and injects the first 200 lines / 25KB of its `MEMORY.md`.                                                                           | `user` (`~/.claude/agent-memory/<name>/`) \| `project` (`.claude/agent-memory/<name>/`) \| `local` (`.claude/agent-memory-local/<name>/`) | No        |
| `background`      | `true` = always run as a background task, even when Claude needs the result right away. Unset = Claude chooses (background by default since v2.1.198).                                                                            | `true` \| `false`                                                                                                                         | No        |
| `effort`          | Effort level while the subagent is active; overrides the session effort. Available levels depend on the model.                                                                                                                    | `low` \| `medium` \| `high` \| `xhigh` \| `max`                                                                                           | No        |
| `isolation`       | `worktree` runs the subagent in a temporary git worktree branched from the default branch (not the parent's `HEAD`); auto-cleaned if the subagent makes no changes.                                                               | `worktree`                                                                                                                                | No        |
| `color`           | Display color in the task list and transcript.                                                                                                                                                                                    | `red` \| `blue` \| `green` \| `yellow` \| `purple` \| `orange` \| `pink` \| `cyan`                                                        | No        |
| `initialPrompt`   | Auto-submitted as the first user turn when the agent runs as the main session agent (`--agent` flag or `agent` setting). Commands and skills are processed; prepended to any user-provided prompt.                                | text                                                                                                                                      | No        |

**Plugin subagents** (every agent in this repo — all ship under `plugins/*/agents/`): `hooks`,
`mcpServers`, and `permissionMode` are ignored for security reasons when the agent loads from a
plugin. If an agent needs them, copy its file into `.claude/agents/` or `~/.claude/agents/`.

**In practice (this repo):**

- Repo agents use only `name`/`description`/`tools`/`model`/`effort`/`color` — none of the other 10
  fields. `effort` is used by the eight code-review specialists only; the `debate` agents and the
  `plugin-session-auditor` analyzers still pin `model` alone.
- `color: magenta` (`plugins/debate/agents/rebuttal.md`) is the one value outside the documented
  set; `purple`, `orange`, and `pink` (also used here) are now officially documented.
- `model` is pinned explicitly, never left to the `inherit` default — `sonnet`/`opus` throughout.
  No agent uses `fable`; the code-review `security` specialist did until 2026-07-28 (see §1's
  `fable` bullet for why security work in particular should not).
- `effort` is pinned across all eight code-review specialists so review depth doesn't drift with
  the invoking session's effort — `medium` for the mechanical `claude-md` compliance check, `high`
  for six, `xhigh` for `security`. `security`'s `xhigh` pairs with `model: opus` exactly as §1
  recommends — `xhigh` is Opus-only and falls back to `high` elsewhere.
- `tools` as a comma-separated string mixing built-ins with MCP tool names matches the documented
  file format (the `["array"]` form belongs to the `--agents` CLI JSON, not agent files).

Official example (docs "Example subagents" `code-reviewer`, comma-string `tools` + `model: inherit`):

```yaml
---
name: code-reviewer
description: Expert code review specialist. Proactively reviews code for quality, security, and maintainability. Use immediately after writing or modifying code.
tools: Read, Grep, Glob, Bash
model: inherit
---
```

Repo example (`plugins/code-review/agents/security.md`, MCP tool names in `tools` + pinned model):

```yaml
---
name: security
description: Security specialist for /code-review. Reviews PR diffs for authentication, authorization, input validation, injection vectors (SQL, NoSQL, command, prompt), request forgery (SSRF/CSRF), path traversal, cryptography misuse, secret handling, and API contract integrity. Always-on specialist; spawned by the /code-review orchestrator.
tools: Read, Grep, Glob, Bash, Write, mcp__plugin_context7_context7__resolve-library-id, mcp__plugin_context7_context7__query-docs
model: opus
color: red
---
```
