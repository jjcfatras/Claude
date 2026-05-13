# jjcfatras-tools — Claude Code marketplace

A Claude Code [plugin marketplace](https://docs.claude.com/en/docs/claude-code/plugin-marketplaces) shipping seven slash commands the author uses for everyday Git, testing, code-review, and documentation workflows.

## Install

```text
/plugin marketplace add jjcfatras/Claude
/plugin install <plugin-name>@jjcfatras-tools
```

## Plugins

| Plugin              | Slash command                                             | What it does                                                                                                                                                         |
| ------------------- | --------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `cherry-pick`       | `/cherry-pick <source-branch> [commit-sha or sha1..sha2]` | Cherry-picks one or more commits from a source branch into the current branch and resolves conflicts intelligently.                                                  |
| `merge`             | `/merge <source-branch>`                                  | Merges a source branch into the current branch with conflict resolution.                                                                                             |
| `test-driven-fix`   | `/test-driven-fix <spec-or-bug>`                          | Autonomous patch → test → revert-on-regression loop, hard-capped at 10 iterations.                                                                                   |
| `respond-to-review` | `/respond-to-review <pr-number> [comment-id]`             | Triages every flagged issue on a PR — inline comments and review-body findings — dismissing false positives and fixing valid ones.                                   |
| `code-review-AT`    | `/code-review-AT [pr-number]`                             | Multi-specialist PR review (security, typescript, react, infra, errors, perf, quality, claude-md) coordinated via a sub-agent team. Posts inline comments.           |
| `code-review`       | `/code-review [pr-number]`                                | Same multi-specialist PR review using parallel native Claude Code subagents — no Agent SDK, no agent team, no cross-agent verification. Posts inline comments.       |
| `doc-audit`         | `/audit-docs`                                             | Scans CLAUDE.md / READMEs / `.claude/commands` / `.claude/skills` / architecture docs for stale claims about the codebase and reports findings with suggested fixes. |

Install only the plugins you want — each is independent.

## Using the plugins

Each subsection below covers how to invoke the plugin, what to have ready first, and what to expect step by step. The full flow lives in each plugin's command file under `plugins/<name>/commands/`.

### `/cherry-pick`

**Invoke:** `/cherry-pick <source-branch> [commit-sha or sha1..sha2]`

**Prereqs:** Clean working tree (commit or stash first); source branch exists locally or as `origin/<branch>`.

**What happens:**

1. Preflight: validates the working tree is clean and the source branch exists.
2. Determines commits to apply — uses the SHA / range you passed, or lists the 15 most recent commits on the source branch and asks you to pick.
3. Shows a summary (target, source, commit list) and asks you to confirm.
4. Applies commits one at a time in chronological order.
5. On conflict: reads each conflicted file, resolves it by combining intent from both sides, strips conflict markers, `git add`s, then runs `git cherry-pick --continue`.
6. Reports a final `git log` summary and lists any conflicts that were resolved.

**Escape hatch:** `git cherry-pick --abort` restores the original state if you want out mid-run.

### `/merge`

**Invoke:** `/merge <source-branch>`

**Prereqs:** Clean working tree; source branch exists locally or as `origin/<branch>`.

**What happens:**

1. Preflight: validates the working tree is clean and the source ref resolves.
2. Classifies the merge as **already up to date**, **fast-forward**, or **divergent (merge commit)** and shows the incoming commits.
3. Asks you to confirm.
4. Runs `git merge <source-ref>` with no flags — git picks fast-forward vs. merge commit based on history.
5. On conflict: same auto-resolution flow as `/cherry-pick`, finishing with `git merge --continue`.
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

### `/code-review-AT`

**Invoke:** `/code-review-AT [pr-number]` — omit the argument to review the PR for the current branch.

**Prereqs:** `export CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` must be set before launching Claude Code (see [Requirements](#requirements) below); `/tmp` must be writable for the agent's `Write` tool, or grant `Write` to `$HOME/.claude/tmp/pr-review-*` as a fallback.

**What happens:**

1. Preps the PR diff, prior reviews, project `CLAUDE.md`, and a one-paragraph PR summary into a temp workspace under `/tmp/pr-review-…`.
2. Picks an applicable subset of specialists — security, typescript, react, infra, errors, perf, quality, claude-md — and spawns one subagent per category in parallel.
3. Specialists scan their domain and cross-verify with peer DMs; findings are written to a shared task list.
4. The Go `code-review-helper` finalizes, dedupes, and gates findings, then assembles the review payload.
5. You're shown the inline + summary findings and asked to approve before anything is posted.
6. On approval: posts the review with inline comments, then cleans up the temp workspace.

### `/code-review`

**Invoke:** `/code-review <pr-number>` — the PR number is required (the command treats an absent or non-integer argument as a hard error).

**Prereqs:** `gh` CLI authenticated for the repo; `/tmp` (or `$TMPDIR`) writable for the scratch workspace under `pr-review-<number>-<epoch>/`. Unlike `/code-review-AT`, this plugin does **not** require `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` — it uses native Claude Code subagents (`Agent` with `subagent_type`) only.

**What happens:**

1. Fetches PR metadata, the full diff, and the most recent prior Claude-Code review (for dedup) via `gh`.
2. Parses the diff with the bundled Go `code-review-helper` and builds the specialist roster — always-on: `security`, `quality`, `errors`, `perf`; conditional by changed-file extension/path: `typescript` (`.ts/.tsx/.cts/.mts`), `react` (`.tsx/.jsx` or component/pages paths), `infra` (`.sql`, `.tf`, `.hcl`, `Dockerfile`, `docker-compose`, `k8s/`, `terraform/`, …).
3. Runs a `pr-summary` pre-pass subagent to produce a one-paragraph technical summary written to the scratch workspace.
4. Spawns all roster specialists in parallel against a shared `spawn-context.md` bundle and a separate `rubric.md`.
5. The helper finalizes — dedup, severity/confidence gating, line-snapping, payload rendering — and you're shown the findings summary with a `Post review? [Y]es/[n]o/[i]ds <csv>` prompt. You can post all, skip, or filter to specific finding IDs.
6. On approval: posts via a three-tier fallback (batched review → pending-then-submit → plain PR comment), then removes the scratch dir.

**Hook:** the plugin bundles a `PreToolUse` hook that auto-approves `Grep` and `Glob` so specialist subagents never stall on permission prompts during parallel scans.

### `/audit-docs`

**Invoke:** `/audit-docs` (no arguments).

**Prereqs:** Run inside a git repo. Outside of one, the command falls back to the current working directory and warns that path-relative claim verification is less reliable.

**What happens:**

1. Locates every doc file matching `CLAUDE.md`, `README.md`, `.claude/commands/*.md`, `.claude/skills/**/*.md`, and `*[Aa]rchitecture*.md` (skipping `node_modules`, `dist`, etc.).
2. If more than 50 files match, lists them and asks whether to proceed, narrow scope, or skip directories.
3. Extracts only **concrete claims** from each file — file paths, versions, scripts, symbol names, cross-doc links — not subjective prose.
4. Verifies each claim against the current codebase.
5. Reports findings grouped by source file with suggested fixes.
6. Offers to apply fixes one at a time. **Read-only until you approve a specific fix.**

## `code-review-AT` — extras

- Bundles a Go helper (`code-review-helper`) used to deterministically parse diffs and assemble review payloads. The plugin ships prebuilt binaries for `darwin-amd64`, `darwin-arm64`, `linux-amd64`, and `linux-arm64`; a `bin/code-review-helper` shell wrapper dispatches to the right one.
- Installs eight `code-review-*` review specialists into the team that runs the review.

### Requirements

`/code-review-AT` orchestrates multiple specialist subagents via Claude Code's agent-team APIs (`TeamCreate`, `SendMessage`, concurrent `Agent` spawns). Those tools are gated behind an experimental flag — without it, the command's preflight aborts with an allowlist error. Enable agent teams in your shell before running `/code-review-AT`:

```sh
export CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1
```

The other plugins (`cherry-pick`, `merge`, `test-driven-fix`, `respond-to-review`, `doc-audit`) do not need this flag.

### Building the helper from source

```sh
cd "${CLAUDE_PLUGIN_ROOT}/tools/code-review-helper"
make release # cross-compile all 4 platforms into ../../bin/
make test
```

`make release` is what the author runs before tagging a new plugin version. End users do not need a Go toolchain.

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
.claude-plugin/marketplace.json   # marketplace manifest
plugins/<name>/                    # one directory per plugin
  .claude-plugin/plugin.json
  commands/<name>.md
  agents/, references/, bin/, tools/  # only where the plugin needs them
.claude/skills/<name>/             # repo-internal skills (not marketplace plugins)
```
