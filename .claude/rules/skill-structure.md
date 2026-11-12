---
paths:
  - ".claude/skills/**"
  - "plugins/*/skills/**"
---

# Skill structure

Every skill follows Anthropic's [Create custom skills](https://claude.com/docs/skills/how-to#directory-structure) layout (the [Agent Skills specification](https://agentskills.io/specification)). This repo has two kinds of skill:

- **Plugin skills**: `plugins/<plugin>/skills/<skill>/SKILL.md`, shipped to every user who installs the plugin.
- **Repo-local skills**: `.claude/skills/<skill>/SKILL.md`, used only inside this repo (`plugin-session-auditor`).

```text
<skill-name>/
├── SKILL.md      # required: frontmatter + instructions
├── references/   # optional: docs Claude reads when a step calls for it
├── assets/       # optional: templates and data Claude copies or fills in
└── scripts/      # optional: code Claude runs
```

Only `SKILL.md` is required. Create a folder when the first file needs it: no empty folders, no placeholders. File names are kebab-case like the rest of the repo (`check-contrast.sh`, not `check_contrast.sh`). Go sources under `tools/` follow Go naming instead (`prior_review.go`).

## Skill-level vs plugin-level files

- A file that only one skill uses goes in that skill's folder, in the layout above. `review-pr` does this for its orchestrator-only files: the procedures only some runs need in `skills/review-pr/references/`, and its summary filter in `skills/review-pr/scripts/summary.jq`. `respond-to-review` keeps every file there: its triage procedure in `references/`, and its Workflow script, agent preamble and output templates in `assets/`.
- A file shared by several skills of a plugin, or by a skill and the plugin's agents, commands or hooks, goes at the plugin root next to `skills/`: `references/`, `agents/`, `bin/`, `tools/`, `hooks/`, `scripts/` (see the per-plugin layout in the root `CLAUDE.md`). Every current plugin works this way. The three jira skills share `plugins/jira/references/`, and `review-pr` reaches `scripts/code-review-helper`, `references/code-review-rubrics.md` and `agents/*.md` at its plugin root.

Don't move an existing plugin-level file into a skill folder just to match the tree above.

## SKILL.md

- `name`: lowercase letters, digits and hyphens, at most 64 characters, the same as the directory name. This is a spec rule; Claude Code doesn't enforce it. The one exception is `plugins/git/skills/clean_gone`. Renaming it changes `/git:clean_gone`, which is a MAJOR bump per `plugin-versioning.md`, so rename it only as a deliberate change of its own.
- `description`: what the skill does **and** when to use it, using the words a request would use (`Use when the user asks to "…"`). At most 1,024 characters (spec). Claude Code also truncates `description` + `when_to_use` together at 1,536 characters in the skill listing, so put the key use case first. The listing is all Claude reads before deciding to load the skill.
- Every other field, and which ones this repo uses: [component-frontmatter.md](component-frontmatter.md) §1. `skills-ref validate` reports Claude Code fields (`argument-hint`, `when_to_use`, `model`, `effort`, `allowed-tools`, `disable-model-invocation`) as non-spec, and you can ignore that.
- Body: the procedure, inputs and outputs, gates and edge cases. Claude reads all of it on every load, so keep it under 500 lines (the longest today is `respond-to-review` at about 300). Move anything needed only some of the time to `references/`.

## Which folder

| Folder        | Holds                                                   | Claude…                                                  | Use when                                                                                                                                                            |
| ------------- | ------------------------------------------------------- | -------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `SKILL.md`    | The steps for every run                                 | reads it all on load                                     | always                                                                                                                                                              |
| `references/` | Background: specs, checklists, field tables, long rules | reads it for guidance, only at the step that names it    | the content is long, or only some runs need it (`plugin-session-auditor/references/jsonl-schema.md`, `plugins/jira/references/ticket-priority.md`)                  |
| `assets/`     | Templates, lookup tables, schemas, images               | copies or fills it in; doesn't follow it as instructions | the output must match a fixed shape, or a file is passed to a tool verbatim (`plugins/respond-to-review/skills/respond-to-review/assets/`)                          |
| `scripts/`    | Executable code                                         | runs it with Bash                                        | a step is more reliable as a program than as prose: parsing, validating, counting (`plugins/caffeine/scripts/`; `plugins/review-pr/scripts/` for prebuilt binaries) |

- Start with instructions only. Add a script when a step keeps going wrong as prose.
- If a worked example already exists in the repo, link to it rather than copying it into the skill, where the copy would drift.
- Keep each file to one topic.

Folders outside the spec that are already in use, at either level:

- `agents/`: instructions for subagents. A plugin's agents (`plugins/review-pr/agents/`), or the analyzer prompts a skill passes to its own subagents (`plugin-session-auditor/agents/`).
- `tools/`: source for a helper program, either built into `scripts/` (`code-review-helper`) or run with `go run .` (`session-parser`).
- `evals/`: eval sets read by skill-creator tooling, not by Claude at run time.

## Referencing files

- Name each file in `SKILL.md` at the step that uses it ("Read `${CLAUDE_PLUGIN_ROOT}/references/prose-standards.md`") so Claude knows when to open it. Claude never opens a file that `SKILL.md` doesn't name, so delete any such file. The exception is a tooling-only file like `evals/`.
- Skill-level `references/` and `assets/`: link them with a path relative to `SKILL.md` (`[references/x.md](references/x.md)`). Use `${CLAUDE_SKILL_DIR}/<folder>/<file>` instead when the path is passed to a tool or subagent as a value, or when the skill moves the session's cwd (`respond-to-review` passes `checksPath` to a Workflow and enters a worktree).
- Skill-level `scripts/`: use `${CLAUDE_SKILL_DIR}/scripts/<file>`. In a plugin skill this placeholder is the skill's own folder, not the plugin root. Running a script is a Bash call, so if the skill sets `allowed-tools`, add `Bash(${CLAUDE_SKILL_DIR}/scripts/<file> *)`. Claude Code substitutes the placeholder there too, so the script runs without a prompt.
- Plugin-level files: use `${CLAUDE_PLUGIN_ROOT}/<folder>/<file>`. Never use a relative path that leaves the plugin, or a path into this repo. Installed users get only the plugin folder.
- Both placeholders are text substitutions Claude Code makes when the skill loads, not environment variables. They are substituted in `SKILL.md` only, not in a `references/` file Claude Reads later, so a reference file should use only values `SKILL.md` already resolved (`review-pr`'s references use `$TMP`, `$OWNER`, …, never `${CLAUDE_PLUGIN_ROOT}`).
- Never put credentials or environment URLs in a skill file. Plugin skills ship to every user who installs the plugin.
