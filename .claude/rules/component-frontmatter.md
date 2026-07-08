# Component frontmatter reference

Every YAML frontmatter field available on the three authorable Claude Code component types:
**skills** (`SKILL.md`), **slash commands** (`plugins/<name>/commands/*.md`), and **agents**
(`plugins/<name>/agents/*.md`).

Sources: §1 (skills) is verified against the official skills docs
(`code.claude.com/docs/en/skills#configure-skills`, checked 2026-07-08); repo-local tier semantics
come from the root `CLAUDE.md`; "in practice" notes from the actual component files in this repo. §2
(commands) and §3 (agents) were **not** re-audited in that pass — commands now share the skill
frontmatter set (see §2), while agent fields live on a separate page (`/en/sub-agents`).

Plugin manifests (`plugins/<name>/.claude-plugin/plugin.json`) are separate config, not component
frontmatter — out of scope here. Use `${CLAUDE_PLUGIN_ROOT}` inside any component to resolve
plugin-relative paths at runtime.

---

## 1. Skills — `SKILL.md`

All fields are optional; only `description` is recommended, so Claude knows when to load the skill.

| Field                      | Purpose                                                                                                                                                                                                                                                                                 | Values / example                                | Required?   |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------- | ----------- |
| `name`                     | Display name in skill listings; defaults to the directory name. Only a plugin-root `SKILL.md` uses it as the command name.                                                                                                                                                              | `plugin-session-auditor`                        | No          |
| `description`              | The triggering field — decides when Claude applies the skill. Falls back to the first body paragraph if omitted. Combined with `when_to_use`, truncated at **1,536 chars** in the listing, so put the key use case first. Write third-person and pack it with concrete trigger phrases. | see below                                       | Recommended |
| `when_to_use`              | Extra trigger context (phrases / example requests). Appended to `description`; counts toward the 1,536-char cap.                                                                                                                                                                        | trigger phrases                                 | No          |
| `argument-hint`            | Autocomplete hint for expected args.                                                                                                                                                                                                                                                    | `[issue-number]`, `[filename] [format]`         | No          |
| `arguments`                | Named positional args for `$name` substitution. Space-separated string or YAML list; names map to positions in order.                                                                                                                                                                   | `[issue, branch]`                               | No          |
| `disable-model-invocation` | `true` = user-only (`/name`); Claude won't auto-load it, it's dropped from Claude's context, and it's not preloaded into subagents.                                                                                                                                                     | `true` \| `false` (default `false`)             | No          |
| `user-invocable`           | `false` = hide from the `/` menu (Claude-only background knowledge).                                                                                                                                                                                                                    | `true` (default) \| `false`                     | No          |
| `allowed-tools`            | Tools pre-approved (no prompt) while the skill is active. Does **not** restrict the pool. Space/comma string or YAML list.                                                                                                                                                              | `Bash(git add *) Bash(git commit *)`            | No          |
| `disallowed-tools`         | Tools removed from the pool while active; clears on your next message.                                                                                                                                                                                                                  | `AskUserQuestion`                               | No          |
| `model`                    | Model while the skill is active (rest of the turn; not saved to settings). Same values as `/model`, or `inherit`.                                                                                                                                                                       | `haiku` \| `sonnet` \| `opus` \| `inherit`      | No          |
| `effort`                   | Effort level while active; overrides the session effort.                                                                                                                                                                                                                                | `low` \| `medium` \| `high` \| `xhigh` \| `max` | No          |
| `context`                  | `fork` runs the skill in a forked subagent — SKILL.md becomes the prompt, with no conversation history.                                                                                                                                                                                 | `fork`                                          | No          |
| `agent`                    | Subagent type when `context: fork` is set. Built-in (`Explore` / `Plan` / `general-purpose`) or a custom agent; defaults to `general-purpose`.                                                                                                                                          | `Explore`                                       | No          |
| `hooks`                    | Hooks scoped to the skill's lifecycle.                                                                                                                                                                                                                                                  | see hooks docs                                  | No          |
| `paths`                    | Globs that gate auto-activation to matching files. Comma string or YAML list.                                                                                                                                                                                                           | `src/**/*.ts`                                   | No          |
| `shell`                    | Shell for `` !`cmd` `` injection. `powershell` needs `CLAUDE_CODE_USE_POWERSHELL_TOOL=1`.                                                                                                                                                                                               | `bash` (default) \| `powershell`                | No          |

**Not a Claude Code skill field:** `version` comes from the [Agent Skills](https://agentskills.io)
open standard and appears in some upstream examples, but it is absent from the Claude Code skills
frontmatter reference and is ignored — none of this repo's `SKILL.md` files use it.

**In practice:** `.claude/skills/plugin-session-auditor/SKILL.md` carries `argument-hint`
(`<jsonl-path-or-dir-or-glob>`) — a documented skill field, used exactly as intended.

```yaml
---
name: skill-name # optional — defaults to the directory name
description: This skill should be used when the user asks to "specific phrase 1", "specific phrase 2", "specific phrase 3". Include exact phrases users would say. Be concrete and specific.
---
```

---

## 2. Slash commands — `commands/*.md`

All command frontmatter is optional to Claude Code — a command works with none. Custom commands are
now merged into skills: a `commands/*.md` file accepts the **same frontmatter as a `SKILL.md`** (§1),
so every field there is available here too. The table below is the subset commonly used on
`plugins/*/commands/*.md`; this repo adds one enforcement hook (see note).

| Field                      | Purpose                                                                                      | Values / example                                | Required?                |
| -------------------------- | -------------------------------------------------------------------------------------------- | ----------------------------------------------- | ------------------------ |
| `description`              | Shown in the `/` menu / `/help`. Defaults to the first line of the prompt if omitted.        | `Create a git commit…`                          | Repo-enforced (see note) |
| `allowed-tools`            | Restrict which tools the command may invoke. Omit to inherit the conversation's permissions. | `Read, Grep, Bash(git:*)`                       | Optional                 |
| `model`                    | Which model runs the command. Inherits the session model if unset.                           | `haiku` \| `sonnet` \| `opus`                   | Optional                 |
| `effort`                   | How thoroughly the model reasons. Inherits the session effort if unset.                      | `low` \| `medium` \| `high` \| `xhigh` \| `max` | Optional                 |
| `argument-hint`            | Documents expected args for autocomplete / users.                                            | `[pr-number]`                                   | Optional                 |
| `disable-model-invocation` | `true` = user-only `/command` trigger; the SlashCommand tool cannot invoke it.               | `true` \| `false` (default `false`)             | Optional                 |

**Repo enforcement:** the `validate-command-frontmatter.sh` PostToolUse hook requires every
`plugins/*/commands/*.md` to start with `---` and include a `description:` field.

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

Example (from `plugins/git/commands/commit.md`):

```yaml
---
description: Create a git commit with an auto-generated message matching the repo's style
allowed-tools: Bash(git add:*), Bash(git status:*), Bash(git diff:*), Bash(git log:*), Bash(git branch:*), Bash(git switch:*), Bash(git commit:*), AskUserQuestion
model: sonnet
effort: low
---
```

### `model` tier semantics (verbatim from `CLAUDE.md`)

- `model` — which Claude model executes the command. Three values (if unset, inherits the session model; omit unless a command has a specific need):
  - `haiku` — fastest and cheapest, smallest reasoning budget. For simple, mechanical, deterministic commands (e.g. `transcript`). Typically pairs with `effort: low`.
  - `sonnet` — balanced cost vs. capability; the practical default. For standard single-agent workflows and routine commands (e.g. `commit`, `code-review-AT`, `jira:create-ticket`). Typically pairs with `effort: low`/`medium`.
  - `opus` — most capable and most expensive. Reserve for genuinely complex multi-agent orchestration and the hardest reasoning / design judgment (e.g. `debate`, `jira:implement-ticket`, `simplify`). Typically pairs with `effort: high`/`xhigh`.

### `effort` tier semantics (verbatim from `CLAUDE.md`)

- `effort` — how thoroughly the model reasons through the command. Five levels (availability varies by model; an unsupported level falls back to the nearest supported one; if unset, inherits the session effort):
  - `low` — minimal thinking, fastest, biggest token savings. For mechanical / deterministic commands.
  - `medium` — moderate thinking; balances cost/latency vs. depth. For light reasoning without deep multi-step planning (e.g. `/code-review`, `/jira:create-ticket`).
  - `high` — deep reasoning; the practical default for substantive commands. For multi-step workflows, conflict resolution, design judgment.
  - `xhigh` — extended reasoning; Opus-only (4.7+), falls back to `high` elsewhere, so pair with `model: opus`. For the hardest analysis / refactor judgment.
  - `max` — maximum reasoning budget; highest cost/latency. Reserve for the most demanding tasks. (Unused in this repo.)

---

## 3. Agents — `agents/*.md`

| Field         | Purpose                                                                                                                                                            | Values / example                                                                                   | Required?         |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------- | ----------------- |
| `name`        | Agent identifier for namespacing / dispatch.                                                                                                                       | lowercase + digits + hyphens, 3–50 chars, start/end alphanumeric, no underscores (`code-reviewer`) | **Yes**           |
| `description` | The dispatch-decision field — loaded into context. State triggering conditions ("Use this agent when…"); conventionally include `<example>`/`<commentary>` blocks. | see below                                                                                          | **Yes**           |
| `model`       | Which model the agent uses.                                                                                                                                        | `inherit` (recommended) \| `sonnet` \| `opus` \| `haiku`                                           | **Yes** (per doc) |
| `color`       | Visual identifier in the UI.                                                                                                                                       | `blue` `cyan` `green` `yellow` `magenta` `red`                                                     | **Yes** (per doc) |
| `tools`       | Restrict the agent to specific tools (least privilege). Omit to grant all tools. MCP tool names allowed.                                                           | `["Read", "Grep", "Glob"]`                                                                         | Optional          |

**In practice (this repo diverges from the docs):**

- `color` uses values beyond the documented six — `purple`, `pink`, `orange` also appear.
- `tools` is written as a **comma-separated string**, not the documented `["array"]` form, and mixes built-ins with MCP tool names.
- `model` is `sonnet`/`opus`, never the doc-recommended `inherit`.

Official example (`complete-agent-examples.md`, array `tools` + `inherit`):

```yaml
---
name: security-analyzer
description: Use this agent when the user implements security-critical code (auth, payments, data handling), explicitly requests security analysis, or before deploying sensitive changes. Examples:

  <example>
  Context: User implemented authentication logic
  user: "I've added JWT token validation"
  assistant: "I'll use the security-analyzer agent to review for vulnerabilities."
  </example>
model: inherit
color: red
tools: ["Read", "Grep", "Glob"]
---
```

Repo example (`plugins/code-review/agents/security.md`, comma-string `tools` + explicit model):

```yaml
---
name: security
description: Security specialist for /code-review. Reviews PR diffs for authentication, authorization, input validation, injection vectors (SQL, command, prompt), secret handling, and API contract integrity. Always-on specialist; spawned by the /code-review orchestrator.
tools: Read, Grep, Glob, Bash, Write, mcp__plugin_context7_context7__resolve-library-id, mcp__plugin_context7_context7__query-docs
model: sonnet
color: red
---
```
