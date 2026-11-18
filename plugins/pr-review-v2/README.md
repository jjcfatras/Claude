# pr-review-v2

Multi-specialist pull-request review for Claude Code, run as a native [Workflow](https://code.claude.com/docs/en/workflows). Parallel specialists (`security`, `quality`, `errors`, `perf`, plus conditional `typescript`, `react`, `infra`, `claude-md`) scan the diff. A skeptic agent then tries to refute each finding against the PR head, a consolidator merges duplicates, and the result posts as inline GitHub review comments.

It is a rework of [`pr-review`](../pr-review/) that replaces its compiled Go helper with a Workflow script plus a little shell. The two plugins can be installed side by side.

## Install

```text
/plugin marketplace add jjcfatras/Claude
/plugin install pr-review-v2@jjcfatras-tools
```

## Usage

**Invoke:** `/pr-review-v2 <pr-number> [--auto|--dry-run]`. The fully namespaced form is `/pr-review-v2:pr-review-v2`. The PR number is required.

- `--auto` posts all eligible findings without prompting, for CI or unattended runs.
- `--dry-run` runs the full review but never posts.
- If both are passed, `--dry-run` wins.

The skill is user-invoked only (`disable-model-invocation: true`). The review runs in the background: watch or stop it in `/workflows`, and the skill picks up again when it finishes. The workflow it runs is also registered as `/pr-review-v2:review`, but it needs the args the skill prepares, so start reviews with `/pr-review-v2`.

## Requirements

- A Claude Code build with the Workflow tool, and `omitClaudeMd` agent support (v2.1.271+) for the two internal agents and six of the eight specialists
- `gh` CLI, authenticated for the repo under review
- `git`, `jq`, `awk`, and `python3` (`python3` is used only by the tier-3 posting fallback)
- A writable `$TMPDIR` (or `/tmp`) for the scratch workspace `pr-review-v2-<number>-<epoch>/`
- The `context7` plugin from `claude-plugins-official`, declared as a dependency, so Claude Code installs it with this one

## What happens

1. `skills/pr-review-v2/scripts/prepare.sh` fetches the PR metadata, diff and prior review threads, and the PR head into the hidden ref `refs/pr-review-v2/pr-<N>`. It parses the diff into hunk ranges and prints the workflow's args.
2. `workflows/review.js` picks the roster:
   - always on: `security`, `quality`, `errors`, `perf`;
   - `typescript` for `.ts/.tsx/.cts/.mts`, plus tsconfig and framework configs;
   - `react` for `.tsx/.jsx`, plus framework and bundler configs;
   - `infra` for `.sql`, `.tf`, `.hcl`, `Dockerfile`, `docker-compose`, `k8s/`, `terraform/`, …;
   - `claude-md` when a changed file has a `CLAUDE.md` ancestor, or matches a path-scoped `.claude/rules/` file, at the PR head.
3. Each specialist returns its findings as validated structured output. As soon as one finishes, its findings go through the confidence gate and the prior-review match, and each survivor gets a `verifier` agent that tries to refute it.
4. When every specialist is done, findings within three lines of each other fold together on a same-defect signal. A `consolidator` agent then groups findings that describe one defect at different anchors, and flags pairs that give opposite advice.
5. The workflow snaps findings to diff lines and renders the review. `scripts/payload.sh` writes the GitHub payloads and prints a summary that accounts for every finding. You're asked whether to post.
6. On approval, it posts through a three-tier fallback (batched review → pending-then-submit → plain PR comment), then removes the scratch dir and the hidden ref.

## What it runs and sends

- **GitHub, via your `gh` login.**
  - Reads: `gh pr view`, `gh pr diff`, and a GraphQL `reviewThreads` query.
  - Writes (only on approval or with `--auto`): one PR review through `gh api …/pulls/<N>/reviews`, or a fallback `gh pr comment`.
- **Git.** `git fetch origin +pull/<N>/head:refs/pr-review-v2/pr-<N>`, deleted at cleanup; read-only `git show` and `git grep` at the PR head.
- **Context7.** The `perf`, `react`, `security` and `typescript` specialists may look up library docs.
- **Local files.** Scratch files go under `$TMPDIR/pr-review-v2-*`. Each run also removes leftovers from earlier runs that are at least 48 hours old.

## How it differs from pr-review

- **No compiled helper.** Roster selection, the confidence gate, dedup, line snapping and rendering are JavaScript in `workflows/review.js`. Diff parsing is `scripts/diff-ranges.awk`, and payload assembly is `scripts/payload.jq`. The script starts with an assert-based self-check, so a broken change fails before any agent runs.
- **Findings come back as structured output**, validated against a schema with the closed category vocabulary, so specialists never write findings files. There is no findings-file Write for auto mode to refuse, and `pr-review`'s allow rule isn't needed.
- **A skeptic verifies every finding** that passes the gate. It refutes only with evidence from the PR head; `uncertain` keeps the finding. `pr-review` has no cross-agent verification.
- **A consolidator agent** replaces `pr-review`'s substring heuristics for findings that describe one defect at different anchors, and for conflicting advice. The agent only names the groups; the posted survivor is picked by a fixed rule (in-diff file, then severity, then confidence).
- **The gate runs before dedup**, so a below-gate finding can no longer absorb an above-gate neighbour.
- **No shared bundle.** Specialists read the diff and `git show` the PR head themselves.
- **Posted comments are byte-compatible.** v2 writes the same `cr-finding` markers, so either plugin recognises the other's prior comments.

**Known cost.** The workflow script can't read files, so `prepare.sh` prints its inputs (changed paths, hunk ranges, prior issues) as one compact JSON line, and the orchestrating model copies it into the Workflow call. Paths are grouped by directory and hunk ranges written as `start-end` text to keep that line short, but it still grows with the number of changed files: about 9 KB, roughly 25 s of copying, on a 136-file PR.
