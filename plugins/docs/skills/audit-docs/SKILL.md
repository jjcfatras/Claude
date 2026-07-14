---
name: audit-docs
description: Audit project docs (CLAUDE.md, READMEs, .claude/commands, .claude/skills, .claude/rules, architecture docs) for stale claims about the codebase. Extract concrete claims (tech stack, paths, scripts, symbols, cross-doc links, provenance/attribution), verify each against current state, and report findings grouped by file with suggested fixes. Offers to apply fixes per finding. Optionally scope the audit to a file, directory, or glob. Use when the user asks to audit docs, check for stale or outdated documentation, verify CLAUDE.md / README claims against the code, or find doc drift.
argument-hint: "[file|dir|glob]"
allowed-tools: Bash(find:*), Bash(ls:*), Bash(test:*), Bash(stat:*), Bash(jq:*), Bash(grep:*), Bash(rg:*), Bash(wc:*), Bash(git:*), Read, Edit, Write, Grep, Glob, AskUserQuestion
model: opus
effort: high
---

Audit project documentation for stale claims about the codebase. The user is asking because docs drift — over time, CLAUDE.md / README / architecture notes accumulate statements that were once true but no longer match the code. Your job: find those statements, verify each, and produce an actionable report.

You audit a fixed set of file globs in the repo, extract only **concrete, verifiable claims** (file paths, versions, scripts, symbols, cross-doc links — not subjective prose like "this codebase is well-tested"), check each claim against current code, and present findings grouped by source file. Then you offer to apply fixes one finding at a time.

This skill is a one-shot read-and-report. Do not modify any files until the user explicitly approves a specific fix in step 5.

## Step 0: Establish repo root

Run `git rev-parse --show-toplevel` to capture the repo root as `$REPO_ROOT`. Resolve all globs and paths relative to it. If the command fails (not a git repo), use the current working directory and warn the user that path-relative claim verification may be less reliable.

## Step 0.5: Determine audit scope

Resolve the audit scope from `$1` (trimmed). This is an optional positional argument; when absent the command behaves exactly as before — a whole-repo audit.

1. **`$1` is empty** — whole-repo audit. Use the default globs in Step 1 rooted at `$REPO_ROOT`, as today.
2. **`$1` resolves to an existing file** (`test -f`, relative to `$REPO_ROOT` or the current working directory) — audit **only that file**, even if its name does not match the default doc globs (the user named it deliberately, so target `docs/setup.md`, `CONTRIBUTING.md`, etc.). The file set is just this one path; skip Step 1 discovery and skip the >50-file count guard.
3. **`$1` resolves to an existing directory** (`test -d`) — run the Step 1 discovery globs **rooted at that directory** instead of `$REPO_ROOT`. Same documentation patterns, but only matches under the passed subtree.
4. **`$1` is any other non-empty value** — treat it as a glob. Expand it with a single `find`/`Glob`, and audit the matches. If it expands to nothing, stop and report "no files match `<arg>`".

The directory exclusions and the `.claude/skills/*/agents|references/` skip from Step 1 apply to every scope (cases 1, 3, 4). For an explicit single file (case 2), do not silently drop it for matching a default-skip pattern — but if it falls inside `node_modules/` or `.claude/worktrees/`, skip it, tell the user that path is out of scope, and stop.

## Step 1: Discover documentation files

If Step 0.5 selected an explicit file (case 2) or a glob match set (case 4), use that file set directly and skip the discovery globs below. Otherwise — an empty argument (case 1) or a directory argument (case 3) — run the discovery globs, rooted at `$REPO_ROOT` for an empty argument or at the passed directory for a directory argument.

Locate every file matching these globs, all relative to the scope root determined above:

- `.claude/commands/*.md`
- `.claude/skills/*/SKILL.md`
- `.claude/rules/*.md`
- `**/README.md`
- `**/CLAUDE.md`
- `**/*[Aa]rchitecture*.md`

Do not include files under `.claude/skills/*/agents/` or `.claude/skills/*/references/` — those are internal specialist prompts, not project documentation.

Exclude these directories from the scan: `node_modules`, `.git`, `dist`, `build`, `vendor`, `.next`, `target`, `out`, `coverage`, and `.claude/worktrees/` (git-worktree copies of the repo — auditing them duplicates findings from the real working tree). Also exclude the plugin's own scratch workspace `docs-workspace/` if present.

Use exactly one `find` invocation. Prune excluded directories with `-name <dir> -prune`, **not** `-path './<dir>'` — `-name` matches the directory at any depth, while `-path './node_modules'` matches only the top-level one and leaks nested copies (e.g. `plugins/*/node_modules`). A correct template, rooted at the scope root `$ROOT` from Step 0.5:

```bash
find "$ROOT" \( -name node_modules -o -name .git -o -name dist -o -name build -o -name vendor -o -name .next -o -name target -o -name out -o -name coverage -o -name docs-workspace -o -path '*/.claude/worktrees' \) -prune -o \
  -type f \( -path "$ROOT/.claude/commands/*.md" -o -path "$ROOT/.claude/skills/*/SKILL.md" -o -path "$ROOT/.claude/rules/*.md" -o -name 'README.md' -o -name 'CLAUDE.md' -o -iname '*architecture*.md' \) -print
```

Do not retry with alternate pruning syntax to "double-check" — if the first result set looks wrong, examine it rather than re-running an equivalent query. If the total file count exceeds 50, list the files, then call the **AskUserQuestion** tool (`multiSelect: false`, header `Scope`) naming the count (e.g. "Audit 73 documentation files?") with three options:

- `Proceed (all)` — "Audit every listed file."
- `Narrow scope` — "Re-run against a narrower file/dir/glob — I'll ask which."
- `Skip directories` — "Exclude some directories — I'll ask which."

For the latter two, collect the specifics from the user's reply (or the Other field) and re-resolve the scope. Large doc sets become noisy and slow — confirming early is cheaper than auditing 200 files and overwhelming the user.

Report the count and a summary list before continuing.

## Step 2: Extract concrete claims per file

Issue the `Read` calls for all discovered files in a single message — they are independent, so reading them sequentially wastes a round-trip per file. For each discovered file, read the full content and extract claims into one of these categories. **Concrete only** — you are verifying facts about the current codebase, not auditing taste, opinions, or aspirational guidance.

| Category                        | What counts                                                                                                                                              | Examples                                                                                                       |
| ------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| **Tech stack / version**        | Specific tools, languages, frameworks, runtime versions, package managers                                                                                | "Node v22", "uses pnpm 10.x", "Go 1.22", "React 18", "TypeScript 5"                                            |
| **Paths / structure**           | Directory or file paths claimed to exist or hold specific contents                                                                                       | "Plugins live under `plugins/`", "see `src/auth/middleware.ts`", "manifest at `.claude-plugin/plugin.json`"    |
| **Commands / scripts / config** | package.json scripts, Makefile targets, CLI commands, env vars, config keys, hook names, settings flags                                                  | "`pnpm test`", "`make release`", "set `DEBUG=1`", "the `prepare-commit-msg` hook"                              |
| **Module / symbol refs**        | Functions, classes, types, exports, API endpoints, CLI subcommands                                                                                       | "the `AuthMiddleware` class", "`POST /api/users`", "`renderReport()` in `report.ts`"                           |
| **Cross-doc links**             | References to other docs that must exist                                                                                                                 | "see `ARCHITECTURE.md`", "details in `references/schemas.md`"                                                  |
| **Provenance / attribution**    | Text claimed to be copied, derived, or quoted verbatim from another named doc or section — the assertion is content _correspondence_, not mere existence | "tier semantics verbatim from `CLAUDE.md`", "mirrors `README` §2", "per the schema in `references/schemas.md`" |

**Skip** subjective prose (style preferences, design rationale, "we believe in X"), rhetorical examples ("for example, an auth middleware..."), pseudo-code, and anything inside fenced code blocks unless the surrounding sentence explicitly asserts the code as current project state. Code blocks often illustrate hypothetical examples; flagging them produces high false-positive rates.

For each claim, record:

- `file` — source markdown path
- `line` — line number (or range)
- `category` — one of the five above
- `claim_text` — verbatim quote of the statement (1 sentence max)
- `assertion` — the precise factual question to verify (e.g., "file `src/auth/middleware.ts` exists and exports `AuthMiddleware`")

Keep working notes in memory or a temp file — do not persist them to the repo.

## Step 3: Verify each claim

**Batch independent verifications.** Issue all verification calls for independent claims in the same message. Only serialize when a probe truly depends on a prior result (e.g., reading a file's contents after confirming it exists, or grepping a symbol inside a path the previous call located). A run that issues all probes one-at-a-time is doing it wrong — claim verification is embarrassingly parallel and the cheapest way to keep wall-clock down on repos with many docs.

Verification depends on category. Use the cheapest tool that answers definitively. For plain text/config files (`.nvmrc`, `.tool-versions`, `.python-version`) use the `Read` tool, not `Bash` — `cat`/`head`/`tail` are blocked by the `enforce-builtin-tools.sh` hook. For JSON, use `Bash(jq:*)`.

- **Tech stack / version** — read `.nvmrc`, `package.json` (`engines`, `packageManager`, dependency versions), `go.mod`, `pyproject.toml`, `Cargo.toml`, framework-specific config. Check whether the doc's claimed version matches.
- **Paths / structure** — `test -e` / `ls` / `Read`. For a directory claim, also check it contains roughly what the doc says (e.g., "plugins/ holds plugins" — confirm subdirs match the description).
- **Commands / scripts / config** — for `package.json` scripts, `jq -r '.scripts | keys[]' package.json`. For `Makefile` targets, grep `^<target>:` in `Makefile`. For env vars / config keys / hook names, grep for usage in source.
- **Module / symbol refs** — prefer LSP if available (`workspaceSymbol`, `goToDefinition`); fall back to `Grep` for symbol name with appropriate file-type filter. For API endpoints, grep route definitions.
- **Cross-doc links** — `test -e` on the resolved path.
- **Provenance / attribution** — locate the named source doc/section, then `Grep` the attributed text (or a distinctive phrase from it) inside that source. Absent or diverged ⇒ ⚠️ **stale**: the attribution is wrong or reversed. Watch for the common form where content was _moved out of_ the cited doc into the current one, leaving a backwards "verbatim from X" pointer.

For each claim, classify as:

- ✅ **accurate** — claim matches current state. Drop from the report.
- ❌ **inaccurate** — claim is contradicted by current state. Include in report with severity `error`.
- ⚠️ **stale** — claim was likely true once but is now off in detail (e.g., version drift "Node 18" → actually 22, or directory still exists but contents diverged). Severity `warning`.
- ❓ **unverifiable** — claim is too vague or refers to runtime state you can't check from source. Note in report with severity `info` and explain _why_ you couldn't verify, so the user can decide whether to ignore or rewrite the claim more precisely.

When a claim looks wrong, do one extra check before flagging: is there an alternate location or naming where the claim _would_ be true? E.g., a doc says "scripts in `package.json`" but it's really in a workspace's `package.json` — that's accurate, not a finding. False positives erode trust faster than missed findings.

## Step 4: Produce report

Output a markdown report grouped by file. Use exactly this structure:

```markdown
# Documentation audit report

**Repo:** `<repo-root>`
**Files audited:** N
**Findings:** X errors · Y warnings · Z info

## Summary

<2–4 sentence high-level read of the doc health — what's drifting most, what categories show up repeatedly, anything systemic. Skip if there are zero findings.>

---

## `<relative/path/to/file.md>`

### ❌ Error: <one-line claim summary>

- **Line:** 42
- **Claim:** > <verbatim quote>
- **Evidence:** <what you found that contradicts it — file path, command, version, etc.>
- **Suggested fix:** <concrete replacement text or "remove this claim">

### ⚠️ Warning: <one-line claim summary>

...

### ❓ Info: <one-line claim summary>

...

---

## `<next file>`

...
```

Order files by finding count, descending. Within a file, order findings by severity (error → warning → info), then by line number. If a file has no findings, omit it entirely — the user wants signal, not exhaustive coverage.

If there are zero findings across the repo, say so plainly and stop.

## Step 5: Offer fixes per finding

After presenting the report, offer fixes for each `error` and `warning` finding (skip `info` — those are advisory) via the **AskUserQuestion** tool.

Batch the findings into `AskUserQuestion` calls of up to four single-select questions (one per finding, `multiSelect: false`), each with a short header derived from the finding (e.g. `CLAUDE.md:42`) and two options — `Apply fix` ("Apply the suggested fix via Edit") and `Skip` ("Leave the claim unchanged"). Apply approved fixes via `Edit`. Batching the _questions_ is encouraged; do **not** batch-_apply_ — each finding keeps its own decision so the human checkpoint between detection and mutation stays intact.

Skip findings the user declines. After the last decision, summarize what was applied and what was deferred.

## Operating notes

- **Performance** — the scan is read-mostly. Use parallel `Glob`/`Read` calls in the same message wherever findings don't depend on each other. Avoid spawning subagents unless the file count exceeds ~30 and per-file extraction starts dominating turn time.
- **Prefer `Grep` over `Bash(grep:*)` for content and symbol searches** — `Grep` exits 0 on empty matches and avoids shell glob-expansion pitfalls (e.g., brace globs like `*.{ts,mjs}` against directories that hold none of those extensions return non-zero in `/bin/sh` and poison the call's exit code). Reserve `Bash(grep:*)` for flags `Grep` does not expose (`-l`, `-A`, `-B`, multi-step pipelines). When chaining several probes in a single `Bash` call, guard each subcommand that is allowed to find nothing with `|| true` so one empty match does not mark the whole call as failed.
- **Be conservative on aspirational docs** — sections labelled "Future work", "TODO", "Roadmap", "Design intent" describe intent, not current state. Skip them.
- **Quote, don't paraphrase** — when you cite a claim, quote it verbatim so the user can grep for it. Paraphrased claims hide the original wording and make fixes harder to apply.
- **Doc-vs-doc agreement** — if two docs make contradictory claims, both are findings. Don't pick a winner; flag both and let the user reconcile.
- **The user knows their repo better than you** — when in doubt, surface the ambiguity as `info` rather than hard-classifying as error.
