# jjcfatras-tools — Claude Code marketplace

A Claude Code [plugin marketplace](https://docs.claude.com/en/docs/claude-code/plugin-marketplaces) shipping one slash command and nineteen skills the author uses for everyday Git, code-review, documentation, and reasoning workflows.

## Install

```text
/plugin marketplace add jjcfatras/Claude
/plugin install <plugin-name>@jjcfatras-tools
```

## Plugins

| Plugin       | Slash command                                                                                                                                                           | What it does                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| ------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `git`        | `/git:commit [--staged]` · `/git:commit-push` · `/git:commit-push-pr` · `/git:push` · `/git:pr` · `/git:clean_gone` · `/git:cherry-pick` · `/git:merge` · `/git:rebase` | Git workflow: auto-message commit (`--staged` commits only what is already in the index), commit + push (refreshes an open PR), commit + push + open PR, push already-committed work (also refreshes an open PR), open a PR for already-pushed work, prune local `[gone]` branches, plus cherry-pick, merge, and rebase with conflict resolution. All nine ship as skills, so Claude can auto-invoke them in addition to the `/` commands.                                                                              |
| `pr-respond` | `/pr-respond <pr-number> [comment-id]`                                                                                                                                  | Triages every flagged issue on a PR — inline comments and review-body findings — dismissing false positives and fixing valid ones. It also checks replies on review threads you started and resolves the ones that hold up.                                                                                                                                                                                                                                                                                             |
| `pr-review`  | `/pr-review <pr-number> [--auto\|--dry-run]`                                                                                                                            | Multi-specialist PR review (security, quality, errors, perf, plus conditional typescript, react, infra, claude-md) using parallel native Claude Code subagents. Posts inline comments. Ships as a skill (`SKILL.md` format) but stays user-invoked only (`disable-model-invocation: true`) — Claude never auto-invokes it.                                                                                                                                                                                              |
| `docs`       | `/docs:audit-docs` · `/docs:enrich-claude-md` · `/docs:revise`                                                                                                          | Scans CLAUDE.md / READMEs / `docs/` trees / `.claude/commands` / `.claude/skills` / `.claude/rules` / architecture docs for stale claims and reports findings with suggested fixes; investigates the codebase for useful, non-obvious facts missing from CLAUDE.md / `.claude/rules` and proposes additions; or captures learnings from the current session into CLAUDE.md / CLAUDE.local.md / `.claude/rules`. All three ship as skills; Claude can auto-invoke the first two, while `revise` stays user-invoked only. |
| `debate`     | `/debate <claim>`                                                                                                                                                       | Adversarial pro/con debate — opening arguments, then up to 5 rounds of attack/defend, then an inline markdown report of surviving, negated, and disputed arguments.                                                                                                                                                                                                                                                                                                                                                     |
| `simplify`   | `/simplify:simplify-code [path\|--staged\|--diff\|--since=<ref>]` · `/simplify:simplify-prose [text\|path\|--staged\|--diff\|--since=<ref>]`                            | Proposes targeted, behavior-preserving simplifications to recently modified code (`simplify-code`) or lossless distillation of verbose prose (`simplify-prose`) — or the whole project when no scope is given; shows diffs per file and applies only on approval. Both ship as skills, so Claude can auto-invoke them in addition to the `/` commands.                                                                                                                                                                  |
| `transcript` | `/transcript`                                                                                                                                                           | Prints the filepath of the current Claude Code session's `.jsonl` transcript. Shipped as a skill, so Claude can also invoke it automatically when it needs the path.                                                                                                                                                                                                                                                                                                                                                    |
| `jira`       | `/jira:create-ticket` · `/jira:implement-ticket <JIRA-key>` · `/jira:create-tests <JIRA-key>`                                                                           | Create a structured JIRA ticket (summary, acceptance criteria, QA steps) from a diff/PR/description; pick up a ticket by key — verify its codebase claims, reconcile a plan, get approval, then implement it; or generate a runnable Postman QA collection from a ticket's acceptance criteria. All three ship as skills (`SKILL.md` format) but stay user-invoked only (`disable-model-invocation: true`) — Claude never auto-invokes them.                                                                            |
| `caffeine`   | — (hooks only)                                                                                                                                                          | Keeps macOS awake while Claude Code works (including background subagents) and lets it sleep after `Stop` or 15 idle minutes. It uses the built-in `caffeinate -i`, so it has no Electron or `npx` dependency, and its hooks never block a tool call. It replaces `cc-caffeine@samber`; disable that plugin when installing this one. macOS only.                                                                                                                                                                       |

Install only the plugins you want — each is independent.

## Using the plugins

Each subsection covers how to invoke the plugin, its prerequisites, and what to expect. The full flow lives in each command or skill file under `plugins/<name>/commands/` or `plugins/<name>/skills/`.

### `/git:cherry-pick`

**Invoke:** `/git:cherry-pick <source-branch> [commit-sha or sha1..sha2]`

**Prereqs:** Clean working tree (commit or stash first); source branch exists locally or as `origin/<branch>`.

**What happens:**

1. Preflight: validates the working tree is clean and the source branch exists.
2. Determines commits to apply — uses the SHA / range you passed (`sha1..sha2` includes both `sha1` and `sha2`, unlike plain git), or lists the 20 most recent commits on the source branch and asks you to pick.
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

### `/git:rebase`

**Invoke:** `/git:rebase <base-branch>`

**Prereqs:** Clean working tree; on a branch (not detached HEAD); base branch exists locally or as `origin/<branch>`.

**What happens:**

1. Preflight: offers to continue or abort a rebase that is already stopped, validates the working tree is clean, and fetches `<base>` (and the current branch) from `origin`. If your local base is behind `origin/<base>`, asks which one to rebase onto. `/git:rebase main` while on `main` rebases onto `origin/main`.
2. Lists the commits to replay (marking any already in the base) and flags merge commits (a plain rebase flattens them), already-pushed commits it will rewrite, and remote commits your branch lacks.
3. Asks you to confirm.
4. Runs `git rebase --no-rebase-merges --no-update-refs <base-ref>`, so your `rebase.*` config can't keep merges or move other branches.
5. On conflict: same auto-resolution flow as `/git:cherry-pick` (applies your commit's intent on top of the base), finishing each commit with `git rebase --continue`. Rebase runs no pre-commit hook, so the report suggests running your tests on resolved files.
6. Reports a `git range-diff` pairing each original commit with its rebased version, plus the undo command.
7. If the branch was already pushed, offers `git push --force-with-lease --force-if-includes` — never pushes without a yes, and never falls back to `--force`.

**Escape hatch:** `git rebase --abort` mid-run; `git reset --hard <old-tip>` (printed in the report) afterwards.

**Note:** all nine git workflows ship as skills (`plugins/git/skills/`), so besides the `/git:*` commands Claude can auto-invoke them when asked in natural language (e.g. "commit and push this").

### `/pr-respond`

**Invoke:** `/pr-respond <pr-number> [comment-id]` — passing a comment ID scopes the run to one inline thread and skips review-body parsing.

**Prereqs:** `gh` CLI authenticated for the repo; the PR exists and its branch lives on `origin` (fork PRs are not supported). Run it from any checkout — your branch and working tree are never touched.

**What happens:**

1. Fetches the PR diff, all inline comments, and all review bodies. If anything needs a response, it creates a throwaway worktree at the PR head (`.claude/worktrees/pr-<number>`), and all triage and fixing happen there.
2. Filters to actionable items only — drops replies, anything you've already replied to, and trivial acks like "LGTM". Reviews you posted yourself (e.g. via `/code-review --comment`) stay in scope. Threads you started that someone has since answered become review responses: it checks the reply instead of triaging the issue again, resolves the thread when the fix or deferral holds, pushes back when it doesn't, and never edits code for them.
3. Parses review bodies into discrete findings (one per bullet / heading / paragraph).
4. Triages each item as **false positive**, **preexisting code (not introduced by this PR)**, or **valid issue**. With more than three items this runs as a Workflow: one analysis agent per item returns a schema-checked verdict, then an independent verifier tries to refute every dismissal by reproducing its cited evidence; a dismissal that can't be reproduced is promoted back to valid before you see the triage.
5. Implements fixes for the valid items, then commits and pushes them from the worktree and refreshes the PR description. It removes the worktree afterwards, but keeps it if a stop leaves unpushed work. Finally it replies confirming each change, and replies to the rest explaining each dismissal.

**Note:** `pr-respond` ships as a skill (`plugins/pr-respond/skills/pr-respond/`), so besides `/pr-respond` Claude can start it when asked in natural language (e.g. "address the review comments on PR 42"). Nothing is pushed or posted before you approve the triage.

### `/pr-review`

Invocation, requirements, what it runs and sends, and how to build its Go helper are in [`plugins/pr-review/README.md`](plugins/pr-review/README.md).

### `/docs:audit-docs`

**Invoke:** `/docs:audit-docs [file|dir|glob]` — no argument audits the whole repo; pass a file, directory, or glob to narrow the scope.

**Prereqs:** Run inside a git repo. Outside of one, the command falls back to the current working directory and warns that path-relative claim verification is less reliable.

**What happens:**

1. Locates every doc file matching `CLAUDE.md`, `README.md`, `.claude/commands/*.md`, `.claude/skills/*/SKILL.md`, `.claude/rules/*.md`, `**/docs/**/*.md`, and `*[Aa]rchitecture*.md` (skipping `node_modules`, `dist`, etc.).
2. If more than 50 files match, lists them and asks whether to proceed, narrow scope, or skip directories.
3. Extracts only **concrete claims** from each file — file paths, versions, scripts, symbol names, cross-doc links — not subjective prose.
4. Verifies each claim against the current codebase.
5. Reports findings grouped by source file with suggested fixes.
6. Offers to apply fixes one at a time. **Read-only until you approve a specific fix.**

**Note:** `audit-docs` and `enrich-claude-md` now ship as skills (`plugins/docs/skills/`), so besides `/docs:audit-docs` / `/docs:enrich-claude-md` Claude can auto-invoke them when it judges the docs need auditing or enriching. `/docs:revise` (capture this session's learnings into CLAUDE.md) is user-only (`disable-model-invocation: true`).

### `/simplify:simplify-code`

**Invoke:** `/simplify:simplify-code [path|--staged|--diff|--since=<ref>]` — no argument audits the whole project. The prose counterpart, `/simplify:simplify-prose [text|path|--staged|--diff|--since=<ref>]`, follows the same flow for prose files (or inline text) with a lossless-distillation rubric.

**Prereqs:** Inside a git repo for `--staged` / `--diff` / `--since=` modes. The skills never propose formatting-only changes or run formatters themselves; they assume a project-level `PostToolUse` formatter hook (e.g. Prettier, gofmt) reformats after edits.

**What happens:**

1. Resolves scope per the argument (path/glob, `--staged`, `--diff`, `--since=<ref>`, or default session + working tree).
2. Drops generated files, binaries, and whitespace-only changes from the candidate set; lists the survivors and asks you to confirm.
3. Loads the root `CLAUDE.md`, nearest non-root `CLAUDE.md` ancestors per file, and the plugin's four-pillar `code-standards.md` rubric.
4. Analyzes each file and proposes hunks — unified diff + one-sentence rationale citing the pillar. Hard guardrails drop hunks that change behavior, alter public API, or duplicate formatter work.
5. For each file with surviving hunks: shows diffs and prompts `apply all` / `apply some` / `skip file` / `edit and apply`.
6. Applies approved hunks via `Edit`. Never commits — that's your call.

**Notes:** single-threaded by design (no subagents); no `--auto` mode — the propose-then-apply checkpoint is the whole point.

**Note:** both simplify workflows ship as skills (`plugins/simplify/skills/`), so besides the `/simplify:*` commands Claude can auto-invoke them when asked in natural language (e.g. "simplify this code").

### `/transcript`

**Invoke:** `/transcript` (no arguments).

**Prereqs:** `CLAUDE_CODE_SESSION_ID` must be set (Claude Code exports it automatically).

**What happens:**

1. Resolves the transcript path as `$HOME/.claude/projects/<encoded-cwd>/$CLAUDE_CODE_SESSION_ID.jsonl`, where `<encoded-cwd>` replaces every `/` and `.` in `$PWD` with `-`.
2. If the expected file is missing (e.g. `$PWD` moved into a worktree mid-session), searches every `~/.claude/projects/*/` dir for `$CLAUDE_CODE_SESSION_ID.jsonl` by exact name; failing that, falls back to the most-recently-modified `*.jsonl` in the encoded directory, accepted only if it records this session.
3. Prints one line: the transcript path. Nothing else.

Use this to hand the file to `/plugin-session-auditor` or any other transcript-consuming workflow.

**Note:** `transcript` is now packaged as a skill (`plugins/transcript/skills/transcript/`) rather than a command, so besides `/transcript` Claude can auto-invoke it when it needs the current session's transcript path.

### `/jira:implement-ticket`

**Invoke:** `/jira:implement-ticket <JIRA-key>` (e.g. `/jira:implement-ticket PROJ-1234`).

**Prereqs:** Authenticated Atlassian MCP access (the command resolves the site via `getAccessibleAtlassianResources` and fetches the ticket via `getJiraIssue`); run inside the repo the ticket targets.

**What happens:**

1. **Fetch** the ticket — description, comments, linked issues, and acceptance criteria (later comments weighted over the original description on conflict).
2. **Understand intent** — distills the underlying goal and sorts the ticket into three buckets: goal/problem, claims about the codebase, and any proposed solution.
3. **Verify every codebase claim** against the actual current code (`rg`/Read/LSP), classifying each as confirmed / false / partial / unverifiable with `file:line` evidence. If a false claim knocks out the ticket's premise, it stops and surfaces the finding instead of building on a phantom.
4. **Design a plan** independently from the goal and verified facts first, then reconciles it against the ticket's proposed solution (if any), recording where each approach won.
5. **Present for approval** — intent, claim-verification table (false claims headlined), the synthesized plan, and reconciliation rationale. Gates with `AskUserQuestion`; edits no file before you approve.
6. **Implement** the approved plan — test-first for testable changes, directly for config/docs — then runs the existing suite and reports results honestly.
7. **Verify coverage** of the finished changeset — enumerates what actually changed (not what was planned), gives each changed behavior a covered / uncovered / not-testable verdict, and retrofits tests for the gaps. Each retrofit test is mutation-checked (break the change, watch that test go red, restore) since a test written after the code can't be seen to fail naturally. Closes gaps that fit the existing test setup; asks before adding new test infrastructure.

## Maintainer scripts

All cross-plugin tasks live in the repo-root `Makefile` and are invoked with `make <target>`. The only toolchain needed is Go. End users never need these — they exist for contributors editing this repo.

| Target                  | Command                                                                                                               | Purpose                                                                                                                                                                                                      |
| ----------------------- | --------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `make hooks`            | `go tool lefthook install`                                                                                            | Installs the lefthook git hooks from `lefthook.yml`; run once after cloning. lefthook is a Go tool pinned in the root `go.mod`; `pre-commit` formats staged files, then runs `make lint-go` and `make test`. |
| `make format`           | `scripts/format.sh`                                                                                                   | Formats every tracked file by type: gofmt, `go mod edit -fmt`, shfmt (shell), yamlfmt (YAML), `tools/jsonfmt` (JSON), go-prettier (Markdown, JS). Skips the pr-review helper's `testdata/`.                  |
| `make lint-go`          | `go vet ./tools/...` + `go -C <module> vet ./...` for both Go modules                                                 | `go vet` on the root `tools/` and both Go modules (the pr-review helper + plugin-session-auditor session-parser). Stdlib only, no external linter.                                                           |
| `make validate-plugins` | `claude plugin validate --strict . && for p in plugins/*/; do claude plugin validate --strict "$p" \|\| exit 1; done` | Anthropic's strict validator on the marketplace and every `plugins/*/`. Needs the `claude` CLI; not wired into pre-commit.                                                                                   |
| `make build-go`         | `make -C plugins/pr-review/tools/code-review-helper release`                                                          | `make release` for the pr-review helper — cross-compiles darwin/linux/windows × amd64/arm64 prebuilts into `skills/pr-review/scripts/` (Windows gets `.exe`). Skips session-parser (no prebuilt shipped).    |
| `make test`             | `go test ./tools/...` + `make -C plugins/pr-review/tools/code-review-helper test`                                     | Runs the `tools/jsonfmt` tests and the `pr-review` Go test suite (`go test ./...`). The plugin-session-auditor session-parser ships no test suite (it runs via `go run .`).                                  |

To build the Go helper, run `make release` (or `make test`) directly from inside `plugins/pr-review/tools/code-review-helper/`.

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
  commands/<file>.md                            # single slash command, <plugin>.md
  skills/<name>/SKILL.md                        # a plugin may ship skills instead (e.g. transcript, docs, git, simplify, jira, pr-review)
  agents/, references/, tools/, hooks/, scripts/, evals/  # only where the plugin needs them
.claude/skills/<name>/                          # repo-internal skills (not marketplace plugins)
```
