---
paths:
  - ".claude/skills/**"
  - "plugins/*/skills/**"
---

# Skill structure

Every skill follows Anthropic's [Create custom skills](https://claude.com/docs/skills/how-to#directory-structure) layout, which is the [Agent Skills specification](https://agentskills.io/specification)'s layout ([how-to](https://claude.com/docs/skills/how-to)). Skills live in two of the [places Claude Code loads skills from](https://code.claude.com/docs/en/skills#choose-where-skills-load):

- **Plugin skills**: `plugins/<plugin>/skills/<skill>/SKILL.md`, loaded wherever the plugin is enabled, as `/<plugin>:<skill>` ([where skills load](https://code.claude.com/docs/en/skills#choose-where-skills-load)).
- **Project skills**: `.claude/skills/<skill>/SKILL.md`, loaded in sessions in that repository ([where skills load](https://code.claude.com/docs/en/skills#choose-where-skills-load)).

```text
<skill-name>/
├── SKILL.md      # required: frontmatter + instructions
├── references/   # optional: docs Claude reads when a step calls for it
├── assets/       # optional: templates and data Claude copies or fills in
└── scripts/      # optional: code Claude runs
```

Only `SKILL.md` is required ([directory structure](https://claude.com/docs/skills/how-to#directory-structure)), and it holds YAML frontmatter plus the instructions ([first skill](https://code.claude.com/docs/en/skills#create-your-first-skill)). Create a folder when the first file needs it: no empty folders, no placeholders.

## Skill-level vs plugin-level files

- A file that only one skill uses goes in that skill's folder, in the layout above. Workflow scripts are the exception: even when one skill runs them, they go in the plugin's `workflows/` ([distribute a workflow in a plugin](https://code.claude.com/docs/en/workflows#distribute-a-workflow-in-a-plugin); see [plugin-structure.md](plugin-structure.md) › Workflows).
- A file shared by several skills of a plugin, or by a skill and the plugin's agents, commands or hooks, goes at the plugin root next to `skills/`: `references/`, `agents/`, `bin/`, `tools/`, `hooks/`, `scripts/`. Skills reach it through `${CLAUDE_PLUGIN_ROOT}`, which covers "resources shared between the plugin's skills" ([string substitutions](https://code.claude.com/docs/en/skills#available-string-substitutions)).

Don't move an existing plugin-level file into a skill folder just to match the tree above.

## SKILL.md

- `name`: lowercase letters, digits and hyphens, at most 64 characters ([best practices](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices#yaml-frontmatter-requirements)), the same as the directory name ([spec](https://agentskills.io/specification#name-field)). claude.ai uploads and the Skills API validate these rules ([frontmatter outside Claude Code](https://code.claude.com/docs/en/skills#using-skill-frontmatter-outside-claude-code)); Claude Code accepts a `name` that differs from the directory ([command name](https://code.claude.com/docs/en/skills#how-a-skill-gets-its-command-name)).
- `description`: what the skill does **and** when to use it ([frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference)), using the words a request would use (`Use when the user asks to "…"`) ([skill not triggering](https://code.claude.com/docs/en/skills#skill-not-triggering)). At most 1,024 characters ([overview](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview#skill-structure)). Claude Code also truncates `description` + `when_to_use` together at 1,536 characters in the skill listing, so put the key use case first ([frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference)). Until the skill is invoked, Claude sees only the listing, not its content ([who invokes a skill](https://code.claude.com/docs/en/skills#control-who-invokes-a-skill)).
- Every other field: [component-frontmatter.md](component-frontmatter.md) §1. `skills-ref validate` reports the Claude Code-only fields (`argument-hint`, `when_to_use`, `model`, `effort`, `disable-model-invocation`) as non-spec; `allowed-tools` is a spec field. Claude Code accepts every field, so ignore those warnings for a skill that runs only in Claude Code. A claude.ai upload or the Skills API accepts only `name`, `description`, `license`, `compatibility`, `metadata` and `allowed-tools` ([frontmatter outside Claude Code](https://code.claude.com/docs/en/skills#using-skill-frontmatter-outside-claude-code)).
- Body: the procedure ([level 2](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview#level-2-instructions-loaded-when-triggered)), inputs and outputs, gates and edge cases. On invocation the whole rendered `SKILL.md` enters the conversation ([content lifecycle](https://code.claude.com/docs/en/skills#skill-content-lifecycle)), so keep it under 500 lines and move anything needed only some of the time to `references/` ([add supporting files](https://code.claude.com/docs/en/skills#add-supporting-files)).

## Which folder

| Folder        | Holds                                                   | Claude…                                                  | Use when                                                                          |
| ------------- | ------------------------------------------------------- | -------------------------------------------------------- | --------------------------------------------------------------------------------- |
| `SKILL.md`    | The steps for every run                                 | reads it all on load                                     | always                                                                            |
| `references/` | Background: specs, checklists, field tables, long rules | reads it for guidance, only at the step that names it    | the content is long, or only some runs need it                                    |
| `assets/`     | Templates, lookup tables, schemas, images               | copies or fills it in; doesn't follow it as instructions | the output must match a fixed shape, or a file is passed to a tool verbatim       |
| `scripts/`    | Executable code                                         | runs it with Bash                                        | a step is more reliable as a program than as prose: parsing, validating, counting |

Sources: `SKILL.md` ([content lifecycle](https://code.claude.com/docs/en/skills#skill-content-lifecycle)); `references/` ([add supporting files](https://code.claude.com/docs/en/skills#add-supporting-files)); `assets/` ([directory structure](https://claude.com/docs/skills/how-to#directory-structure), [spec](https://agentskills.io/specification#assets/)); `scripts/` ([directory structure](https://claude.com/docs/skills/how-to#directory-structure), [add scripts](https://claude.com/docs/skills/how-to#add-scripts)).

- Start with instructions only. Add a script when a step needs code ([best practices](https://claude.com/docs/skills/how-to#best-practices)).
- If a worked example already exists in the repo, link to it rather than copying it into the skill, where the copy would drift.
- Keep each file to one topic, so Claude loads only what the task needs ([domain-specific organization](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices#pattern-2-domain-specific-organization)).

Folders outside the spec, at either level:

- `agents/`: instructions for subagents — a plugin's agents ([plugin agents](https://code.claude.com/docs/en/plugins/components#agents), [standard layout](https://code.claude.com/docs/en/plugins-reference#standard-layout)), or the prompts a skill passes to its own subagents.
- `tools/`: source for a helper program, either built into `scripts/` or run from source.
- `evals/`: eval sets read by eval tooling, not by Claude at run time. At skill level, skill-creator reads `evals/evals.json` ([run evals with skill-creator](https://code.claude.com/docs/en/skills#run-evals-with-skill-creator)).

## Referencing files

- Name each file in `SKILL.md` at the step that uses it ("Read `${CLAUDE_PLUGIN_ROOT}/references/prose-standards.md`") so Claude knows when to open it ([add supporting files](https://code.claude.com/docs/en/skills#add-supporting-files)). Claude reads bundled files only when something references them ([level 3](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview#level-3-resources-and-code-loaded-as-needed)), so delete any file `SKILL.md` doesn't name. The exception is a tooling-only file like `evals/`.
- Skill-level `references/` and `assets/`: link them with a path relative to `SKILL.md` (`[references/x.md](references/x.md)`) ([add supporting files](https://code.claude.com/docs/en/skills#add-supporting-files)). Use `${CLAUDE_SKILL_DIR}/<folder>/<file>` instead when the skill moves the session's cwd ([how injected commands run](https://code.claude.com/docs/en/skills#how-injected-commands-run)), or when the path is passed to a tool or subagent as a value.
- Skill-level `scripts/`: use `${CLAUDE_SKILL_DIR}/scripts/<file>`. In a plugin skill this placeholder is the skill's own folder, not the plugin root. Running a script is a Bash call, so if the skill sets `allowed-tools`, add `Bash(${CLAUDE_SKILL_DIR}/scripts/<file> *)`. Claude Code substitutes the placeholder there too, so the script runs without a prompt ([string substitutions](https://code.claude.com/docs/en/skills#available-string-substitutions)).
- Plugin-level files: use `${CLAUDE_PLUGIN_ROOT}/<folder>/<file>` ([string substitutions](https://code.claude.com/docs/en/skills#available-string-substitutions)). Never use a relative path that leaves the plugin, or a path into the plugin's source repo. An install from any marketplace not added from a local path copies only the plugin folder into the plugin cache, and files outside it aren't copied ([in-place and copied plugins](https://code.claude.com/docs/en/plugins/loading#in-place-and-copied-plugins)).
- Both placeholders are text substitutions Claude Code makes when the skill loads, not environment variables. The docs list only the skill's markdown content and `allowed-tools` Bash rules as substitution sites ([string substitutions](https://code.claude.com/docs/en/skills#available-string-substitutions), [where each variable resolves](https://code.claude.com/docs/en/plugins/manifest-reference#where-each-variable-resolves)). A `references/` file Claude Reads later is not substituted, so it should use only values `SKILL.md` already resolved.
- Never put credentials ([review checklist](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/enterprise#review-checklist)) or environment URLs in a skill file. Plugin skills ship to everyone the plugin reaches ([where skills load](https://code.claude.com/docs/en/skills#choose-where-skills-load)).
