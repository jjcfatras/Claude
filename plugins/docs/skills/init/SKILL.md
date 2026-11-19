---
name: init
description: Set up a minimal CLAUDE.md (and optionally CLAUDE.local.md, skills and hooks) for the current repo through a guided flow - check for an existing file, ask what to set up, survey the codebase with a subagent, interview the user about what code can't answer, propose everything for approval, then write it. Works on any install, independent of the bundled /init. Use when the user asks to "init CLAUDE.md", "set up CLAUDE.md for this repo", "bootstrap project memory", or "initialize Claude for this project".
disable-model-invocation: true
allowed-tools: Bash(git:*), Bash(rg:*), Bash(find:*), Bash(ls:*), Bash(which:*), Bash(jq:*), Read, Write, Edit, Agent, AskUserQuestion, Skill
model: opus
effort: medium
---

Set up a minimal CLAUDE.md for this repo, and optionally CLAUDE.local.md, skills and hooks. CLAUDE.md loads into every session, so it must be concise: include only what Claude would get wrong without it. Every line must pass "would removing this cause a mistake?"; if not, cut it.

Nothing is written until the user approves the proposal in Phase 3 (or the diffs in Phase 4 on the improve path).

## Phase 0: Check for an existing CLAUDE.md

Read `./CLAUDE.md` at the project root. Only the root file counts; don't explore the tree yet. The result branches Phase 1.

## Phase 1: Ask what to set up

First print this primer as plain text:

> Quick context:
>
> - **CLAUDE.md** files give Claude persistent instructions for a project, your personal workflow, or your organization. Claude reads them at the start of every session.
> - **Skills** are packaged instructions Claude invokes automatically when a task matches, or that you trigger with a slash command.
> - **Hooks** run shell commands on lifecycle events (format after edits, notify when blocked). They are deterministic; Claude can't skip them.

Then call AskUserQuestion with **only Q1**. Ask Q2 afterwards, and only when the Q1 answer calls for it.

**CLAUDE.md exists** — ask "I found an existing CLAUDE.md. What would you like to do?":

| Option                        | Description                                                           | Routing                                                                                                                                                                                                                                                                                                 |
| ----------------------------- | --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Review and improve it         | Explore what changed and propose targeted edits to the existing file. | Skip Q1/Q2. Phase 2, then the single Phase 3-lite question, then Phase 4's diff proposal, then Phase 8.                                                                                                                                                                                                 |
| Leave it, set up other things | Skip CLAUDE.md. Go straight to skills and hooks.                      | Skip Q1, ask Q2 with its fourth option renamed "Neither — skip setup". If chosen, jump to Phase 8 with "Nothing to set up — your CLAUDE.md is unchanged." Otherwise Phase 2, Phase 3 proposal (no interview), Phases 6/7 per queue, Phase 8. Hook target defaults to project (`.claude/settings.json`). |
| Start fresh (replace it)      | Discard it and write new file(s).                                     | Continue to Q1 as if no file existed.                                                                                                                                                                                                                                                                   |

**No CLAUDE.md** (or "Start fresh") — ask Q1, "Which CLAUDE.md files should init set up?":

- **Project CLAUDE.md** — team-shared instructions checked into source control: architecture, coding standards, common workflows.
- **Personal CLAUDE.local.md** — your private preferences for this project (gitignored): role, sandbox URLs, test data, workflow quirks.
- **Both project + personal**
- **Let Claude decide** — fastest path: project CLAUDE.md plus whatever skills or hooks fit. No Q2; everything is approved before it's written.

Q2, "Also set up skills and hooks?": **Skills + hooks** | **Skills only** | **Hooks only** | **Neither, just CLAUDE.md**. Q2 is a hint, not a filter: Phase 3 proposes what fits the codebase and notes any deviation.

## Phase 2: Explore the codebase

Launch an `Explore` subagent to survey the repo. Have it read: manifests (`package.json`, `Cargo.toml`, `pyproject.toml`, `go.mod`, `pom.xml`, …), README, Makefile and build configs, CI config, an existing CLAUDE.md, `.claude/rules/`, `AGENTS.md`, `.cursor/rules` or `.cursorrules`, `.github/copilot-instructions.md`, `.devin/rules/`, `.windsurf/rules/` or `.windsurfrules`, `.clinerules`, `.mcp.json`.

Also a cheap presence check (existence only, no reads) for OpenAI Codex config (`~/.codex/config.toml` or `./.codex/`) and Gemini CLI config (`~/.gemini/settings.json`, `./.gemini/`, or a root `GEMINI.md`). Phase 8 uses the result.

Detect:

- Build, test and lint commands, especially non-standard ones
- Languages, frameworks, package manager
- Project shape: monorepo with workspaces, multi-module, or single project
- Code style rules that differ from language defaults
- Non-obvious gotchas, required env vars, workflow quirks
- Existing `.claude/skills/` and `.claude/rules/`
- Formatter config (prettier, biome, ruff, black, gofmt, rustfmt, or a unified `format`/`fmt` script)
- Worktree usage via `git worktree list` (only matters if the user wants CLAUDE.local.md)

Note what could NOT be determined from code alone; those become interview questions.

## Phase 3: Fill in the gaps

Use AskUserQuestion for what the code can't answer. Never mark an option "recommended" here: this is about how the team or user works, not best practice.

- **Project, both, or "Let Claude decide"**: ask about codebase practices: non-obvious commands, gotchas, branch/PR conventions, required env setup, testing quirks. Skip anything in the README or manifests.
- **Personal or both**: ask about the user, not the codebase: role, familiarity with the codebase and languages (to calibrate explanation depth), personal sandbox URLs, test accounts or local setup, communication preferences. Only if Phase 2 found multiple worktrees, ask whether they are nested inside the main repo (the upward file walk finds its CLAUDE.local.md; no special handling) or siblings/external (put the content in `~/.claude/<project-name>-instructions.md` and make each worktree's CLAUDE.local.md a one-line `@~/.claude/<project-name>-instructions.md` stub; never put this import in the project CLAUDE.md).
- **"Review and improve"**: ask one question, "Has anything changed about how the team works since this CLAUDE.md was written (new conventions, commands, gotchas)?" with "No, nothing's changed" | "Yes — let me describe". On Yes, ask what changed (free text), then skip to Phase 4.

**Synthesize a proposal.** Pick the artifact that fits each item:

- **Hook**: deterministic, fast, per-edit shell command (format, lint a changed file).
- **Skill**: on-demand multi-step workflow (`/verify`, `/deploy-staging`).
- **CLAUDE.md note**: guidance that shapes behavior but isn't enforced.

List the CLAUDE.md file(s) implied by Q1 first, each with a one-line summary of coverage; then skills, hooks and notes. On the "Leave it" path omit file and note bullets. On "Start fresh" with personal-only, add a bullet saying the existing project CLAUDE.md is left untouched. If the proposal deviates from the Q2 hint, say so in one line at the top.

Print the proposal as plain text, one bullet per item (`**[file|hook|skill|note]** — description`), then call AskUserQuestion "Does this look right?" with "Looks good — proceed" | "Drop the hook" | "Drop the skill" (no `preview`; "Other" is added automatically).

**Build the preference queue** from the accepted proposal: entries of `{type: hook|skill|note, description, target file, Phase 2 details such as the real test/format command}`. Phase 6 and Phase 7's hook step consume it; Phases 4 and 5 gate on the approved file bullets.

## Phase 4: Write CLAUDE.md

Applies when the approved proposal includes it, or on the improve path.

Write a minimal root CLAUDE.md, prefixed with:

```
# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.
```

**Improve path**: don't write fresh. Read the existing file, compare it with Phase 2 findings and the Phase 3-lite answer, and propose additions and removals as diffs with a one-line reason each. The existing file is the baseline; catch what is missing, outdated or bloated. Before writing, call AskUserQuestion "Apply these edits?" with "Apply all" | "Let me pick which" | "Skip — leave it as is".

Add each queued `note` targeted at CLAUDE.md as a concise line in the most relevant section. Leave personal-targeted notes for Phase 5.

**Include**:

- Build/test/lint commands Claude can't guess (non-standard scripts, flags, sequences)
- Code style rules that differ from language defaults
- Testing instructions and quirks (e.g. running a single test)
- Repo etiquette: branch naming, PR conventions, commit style
- Required env vars or setup steps
- Non-obvious gotchas and architectural decisions
- Important parts of existing AI tool configs (AGENTS.md, `.cursor/rules`, `.cursorrules`, `.github/copilot-instructions.md`, `.devin/rules/`, `.windsurf/rules/`, `.windsurfrules`, `.clinerules`)

**Exclude**:

- File-by-file structure or component lists (discoverable by reading)
- Standard language conventions
- Generic advice ("write clean code")
- Long API docs or references: use `@path/to/import` instead
- Frequently changing information: reference the source with `@path/to/import`
- Long tutorials: move to a separate file or a skill
- Commands obvious from manifests (`npm test`, `cargo test`, `pytest`)

Be specific ("Use 2-space indentation in TypeScript", not "Format code properly"). Don't repeat yourself, and don't invent sections like "Common Development Tasks" or "Tips"; include only what you found in files you read.

For repos with multiple concerns, suggest splitting into `.claude/rules/` files (`code-style.md`, `testing.md`, …), which load automatically and can be scoped with `paths` frontmatter. For monorepos and multi-module projects, mention that subdirectory CLAUDE.md files load when Claude works in those directories, and offer to create them.

## Phase 5: Write CLAUDE.local.md

Applies when the approved proposal includes it.

Write a minimal CLAUDE.local.md at the project root and add `CLAUDE.local.md` to `.gitignore`. Add each queued `note` targeted at it; with a personal-only choice, that is its sole source. Include only what makes Claude's answers noticeably better for this user: role and codebase familiarity, personal sandbox URLs or setup, communication preferences.

Sibling/external worktrees: write the content to `~/.claude/<project-name>-instructions.md` and make CLAUDE.local.md the one-line import stub from Phase 3.

If CLAUDE.local.md already exists, read it, propose specific additions, and never silently overwrite.

## Phase 6: Create skills

Applies when the approved proposal includes any.

Turn each queued `skill` entry into a SKILL.md at `.claude/skills/<skill-name>/SKILL.md`, named from the preference and written in the user's own words plus Phase 2 details (real test command, report format, deploy target). If it maps to a bundled skill (e.g. `/verify`), write a project skill that adds the user's constraints on top and tell them the bundled one still exists. Ask a quick follow-up if a preference is underspecified.

Then suggest extra skills beyond the queue only where the repo shows reference knowledge for a subsystem or a repeatable workflow (deploy, release, verify). For each give name, one-line purpose, and why it fits.

If `.claude/skills/` already has skills, read them first, never overwrite, and propose only complementary ones.

```yaml
---
name: <skill-name>
description: <what the skill does and when to use it>
---
<Instructions for Claude>
```

Both user and Claude can invoke a skill by default. For workflows with side effects (`/deploy`, `/fix-issue 123`) add `disable-model-invocation: true` and accept input through `$ARGUMENTS`.

## Phase 7: Additional optimizations

Say you're moving on to a few optimizations, then check each gap and ask with AskUserQuestion. Act on each "yes" before moving on.

- **GitHub CLI**: run `which gh`. If missing AND `git remote -v` shows github.com, offer to install it (commits, PRs, issues and review from Claude).
- **Linting**: if Phase 2 found no lint config for the project's language, offer to set it up (fast feedback on Claude's own edits).
- **Hooks**: consume queued `hook` entries; if Phase 2 found a formatter and the queue has no formatting hook, offer format-on-edit as a fallback. For each:
  1. **Target file**: project CLAUDE.md choice → `.claude/settings.json` (committed); personal → `.claude/settings.local.json`. Ask once for all hooks, only when the choice was "both" or the preference is ambiguous.
  2. **Event and matcher**: "after every edit" → `PostToolUse`, matcher `Write|Edit`. "When Claude finishes" → `Stop` (fires every turn, including read-only ones). "Before running bash" → `PreToolUse`, matcher `Bash`. "Before committing" is not a settings hook, since matchers can't filter Bash by command content: offer a git pre-commit hook (`.git/hooks/pre-commit`, husky, pre-commit framework) instead, unless the user means "before I review", which is `Stop`. Probe when ambiguous.
  3. **Build the hook**: invoke the `update-config` skill once per run, with args starting `[hooks-only]` and a one-line summary (e.g. `[hooks-only] PostToolUse/Write|Edit format hook for .claude/settings.json using ruff`), then follow its "Constructing a Hook" flow: dedup check, construct for this project, pipe-test the raw command, wrap, write JSON, `jq -e` validate, live-proof on triggerable matchers, clean up. If that skill is not available, do the same steps by hand: pipe-test the command, merge the entry into the settings file's `hooks` key without clobbering existing hooks, and validate with `jq -e`.

## Phase 8: Summary and next steps

Recap which files were written and the key points in each. Remind the user these are a starting point to review and tweak, and that `/docs:init` can be re-run anytime to re-scan.

Then present one to-do list, most impactful first, with only items that apply:

- Codex or Gemini config found in Phase 2: offer `/import` (lists importable MCP servers, commands, subagents, skills, instructions), then `/import --yes=<digest>` using the digest the scan prints. If `/import` isn't available on this surface, say `claude import` from a terminal does the same. Don't read those foreign config files or write Claude Code config yourself. List this first.
- Frontend code detected: `/plugin install frontend-design@claude-plugins-official` (design principles) and `/plugin install playwright@claude-plugins-official` (real-browser screenshots and visual fixes).
- Phase 7 gaps the user declined (GitHub CLI, linting): one line on why each helps.
- Missing or sparse tests: suggest a test framework so Claude can verify its own changes.
- Always: `/plugin install skill-creator@claude-plugins-official`, then `/skill-creator <skill-name>` to create and eval skills.
- Always: browse official plugins with `/plugin`; custom plugins can be built and shared.
