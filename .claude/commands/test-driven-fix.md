---
description: Autonomously fix failing tests by iterating patch → test → revert-on-regression, with a 10-iteration cap
argument-hint: <spec-path-or-bug-description>
allowed-tools: Bash(git *), Bash(pnpm *), Bash(npm *), Bash(yarn *), Bash(pytest *), Bash(cargo *), Bash(go *), Bash(make *), Bash(ast-grep *), Bash(jq *), Bash(mktemp *), Bash(ls *), Bash(wc *), Bash(find *), Read, Edit, Write, Grep, Glob, TaskCreate, TaskUpdate, TaskList
model: opus
effort: high
---

Run an autonomous debug-loop against a failing spec or bug description. The test suite is the oracle: never prompt for mid-loop confirmation. Safety envelope: a baseline `git stash` preserves the starting tree, each patch is reverted the moment it regresses a previously-green test, the loop is hard-capped at 10 iterations, and a commit only lands on a fully-green run. On exhaustion, best-effort patches stay in the working tree and the baseline stash is preserved so the user can diff against it.

**Shell Command Safety** (applies to every step — see `~/.claude/references/shell-safety.md` for the full rules):

- **Never include `#` comments in bash commands** — use the Bash tool's `description` parameter instead.
- **Never pass markdown or JSON as inline bash arguments** — write them to files with the Write tool, then reference the files.
- **Never use heredocs (`<<`, `<<<`)** — use the Write tool.
- **Never use `sed`, `awk`, `du`, or `grep` as bash commands** — use the Read tool, the Grep tool, or `jq`.
- **Never combine curly braces (`{`, `}`) with quote characters in the same bash command.**
- **Never use `$()` command substitution** — save intermediate results to temp files with separate commands, then reference those files.
- **Never use output redirection (`>`, `>>`)** — use the Write tool to create files.
- **Never use adjacent/consecutive quote characters** (`'"`, `"'`, `''`).
- **Never use ANSI-C quoting (`$'...'`).**
- **Keep every Bash command on a single line** — chain with `&&` or `|`.
- **Never use `jq -f`, `--rawfile`, or `--slurpfile`** — build JSON with the Write tool.

Follow these steps precisely. Do not ask the user for confirmation between iterations.

## Step 0: Setup and input parsing

1. Run `mktemp -d /tmp/tdf-XXXXXX` to create a temp directory. Store the path as `$TMPDIR`.
2. Parse `$ARGUMENTS`:
   - If `ls $ARGUMENTS` succeeds (i.e., the string resolves to an existing file), treat it as a **spec path** and record `INPUT_KIND=path`, `SPEC_PATH=$ARGUMENTS`.
   - Otherwise, treat it as a **free-text bug description** and record `INPUT_KIND=text`, `BUG_DESC=$ARGUMENTS`.
3. Run `git rev-parse HEAD` — record as `BASELINE_SHA`.
4. Run `git status --porcelain` and use `wc -l` to count modified/untracked lines. Save the count to `$TMPDIR/dirty-count.txt` via the Bash tool output (do not use `>`).
5. If the dirty count is non-zero, run `git stash push -u -m tdf-baseline` and then `git stash list` and Read the output to capture the resulting stash ref (first line, format `stash@{0}`). Record as `BASELINE_STASH`. If the dirty count is zero, set `BASELINE_STASH=none` and skip stash management at the end.
6. **Detect commands** by reading repo metadata (use the Read tool, not bash):
   - If `package.json` exists, Read it and inspect `scripts`. Prefer `test`, `lint`, `typecheck` (or `tsc`) keys. Record the package manager by checking for `pnpm-lock.yaml`, `yarn.lock`, or `package-lock.json` (Read each; whichever exists wins; default to `npm`). Build `TEST_CMD`, `LINT_CMD`, `TYPECHECK_CMD` as `<pm> run <script>`. If `typecheck` is missing, fall back to `<pm> exec tsc --noEmit` when `tsconfig.json` exists.
   - Else if `pyproject.toml` or `pytest.ini` exists: `TEST_CMD=pytest`, `LINT_CMD=ruff check .` (skip if `ruff` absent via Glob), `TYPECHECK_CMD=mypy .` (skip if `mypy` absent).
   - Else if `Cargo.toml` exists: `TEST_CMD=cargo test`, `LINT_CMD=cargo clippy -- -D warnings`, `TYPECHECK_CMD=cargo check`.
   - Else if `go.mod` exists: `TEST_CMD=go test ./...`, `LINT_CMD=go vet ./...`, `TYPECHECK_CMD=go build ./...`.
   - Else if `Makefile` exists and exposes `test`/`lint`/`typecheck` targets: use `make test`, etc.
   - If no stack can be detected, abort with a clear error.
7. Write `$TMPDIR/state.json` via the Write tool with the shape:

```json
{
  "input_kind": "path|text",
  "spec_path": "...",
  "bug_desc": "...",
  "baseline_sha": "...",
  "baseline_stash": "stash@{0}|none",
  "test_cmd": "...",
  "lint_cmd": "...",
  "typecheck_cmd": "...",
  "iteration": 0,
  "max_iterations": 10,
  "failures": [],
  "applied_patches": [],
  "reverted_patches": [],
  "resolved_failure_ids": []
}
```

## Step 1: Baseline run and failure parsing

1. Run `TEST_CMD` via the Bash tool. The tool captures stdout/stderr — do not redirect. Write the captured output to `$TMPDIR/test.log` using the Write tool.
2. Run `LINT_CMD`; Write output to `$TMPDIR/lint.log`.
3. Run `TYPECHECK_CMD`; Write output to `$TMPDIR/typecheck.log`.
4. Parse each log into failure objects using the Read + Grep tools. Do not use `grep`/`sed`/`awk` as bash commands.
   - Each failure object: `{ "id": "f-<n>", "kind": "test|lint|type", "file": "...", "line": 0, "symbol": "...", "message": "...", "raw": "..." }`.
   - Deduplicate by `(file, line, message)`.
5. Write the resulting array to `$TMPDIR/failures.json` via the Write tool. Also copy it into `state.json` under `failures`.
6. If the array is empty, report `no failures detected — baseline is already green` and stop. Pop the baseline stash if one was created.
7. For each failure, call TaskCreate with:
   - `subject`: `Fix <kind>: <file>:<line> — <short message>`
   - `description`: the `raw` field plus the `symbol` if known
   - `activeForm`: `Resolving <file>:<line>`
     Store the returned task IDs back into `state.json` alongside their failure `id`s.

## Step 2: Iteration loop (hard cap: 10)

Repeat the sub-steps below until every failure is resolved OR `iteration == 10`. Increment `state.json#iteration` at the top of each pass and re-Write `state.json`. Never prompt the user between iterations.

### 2a — Pick next failure

- Filter to unresolved failures (`failure.id` not in `resolved_failure_ids`).
- Order: `type` > `test` > `lint`. Within a kind, prefer failures with a concrete `symbol` and a non-empty `file:line`.
- If no unresolved failures remain, go to Step 3.
- TaskUpdate the chosen failure's task: `status=in_progress`, `activeForm` reflects the current approach (e.g., `Iteration 3: patching handleClick off-by-one`).

### 2b — Locate the symbol

- Prefer `ast-grep` for structural queries: e.g., `ast-grep --pattern 'function $F($$$)' path/to/file`. Use this when the failure references a function, class, or method by name.
- Fall back to the Grep tool when `ast-grep` is unavailable or when the search is textual (error strings, config keys).
- If the file/line was provided in the failure, skip search and go straight to the Read step.

### 2c — Read surrounding context

- Use the Read tool to load the target file ±40 lines around the symbol. If the file is small (<200 lines), Read the whole file.
- If the failure crosses files (e.g., a broken import), Read each referenced file similarly.

### 2d — Propose and apply the minimal patch

- Keep the change as small as possible — one concern, one edit.
- Apply with the Edit tool.
- Append an entry to `state.json#applied_patches`:

```json
{
  "failure_id": "f-3",
  "iteration": 4,
  "file": "src/foo.ts",
  "approach": "short description"
}
```

### 2e — Narrow re-run

- Infer a filter pattern from the failure's `file` and/or `symbol`:
  - Jest/Vitest: `--testPathPattern <file>` or `-t <symbol>`.
  - pytest: `<file>::<symbol>` or `-k <symbol>`.
  - cargo: `cargo test <symbol>`.
  - go: `go test ./<dir>/... -run <symbol>`.
  - If the runner doesn't support filtering, skip straight to the full run in 2f.
- Run the narrow command. Write output to `$TMPDIR/narrow-<iteration>.log`.
- If the targeted test still fails: revert with `git checkout -- <file>` for each file touched this iteration. Append the failed approach to `state.json#reverted_patches`. If two distinct approaches have already been tried and reverted for this failure, add its id to `resolved_failure_ids` with a `skipped` marker and TaskUpdate the task description noting exhaustion of attempts. Continue the loop.

### 2f — Full re-run

- Run `TEST_CMD`, `LINT_CMD`, `TYPECHECK_CMD` in sequence. Write each to `$TMPDIR/full-<iteration>-{test,lint,typecheck}.log` via the Write tool.
- Parse failures the same way as Step 1. Produce a new array, Write it to `$TMPDIR/failures-<iteration>.json`.

### 2g — Regression check

- Compare the new failure set to `$TMPDIR/failures.json` (the baseline). A regression is any `(file, line, message)` present in the new set but not in the baseline.
- If any regression is detected:
  - Run `git checkout -- <file>` for every file touched this iteration.
  - Append the failed approach to `state.json#reverted_patches` with `reason: regression`.
  - Remove the matching entry from `applied_patches`.
  - TaskUpdate the failure's task: stays `in_progress`; update the `description` to note the rollback and what regressed.
  - Continue the loop (do not mark the failure resolved).
- Else mark resolved failures: any baseline failure not present in the new set gets its id added to `resolved_failure_ids`, and its task gets `TaskUpdate status=completed`.

### 2h — Success check

- If `resolved_failure_ids` covers every non-`skipped` baseline failure AND the new failure set is empty, go to Step 3.
- Else loop back to 2a.

## Step 3: Commit on full success

1. Run `git diff --name-only` and Read the output to list touched files.
2. Derive the scope: longest common leading path segment of the touched files (e.g., `src/auth` if all files are under it; otherwise the single dir name; fall back to the first file's top-level dir).
3. Build the one-line subject: `fix(<scope>): <summary under 72 chars>`. Derive `<summary>` from the resolved failures (e.g., `handle off-by-one in paginated cursor`).
4. Write the full commit message to `$TMPDIR/commit-msg.txt` using the Write tool:

```
fix(<scope>): <summary>

<root-cause one-liner>

Tests moved red → green:
- <file>:<line> <short message>
- ...

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

Keep every body line ≤ 100 characters. 5. `git add <paths>` for each touched file (space-separated on one line). 6. `git commit -F /tmp/tdf-XXXXXX/commit-msg.txt` (use the real `$TMPDIR` path). 7. If a baseline stash was created and is still present, run `git stash drop <BASELINE_STASH>`. 8. Run `git rev-parse HEAD` and Read the output; report the new commit SHA and the subject line.

## Step 4: Exhaustion (loop hit 10 iterations without full green)

1. Do **not** revert applied patches. Leave the working tree in its best-effort state.
2. Do **not** pop or drop `BASELINE_STASH`. The user can run `git stash show -p <ref>` to compare.
3. For every failure still `in_progress`, TaskUpdate with a final `description` noting the approaches tried and why each was reverted (pull from `state.json#reverted_patches`).
4. Report a summary to the user:
   - Iterations used: 10
   - Failures resolved: N of M
   - Failures remaining: list of `file:line — message`
   - Baseline stash ref (so the user can diff)
   - Files touched during best-effort work
   - Temp dir path (`$TMPDIR`) for log inspection
