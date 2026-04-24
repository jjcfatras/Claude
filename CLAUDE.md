# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Environment

- **Node.js**: v22.21.1 (see `.nvmrc`)
- **Package manager**: pnpm (v10.32.1)
- **Formatting**: Prettier 3.8.1 (default config, no `.prettierrc`)

## Commands

- `pnpm install` — install dependencies
- `pnpm prettier --write .` — format all files
- `pnpm prettier --check .` — check formatting without writing

Note: `.claude/settings.json` registers a `PostToolUse` hook that runs `prettier --write` on every file touched by Edit/Write. Don't run the formatter manually after editing — it's already done.

## Project Structure

This is a Claude Code skills/customization project. The `.claude/commands/` directory contains custom skill definitions (markdown files with frontmatter) that extend Claude Code's capabilities — e.g., `code-review.md` defines a structured PR review workflow. Shared reference docs live under `.claude/references/`.

## Skill File Structure

Each skill in `.claude/commands/` is a markdown file with YAML frontmatter:

- `description` — what the skill does (shown in `/` menu)
- `allowed-tools` — restricts which tools the skill can invoke
- `model` — which Claude model to use: `haiku` for simple tasks, `sonnet` for moderate complexity, `opus` for complex multi-agent orchestration
- `effort` — set to `high` for thorough analysis
- `argument-hint` — usage hint shown to the user (optional)
- `disable-model-invocation` — if `true`, only the user can trigger it (optional)

## Shell Command Safety

Skills issue `gh`, `jq`, and other shell commands under a strict permission system. When writing or editing skills, follow the rules in @.claude/references/shell-safety.md — they explain why patterns like heredocs, `$()`, `>>`, and `jq -f` are rejected and how to rewrite them.
