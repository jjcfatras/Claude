---
name: enrich-claude-md
description: Investigate the codebase for useful, durable, non-obvious facts not yet captured in CLAUDE.md (or .claude/rules/) and propose them as additions. Researches current best practices for what belongs in a CLAUDE.md / .claude/rules file via context7, then scans the repo (build/test/lint commands, project structure, conventions, environment quirks, gotchas), filters out what's obvious or already documented, and reports proposed additions grouped by target file with evidence and a paste-ready snippet — including proposing a new nested CLAUDE.md when a cluster of facts is specific to a subtree. Offers to apply each addition. Optionally scope to a directory or glob. Use when the user asks to enrich or update CLAUDE.md / .claude/rules, capture undocumented conventions or gotchas into project memory, or find facts worth adding to a memory file.
argument-hint: "[dir|glob]"
allowed-tools: Bash(find:*), Bash(ls:*), Bash(test:*), Bash(stat:*), Bash(jq:*), Bash(grep:*), Bash(rg:*), Bash(wc:*), Bash(git:*), Read, Edit, Write, Grep, Glob, Agent, AskUserQuestion, mcp__plugin_context7_context7__resolve-library-id, mcp__plugin_context7_context7__query-docs
model: opus
effort: high
---

Investigate the codebase for useful, durable, non-obvious facts that would help a future Claude session but aren't yet captured in the project's memory files (the root `CLAUDE.md`, directory-scoped nested `CLAUDE.md` files, and `.claude/rules/*.md`). This is the inverse of `/docs:audit-docs`: that command finds claims in the docs that are now _wrong_; this one finds facts in the _code_ that are missing from the docs and worth adding.

The bar is high on purpose. A memory file earns its value by being short and high-signal — every line a future session must read. So you are not transcribing the codebase; you are hunting for the handful of facts that are **durable** (won't churn next week), **non-obvious** (a fresh session would waste time or get it wrong without being told), and **project-specific** (not generic best practice the model already knows). Most of what you find will not clear that bar, and that's expected.

This skill is a one-shot investigate-and-report. Do not modify any files until the user explicitly approves a specific addition in Step 6.

## Step 0: Establish repo root and scope

Run `git rev-parse --show-toplevel` to capture the repo root as `$REPO_ROOT`. Resolve all paths relative to it. If the command fails (not a git repo), use the current working directory and warn the user that path-relative evidence may be less reliable.

Resolve the investigation scope from `$1` (trimmed) — an optional positional argument:

1. **`$1` is empty** — investigate the whole repo rooted at `$REPO_ROOT`.
2. **`$1` resolves to an existing directory** (`test -d`) — investigate only that subtree, but still read the _nearest enclosing_ memory files (the root `CLAUDE.md` and any `.claude/rules/`) so you don't re-propose facts already documented upstream.
3. **`$1` is any other non-empty value** — treat it as a glob, expand it with a single `find`/`Glob`, and investigate the matches. If it expands to nothing, stop and report "no files match `<arg>`".

The directory exclusions in Step 3 apply to every scope.

## Step 1: Research what belongs in a memory file (context7)

Do this once, up front — it shapes everything downstream. Before scanning the code, ground your sense of _what is worth documenting_ in current guidance rather than only your priors. Call `mcp__plugin_context7_context7__resolve-library-id` for Claude Code (its memory / `CLAUDE.md` / `.claude/rules` documentation), then `mcp__plugin_context7_context7__query-docs` on the resolved ID for what an effective `CLAUDE.md` and `.claude/rules/` file should contain, how they differ, when a directory-scoped (nested) `CLAUDE.md` is warranted over the root one, and what to leave out.

Use what comes back to build the **search taxonomy** that drives Steps 3–4 — the concrete categories of fact you'll hunt for and the kinds of content current guidance says to _omit_. Adapt the inline fallback taxonomy below to match it (add categories it emphasizes, drop ones it warns against).

If context7 has no relevant entry for Claude Code memory files, say so in one line and fall back to the inline taxonomy as-is. Don't burn repeated calls trying variant names — one resolve + one query is enough to orient; the codebase, not context7, is the source of the facts themselves.

**Fallback taxonomy — what tends to earn a place in a memory file:**

| Category                       | What counts                                                                          | Example finding                                                                             |
| ------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------- |
| **Commands / scripts**         | Non-obvious build/test/lint/format/run invocations, esp. ones with required flags    | "Build all Go helpers with `pnpm build:go`; a single helper via `make release` in its dir"  |
| **Project structure**          | Layout or module boundaries a newcomer would otherwise have to reverse-engineer      | "Plugins live under `plugins/<name>/`; each has a manifest at `.claude-plugin/plugin.json`" |
| **Conventions / patterns**     | A rule the code follows _consistently_ that isn't enforced by an obvious linter      | "Slash commands are one `.md` per file under `commands/`, named after the plugin"           |
| **Environment / setup quirks** | Pinned versions, required tools, ordering, or setup that fails silently if skipped   | "Node pinned in `.nvmrc`; pnpm is the only supported package manager"                       |
| **Gotchas / footguns**         | Things that will bite a session: write-locked paths, codegen, hooks that block edits | "`bin/*` prebuilts are write-locked by a PreToolUse hook — rebuild via `make release`"      |
| **Testing approach**           | How tests are actually run and where they live, when not obvious from a glance       | "No JS test suite; `pnpm test` runs the Go suite via `make -C .../code-review-helper`"      |

**What does _not_ earn a place** (filter these in Step 4): facts obvious from a glance at the code, generic best practices the model already knows, one-off details unlikely to recur, exhaustive file listings, and anything already stated in an existing memory file.

## Step 2: Map existing memory files

Discover the memory files already in scope so you never propose something that's already documented, and so each addition has a home with the right style. Read each in full.

Use one `find` invocation, pruning the same directories as Step 3:

```bash
find "$ROOT" \( -name node_modules -o -name .git -o -name dist -o -name build -o -name vendor -o -name .next -o -name target -o -name out -o -name coverage -o -name docs-workspace -o -path '*/.claude/worktrees' \) -prune -o \
  -type f \( -name 'CLAUDE.md' -o -name 'CLAUDE.local.md' -o -path '*/.claude/rules/*.md' \) -print
```

Note for each file: its path, the sections it already has, and its tone (terse table vs. prose, heading depth). Additions you propose later must slot into an existing section where one fits, and match that file's style — a snippet that reads like the surrounding lines is one the user can accept without reformatting.

Because `-name 'CLAUDE.md'` matches at any depth, this also surfaces **nested** `CLAUDE.md` files. Treat an existing nested `CLAUDE.md` as the preferred home for facts scoped to its subtree — routing there keeps the root lean and only loads those facts when a session touches that subtree.

`CLAUDE.local.md` is personal and gitignored. Read it (so you don't duplicate what it already covers), but only propose writing _to_ it if the user asks — by default, route shared facts to the committed `CLAUDE.md`.

## Step 3: Investigate the codebase

Using the Step 1 taxonomy, scan the code for documentation-worthy facts. Read the high-signal sources first — `package.json` scripts, `Makefile`, `.nvmrc`/`.tool-versions`, CI workflows, hook configs (`.claude/settings.json`, `hooks/`), top-level directory layout, and any obvious config that changes tool behavior — then sample representative source to confirm conventions hold _consistently_ rather than in one file.

**Batch independent reads.** Issue `Read`/`Grep` calls for unrelated sources in the same message; sequential single reads waste a round-trip each. On a large or unfamiliar repo (roughly >30 candidate files, or when you can't hold the layout in your head), spawn `Explore` subagents to fan out — one per area (build/CI, conventions, gotchas) — and collect their findings here. Investigation breadth is the whole point of this command; don't try to read everything yourself when fan-out is cheaper.

For each candidate fact, record:

- `fact` — the one-line statement you'd add
- `evidence` — `file:line` (or command output) that proves it, so the user can verify without trusting you
- `category` — one from the taxonomy
- `target` — which memory file + section it belongs in

## Step 4: Filter to what earns its place

This is where most candidates die — be ruthless, because a bloated memory file is worse than a short one. Drop a candidate if any of these hold:

- **Already documented** — it's stated (even loosely) in a file from Step 2.
- **Obvious from a glance** — a fresh session would infer it immediately from the code or filenames.
- **Generic best practice** — true of many projects; the model already knows it.
- **One-off / churny** — a detail unlikely to recur, or one that'll be stale next week.
- **Verbose** — it can't be said in a line or two without losing the signal.

**Secondary context7 check (optional, only when it changes the verdict):** if a candidate hinges on a framework or library — e.g. "tests use `<framework>`'s global setup" — and you're unsure whether it's just the framework's documented default (drop it as generic) or a project-specific deviation (keep it, it's exactly the non-obvious kind), verify against current docs (`resolve-library-id` → `query-docs`). Also use it to get terminology and API names current so the addition isn't stale on arrival. Skip this for project-internal facts verifiable from the code alone — don't burn calls on claims that don't depend on external library behavior.

What survives is the report. If nothing survives, that's a valid and useful result — say so in Step 5 and stop.

## Step 5: Report proposed additions

### Choosing a target: root, nested, or a new file

Before writing the report, decide where each surviving fact belongs. Most route into an existing file from Step 2 — the root `CLAUDE.md`, an existing nested `CLAUDE.md`, or a `.claude/rules/` file. Sometimes the best home doesn't exist yet.

**Propose a new nested `CLAUDE.md`** (placed in the subtree it describes) when _all_ of these hold:

- **Clustered** — two or more surviving facts are specific to the same subtree (a package, app, service, or plugin dir) and would be noise to sessions working elsewhere.
- **Real boundary** — the subtree is a meaningful, stable unit with its own build/deps/conventions, not an arbitrary folder.
- **No home there yet** — there's no nested `CLAUDE.md` in that subtree already (if there were, Step 2 found it — route into it instead).
- **Keeps the root lean** — folding these into the root `CLAUDE.md` would burden every session with facts most don't need. A nested `CLAUDE.md` loads only when Claude touches that subtree, which is the whole point.

**Don't** spin up a new file for a single isolated fact (put it in the root, or drop it), for facts that apply repo-wide (root), or when it would scatter memory across many thin files — prefer the fewest memory files that keep the signal high. This is the same high bar as Step 4: a new file must earn its place.

Output a markdown report grouped by target memory file. Use this structure:

```markdown
# CLAUDE.md enrichment report

**Repo:** `<repo-root>`
**Scope:** <whole repo | dir | glob>
**Proposed additions:** N (across M files)

## Summary

<2–4 sentences: the biggest gaps you found, which categories dominate, anything systemic. Skip if zero additions.>

---

## `<relative/path/to/CLAUDE.md>` → _<section it slots into>_

### <category>: <one-line fact>

- **Evidence:** `path/to/source.ext:42` — <what there proves it>
- **Why it helps:** <one line: what a future session gets wrong or wastes time on without this>
- **Proposed snippet:**
  > <paste-ready text, sized and styled to match the target file>

### <category>: <next fact>

...

---

## `<next target file>`

...
```

For a file that **doesn't exist yet** — a proposed new nested `CLAUDE.md` or a new `.claude/rules/` file — mark the target as new and omit the "section it slots into" (there's no existing section to name):

```markdown
## `plugins/foo/CLAUDE.md` _(new nested file)_
```

Group every fact destined for a new file under that one target so the user reviews the whole proposed file at once, not scattered across the report.

Order target files by addition count, descending; within a file, group by section, then category. Quote evidence as `file:line` so the user can jump to it. If there are zero surviving additions, say so plainly and stop — no fixes to offer.

## Step 6: Offer to apply each addition

After the report, offer each proposed addition via the **AskUserQuestion** tool. Batch into calls of up to four single-select questions (one per addition, `multiSelect: false`), each with a short header derived from the target (e.g. `CLAUDE.md › Commands`) and two options — `Add` ("Insert this snippet via Edit") and `Skip` ("Don't add it"). Apply approved additions with `Edit`, inserting into the section named in the report and matching its style.

A proposed addition whose best home is a file that doesn't exist yet — a new `.claude/rules/` file or a new nested `CLAUDE.md` — is a special case: only create it (via `Write`) if the user approves, and confirm the path/location with them first. When several approved additions target the same new file, create it once containing exactly the approved facts (one coherent file), rather than one file per fact. Batching the _questions_ is encouraged; do **not** batch-_apply_ — each addition keeps its own decision so the human checkpoint between proposal and mutation stays intact.

After the last decision, summarize what was added and what was deferred.

## Operating notes

- **Be a skeptic about your own findings** — before proposing a fact, ask "would a competent engineer reading this repo for the first time genuinely benefit, or is this noise?" When unsure, drop it. Missed additions cost nothing; bad ones erode trust in the memory file and get the whole thing ignored.
- **Prefer `Grep` over `Bash(grep:*)` for content searches** — `Grep` exits 0 on empty matches and avoids shell glob-expansion pitfalls. Reserve `Bash(grep:*)` for flags `Grep` doesn't expose (`-l`, `-A`, `-B`, pipelines). When chaining probes in one `Bash` call, guard each subcommand allowed to find nothing with `|| true` so one empty match doesn't fail the whole call.
- **Use `jq` for `package.json`** — `jq -r '.scripts | keys[]' package.json` to enumerate scripts; read plain version files (`.nvmrc`, `.tool-versions`) with `Read`, not `cat` (blocked by the `enforce-builtin-tools.sh` hook).
- **Match the house style** — a snippet that reads like the lines around it gets accepted; one that doesn't gets reformatted or rejected. Mirror table-vs-prose, heading depth, and terseness of the target file.
- **Don't restate the obvious to pad the report** — a short report with three real additions beats a long one padded with facts the user already knows.
- **Confirm scope before a full-repo pass** — this is a whole-repo scan. When auto-invoked mid-task rather than run explicitly, confirm the intended scope with the user before scanning the entire repo. Keep working notes in memory or a temp file; do not persist them to the repo.
