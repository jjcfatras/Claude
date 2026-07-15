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
- `pnpm format:go` — `gofmt -w` + `go mod edit -fmt` across both Go modules (`plugins/code-review/tools/code-review-helper`, `.claude/skills/plugin-session-auditor/tools/session-parser`)
- `pnpm build:go` — `make release` for the `code-review` helper; cross-compiles prebuilts into `plugins/code-review/bin/` (darwin/linux/windows × amd64/arm64; Windows binaries get a `.exe` suffix). Does **not** build the plugin-session-auditor session-parser (no prebuilt is shipped — it runs via `go run .`)
- `pnpm check-types` — `tsc --noEmit` using the root `tsconfig.json`
- `pnpm test` — runs the `code-review` Go test suite via `make -C plugins/code-review/tools/code-review-helper test` (`go test ./...`). No JS/TS test suite exists; the other Go modules' tests run via their own `make test`. The helper's e2e goldens (`testdata/golden/`) are byte-compared and excluded from Prettier — never hand-edit or format them; regenerate with `go test ./cmd/helper -update` from the helper dir after an intentional behavior change
- `pnpm prepare` — installs the Husky git hooks; runs automatically after `pnpm install`. The repo's `pre-commit` hook runs `pnpm exec lint-staged` per `lint-staged.config.mjs`

To build the Go helper, run `make release` (or `make test`) directly from inside `plugins/code-review/tools/code-review-helper/`.

Note: `.claude/settings.json` registers hooks that block bad edits at write time — don't fight them, fix the underlying issue:

- **Auto-format** (`PostToolUse`): `gofmt -w` for `.go`, `go mod edit -fmt` for `go.mod`, `prettier --write` for everything else. Don't run formatters manually.
- **`plugin.json` validator** (`PostToolUse`): every `plugins/*/.claude-plugin/plugin.json` must have top-level `.name` and `.version`.
- **Command frontmatter validator** (`PostToolUse`): every `plugins/*/commands/*.md` must start with `---` and include a `description:` field.
- **Version-sync validator** (`PostToolUse`): a plugin's `plugin.json` `version` must match its entry in `.claude-plugin/marketplace.json`; editing either file triggers the check.

## Project Structure

This repo is a Claude Code **plugin marketplace** (`.claude-plugin/marketplace.json`) shipping eleven plugins under `plugins/`:

- `test-driven-fix`, `respond-to-review`, `debate` — single slash command each; `transcript` ships a single skill (`skills/transcript/SKILL.md`, model-invocable)
- `simplify` — two skills: `simplify-code` (behavior-preserving code simplifications) and `simplify-prose` (lossless prose distillation), both model-invocable
- `docs` — documentation skills (`audit-docs`, `enrich-claude-md`), both model-invocable
- `git` — git workflow skills (`commit`, `commit-push`, `commit-push-pr`, `clean_gone`, `cherry-pick`, `merge`), all model-invocable
- `jira` — JIRA workflow commands (`create-ticket`, `implement-ticket`, `create-tests`)
- `tool-discipline`, `tool-discipline-lsp` — **hook-only** plugins (no slash command); each ships just `hooks/hooks.json` + hook scripts. `tool-discipline` bundles three `PreToolUse` guardrails: two durable (no-cd-chaining, prefer-builtin-tools) plus a conditional one that redirects ToolSearch loads of Grep/Glob — removed by design on native builds since CC 2.1.117 in favor of embedded ripgrep/ugrep/bfs exposed through Bash — to those embedded engines, self-disabling on builds that still ship the tools (#52121/#61845 track the ToolSearch catalog gap); `tool-discipline-lsp` adds the prefer-LSP `PreToolUse` guardrail plus a `PostToolUse` advisory that nudges a retry/pivot when `workspaceSymbol` returns empty
- `code-review` — multi-specialist review using parallel native Claude Code subagents (no SDK, no agent team, no cross-agent verification); ships .md agent files, references, a Go helper, and prebuilt binaries

Besides the plugins, one repo-local skill ships at `.claude/skills/plugin-session-auditor/` — session-transcript audit: `SKILL.md`, four analyzer agents, evals, and the `session-parser` Go module (run via `go run .`).

Per-plugin layout:

```
plugins/<name>/
  .claude-plugin/plugin.json                      # plugin manifest
  commands/<command>.md                           # slash command(s); usually `<plugin-name>.md`, but `jira` ships `create-ticket.md`/`implement-ticket.md`/`create-tests.md`
  skills/<name>/SKILL.md                          # skill(s); `transcript` ships one, `docs` ships two (`audit-docs`, `enrich-claude-md`), `git` ships six, and `simplify` ships two (`simplify-code`, `simplify-prose`) instead of commands
  agents/, references/, bin/, tools/, hooks/      # only where needed
```

Root-level `*-workspace/` dirs (e.g. `code-review-workspace/`, `docs-workspace/`, `jira-ticket-workspace/`, `debate-workspace/`, `plugin-session-auditor-workspace/`) are gitignored scratch space for the skill-creator / audit / command workflows — safe to ignore. Each is listed individually in `.gitignore` (no `*-workspace/` glob — add new workspace dirs there explicitly).

## Plugin & Command File Structure

Each plugin has a manifest at `plugins/<name>/.claude-plugin/plugin.json` (name, version, description, repository, license, keywords). A plugin may declare `dependencies` on plugins from another marketplace (e.g. `jira` depends on `context7` and `superpowers` from `claude-plugins-official`); the root `marketplace.json` must allowlist that marketplace in `allowCrossMarketplaceDependenciesOn`.

Each slash command is a markdown file in `plugins/<name>/commands/` with YAML frontmatter (`description`, `allowed-tools`, `model`, `effort`, `argument-hint`, `disable-model-invocation`). See `.claude/rules/component-frontmatter.md` §1 (skills & slash commands share one frontmatter set) for the full field reference and the `model`/`effort` tier semantics.

Reference docs and shared rubrics live under `plugins/<name>/references/`. Use `${CLAUDE_PLUGIN_ROOT}` inside command files to resolve plugin-relative paths at runtime.
