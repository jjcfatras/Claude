# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Environment

- **Node.js**: v24.15.0 (see `.nvmrc`)
- **Package manager**: pnpm (v10.32.1)
- **Formatting**: Prettier 3.8.1 (config in `prettier.config.ts`, `prettier-plugin-sh` for shell)

## Commands

All scripts live in `package.json` and are invoked with `pnpm <script>`:

- `pnpm install` — install dependencies; also runs `pnpm prepare` automatically
- `pnpm format` — `prettier --write .` across the repo (uses `prettier-plugin-sh` for shell files)
- `pnpm format:go` — `gofmt -w` + `go mod edit -fmt` across all three Go modules (`plugins/code-review/tools/code-review-helper`, `plugins/code-review-AT/tools/code-review-helper`, `.claude/skills/plugin-session-auditor/tools/session-parser`)
- `pnpm build:go` — `make release` for **both** code-review helpers; cross-compiles prebuilts into each plugin's `bin/`. `plugins/code-review/` emits darwin/linux/windows × amd64/arm64 (Windows binaries get a `.exe` suffix); `plugins/code-review-AT/` emits darwin/linux × amd64/arm64. Does **not** build the plugin-session-auditor session-parser (no prebuilt is shipped — it runs via `go run .`)
- `pnpm check-types` — `tsc --noEmit` using the root `tsconfig.json` (also extended by `plugins/code-review-AT/tsconfig.json`)
- `pnpm test` — **stub** that prints "Error: no test specified" and exits 1. No JS/TS test suite exists; Go tests live in each helper's `make test`
- `pnpm prepare` — installs the Husky git hooks; runs automatically after `pnpm install`. The repo's `pre-commit` hook runs `pnpm exec lint-staged` per `lint-staged.config.mjs`

To build a single Go helper without the others, run `make release` (or `make test`) directly from inside `plugins/code-review-AT/tools/code-review-helper/` or `plugins/code-review/tools/code-review-helper/`.

Note: `.claude/settings.json` registers hooks that block bad edits at write time — don't fight them, fix the underlying issue:

- **Auto-format** (`PostToolUse`): `gofmt -w` for `.go`, `go mod edit -fmt` for `go.mod`, `prettier --write` for everything else. Don't run formatters manually.
- **`plugin.json` validator** (`PostToolUse`): every `plugins/*/.claude-plugin/plugin.json` must have top-level `.name` and `.version`.
- **Command frontmatter validator** (`PostToolUse`): every `plugins/*/commands/*.md` must start with `---` and include a `description:` field.
- **`go vet` on helper edits** (`PostToolUse`): edits to `plugins/code-review-AT/tools/code-review-helper/**/*.go` run `go vet ./...`; fix any reported issues.
- **Prebuilt binaries are write-locked** (`PreToolUse`): direct edits to `plugins/code-review-AT/bin/*` are blocked. Rebuild via `cd plugins/code-review-AT/tools/code-review-helper && make release`.

## Project Structure

This repo is a Claude Code **plugin marketplace** (`.claude-plugin/marketplace.json`) shipping eight plugins under `plugins/`:

- `cherry-pick`, `merge`, `test-driven-fix`, `respond-to-review`, `doc-audit`, `debate` — single slash command each
- `code-review-AT` — multi-agent review via Anthropic Agent SDK + agent teams; ships TypeScript source under `src/` (specialist agents at `src/agents/*.ts`), references, hooks, a Go helper (`tools/code-review-helper/`), and prebuilt binaries (`bin/`); builds to `dist/` via tsup
- `code-review` — same multi-specialist review but native Claude Code only (no SDK, no agent team, no cross-agent verification); ships .md agent files, references, a Go helper, prebuilt binaries, and a hook

Per-plugin layout:

```
plugins/<name>/
  .claude-plugin/plugin.json                      # plugin manifest
  commands/<command>.md                           # slash command(s); usually `<plugin-name>.md`, but `doc-audit` ships `audit-docs.md`
  agents/, references/, bin/, tools/, hooks/      # only where needed
  src/, dist/, package.json, tsconfig.json        # code-review-AT only — SDK build with tsup
```

`code-review-workspace/`, `doc-audit-workspace/`, and `plugin-session-auditor-workspace/` at the repo root are gitignored scratch dirs for the skill-creator / audit workflows — safe to ignore.

## Plugin & Command File Structure

Each plugin has a manifest at `plugins/<name>/.claude-plugin/plugin.json` (name, version, description, repository, license, keywords).

Each slash command is a markdown file in `plugins/<name>/commands/` with YAML frontmatter:

- `description` — what the command does (shown in `/` menu)
- `allowed-tools` — restricts which tools the command can invoke
- `model` — `haiku` / `sonnet` / `opus` (simple → moderate → multi-agent orchestration)
- `effort` — `low` / `high` (controls how thoroughly the model executes the command)
- `argument-hint` — usage hint (optional)
- `disable-model-invocation` — `true` = user-only trigger (optional)

Reference docs and shared rubrics live under `plugins/<name>/references/`. Use `${CLAUDE_PLUGIN_ROOT}` inside command files to resolve plugin-relative paths at runtime.

## Plugin Versioning

When a change touches anything under `plugins/<name>/` (commands, agents, references, helper sources, prebuilt binaries, the plugin manifest itself), bump the `version` field in `plugins/<name>/.claude-plugin/plugin.json` per [Semantic Versioning 2.0](https://semver.org/) — `MAJOR.MINOR.PATCH`:

- **MAJOR** — backwards-incompatible change. Examples: removing or renaming a slash command, removing a command flag/argument, changing a command's required arguments, removing or renaming an agent, breaking a published reference path that other tools consume.
- **MINOR** — backwards-compatible new functionality. Examples: adding a new slash command, adding a new agent or specialist domain, adding a new optional flag/argument to an existing command, adding a new reference doc.
- **PATCH** — backwards-compatible fix or internal-only change. Examples: bug fix in a command/agent, prompt or wording tweaks, refactoring the Go helper without changing its CLI surface, rebuilding `bin/*` prebuilts from unchanged sources, dependency-only updates, formatting/typo fixes.

Bump rules:

- Bump only the affected plugin(s). A change scoped to `plugins/code-review-AT/` does not touch `plugins/cherry-pick/.claude-plugin/plugin.json`.
- A single change picks one tier — the highest tier triggered by any part of the diff. (A breaking command rename plus a bug fix is MAJOR, not MAJOR + PATCH.)
- Bumping a higher tier resets lower tiers to `0` (1.4.7 → MINOR → 1.5.0, not 1.5.7).
- Pure non-plugin changes (root `CLAUDE.md`, `.claude/settings.json`, `.claude-plugin/marketplace.json`, repo-level docs, `code-review-workspace/`) do not require any plugin version bump.
- Include the manifest version bump in the same commit as the plugin change.
