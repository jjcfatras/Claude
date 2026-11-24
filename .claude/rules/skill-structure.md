---
paths:
  - ".claude/skills/**"
  - "plugins/*/skills/**"
---

# Skill structure

Every skill follows Anthropic's [Create custom skills](https://claude.com/docs/skills/how-to#directory-structure) layout, which is the [Agent Skills specification](https://agentskills.io/specification)'s layout ([how-to](https://claude.com/docs/skills/how-to)). Skills live in two of the [places Claude Code loads skills from](https://code.claude.com/docs/en/skills#where-skills-live):

- **Plugin skills**: `plugins/<plugin>/skills/<skill>/SKILL.md`, loaded wherever the plugin is enabled, as `/<plugin>:<skill>` ([where skills load](https://code.claude.com/docs/en/skills#where-skills-live)).
- **Project skills**: `.claude/skills/<skill>/SKILL.md`, loaded in sessions in that repository ([where skills load](https://code.claude.com/docs/en/skills#where-skills-live)).

```text
<skill-name>/
├── SKILL.md      # required: frontmatter + instructions
├── references/   # optional: docs Claude reads when a step calls for it
├── assets/       # optional: templates and data Claude copies or fills in
└── scripts/      # optional: code Claude runs
```

Only `SKILL.md` is required ([directory structure](https://claude.com/docs/skills/how-to#directory-structure)), and it holds YAML frontmatter plus the instructions ([first skill](https://code.claude.com/docs/en/skills#create-your-first-skill)). The other folders are optional ([directory structure](https://claude.com/docs/skills/how-to#directory-structure)), so create one when its first file needs it: no empty folders, no placeholders.

## Skill-level vs plugin-level files

- A file that only one skill uses goes in a folder next to that skill's `SKILL.md` ([add resources](https://claude.com/docs/skills/how-to#add-resources)). Workflow scripts are the exception: they go in the plugin's `workflows/` ([distribute a workflow in a plugin](https://code.claude.com/docs/en/workflows#distribute-a-workflow-in-a-plugin); see [plugin-structure.md](plugin-structure.md) › Workflows).
- A file shared by several skills of a plugin goes at the plugin root next to `skills/`, and skills reach it through `${CLAUDE_PLUGIN_ROOT}`, which covers "resources shared between the plugin's skills" ([string substitutions](https://code.claude.com/docs/en/skills#available-string-substitutions)). Don't use the plugin-root `agents/` or `bin/` for this: every Markdown file under `agents/`, subfolders included, loads as an agent ([plugin agents](https://code.claude.com/docs/en/plugins/components#agents), [subfolders](https://code.claude.com/docs/en/plugins/components#organize-agents-in-subfolders)), and `bin/` goes on the Bash tool's `PATH` ([executables](https://code.claude.com/docs/en/plugins/components#executables)).

## SKILL.md

- `name`: lowercase letters, digits and hyphens, at most 64 characters, no XML tags, no reserved words (`anthropic`, `claude`) ([best practices](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices#yaml-frontmatter-requirements), [Skills API](https://platform.claude.com/docs/en/build-with-claude/skills-guide#creating-a-skill)). It must match the directory name ([required fields](https://claude.com/docs/skills/how-to#required-fields), [spec](https://agentskills.io/specification#name-field)). Claude Code accepts a `name` that differs from the directory ([command name](https://code.claude.com/docs/en/skills#how-a-skill-gets-its-command-name)).
- `description`: what the skill does **and** when to use it ([frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference)), using the words a request would use (`Use when the user asks to "…"`) ([best practices](https://claude.com/docs/skills/how-to#best-practices), [skill not triggering](https://code.claude.com/docs/en/skills#skill-not-triggering)). At most 1,024 characters ([overview](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview#skill-structure)). Claude Code also truncates `description` + `when_to_use` together at 1,536 characters in the skill listing, so put the key use case first ([frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference)). Until the skill is invoked, Claude sees only the listing, not its content ([who invokes a skill](https://code.claude.com/docs/en/skills#control-who-invokes-a-skill), [best practices](https://claude.com/docs/skills/how-to#best-practices)).
- Every other field: [frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference). `allowed-tools` is a spec field; `argument-hint`, `when_to_use`, `model`, `effort` and `disable-model-invocation` are Claude Code-only, and Claude Code accepts every field ([frontmatter outside Claude Code](https://code.claude.com/docs/en/skills#using-skill-frontmatter-outside-claude-code)). `skills-ref validate` reports a field the spec doesn't define as an error ([before uploading](https://claude.com/docs/skills/how-to#before-uploading)); for a skill that runs only in Claude Code, ignore those errors. A claude.ai upload or the Skills API accepts only `name`, `description`, `license`, `compatibility`, `metadata` and `allowed-tools` ([frontmatter outside Claude Code](https://code.claude.com/docs/en/skills#using-skill-frontmatter-outside-claude-code)).
- Body: the procedure ([level 2](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview#level-2-instructions-loaded-when-triggered)), examples of inputs and outputs, and edge cases ([write the instructions](https://claude.com/docs/skills/how-to#write-the-instructions)). On invocation the whole rendered `SKILL.md` enters the conversation ([content lifecycle](https://code.claude.com/docs/en/skills#skill-content-lifecycle)), so keep it under 500 lines and move anything needed only some of the time to `references/` ([add supporting files](https://code.claude.com/docs/en/skills#add-supporting-files), [add resources](https://claude.com/docs/skills/how-to#add-resources)).

## Which folder

| Folder        | Holds                                     | Claude…                                                  | Use when                                                                    |
| ------------- | ----------------------------------------- | -------------------------------------------------------- | --------------------------------------------------------------------------- |
| `SKILL.md`    | The steps for every run                   | reads it all every time the skill loads                  | —                                                                           |
| `references/` | Background and long reference material    | opens it only when a step calls for it                   | the content is long, or only some runs need it                              |
| `assets/`     | Templates, images, lookup tables, schemas | copies or fills it in; doesn't follow it as instructions | the output must match a fixed shape, or a file is passed to a tool verbatim |
| `scripts/`    | Executable code                           | runs it with Bash                                        | a step is more reliable as a program than as generated code                 |

Sources: `SKILL.md` ([content lifecycle](https://code.claude.com/docs/en/skills#skill-content-lifecycle), [add resources](https://claude.com/docs/skills/how-to#add-resources)); `references/` ([add supporting files](https://code.claude.com/docs/en/skills#add-supporting-files), [add resources](https://claude.com/docs/skills/how-to#add-resources)); `assets/` ([add resources](https://claude.com/docs/skills/how-to#add-resources), [directory structure](https://claude.com/docs/skills/how-to#directory-structure), [spec](https://agentskills.io/specification#assets/)); `scripts/` ([directory structure](https://claude.com/docs/skills/how-to#directory-structure), [add scripts](https://claude.com/docs/skills/how-to#add-scripts), [utility scripts](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices#provide-utility-scripts)).

- Start with instructions only. Add a script when a step needs code ([best practices](https://claude.com/docs/skills/how-to#best-practices)).
- If a worked example already exists in the repo, link to it rather than copying it into the skill.
- Keep each file to one topic, so Claude loads only what the task needs ([domain-specific organization](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices#pattern-2-domain-specific-organization), [add resources](https://claude.com/docs/skills/how-to#add-resources)).

A skill may hold other files and folders beyond `SKILL.md` ([spec](https://agentskills.io/specification#optional-directories)):

- `agents/` at skill level: the prompts a skill passes to its own subagents. At plugin level, `agents/` holds the plugin's agents ([plugin agents](https://code.claude.com/docs/en/plugins/components#agents), [standard layout](https://code.claude.com/docs/en/plugins/manifest-reference#standard-layout)).
- `evals/`: eval sets read by eval tooling, not by Claude at run time. At skill level, skill-creator reads `evals/evals.json` ([run evals with skill-creator](https://code.claude.com/docs/en/skills#run-evals-with-skill-creator)); at plugin level, `claude plugin eval` reads `evals/` ([plugin evals](https://code.claude.com/docs/en/plugin-evals#how-an-eval-run-works)).

## Referencing files

- Name each file in `SKILL.md` at the step that uses it ("Read `${CLAUDE_PLUGIN_ROOT}/references/prose-standards.md`") so Claude knows when to open it ([add supporting files](https://code.claude.com/docs/en/skills#add-supporting-files), [add resources](https://claude.com/docs/skills/how-to#add-resources)). Claude reads bundled files only when something references them ([level 3](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview#level-3-resources-and-code-loaded-as-needed)), and every reference file should link directly from `SKILL.md` ([avoid nested references](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices#avoid-deeply-nested-references)), so delete any `references/` or `assets/` file `SKILL.md` doesn't name.
- Skill-level `references/` and `assets/`: link them with a path relative to `SKILL.md` (`[references/x.md](references/x.md)`) ([add supporting files](https://code.claude.com/docs/en/skills#add-supporting-files)). Use `${CLAUDE_SKILL_DIR}/<folder>/<file>` instead when the skill moves the session's cwd ([how injected commands run](https://code.claude.com/docs/en/skills#how-injected-commands-run)).
- Skill-level `scripts/`: use `${CLAUDE_SKILL_DIR}/scripts/<file>` ([add scripts](https://claude.com/docs/skills/how-to#add-scripts)). In a plugin skill this placeholder is the skill's own folder, not the plugin root ([string substitutions](https://code.claude.com/docs/en/skills#available-string-substitutions)). Running a script is a Bash tool call ([add scripts](https://claude.com/docs/skills/how-to#add-scripts)), so if the skill sets `allowed-tools`, add `Bash(${CLAUDE_SKILL_DIR}/scripts/<file> *)`. Claude Code substitutes the placeholder there too, so the script runs without a prompt ([string substitutions](https://code.claude.com/docs/en/skills#available-string-substitutions)).
- Plugin-level files: use `${CLAUDE_PLUGIN_ROOT}/<folder>/<file>` ([string substitutions](https://code.claude.com/docs/en/skills#available-string-substitutions)). Never use a relative path that leaves the plugin, or a path into the plugin's source repo: a copied install puts only the plugin folder in the plugin cache, and files outside it aren't copied ([in-place and copied plugins](https://code.claude.com/docs/en/plugins/loading#in-place-and-copied-plugins)).
- Both placeholders are text substitutions Claude Code makes when the skill loads, not environment variables ([add scripts](https://claude.com/docs/skills/how-to#add-scripts), [where each variable resolves](https://code.claude.com/docs/en/plugins/manifest-reference#where-each-variable-resolves)). The docs list only the skill's markdown content and `allowed-tools` Bash rules as substitution sites ([string substitutions](https://code.claude.com/docs/en/skills#available-string-substitutions)).
- Never put credentials in a skill file ([review checklist](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/enterprise#review-checklist), [add scripts](https://claude.com/docs/skills/how-to#add-scripts)). Everyone the skill is shared with receives its files ([add scripts](https://claude.com/docs/skills/how-to#add-scripts)), and a plugin skill loads wherever the plugin is enabled ([where skills load](https://code.claude.com/docs/en/skills#where-skills-live)).

Observed in Claude Code, not in the docs:

- A `references/` file Claude Reads later is not substituted, so it should use only values `SKILL.md` already resolved.
