# CLAUDE.md

Guidance for Claude Code (claude.ai/code) when working with code in this repository.

## Development Environment

- **Node.js**: v24.22.2 (see `.nvmrc`)
- **Package manager**: pnpm (v12.16.1)
- **Formatting**: Prettier 3.9.10 (config in `prettier.config.ts`, `prettier-plugin-sh` for shell)

## Commands

All scripts live in `package.json` and are invoked with `pnpm <script>`:

- `pnpm install` — install dependencies; also runs `pnpm prepare` automatically
- `pnpm format` — `prettier --write .` across the repo (uses `prettier-plugin-sh` for shell files)
- `pnpm format:go` — `gofmt -w` + `go mod edit -fmt` across both Go modules (`plugins/review-pr/tools/code-review-helper`, `.claude/skills/plugin-session-auditor/tools/session-parser`)
- `pnpm lint:go` — `go vet ./...` across both Go modules (same module list as `format:go`); stdlib only, no external linter
- `pnpm build:go` — `make release` for the `review-pr` helper; cross-compiles prebuilts into `plugins/review-pr/scripts/` (darwin/linux/windows × amd64/arm64; Windows binaries get a `.exe` suffix). Does **not** build the plugin-session-auditor session-parser (no prebuilt is shipped — it runs via `go run .`)
- `pnpm check-types` — `tsc --noEmit` using the root `tsconfig.json`
- `pnpm validate:plugins` — Anthropic's `claude plugin validate --strict` on the marketplace and every `plugins/*/`; needs the `claude` CLI, not wired into pre-commit. Checks more than the PostToolUse hooks below (all component frontmatter, path containment)
- `pnpm test` — runs the `review-pr` Go test suite via `make -C plugins/review-pr/tools/code-review-helper test` (`go test ./...`). No JS/TS test suite exists; the session-parser Go module ships no tests. The helper's e2e goldens (`testdata/golden/`) are byte-compared and excluded from Prettier — never hand-edit or format them; regenerate with `go test ./cmd/helper -update` from the helper dir after an intentional behavior change. They cover the `diff` and `finalize` outputs only — the bundle (`spawn-context.md`) and spawn prompt (`spawn-manifest.json`) have no golden, so changes there pass silently and need their own unit tests. `spawn-manifest_test.go` also reads the eight `agents/*.md` files and asserts substrings in their `## Environment` block, plus each one's own `$REVIEW_TMPDIR/findings/<role>.json` path (the spawn prompt no longer carries it) — rewording an agent can fail `pnpm test`
- `pnpm prepare` — installs the Husky git hooks; runs automatically after `pnpm install`. The repo's `pre-commit` hook runs `pnpm exec lint-staged` per `lint-staged.config.mjs`

To build the Go helper, run `make release` (or `make test`) directly from inside `plugins/review-pr/tools/code-review-helper/`.

`claude plugin list` shows what is actually installed (name, version, scope, status). Shipping in `.claude-plugin/marketplace.json` does **not** mean a plugin is enabled — `tool-discipline` sat in the marketplace uninstalled while repo docs described its hooks as actively enforcing.

Note: `.claude/settings.json` registers hooks that block bad edits at write time — don't fight them, fix the underlying issue:

- **Auto-format** (`PostToolUse`): `gofmt -w` for `.go`, `go mod edit -fmt` for `go.mod`, `prettier --write` for everything else. Don't run formatters manually.
- **`plugin.json` validator** (`PostToolUse`): every `plugins/*/.claude-plugin/plugin.json` must have top-level `.name` and `.version`.
- **Frontmatter validator** (`PostToolUse`): every `plugins/*/commands/*.md`, `plugins/*/skills/*/SKILL.md`, and `.claude/skills/*/SKILL.md` must start with `---` and include a `description:` field.
- **Version-sync validator** (`PostToolUse`): a plugin's `plugin.json` `version` must match its entry in `.claude-plugin/marketplace.json`; editing either file triggers the check.

All four match on `Edit|Write` only — a scripted multi-file edit (`python3` heredoc, shell redirect) bypasses every one of them. After one, run `pnpm format` / `pnpm format:go` and re-check manifests yourself.

## Project Structure

This repo is a Claude Code **plugin marketplace** (`.claude-plugin/marketplace.json`) shipping nine plugins under `plugins/`:

- `debate` — single slash command; `transcript` and `respond-to-review` ship a single model-invocable skill each (`skills/<name>/SKILL.md`). `respond-to-review` keeps its triage procedure in the skill's `references/` and its Workflow script, agent preamble and output templates in `assets/`
- `simplify` — two skills: `simplify-code` (behavior-preserving code simplifications) and `simplify-prose` (lossless prose distillation), both model-invocable
- `docs` — documentation skills (`audit-docs`, `enrich-claude-md`), both model-invocable
- `git` — git workflow skills (`commit`, `commit-push`, `commit-push-pr`, `push`, `pr`, `clean_gone`, `cherry-pick`, `merge`), all model-invocable
- `jira` — JIRA workflow skills (`create-ticket`, `implement-ticket`, `create-tests`), all user-only (`disable-model-invocation: true`)
- `caffeine` — hooks only, no commands or skills. It keeps macOS awake while a session works, via the built-in `caffeinate -i`, and replaces `cc-caffeine@samber`, whose synchronous `npx` hooks cost ~3 s per tool call.
  - The `on` hooks are async, and per-session state lives in `${CLAUDE_PLUGIN_DATA}`.
  - `caffeinate -i <cmd>` keeps `<cmd>` in the original pid and asserts from a forked child. So `$!` and the pid file name the heartbeat loop, not caffeinate; killing the loop releases the assertion.
- `review-pr` — multi-specialist review using parallel native Claude Code subagents (no SDK, no agent team, no cross-agent verification); ships the orchestrator as a user-only skill (`skills/review-pr/SKILL.md`, `disable-model-invocation: true`; invoked as `/review-pr`; the plugin and skill were both `code-review` before 3.0.0, renamed because the bundled `/code-review` owns the bare name, and `marketplace.json` `renames` migrates old installs), plus .md agent files, references, a Go helper, and prebuilt binaries in `scripts/` (not a top-level `bin/`, which claude.ai and Cowork refuse to install)
  - The eight `agents/*.md` duplicate their `## Environment` block and source-fetching paragraph near-verbatim, so shared guidance must be edited in all eight. The `## Environment` block (three bullets) is byte-identical in seven; only `claude-md` adds a clause, exempting CLAUDE.md and `.claude/rules/` files from the never-read-a-live-path rule. The oversized-Read retry line just above the block is identical in all eight. The source-fetching paragraph has five variants that differ only in the trailing `git grep` pathspec (`'*.ts'`, `'*.tsx'`, or none) and an optional "never `find | xargs grep`" clause (`infra`, `security`); `security` alone follows it with a one-line never-Read-from-cwd paragraph. Check the outliers individually rather than blind-replacing.

Component prose says `rg` (or `git grep`), never the `Grep`/`Glob` tools — those are removed on native macOS/Linux builds (CC 2.1.117+; upstream #52121 open). `review-pr` specialists use `git grep <symbol> <HEAD_SHA>` specifically: they must read at `HEAD_SHA`, not the working tree.

Besides the plugins, one repo-local skill ships at `.claude/skills/plugin-session-auditor/` — session-transcript audit: `SKILL.md`, four analyzer agents, evals, and the `session-parser` Go module (run via `go run .`).

Per-plugin layout:

```
plugins/<name>/
  .claude-plugin/plugin.json                      # plugin manifest
  commands/<command>.md                           # single slash command, named `<plugin-name>.md`
  skills/<name>/SKILL.md                          # skill(s); `transcript` and `respond-to-review` ship one each, `docs` ships two (`audit-docs`, `enrich-claude-md`), `git` ships eight, `simplify` ships two (`simplify-code`, `simplify-prose`), `jira` ships three (`create-ticket`, `implement-ticket`, `create-tests`), and `review-pr` ships one (`review-pr`) instead of commands
  agents/, references/, bin/, tools/, hooks/, scripts/  # only where needed
```

Plugin layout against Anthropic's standard layout (what each folder means, manifest rules, `${CLAUDE_PLUGIN_ROOT}`/`${CLAUDE_PLUGIN_DATA}`, accepted deviations): see `.claude/rules/plugin-structure.md`. Skill folder layout (skill-level vs plugin-level files, `references/`/`assets/`/`scripts/`, `${CLAUDE_SKILL_DIR}` vs `${CLAUDE_PLUGIN_ROOT}`): see `.claude/rules/skill-structure.md`.

Root-level `*-workspace/` dirs (e.g. `code-review-workspace/`, `docs-workspace/`, `jira-ticket-workspace/`, `debate-workspace/`, `plugin-session-auditor-workspace/`) are gitignored scratch space for the skill-creator / audit / command workflows — safe to ignore. Each is listed individually in `.gitignore` (no `*-workspace/` glob — add new workspace dirs there explicitly).

## Plugin & Command File Structure

Each plugin has a manifest at `plugins/<name>/.claude-plugin/plugin.json` (name, version, description, repository, license, keywords). A plugin may declare `dependencies` on plugins from another marketplace (e.g. `jira` depends on `context7` and `superpowers` from `claude-plugins-official`); the root `marketplace.json` must allowlist that marketplace in `allowCrossMarketplaceDependenciesOn`.

Each slash command is a markdown file in `plugins/<name>/commands/` with YAML frontmatter (`description`, `allowed-tools`, `model`, `effort`, `argument-hint`, `disable-model-invocation`). See `.claude/rules/component-frontmatter.md` §1 (skills & slash commands share one frontmatter set) for the full field reference and the `model`/`effort` tier semantics.

Reference docs and shared rubrics live under `plugins/<name>/references/`. Use `${CLAUDE_PLUGIN_ROOT}` inside command files to resolve plugin-relative paths at runtime.
