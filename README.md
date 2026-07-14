# jjcfatras-tools — Claude Code marketplace

A Claude Code [plugin marketplace](https://docs.claude.com/en/docs/claude-code/plugin-marketplaces) shipping seventeen slash commands and a skill the author uses for everyday Git, testing, code-review, documentation, and reasoning workflows.

## Install

```text
/plugin marketplace add jjcfatras/Claude
/plugin install <plugin-name>@jjcfatras-tools
```

## Plugins

| Plugin              | Slash command                                                                                                      | What it does                                                                                                                                                                                                                                                                                    |
| ------------------- | ------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `git`               | `/git:commit` · `/git:commit-push` · `/git:commit-push-pr` · `/git:clean_gone` · `/git:cherry-pick` · `/git:merge` | Git workflow: auto-message commit, commit + push (refreshes an open PR), commit + push + open PR, prune local `[gone]` branches, plus cherry-pick and merge with conflict resolution.                                                                                                           |
| `test-driven-fix`   | `/test-driven-fix <spec-or-bug>`                                                                                   | Autonomous patch → test → revert-on-regression loop, hard-capped at 10 iterations.                                                                                                                                                                                                              |
| `respond-to-review` | `/respond-to-review <pr-number> [comment-id]`                                                                      | Triages every flagged issue on a PR — inline comments and review-body findings — dismissing false positives and fixing valid ones.                                                                                                                                                              |
| `code-review`       | `/code-review [pr-number]`                                                                                         | Multi-specialist PR review (security, quality, errors, perf, plus conditional typescript, react, infra, claude-md) using parallel native Claude Code subagents. Posts inline comments.                                                                                                          |
| `docs`              | `/audit-docs` · `/enrich-claude-md`                                                                                | Scans CLAUDE.md / READMEs / `.claude/commands` / `.claude/skills` / `.claude/rules` / architecture docs for stale claims and reports findings with suggested fixes; or investigates the codebase for useful, non-obvious facts missing from CLAUDE.md / `.claude/rules` and proposes additions. |
| `debate`            | `/debate <claim>`                                                                                                  | Adversarial pro/con debate — opening arguments, then up to 5 rounds of attack/defend, then an inline markdown report of surviving, negated, and disputed arguments.                                                                                                                             |
| `simplify`          | `/simplify [path\|--staged\|--since=<ref>]`                                                                        | Proposes targeted, behavior-preserving simplifications to recently modified code — or the whole project when no scope is given; shows diffs per file and applies only on approval.                                                                                                              |
| `simplify-prose`    | `/simplify-prose [text\|path\|--staged\|--since=<ref>]`                                                            | Proposes lossless distillation of verbose prose — inline text, recently modified prose files, or the whole project when no scope is given — and applies only on approval.                                                                                                                       |
| `transcript`        | `/transcript`                                                                                                      | Prints the filepath of the current Claude Code session's `.jsonl` transcript, with size and line count. Shipped as a skill, so Claude can also invoke it automatically when it needs the path.                                                                                                  |
| `jira`              | `/jira:create-ticket` · `/jira:implement-ticket <JIRA-key>` · `/jira:create-tests <JIRA-key>`                      | Create a structured JIRA ticket (summary, acceptance criteria, QA steps) from a diff/PR/description; pick up a ticket by key — verify its codebase claims, reconcile a plan, get approval, then implement it; or generate a runnable Postman QA collection from a ticket's acceptance criteria. |

Install only the plugins you want — each is independent.

Two additional hook-only plugins ship no slash command: `tool-discipline` (`PreToolUse` guardrails — no-cd-chaining, prefer-builtin-tools, plus a conditional ToolSearch shim that redirects Grep/Glob loads to the embedded Bash search on native builds that dropped those tools and self-disables elsewhere) and `tool-discipline-lsp` (the prefer-LSP `PreToolUse` guardrail plus a `PostToolUse` advisory that nudges a retry when `workspaceSymbol` returns empty).

## Using the plugins

Each subsection covers how to invoke the plugin, its prerequisites, and what to expect. The full flow lives in each command file under `plugins/<name>/commands/`.

### `/git:cherry-pick`

**Invoke:** `/git:cherry-pick <source-branch> [commit-sha or sha1..sha2]`

**Prereqs:** Clean working tree (commit or stash first); source branch exists locally or as `origin/<branch>`.

**What happens:**

1. Preflight: validates the working tree is clean and the source branch exists.
2. Determines commits to apply — uses the SHA / range you passed, or lists the 15 most recent commits on the source branch and asks you to pick.
3. Shows a summary (target, source, commit list) and asks you to confirm.
4. Applies commits one at a time in chronological order.
5. On conflict: reads each conflicted file, resolves it by combining intent from both sides, strips conflict markers, `git add`s, then runs `git cherry-pick --continue`.
6. Reports a final `git log` summary and lists any conflicts that were resolved.

**Escape hatch:** `git cherry-pick --abort` restores the original state if you want out mid-run.

### `/git:merge`

**Invoke:** `/git:merge <source-branch>`

**Prereqs:** Clean working tree; source branch exists locally or as `origin/<branch>`.

**What happens:**

1. Preflight: validates the working tree is clean and the source ref resolves.
2. Classifies the merge as **already up to date**, **fast-forward**, or **divergent (merge commit)** and shows the incoming commits.
3. Asks you to confirm.
4. Runs `git merge <source-ref>` with no flags — git picks fast-forward vs. merge commit based on history.
5. On conflict: same auto-resolution flow as `/git:cherry-pick`, finishing with `git merge --continue`.
6. Reports a final `git log --graph` summary and a `git diff ORIG_HEAD..HEAD --stat`.

**Escape hatch:** `git merge --abort`.

### `/test-driven-fix`

**Invoke:** `/test-driven-fix <spec-path-or-bug-description>` — a path that resolves to an existing file is treated as a spec; anything else is treated as a free-text bug description.

**Prereqs:** A detectable test stack — `package.json`, `pyproject.toml` / `pytest.ini`, `Cargo.toml`, `go.mod`, or a `Makefile` exposing `test` / `lint` / `typecheck`. A dirty working tree is auto-stashed under `tdf-baseline` before the loop starts.

**What happens:**

1. Detects test/lint/typecheck commands from the project metadata.
2. Runs the baseline and parses failures into a tracked task list.
3. Iterates up to **10** times: locate the symbol → propose a minimal patch → narrow re-run → full re-run → revert any patch that regresses a previously-green test → repeat. Never prompts mid-loop.
4. On full green: stages the touched files and creates a `fix(<scope>): …` commit with a body listing the failures that moved red → green.
5. On exhaustion (10 iterations, still red): leaves best-effort patches in the working tree and **does not commit**. The baseline stash is preserved so you can `git stash show -p stash@{…}` to diff.

### `/respond-to-review`

**Invoke:** `/respond-to-review <pr-number> [comment-id]` — passing a comment ID scopes the run to one inline thread and skips review-body parsing.

**Prereqs:** `gh` CLI authenticated for the repo; the PR exists.

**What happens:**

1. Fetches the PR diff, all inline comments, and all review bodies.
2. Filters to actionable items only — drops replies, your own comments, anything you've already replied to, and trivial acks like "LGTM".
3. Parses review bodies into discrete findings (one per bullet / heading / paragraph).
4. Triages each item as **false positive**, **preexisting code (not introduced by this PR)**, or **valid issue**.
5. Implements fixes for the valid items and replies confirming the change; replies to the others with an explanation dismissing the finding.

### `/code-review`

**Invoke:** `/code-review <pr-number>` — the PR number is required (the command treats an absent or non-integer argument as a hard error).

**Prereqs:** `gh` CLI authenticated for the repo; `/tmp` (or `$TMPDIR`) writable for the scratch workspace under `pr-review-<number>-<epoch>/`. This plugin uses native Claude Code subagents (`Agent` with `subagent_type`) only — no experimental flag required.

**What happens:**

1. Fetches PR metadata, the full diff, and the most recent prior Claude-Code review (for dedup) via `gh`.
2. Parses the diff with the bundled Go `code-review-helper` and builds the specialist roster — always-on: `security`, `quality`, `errors`, `perf`; conditional by changed-file extension/path: `typescript` (`.ts/.tsx/.cts/.mts`), `react` (`.tsx/.jsx` or component/pages paths), `infra` (`.sql`, `.tf`, `.hcl`, `Dockerfile`, `docker-compose`, `k8s/`, `terraform/`, …).
3. The helper synthesizes the `## Summary` section deterministically from changed-file metadata — no pre-pass subagent runs.
4. Spawns all roster specialists in parallel against a shared `spawn-context.md` bundle and a separate `rubric.md`.
5. The helper finalizes — dedup, severity/confidence gating, line-snapping, payload rendering — and you're shown the findings summary with a `Post review? [Y]es/[n]o/[i]ds <csv>` prompt. You can post all, skip, or filter to specific finding IDs.
6. On approval: posts via a three-tier fallback (batched review → pending-then-submit → plain PR comment), then removes the scratch dir.

### `/audit-docs`

**Invoke:** `/audit-docs [file|dir|glob]` — no argument audits the whole repo; pass a file, directory, or glob to narrow the scope.

**Prereqs:** Run inside a git repo. Outside of one, the command falls back to the current working directory and warns that path-relative claim verification is less reliable.

**What happens:**

1. Locates every doc file matching `CLAUDE.md`, `README.md`, `.claude/commands/*.md`, `.claude/skills/*/SKILL.md`, `.claude/rules/*.md`, and `*[Aa]rchitecture*.md` (skipping `node_modules`, `dist`, etc.).
2. If more than 50 files match, lists them and asks whether to proceed, narrow scope, or skip directories.
3. Extracts only **concrete claims** from each file — file paths, versions, scripts, symbol names, cross-doc links — not subjective prose.
4. Verifies each claim against the current codebase.
5. Reports findings grouped by source file with suggested fixes.
6. Offers to apply fixes one at a time. **Read-only until you approve a specific fix.**

### `/simplify`

**Invoke:** `/simplify [path|--staged|--since=<ref>]` — no argument audits the whole project.

**Prereqs:** Inside a git repo for `--staged` / `--since=` modes. The plugin's `PostToolUse` formatter hooks (Prettier, gofmt) handle reformatting after edits, so don't run formatters manually.

**What happens:**

1. Resolves scope per the argument (path/glob, `--staged`, `--since=<ref>`, or default session + working tree).
2. Drops generated files, binaries, and whitespace-only changes from the candidate set; lists the survivors and asks you to confirm.
3. Loads the root `CLAUDE.md`, nearest non-root `CLAUDE.md` ancestors per file, and the plugin's four-pillar `standards.md` rubric.
4. Analyzes each file and proposes hunks — unified diff + one-sentence rationale citing the pillar. Hard guardrails drop hunks that change behavior, alter public API, or duplicate formatter work.
5. For each file with surviving hunks: shows diffs and prompts `apply all` / `apply some` / `skip file` / `edit and apply`.
6. Applies approved hunks via `Edit`. Never commits — that's your call.

**Notes:** single-threaded by design (no subagents); no `--auto` mode — the propose-then-apply checkpoint is the whole point.

### `/transcript`

**Invoke:** `/transcript` (no arguments).

**Prereqs:** `CLAUDE_CODE_SESSION_ID` must be set (Claude Code exports it automatically).

**What happens:**

1. Resolves the transcript path as `$HOME/.claude/projects/<encoded-cwd>/$CLAUDE_CODE_SESSION_ID.jsonl`, where `<encoded-cwd>` replaces every `/` and `.` in `$PWD` with `-`.
2. If the expected file is missing but the encoded directory exists, falls back to the most-recently-modified `*.jsonl` in that directory and labels the output `path (fallback):`.
3. Prints three lines: the path, size in MB, and line count. Nothing else.

Use this to hand the file to `/plugin-session-auditor` or any other transcript-consuming workflow.

**Note:** `transcript` is now packaged as a skill (`plugins/transcript/skills/transcript/`) rather than a command, so besides `/transcript` Claude can auto-invoke it when it needs the current session's transcript path.

### `/jira:implement-ticket`

**Invoke:** `/jira:implement-ticket <JIRA-key>` (e.g. `/jira:implement-ticket PROJ-1234`).

**Prereqs:** Authenticated Atlassian MCP access (the command resolves the site via `getAccessibleAtlassianResources` and fetches the ticket via `getJiraIssue`); run inside the repo the ticket targets.

**What happens:**

1. **Fetch** the ticket — description, comments, linked issues, and acceptance criteria (later comments weighted over the original description on conflict).
2. **Understand intent** — distills the underlying goal and sorts the ticket into three buckets: goal/problem, claims about the codebase, and any proposed solution.
3. **Verify every codebase claim** against the actual current code (Grep/Glob/Read/LSP), classifying each as confirmed / false / partial / unverifiable with `file:line` evidence. If a false claim knocks out the ticket's premise, it stops and surfaces the finding instead of building on a phantom.
4. **Design a plan** independently from the goal and verified facts first, then reconciles it against the ticket's proposed solution (if any), recording where each approach won.
5. **Present for approval** — intent, claim-verification table (false claims headlined), the synthesized plan, and reconciliation rationale. Gates with `AskUserQuestion`; edits no file before you approve.
6. **Implement** the approved plan — test-first for testable changes, directly for config/docs — then runs the existing suite and reports results honestly.

## `code-review` — extras

- Bundles a Go helper (`code-review-helper`) used to deterministically parse diffs and assemble review payloads. The plugin ships prebuilt binaries for `darwin`, `linux`, and `windows` × `amd64`/`arm64` (Windows binaries carry a `.exe` suffix); a `bin/code-review-helper` shell wrapper dispatches to the right one.
- Spawns its review specialists (`security`, `quality`, `errors`, `perf`, plus conditional `typescript`, `react`, `infra`, `claude-md`) as parallel native Claude Code subagents — no agent-team API, no experimental flag.

### Building the helper from source

```sh
cd "${CLAUDE_PLUGIN_ROOT}/tools/code-review-helper"
make release # cross-compile all platforms into ../../bin/
make test
```

The author runs `make release` before tagging a new plugin version; end users do not need a Go toolchain.

## Maintainer scripts

All cross-plugin scripts live in the repo-root `package.json` and are invoked with `pnpm <script>`. End users never need these — they exist for contributors editing this repo.

| Script             | Command                                                                                                                                                                            | Purpose                                                                                                                                                                                 |
| ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pnpm install`     | Installs dependencies; also fires `prepare`.                                                                                                                                       | Standard install.                                                                                                                                                                       |
| `pnpm format`      | `prettier --write .`                                                                                                                                                               | Format every file the prettier config matches. Uses `prettier-plugin-sh` for shell.                                                                                                     |
| `pnpm format:go`   | `for d in plugins/code-review/tools/code-review-helper .claude/skills/plugin-session-auditor/tools/session-parser; do gofmt -w "$d" && go -C "$d" mod edit -fmt \|\| exit 1; done` | `gofmt -w` and `go mod edit -fmt` across both Go modules (the code-review helper + plugin-session-auditor session-parser).                                                              |
| `pnpm build:go`    | `make -C plugins/code-review/tools/code-review-helper release`                                                                                                                     | `make release` for the code-review helper — cross-compiles darwin/linux/windows × amd64/arm64 prebuilts into `bin/` (Windows gets `.exe`). Skips session-parser (no prebuilt shipped).  |
| `pnpm check-types` | `tsc --noEmit`                                                                                                                                                                     | TypeScript type-check using the root `tsconfig.json`.                                                                                                                                   |
| `pnpm test`        | `make -C plugins/code-review/tools/code-review-helper test`                                                                                                                        | Runs the `code-review` Go test suite (`go test ./...`). No JS/TS suite exists; the plugin-session-auditor session-parser's tests run via its own `make test`.                           |
| `pnpm prepare`     | `husky`                                                                                                                                                                            | Installs the Husky git hooks (runs automatically after `pnpm install`). The repo's `pre-commit` runs `pnpm exec lint-staged`; the lint-staged config lives at `lint-staged.config.mjs`. |

To build the Go helper, run `make release` (or `make test`) directly from inside `plugins/code-review/tools/code-review-helper/`.

## Repo-internal skills

The repo also ships skills under `.claude/skills/`. These are **not** part of the marketplace and are not installed by `/plugin install` — they activate only when Claude Code is run inside a clone of this repo. They exist for maintainers and contributors working on the plugins themselves.

### `plugin-session-auditor`

**Triggers:** auto-invokes when you hand Claude Code a `.jsonl` session log and ask to audit, review, analyze, or find issues in a plugin run. Phrases like "look at this transcript", "what went wrong in this session", or "the plugins were misbehaving in this run" also trigger it.

**What it does:**

1. Parses the jsonl transcript(s) into structured event JSON via a bundled Go tool (`tools/session-parser/`).
2. Detects which plugins under `plugins/` were exercised in the session.
3. Spawns four specialist subagents in parallel — `permissions`, `errors`, `tool-failures`, `orchestration` — each writing findings to a shared `$RUN_DIR/findings/<category>.md`.
4. Consolidates findings into `proposals.md` with evidence (timestamps + tool_use_ids), 2+ fix options per issue, and a recommendation.
5. Asks which proposals to implement before touching any `plugins/<name>/` source. On approval, applies fixes and bumps the affected `plugin.json` `version` per the repo's SemVer rules.

**Where it lives:** `.claude/skills/plugin-session-auditor/` (`SKILL.md`, `agents/`, `references/`, `tools/`, `evals/`).

## Repository layout

```
.claude-plugin/marketplace.json                # marketplace manifest
plugins/<name>/                                 # one directory per plugin
  .claude-plugin/plugin.json
  commands/<file>.md                            # usually <plugin>.md; docs ships audit-docs.md
  skills/<name>/SKILL.md                        # a plugin may ship a skill instead (e.g. transcript)
  agents/, references/, bin/, tools/, hooks/    # only where the plugin needs them
.claude/skills/<name>/                          # repo-internal skills (not marketplace plugins)
```
