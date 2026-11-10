# review-pr

Multi-specialist pull-request review for Claude Code. One orchestrator skill fans the PR out to parallel native subagents (`security`, `quality`, `errors`, `perf`, plus conditional `typescript`, `react`, `infra`, `claude-md`), consolidates their findings with a bundled Go helper, and posts them as inline GitHub review comments.

## Install

```text
/plugin marketplace add jjcfatras/Claude
/plugin install review-pr@jjcfatras-tools
```

## Usage

**Invoke:** `/review-pr <pr-number> [--auto|--dry-run]`. The fully namespaced form is `/review-pr:review-pr`. The PR number is required: an absent or non-integer argument is a hard error.

- `--auto` posts all eligible findings without prompting, for CI or unattended runs.
- `--dry-run` runs the full review but never posts.
- If both are passed, `--dry-run` wins.

The skill is user-invoked only (`disable-model-invocation: true`), so Claude never starts a review on its own. Before 3.0.0 this was the `code-review` plugin with a `code-review` skill. It was renamed because Claude Code's bundled `/code-review` owns that bare name. The marketplace's `renames` entry moves existing `code-review@jjcfatras-tools` installs to `review-pr` automatically.

## Requirements

- `gh` CLI, authenticated for the repo under review
- `git`, `jq`, and `python3` (`python3` is used only by the tier-3 posting fallback)
- A writable `$TMPDIR` (or `/tmp`) for the scratch workspace `pr-review-<number>-<epoch>/`
- The `context7` plugin from `claude-plugins-official`. It's declared as a dependency, so Claude Code installs it with this one.

No Go toolchain is needed. The plugin ships prebuilt helper binaries.

## What happens

1. Fetches PR metadata, the full diff, and prior review threads (for dedup) via `gh`.
2. Parses the diff with the bundled Go `code-review-helper` and builds the specialist roster:
   - always on: `security`, `quality`, `errors`, `perf`;
   - conditional, by changed-file extension or path:
     - `typescript` for `.ts/.tsx/.cts/.mts`, plus tsconfig and framework configs;
     - `react` for `.tsx/.jsx`, plus framework and bundler configs;
     - `infra` for `.sql`, `.tf`, `.hcl`, `Dockerfile`, `docker-compose`, `k8s/`, `terraform/`, …;
     - `claude-md` for any changed file with a `CLAUDE.md` ancestor.
3. The helper writes the `## Summary` section straight from changed-file metadata, so no pre-pass subagent runs.
4. Spawns all roster specialists in parallel against a shared `spawn-context.md` bundle and a separate `rubric.md`.
5. The helper finalizes the findings: dedup, severity/confidence gating, line-snapping, payload rendering. You see the summary and are asked whether to post (`--auto` skips the question, `--dry-run` never posts).
6. On approval, it posts through a three-tier fallback (batched review → pending-then-submit → plain PR comment), then removes the scratch dir.

## What it runs and sends

- **GitHub, via your `gh` login.**
  - Reads: `gh pr view`, `gh pr diff`, and a GraphQL `reviewThreads` query.
  - Writes (only on approval or with `--auto`): one PR review through `gh api …/pulls/<N>/reviews`, or a fallback `gh pr comment`.
- **Git.**
  - `git fetch origin +pull/<N>/head:refs/code-review/pr-<N>`, a hidden ref deleted at cleanup.
  - Read-only `git show` and `git grep` at the PR head.
- **Context7.** The `perf`, `react`, `security` and `typescript` specialists may look up library docs through the context7 MCP tools.
- **Local files.** Scratch files go under `$TMPDIR/pr-review-*`. Each run also removes leftovers from earlier runs that are at least 48 hours old.

## Helper binary

The Go helper `code-review-helper` parses diffs and assembles review payloads deterministically.

- Prebuilt binaries for `darwin`, `linux`, and `windows` × `amd64`/`arm64` live in `scripts/`. Windows binaries carry a `.exe` suffix.
- The `scripts/code-review-helper` shell wrapper dispatches to the right binary.
- They live in `scripts/` rather than a top-level `bin/` because claude.ai and Cowork refuse to install a plugin that has a `bin/`.

### Building from source

The source is in `tools/code-review-helper/`.

```sh
cd tools/code-review-helper
make release # cross-compile all platforms into ../../scripts/
make test
```

The author runs `make release` before tagging a new plugin version.
