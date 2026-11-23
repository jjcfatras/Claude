# Conflict resolution

Shared by `cherry-pick`, `merge` and `rebase`. The calling skill tells you what each side of the markers holds, which commands give the intent, which side wins a same-line clash, how to handle deletions and binaries, and how to continue. This file covers the rest.

## Resolve in this context

Resolve every conflicting file directly in this context using the `Read` and `Edit` tools. Do not spawn a subagent: delegating conflict resolution breaks the per-context Read-before-Edit guard and causes cascading `File has not been read yet` errors when the main thread later edits files the subagent touched.

## Per-file procedure

Resolve all conflict hunks in a file in a single Read + Edit pass: read the full file once (not marker-by-marker) so you see every hunk before editing, rather than resolving one hunk and re-searching for the next.

For each conflicting file:

1. **Read the file** with the Read tool to see its full contents, including the conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`).
2. **Understand both sides** as the calling skill describes them, using its context commands.
3. **Resolve the conflict:**
   - If the changes are in different logical sections (different functions, different import groups), keep both.
   - If one side adds new code and the other modifies existing code, integrate both.
   - If both sides change the same code, apply the calling skill's precedence rule.
   - For import statements or dependency lists, include everything from both sides and remove exact duplicates.
   - **Never leave conflict markers in the file.**
4. **Edit the file** with the Edit tool to write the resolved version, removing all conflict markers.
5. **Stage it:** `git add <filepath>`.

## Check before continuing

After every conflicting file is resolved and staged:

1. Run `git diff --cached --check`. It compares the index with HEAD, so it sees markers in files already `git add`ed; plain `git diff --check` does not. Only `path:line: leftover conflict marker` lines matter. Whitespace warnings from the incoming changes don't block.
2. Run `git diff --name-only --diff-filter=U` to list any files git still considers unmerged.

Fix anything either command reports before running the calling skill's continue command.

## Pre-commit hooks

`git cherry-pick --continue`, `git merge --continue` and any explicit `git commit` run the repo's pre-commit hook. `git rebase --continue` does not.

- If the hook is slow or noisy (e.g. a monorepo linting or testing many projects), run the continue command **bare**, with no pipe, redirect or `echo "exit=..."` suffix, using `run_in_background: true`. The completion notice then reports git's real exit code, and the full output lands in the task's output file. Wrapping the command loses the exit code: the Bash tool's shell may be zsh, where `${PIPESTATUS[0]}` expands to nothing.
- Search that output file with `rg` for the hook's summary or failure lines instead of `tail`-ing it, because a hook's last lines are often a later step, not the summary. Never `Read` the output file whole: a verbose hook can exceed the Read size cap.
- If the hook rejects the commit, read its output, fix the reported issues (type errors, lint failures, failed tests, duplicate declarations left by the resolution), re-stage the corrected files, and retry. **Never pass `--no-verify`**: the hook is catching bugs the resolution introduced.
