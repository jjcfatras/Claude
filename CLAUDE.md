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

Go helper (only needed when releasing the `code-review` plugin):

- `cd plugins/code-review/tools/code-review-helper && make release` — cross-compile prebuilts for darwin/linux × amd64/arm64 into `plugins/code-review/bin/`
- `make test` — run Go tests for the helper

Note: `.claude/settings.json` registers a `PostToolUse` hook that auto-formats every file touched by Edit/Write — `gofmt -w` for `.go`, `prettier --write` for everything else. Don't run formatters manually.

## Project Structure

This repo is a Claude Code **plugin marketplace** (`.claude-plugin/marketplace.json`) shipping four plugins under `plugins/`:

- `cherry-pick`, `test-driven-fix`, `respond-to-review` — single slash command each
- `code-review` — multi-agent review; ships agents, references, a Go helper (`tools/code-review-helper/`), and prebuilt binaries (`bin/`)

Per-plugin layout:

```
plugins/<name>/
  .claude-plugin/plugin.json   # plugin manifest
  commands/<name>.md           # slash command(s)
  agents/, references/, bin/, tools/   # only where needed
```

`code-review-workspace/` at the repo root is a gitignored scratch dir for the skill-creator workflow — safe to ignore.

## Plugin & Command File Structure

Each plugin has a manifest at `plugins/<name>/.claude-plugin/plugin.json` (name, version, description, repository, license, keywords).

Each slash command is a markdown file in `plugins/<name>/commands/` with YAML frontmatter:

- `description` — what the command does (shown in `/` menu)
- `allowed-tools` — restricts which tools the command can invoke
- `model` — `haiku` / `sonnet` / `opus` (simple → moderate → multi-agent orchestration)
- `effort` — `high` for thorough analysis
- `argument-hint` — usage hint (optional)
- `disable-model-invocation` — `true` = user-only trigger (optional)

Reference docs and shared rubrics live under `plugins/<name>/references/`. Use `${CLAUDE_PLUGIN_ROOT}` inside command files to resolve plugin-relative paths at runtime.
