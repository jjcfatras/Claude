# CLAUDE.md

Guidance for Claude Code (claude.ai/code) when working with code in this repository.

## Development Environment

- **Toolchain**: Go only — no Node/pnpm. Tasks live in the root `Makefile`.
- **Formatting**: `scripts/format.sh` routes files by type — `gofmt` (`.go`), `go mod edit -fmt` (`go.mod`), `shfmt -i 2 -bn -ci -sr` (`.sh` + `#!…sh` shebangs), `yamlfmt` (`.yml`/`.yaml`, `retain_line_breaks_single`), `tools/jsonfmt` (`.json`; stdlib `json.Indent`, 2-space, whitespace-only), and go-prettier (`.md`/`.js`; real Prettier in wazero, default options). Native Go formatters were tried for Markdown and all broke YAML frontmatter or list nesting. go-prettier runs via `go run …@<version>` (pinned in the script) because it doesn't support `go tool`. The script skips `plugins/pr-review/tools/code-review-helper/testdata/`.
- **Root Go module**: pins lefthook v2.2.2, shfmt, and yamlfmt as `go tool`s, and holds the one package `tools/jsonfmt`. Make targets use `./tools/...`, never `./...` — root `./...` also walks gitignored `*-workspace/` scratch packages.

## Commands

All tasks live in the root `Makefile` and are invoked with `make <target>`:

- `make format` — runs `scripts/format.sh` over every tracked file (Go files included)
- `make lint-go` — `go vet` on `./tools/...` plus both Go modules (`plugins/pr-review/tools/code-review-helper`, `.claude/skills/plugin-session-auditor/tools/session-parser`); stdlib only, no external linter
- `make build-go` — `make release` for the `pr-review` helper; cross-compiles prebuilts into `plugins/pr-review/skills/pr-review/scripts/` (darwin/linux/windows × amd64/arm64; Windows binaries get a `.exe` suffix). Does **not** build the plugin-session-auditor session-parser (no prebuilt is shipped — it runs via `go run .`)
- `make validate-plugins` — Anthropic's `claude plugin validate --strict` on the marketplace and every `plugins/*/`; needs the `claude` CLI, not wired into pre-commit. Checks more than the PostToolUse hooks below (all component frontmatter, path containment)
- `make test` — `go test ./tools/...` plus the `pr-review` Go test suite via `make -C plugins/pr-review/tools/code-review-helper test` (`go test ./...`). The session-parser Go module ships no tests. The helper's e2e goldens (`testdata/golden/`) are byte-compared and skipped by `scripts/format.sh` — never hand-edit or format them; regenerate with `go test ./cmd/helper -update` from the helper dir after an intentional behavior change. They cover the `diff` and `finalize` outputs only — the bundle (`spawn-context.md`) and spawn prompt (`spawn-manifest.json`) have no golden, so changes there pass silently and need their own unit tests. `spawn-manifest_test.go` also reads the eight `agents/*.md` files and asserts substrings in their `## Environment` block, plus each one's own `$REVIEW_TMPDIR/findings/<role>.json` path (the spawn prompt no longer carries it) — rewording an agent can fail `make test`
- `make hooks` — `go tool lefthook install`; installs the git hooks defined in `lefthook.yml`. Run it once after cloning, and again after editing hook names; nothing installs them automatically. `assert_lefthook_installed: true` makes lefthook fail loudly when they're missing. The `pre-commit` hook runs `scripts/format.sh` on staged files, then re-stages them, then runs `make lint-go` and `make test`. A clone that predates lefthook needs `git config --unset core.hooksPath` first (husky had set it to `.husky/_`)

To build the Go helper, run `make release` (or `make test`) directly from inside `plugins/pr-review/tools/code-review-helper/`.

`claude plugin list` shows what is actually installed (name, version, scope, status). Shipping in `.claude-plugin/marketplace.json` does **not** mean a plugin is enabled — `tool-discipline` sat in the marketplace uninstalled while repo docs described its hooks as actively enforcing.

Note: `.claude/settings.json` registers hooks that block bad edits at write time — don't fight them, fix the underlying issue:

- **Auto-format** (`PostToolUse`): `.claude/hooks/format.sh` hands the edited file to `scripts/format.sh`. Don't run formatters manually.
- **`plugin.json` validator** (`PostToolUse`): every `plugins/*/.claude-plugin/plugin.json` must have top-level `.name` and `.version`, and a matching entry in `.claude-plugin/marketplace.json`. `version` lives only in `plugin.json`; marketplace entries carry none.
- **Frontmatter validator** (`PostToolUse`): every `plugins/*/commands/*.md`, `plugins/*/skills/*/SKILL.md`, and `.claude/skills/*/SKILL.md` must start with `---` and include a `description:` field.

All three match on `Edit|Write` only — a scripted multi-file edit (`python3` heredoc, shell redirect) bypasses every one of them. After one, run `make format` and re-check manifests yourself.

## Project Structure

This repo is a Claude Code **plugin marketplace** (`.claude-plugin/marketplace.json`) shipping eleven plugins under `plugins/`:

- `debate` — single slash command; `transcript` and `pr-respond` ship a single model-invocable skill each (`skills/<name>/SKILL.md`). `transcript` keeps its lookup script in the skill's `scripts/`; `pr-respond` keeps its triage procedure in the skill's `references/`, its agent preamble and output templates in `assets/`, and its Workflow script at the plugin root in `workflows/triage.js` (`/pr-respond:triage`)
- `simplify` — two skills: `simplify-code` (behavior-preserving code simplifications) and `simplify-prose` (lossless prose distillation), both model-invocable
- `docs` — documentation skills (`audit-docs`, `enrich-claude-md`, `revise`, `init`); the first two are model-invocable, `revise` and `init` are user-only (`init` ports the native `/init` flow and does not depend on the bundled one)
- `git` — git workflow skills (`commit`, `commit-push`, `commit-push-pr`, `push`, `pr`, `clean_gone`, `cherry-pick`, `merge`, `rebase`), all model-invocable. Shared files sit at the plugin root: `references/conflict-resolution.md` (the conflict core `cherry-pick`, `merge` and `rebase` Read at their Step 4) and `scripts/default-branch.sh` (injected into the `## Context` of `push`, `pr`, `commit-push` and `commit-push-pr`). `clean_gone` runs its own `scripts/clean-gone.sh`
- `jira` — JIRA workflow skills (`create-ticket`, `implement-ticket`, `create-tests`), all user-only (`disable-model-invocation: true`). Shared references sit at the plugin root in `references/`; `implement-ticket` runs two Workflow scripts shipped at the plugin root in `workflows/` (`/jira:implement-ticket-verify-claims`, `/jira:implement-ticket-audit-coverage`)
- `deep` — one user-only skill, `research` (`/deep:research`), a reimplementation of the bundled `/deep-research` Workflow that adds library-docs (Context7) and read-only codebase angles and pins a model per phase. Its Workflow script lives at the plugin root in `workflows/research-pipeline.js` (`/deep:research-pipeline`) and starts with an assert-based self-check (URL dedup, host labels, untrusted-text stripping, claim-id citations, the verify tie-break, local-path validation). The script runs every agent as one of the plugin's two `agents/` (`deep:researcher` with web and Context7 tools only, `deep:code-reader` with Read and Bash), both `omitClaudeMd: true`, via `agent()`'s `agentType`. This keeps each agent's base context small and keeps web-reading agents away from local files. The skill's `scripts/result.sh` extracts the return value when the completion notification truncates it. Write any non-ASCII escape in that script as `\u{…}`: a raw U+2028/U+2029 ends a regex literal
- `caffeine` — hooks only, no commands or skills. It keeps macOS awake while a session works, via the built-in `caffeinate -i`, and replaces `cc-caffeine@samber`, whose synchronous `npx` hooks cost ~3 s per tool call.
  - The `on` hooks are async, and per-session state lives in `${CLAUDE_PLUGIN_DATA}`.
  - `caffeinate -i <cmd>` keeps `<cmd>` in the original pid and asserts from a forked child. So `$!` and the pid file name the heartbeat loop, not caffeinate; killing the loop releases the assertion.
- `pr-review` — multi-specialist review using parallel native Claude Code subagents (no SDK, no agent team, no cross-agent verification); ships the orchestrator as a user-only skill (`skills/pr-review/SKILL.md`, `disable-model-invocation: true`; invoked as `/pr-review`; the plugin and skill were both `review-pr` before 4.0.0 and `code-review` before 3.0.0, which they left because the bundled `/code-review` owns the bare name; the append-only `marketplace.json` `renames` chain migrates old installs), plus .md agent files, a Go helper, and, inside the skill folder, its references and the prebuilt binaries in `skills/pr-review/scripts/` (not a top-level `bin/`, which claude.ai and Cowork refuse to install)
  - The eight `agents/*.md` duplicate their `## Environment` block and source-fetching paragraph near-verbatim, so shared guidance must be edited in all eight. The `## Environment` block (three bullets) is byte-identical in seven; only `claude-md` adds a clause, exempting CLAUDE.md and `.claude/rules/` files from the never-read-a-live-path rule. The oversized-Read retry line just above the block is identical in all eight. The source-fetching paragraph has five variants that differ only in the trailing `git grep` pathspec (`'*.ts'`, `'*.tsx'`, or none) and an optional "never `find | xargs grep`" clause (`infra`, `security`); `security` alone follows it with a one-line never-Read-from-cwd paragraph. Check the outliers individually rather than blind-replacing.
- `pr-review-v2` — the `pr-review` review rebuilt on a native Workflow, with no Go helper. Its user-only skill `pr-review-v2` (`/pr-review-v2`) runs `skills/pr-review-v2/scripts/prepare.sh`, launches the workflow `workflows/review.js` (`/pr-review-v2:review`) with the ARGS line that script prints, then writes the GitHub payloads from the run's output file with `scripts/payload.sh` (`payload.jq`). The script ports the helper's roster, gate, prior-review match, positional dedup, line snap and rendering to JS, and opens with an assert-based self-check. Specialists return findings as schema-validated structured output, not files. Each finding that passes the gate goes to the internal `verifier` agent, and the `consolidator` agent groups duplicates across anchors; both are `omitClaudeMd: true`. It keeps its own namespaces (`$TMPDIR/pr-review-v2-*`, `refs/pr-review-v2/*`), because `pr-review`'s sweep deletes `refs/code-review/*` refs that have no live `pr-review-<n>-*` dir.
  - Its eight specialists are copies of `pr-review`'s with only the intro, source-fetching paragraph, first two `## Environment` bullets (scratch copies are named `<role>-<path>` because every agent shares `scratch/`) and `## Output` rewritten (no bundle, no findings file; all but `typescript` drop `Write`). The source-fetching paragraph points at `prepare.sh`'s `diff-index.tsv` and range reads, and `## Output` adds a `files_reviewed` list that the workflow's summary checks for thin coverage. All but `quality` and `claude-md` also set `omitClaudeMd: true` (`claude-md`'s prompt treats the auto-loaded root CLAUDE.md and unscoped rules as already read). A domain-checklist change belongs in both plugins; otherwise they evolve independently, so a differing model/effort pin is deliberate, not drift.
  - `prepare.sh` sends the workflow's `files` arg as compact text (directory lines, then `name\thunks\tadded`), not JSON objects, and trims each rule's frontmatter to its `paths:` key, because the orchestrating model re-types the whole ARGS line. `review.js`'s `decodeFiles` reverses it; change both together.
  - Its comment bodies and `cr-finding` markers are byte-identical to the Go helper's output, so each plugin's prior-review dedup recognises the other's comments. `scripts/diff-ranges.awk` ends a hunk body by the header's line counts, where the Go parser ended it at any `+++` line. That makes their added-line runs differ on diffs that add lines starting with `++`.

Component prose says `rg` (or `git grep`), never the `Grep`/`Glob` tools — those are removed on native macOS/Linux builds (CC 2.1.117+; upstream #52121 open). `pr-review` and `pr-review-v2` specialists use `git grep <symbol> <HEAD_SHA>` specifically: they must read at `HEAD_SHA`, not the working tree.

Besides the plugins, one repo-local skill ships at `.claude/skills/plugin-session-auditor/` — session-transcript audit: `SKILL.md`, five analyzer agents, evals, and the `session-parser` Go module (run via `go run .`).

Per-plugin layout:

```
plugins/<name>/
  .claude-plugin/plugin.json                      # plugin manifest
  commands/<command>.md                           # single slash command, named `<plugin-name>.md`
  skills/<name>/SKILL.md                          # skill(s); `transcript`, `pr-respond` and `deep` (`research`) ship one each, `docs` ships four (`audit-docs`, `enrich-claude-md`, `revise`, `init`), `git` ships nine, `simplify` ships two (`simplify-code`, `simplify-prose`), `jira` ships three (`create-ticket`, `implement-ticket`, `create-tests`), and `pr-review` and `pr-review-v2` ship one each (named after the plugin) instead of commands
  agents/, references/, bin/, tools/, hooks/, scripts/, workflows/  # only where needed
```

Plugin layout against Anthropic's standard layout (what each folder means, manifest rules, `${CLAUDE_PLUGIN_ROOT}`/`${CLAUDE_PLUGIN_DATA}`, workflows): see `.claude/rules/plugin-structure.md`. Skill folder layout (skill-level vs plugin-level files, `references/`/`assets/`/`scripts/`, `${CLAUDE_SKILL_DIR}` vs `${CLAUDE_PLUGIN_ROOT}`): see `.claude/rules/skill-structure.md`. CLAUDE.md and `.claude/rules/` authoring (what belongs, size, imports, path-scoped rules): see `.claude/rules/claude-md-structure.md`.

Root-level `*-workspace/` dirs (e.g. `code-review-workspace/`, `docs-workspace/`, `jira-ticket-workspace/`, `debate-workspace/`, `plugin-session-auditor-workspace/`) are gitignored scratch space for the skill-creator / audit / command workflows — safe to ignore. Each is listed individually in `.gitignore` (no `*-workspace/` glob — add new workspace dirs there explicitly).

## Plugin & Command File Structure

Each plugin has a manifest at `plugins/<name>/.claude-plugin/plugin.json` (name, version, description, repository, license, keywords). A plugin may declare `dependencies` on plugins from another marketplace (e.g. `jira` depends on `context7` and `superpowers` from `claude-plugins-official`); the root `marketplace.json` must allowlist that marketplace in `allowCrossMarketplaceDependenciesOn`.

Each slash command is a markdown file in `plugins/<name>/commands/` with YAML frontmatter (`description`, `allowed-tools`, `model`, `effort`, `argument-hint`, `disable-model-invocation`). Field reference: Anthropic's [skills frontmatter reference](https://code.claude.com/docs/en/skills#frontmatter-reference) (a command file takes the same fields except `name` and `paths`) and, for `agents/*.md`, [subagent frontmatter](https://code.claude.com/docs/en/sub-agents#supported-frontmatter-fields).

Reference docs and shared rubrics live under `plugins/<name>/references/`. Use `${CLAUDE_PLUGIN_ROOT}` inside command files to resolve plugin-relative paths at runtime.
