---
paths:
  - "**/SKILL.md"
  - "**/commands/**/*.md"
  - "**/agents/**/*.md"
---

# Component frontmatter reference

Every YAML frontmatter field available on the three component types this repo authors:
**skills** (`SKILL.md`), **slash commands** (`plugins/<name>/commands/*.md`), and **agents**
(`plugins/<name>/agents/*.md`) ([plugin components](https://code.claude.com/docs/en/plugins/components#add-each-kind-of-component)).
Custom commands were merged into skills — a command file accepts the same frontmatter as a
`SKILL.md` except `name` and `paths` ([frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference))
— so they share one reference here (§1); agents have their own set (§2).

Sources: §1 (skills & slash commands) is verified against the official skills docs
(`code.claude.com/docs/en/skills#frontmatter-reference`; `/docs/en/slash-commands` serves the same
page), checked 2026-11-18; §2 (agents) against the official subagents docs
(`code.claude.com/docs/en/sub-agents#supported-frontmatter-fields`), checked 2026-11-18.

Plugin manifests (`plugins/<name>/.claude-plugin/plugin.json`) are separate config, not component
frontmatter — out of scope here ([manifest file](https://code.claude.com/docs/en/plugins-reference#manifest-file)).
Use `${CLAUDE_PLUGIN_ROOT}` in a component's Markdown body to resolve plugin-relative paths; Claude
Code substitutes it inline when it loads the content ([where each variable resolves](https://code.claude.com/docs/en/plugins-reference#where-each-variable-resolves)).

---

## 1. Skills & slash commands

Files in `.claude/skills/<name>/SKILL.md`, `.claude/commands/*.md`, and plugin `commands/*.md` all
create a `/command` ([command name](https://code.claude.com/docs/en/skills#how-a-skill-gets-its-command-name),
[plugin commands](https://code.claude.com/docs/en/plugins/components#commands));
`SKILL.md` is the recommended authoring format and plain command files still work
([where skills load](https://code.claude.com/docs/en/skills#choose-where-skills-load)). All fields are
optional; only `description` is recommended, so Claude knows when to load the skill/command
([frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference)).

Every field below is **accepted** on both except the two tagged `(not in command files)`: a command
file "supports the same frontmatter except `name` and `paths`"
([where skills load](https://code.claude.com/docs/en/skills#choose-where-skills-load)). Claude can
auto-invoke command files too, so `description` and `when_to_use` matter for both.

Unless a row cites another page, its source is [frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference).

| Field                            | Purpose                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | Values / example                                                         | Required?   |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ | ----------- |
| `name` `(not in command files)`  | Command name shown in the `/` menu; defaults to the directory name. In a plugin skill it sets the part after `<plugin>:`. A command file's name comes from its filename (`commands/<file>.md` → `/<plugin>:<file>`) ([command name](https://code.claude.com/docs/en/skills#how-a-skill-gets-its-command-name), [plugin commands](https://code.claude.com/docs/en/plugins/components#commands)).                                                                                                                                             | `my-skill`                                                               | No          |
| `description`                    | The triggering field: what the skill does and when to use it. Falls back to the first non-empty line of the body if omitted. Combined with `when_to_use`, truncated at **1,536 chars** in the listing, so put the key use case first. Write it in the third person ([writing descriptions](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices#writing-effective-descriptions)) with keywords users would naturally say ([skill not triggering](https://code.claude.com/docs/en/skills#skill-not-triggering)). | see below                                                                | Recommended |
| `when_to_use`                    | Extra context for when Claude should invoke it (trigger phrases / example requests). Appended to `description` in the skill listing; counts toward the 1,536-char cap. Claude uses it when deciding whether to invoke the skill.                                                                                                                                                                                                                                                                                                            | trigger phrases                                                          | No          |
| `argument-hint`                  | Autocomplete hint for expected args.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        | `[issue-number]`, `[filename] [format]`                                  | No          |
| `arguments`                      | Named positional args for `$name` substitution. Space-separated string or YAML list; names map to positions in order.                                                                                                                                                                                                                                                                                                                                                                                                                       | `[issue, branch]`                                                        | No          |
| `disable-model-invocation`       | `true` = user-only (`/name`); Claude won't auto-load it, it's removed from Claude's context ([restrict skill access](https://code.claude.com/docs/en/skills#restrict-claudes-skill-access)), and it can't be preloaded into subagents ([preload skills](https://code.claude.com/docs/en/sub-agents#preload-skills-into-subagents)).                                                                                                                                                                                                         | `true` \| `false` (default `false`)                                      | No          |
| `user-invocable`                 | `false` = hide from the `/` menu and don't run on `/name`; only Claude invokes it (background knowledge).                                                                                                                                                                                                                                                                                                                                                                                                                                   | `true` (default) \| `false`                                              | No          |
| `allowed-tools`                  | Tools pre-approved (no prompt) during the turn that invokes the skill/command; the grant clears on your next message. Does **not** restrict the pool ([pre-approve tools](https://code.claude.com/docs/en/skills#pre-approve-tools-for-a-skill)). Space/comma string or YAML list.                                                                                                                                                                                                                                                          | `Bash(git add *) Bash(git commit *)`                                     | No          |
| `disallowed-tools`               | Tools removed from the pool while active; clears on your next message.                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | `AskUserQuestion`                                                        | No          |
| `model`                          | Model for the rest of the current turn; not saved to settings. Same values as `/model`, or `inherit`.                                                                                                                                                                                                                                                                                                                                                                                                                                       | `haiku` \| `sonnet` \| `opus` \| `fable` \| full model name \| `inherit` | No          |
| `effort`                         | Effort level for the rest of the current turn; overrides the session effort but not `CLAUDE_CODE_EFFORT_LEVEL`. With `context: fork`, sets the forked subagent's effort instead.                                                                                                                                                                                                                                                                                                                                                            | `low` \| `medium` \| `high` \| `xhigh` \| `max`                          | No          |
| `context`                        | `fork` runs the skill/command in a forked subagent — the file body becomes the prompt, with no conversation history ([run in a subagent](https://code.claude.com/docs/en/skills#run-skills-in-a-subagent)).                                                                                                                                                                                                                                                                                                                                 | `fork`                                                                   | No          |
| `agent`                          | Subagent type when `context: fork` is set. Built-in (`Explore` / `Plan` / `general-purpose`) or a custom agent; defaults to `general-purpose` ([run in a subagent](https://code.claude.com/docs/en/skills#run-skills-in-a-subagent)).                                                                                                                                                                                                                                                                                                       | `Explore`                                                                | No          |
| `background`                     | Only with `context: fork`. `false` waits for the forked subagent's result in the invoking turn instead of running it in the background.                                                                                                                                                                                                                                                                                                                                                                                                     | `true` (default) \| `false`                                              | No          |
| `hooks`                          | Hooks registered when the skill/command is invoked and kept running for the rest of the session, not only its own turn; `once: true` removes a hook after its first successful run ([hooks in skills](https://code.claude.com/docs/en/hooks#hooks-in-skills-and-agents)).                                                                                                                                                                                                                                                                   | see hooks docs                                                           | No          |
| `paths` `(not in command files)` | Globs that limit when Claude loads the skill automatically. Comma string or YAML list.                                                                                                                                                                                                                                                                                                                                                                                                                                                      | `src/**/*.ts`                                                            | No          |
| `shell`                          | Shell for `` !`cmd` `` injection. `powershell` needs the PowerShell tool: on by default on Windows without Git Bash (and with Git Bash for claude.ai and Console accounts); elsewhere, including macOS, Linux and WSL, set `CLAUDE_CODE_USE_POWERSHELL_TOOL=1`.                                                                                                                                                                                                                                                                             | `bash` (default) \| `powershell`                                         | No          |
| `metadata`                       | Free-form YAML map for your own tooling; Claude Code doesn't act on it.                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | map                                                                      | No          |
| `license`                        | Agent Skills spec field; Claude Code accepts it but doesn't act on it.                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | text                                                                     | No          |
| `compatibility`                  | Agent Skills spec field for environment requirements; Claude Code accepts it but doesn't act on it.                                                                                                                                                                                                                                                                                                                                                                                                                                         | string, ≤500 chars                                                       | No          |

**Positional substitution is 0-indexed:** `$N` expands to `$ARGUMENTS[N]`, so `$0` is the first
argument and `$1` the second. A `$1` meant as "the first argument" silently receives the second
argument, or stays a literal `$1` when fewer arguments are passed. Indexed arguments use shell-style
quoting: a multi-word value fills one `$N` (or one named `arguments:` slot) only if the caller
quotes it, so an unquoted multi-value argument (several SHAs, inline prose) splits across slots
([string substitutions](https://code.claude.com/docs/en/skills#available-string-substitutions)).
Prefer `$ARGUMENTS` plus an explicit parse rule in the body.

**Not a Claude Code skill field:** a top-level `version` appears in some upstream examples, but it
is absent from the Claude Code skills frontmatter reference
([frontmatter outside Claude Code](https://code.claude.com/docs/en/skills#using-skill-frontmatter-outside-claude-code)),
and the [Agent Skills](https://agentskills.io/specification#metadata-field) open standard has it
only as a `metadata.version` key.

```yaml
---
name: skill-name # optional — defaults to the directory name
description: Processes X and generates Y. Use when the user asks to "specific phrase 1", "specific phrase 2", or mentions <key terms>. Include exact phrases users would say.
---
```

### `model` tier semantics

These govern the `model` row above for both skills and slash commands.

- `model` — which Claude model executes the command. Accepts any value `/model` accepts, or `inherit`
  ([frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference)); omitted
  means no override. This repo uses four aliases; omit unless a command has a specific need:
  - `haiku` — fastest and cheapest tier ([models overview](https://platform.claude.com/docs/en/about-claude/models/overview#latest-models)), for simple tasks ([model aliases](https://code.claude.com/docs/en/model-config#model-aliases), [choose the right model](https://code.claude.com/docs/en/costs#choose-the-right-model)) — mechanical, deterministic commands or skills. Typically pairs with `effort: low`.
  - `sonnet` — balanced cost vs. capability: it "handles most coding tasks well and costs less than Opus" ([choose the right model](https://code.claude.com/docs/en/costs#choose-the-right-model)), for daily coding tasks ([model aliases](https://code.claude.com/docs/en/model-config#model-aliases)). The practical default for standard single-agent workflows and routine commands. Typically pairs with `effort: low`/`medium`.
  - `opus` — priced above Sonnet and Haiku, below Fable ([models overview](https://platform.claude.com/docs/en/about-claude/models/overview#latest-models)). Reserve for complex architectural decisions and multi-step reasoning ([choose the right model](https://code.claude.com/docs/en/costs#choose-the-right-model)). Typically pairs with `effort: high`/`xhigh`.
  - `fable` — the most capable models ([work with Fable](https://code.claude.com/docs/en/model-config#work-with-fable)); the alias resolves to Fable 5.2, or Fable 5 in Claude apps gateway sessions ([model aliases](https://code.claude.com/docs/en/model-config#model-aliases)). For the hardest, longest-running tasks. Never a default. Requests its safety classifiers flag, most often cybersecurity or biology, fall back to an Opus model automatically ([automatic model fallback](https://code.claude.com/docs/en/model-config#automatic-model-fallback)), and its documented bug-finding gains exclude the cybersecurity domains those classifiers cover ([capability improvements](https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/prompting-claude-fable-5#capability-improvements)) — so it is the wrong pin for security-domain work; offensive-security workloads trigger fallback frequently, often on the first request ([security and biology workloads](https://code.claude.com/docs/en/model-config#security-research-and-biology-workloads)).

### `effort` tier semantics

These govern the `effort` row above for both skills and slash commands.

- `effort` — how thoroughly the model reasons through the command. Five levels; availability varies by
  model, and an unsupported level falls back to the highest supported level at or below it
  ([adjust effort level](https://code.claude.com/docs/en/model-config#adjust-effort-level)). If unset,
  the session effort applies ([frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference)):
  - `low` — most efficient: significant token savings and the best speed, with some capability reduction ([effort levels](https://platform.claude.com/docs/en/build-with-claude/effort#effort-levels)). For mechanical / deterministic commands.
  - `medium` — a balance of speed, cost, and performance ([effort levels](https://platform.claude.com/docs/en/build-with-claude/effort#effort-levels)); the default on Fable 5.2, Opus 5.5, Sonnet 5.5 and Haiku 5.5 ([choose an effort level](https://code.claude.com/docs/en/model-config#choose-an-effort-level)). For light reasoning without deep multi-step planning.
  - `high` — for complex reasoning, difficult coding problems and agentic tasks ([effort levels](https://platform.claude.com/docs/en/build-with-claude/effort#effort-levels)), and work where verification matters or edge cases are likely ([choose an effort level](https://code.claude.com/docs/en/model-config#choose-an-effort-level)). For multi-step workflows, conflict resolution, design judgment.
  - `xhigh` — extended reasoning; supported on Fable, Opus 5.5/5/4.8/4.7, Sonnet 5.5/5 and Haiku 5.5, and runs as `high` on Opus 4.6 and Sonnet 4.6 ([adjust effort level](https://code.claude.com/docs/en/model-config#adjust-effort-level)). For the hardest analysis / refactor judgment.
  - `max` — maximum reasoning budget; highest cost/latency ([recommended effort levels](https://platform.claude.com/docs/en/build-with-claude/effort#recommended-effort-levels)). Reserve for the most demanding tasks.

**`ultracode` is not one of these.** It is "a Claude Code setting rather than a model effort level"
that has Claude plan a workflow for each substantive task
([adjust effort level](https://code.claude.com/docs/en/model-config#adjust-effort-level),
[let Claude decide](https://code.claude.com/docs/en/workflows#let-claude-decide-with-ultracode)). It
does not unlock the `Workflow` tool: a direct request such as "use a workflow" starts one without it
([ask for a workflow](https://code.claude.com/docs/en/workflows#ask-for-a-workflow-in-your-prompt)).
It turns on via `/effort ultracode` (current session), `claude --effort ultracode` (v2.1.203+), or
the `ultracode` setting — `"ultracode": true` in a settings file, with `--settings`, or in an Agent
SDK control request ([adjust effort level](https://code.claude.com/docs/en/model-config#adjust-effort-level));
Claude Code reads that key but never writes it ([settings reference](https://code.claude.com/docs/en/settings-reference#ultracode)).
It is **never set from component frontmatter**: `effort: ultracode` in a `SKILL.md` is not a valid
level, since `effort` accepts only `low`–`max`
([frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference)).

To have a skill use workflows, instruct it to call the `Workflow` tool in its **body**: "the user
invoked a skill or slash command whose instructions tell you to call Workflow" is one of that tool's
opt-in paths, per its own tool description. Ship the script in the plugin's `workflows/` and run it
by name (`/<plugin>:<meta.name>`, [distribute a workflow](https://code.claude.com/docs/en/workflows#distribute-a-workflow-in-a-plugin)),
so the schema is never hand-written (see [plugin-structure.md](plugin-structure.md) › Workflows).
Don't pass a plugin-shipped script as `scriptPath`: Workflow starts only a script the session may
already read — under the working directory, an `/add-dir` directory, a Read allow rule, or a path a
previous run returned ([how a workflow runs](https://code.claude.com/docs/en/workflows#how-a-workflow-runs))
— and a plugin installed from a marketplace not added from a local path is copied into the plugin
cache, outside all of those for every user
not developing the plugin ([in-place and copied plugins](https://code.claude.com/docs/en/plugins/loading#in-place-and-copied-plugins)).
If running by name fails, Read the file and pass it as `script`
([Workflow input](https://code.claude.com/docs/en/agent-sdk/typescript#workflow)).

### Slash-command specifics

**`allowed-tools` formats** — "a space- or comma-separated string, or a YAML list"
([frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference)); entries can
be `Bash(...)` command filters ([pre-approve tools](https://code.claude.com/docs/en/skills#pre-approve-tools-for-a-skill),
[wildcard patterns](https://code.claude.com/docs/en/permissions#wildcard-patterns)). Don't use `"*"`
as an all-tools grant: the skills docs document no all-tools form, and in permission allow rules an
unanchored glob such as `"*"` "is skipped and doesn't auto-approve anything"
([tool name wildcards](https://code.claude.com/docs/en/permissions#tool-name-wildcards)).

```yaml
allowed-tools: Read
allowed-tools: Read, Write, Edit
allowed-tools:
  - Read
  - Write
  - Bash(git:*)
allowed-tools: Bash(git:*) # only git; also Bash(npm:*), Bash(docker:*)
```

Example ([frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference)):

```yaml
---
name: commit
description: Create a git commit with an auto-generated message matching the repo's style. Commits only — does not push.
when_to_use: Use when the user asks to "commit", "commit this", "make a commit", or wants current changes committed with a well-formed message. Pass `--staged` when the user asks to "commit staged", "commit only what I staged", "commit the index", or otherwise wants only the already-staged changes committed.
argument-hint: [--staged]
allowed-tools: Bash(git add:*), Bash(git status:*), Bash(git diff:*), Bash(git log:*), Bash(git branch:*), Bash(git switch:*), Bash(git commit:*), AskUserQuestion
model: sonnet
effort: low
---
```

---

## 2. Agents — `agents/*.md`

Only `name` and `description` are required. Unless a row cites another page, its source is
[supported frontmatter fields](https://code.claude.com/docs/en/sub-agents#supported-frontmatter-fields)
(the visible heading reads "Frontmatter reference"; the rendered id is `supported-frontmatter-fields`).

| Field               | Purpose                                                                                                                                                                                                                                                                                                                                                                        | Values / example                                                                                                                          | Required? |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------- | --------- |
| `name`              | Unique identifier, at most 256 characters; can't contain `:` (reserved for plugin-scoped names) or start with `-`. Hooks receive this value as `agent_type`. The filename doesn't have to match.                                                                                                                                                                               | `code-reviewer`                                                                                                                           | **Yes**   |
| `description`       | The dispatch-decision field — when Claude should delegate to this agent. State triggering conditions ("Use this agent when…"); "use proactively" encourages delegation ([automatic delegation](https://code.claude.com/docs/en/sub-agents#understand-automatic-delegation)). Conventionally include `<example>`/`<commentary>` blocks.                                         | see below                                                                                                                                 | **Yes**   |
| `tools`             | Allowlist of tools (least privilege). If omitted, inherits every tool available to subagents. Comma-separated string or YAML list; MCP tool names allowed (`mcp__server__tool`, or `mcp__server__*`) ([restrict tools](https://code.claude.com/docs/en/sub-agents#restrict-which-tools-a-subagent-can-use)). To preload skills, use `skills` rather than listing `Skill` here. | `Read, Grep, Glob`                                                                                                                        | No        |
| `disallowedTools`   | Denylist — removed from the inherited or specified list. Applied first; `tools` resolves against the remaining pool. Accepts `mcp__<server>` / `mcp__*` patterns ([restrict tools](https://code.claude.com/docs/en/sub-agents#restrict-which-tools-a-subagent-can-use)).                                                                                                       | `Write, Edit`                                                                                                                             | No        |
| `model`             | Which model the agent uses. Precedence: per-invocation `model` parameter > frontmatter `model` > `CLAUDE_CODE_SUBAGENT_MODEL` > parent model; setting `CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1` makes the env var override both ([choose a model](https://code.claude.com/docs/en/sub-agents#choose-a-model)).                                                                      | `sonnet` \| `opus` \| `haiku` \| `fable` \| full model ID \| `inherit`                                                                    | No        |
| `permissionMode`    | Permission mode for the subagent. A parent in `bypassPermissions`/`acceptEdits`/auto mode overrides it, and a `plan` parent keeps the subagent in plan mode; under a `default`/`dontAsk` parent, a `bypassPermissions` value is ignored too ([permission modes](https://code.claude.com/docs/en/sub-agents#permission-modes)). Ignored for plugin subagents (see note).        | `default` \| `acceptEdits` \| `auto` \| `dontAsk` \| `bypassPermissions` \| `plan` (+ `manual` alias for `default`)                       | No        |
| `maxTurns`          | Maximum number of agentic turns before the subagent stops.                                                                                                                                                                                                                                                                                                                     | `30`                                                                                                                                      | No        |
| `autoCompactWindow` | Lowers this subagent's auto-compact window to a token count. Requires v2.1.296+.                                                                                                                                                                                                                                                                                               | `100000`–`1000000`                                                                                                                        | No        |
| `skills`            | Skills preloaded into the subagent's context at startup — full content injected, not just descriptions. Unlisted skills stay invocable via the Skill tool unless `Skill` is withheld via `tools` or `disallowedTools` ([preload skills](https://code.claude.com/docs/en/sub-agents#preload-skills-into-subagents)).                                                            | YAML list of skill names                                                                                                                  | No        |
| `mcpServers`        | MCP servers for this subagent: string references to already-configured servers, or inline definitions scoped to the subagent ([scope MCP servers](https://code.claude.com/docs/en/sub-agents#scope-mcp-servers-to-a-subagent)). Ignored for plugin subagents (see note).                                                                                                       | server-name refs or inline defs (`stdio`/`http`/`sse`/`ws`)                                                                               | No        |
| `hooks`             | Lifecycle hooks scoped to this subagent; frontmatter `Stop` converts to `SubagentStop` at runtime ([hooks in frontmatter](https://code.claude.com/docs/en/sub-agents#hooks-in-subagent-frontmatter)). Ignored for plugin subagents (see note).                                                                                                                                 | see hooks docs                                                                                                                            | No        |
| `memory`            | Persistent memory directory that survives across conversations; auto-enables Read/Write/Edit and injects the first 200 lines / 25KB of its `MEMORY.md` ([persistent memory](https://code.claude.com/docs/en/sub-agents#enable-persistent-memory)).                                                                                                                             | `user` (`~/.claude/agent-memory/<name>/`) \| `project` (`.claude/agent-memory/<name>/`) \| `local` (`.claude/agent-memory-local/<name>/`) | No        |
| `background`        | `true` = stay in the background even when Claude asks to wait for it. Unset = a subagent Claude spawns from the main conversation starts in the background ([background subagents](https://code.claude.com/docs/en/sub-agents#which-subagents-run-in-the-background)).                                                                                                         | `true` \| `false`                                                                                                                         | No        |
| `omitClaudeMd`      | `true` = start without the user, project, and local CLAUDE.md files (managed policy files still load, except for managed subagents defined in managed settings), for subagents that take everything from the delegation prompt. Ignored when run as the main session agent. Requires v2.1.271+.                                                                                | `true` \| `false`                                                                                                                         | No        |
| `effort`            | Effort level while the subagent is active; overrides the session effort but not `CLAUDE_CODE_EFFORT_LEVEL`. Available levels depend on the model.                                                                                                                                                                                                                              | `low` \| `medium` \| `high` \| `xhigh` \| `max`                                                                                           | No        |
| `isolation`         | `worktree` runs the subagent in a temporary git worktree, branched by default from the default branch rather than the parent's `HEAD` (`worktree.baseRef: "head"` in settings changes that); auto-cleaned if the subagent makes no changes.                                                                                                                                    | `worktree`                                                                                                                                | No        |
| `color`             | Display color in the task list and transcript.                                                                                                                                                                                                                                                                                                                                 | `red` \| `blue` \| `green` \| `yellow` \| `purple` \| `orange` \| `pink` \| `cyan`                                                        | No        |
| `initialPrompt`     | Auto-submitted as the first user turn when the agent runs as the main session agent (`--agent` flag or `agent` setting). Commands and skills are processed; prepended to any user-provided prompt. Ignored for plugin subagents (see note).                                                                                                                                    | text                                                                                                                                      | No        |
| `experimental`      | Map of experimental options. Its documented key is `cacheTtl`: this subagent's prompt-cache lifetime (`1h` is ignored while the subscription uses usage credits). Requires v2.1.248+.                                                                                                                                                                                          | `cacheTtl: 5m` \| `cacheTtl: 1h`                                                                                                          | No        |

**Plugin subagents**: `hooks`, `mcpServers`, `permissionMode`, and `initialPrompt` are ignored when
the agent loads from a plugin. If an agent needs them, copy its file into `.claude/agents/` or
`~/.claude/agents/`, or add the hooks and MCP servers at plugin level (`hooks/hooks.json`,
`.mcp.json`) ([subagent scope](https://code.claude.com/docs/en/sub-agents#choose-the-subagent-scope);
`initialPrompt`: [supported frontmatter fields](https://code.claude.com/docs/en/sub-agents#supported-frontmatter-fields)).

Official example (docs "Example subagents" `code-reviewer`, comma-string `tools` + `model: inherit`)
([code-reviewer](https://code.claude.com/docs/en/sub-agents#code-reviewer)):

```yaml
---
name: code-reviewer
description: Expert code review specialist. Proactively reviews code for quality, security, and maintainability. Use immediately after writing or modifying code.
tools: Read, Grep, Glob, Bash
model: inherit
---
```
