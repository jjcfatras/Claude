---
name: cherry-pick
description: Cherry-pick commits from another branch with conflict resolution. Use when the user asks to "cherry-pick" commits, "bring/port a commit from another branch", or "apply commit X here".
argument-hint: <source-branch> [commit-sha or sha1..sha2]
allowed-tools: Bash(git *), Bash(rg *), Read, Edit, AskUserQuestion
model: opus
effort: high
---

Cherry-pick one or more commits from a source branch into the current branch, automatically resolving any merge conflicts.

Arguments: `$ARGUMENTS`. Split on whitespace: the first token is the **source branch** (`<source>` below); any remaining tokens are the **commit spec** (`<commits>` below) — one or more SHAs, or a single `sha1..sha2` range. If there is no first token, ask for the source branch.

Run shell work as `git` or `rg` commands and view files with `Read` (not `cat`, `sed -n`, or `ls`) — those are what `allowed-tools` pre-approves.

## Step 0: Validate preconditions

1. Run `git status --porcelain` to check for uncommitted changes. If the working tree is dirty, **stop** and tell the user to commit or stash changes first.
2. Run `git branch --show-current` to confirm the current branch. Display it and state that cherry-picked commits will be applied here.
3. Verify the source branch `<source>` exists by running `git rev-parse --verify <source>`. If it does not exist, try `origin/<source>`. If neither exists, stop and report the error.

## Step 1: Determine which commits to cherry-pick

**If a specific commit or range is provided (`<commits>` is non-empty):**

- If `<commits>` contains `..` (e.g., `abc123..def456`), it is a range, **inclusive of both endpoints**: the user means "from `abc123` through `def456`", but git's `abc123..def456` excludes `abc123`. Split on `..` into `<start>` and `<end>`, then run `git log --oneline --reverse <start>^..<end>` to list the commits in order. If `<start>` already ends in `^` or `~<n>` (git's own inclusive idiom), run `git log --oneline --reverse <commits>` unchanged so the parent isn't added twice.
- If multiple space-separated SHAs are provided, collect all of them. Verify each exists with `git rev-parse --verify`.
- If a single SHA is provided, verify it exists and use it.

**If no specific commits are provided (only `<source>` given):**

- Run `git log --oneline -20 <source>` to show the 20 most recent commits on the source branch.
- Present the list and ask which commits to cherry-pick. Accept commit SHAs, a range, or a count from the top (e.g., "the last 3").

## Step 2: Check relevance, then confirm

Before asking, check that each commit still fits this branch. One Bash call can cover every commit:

- `git show --name-status --format='=== %h %s' <sha>`: the subject plus every path the commit adds (`A`), modifies (`M`), or deletes (`D`).
- `git cherry -v HEAD <sha> <sha>^`: a leading `-` means an equivalent patch is already on the current branch.
- `git ls-files -- <every M path from above>`: prints only the paths that still exist here. A missing one was deleted or moved on this branch. `git log --format='%h %s' -1 HEAD -- <path>` names the commit that removed it. If that prints nothing, the path never existed here, so look for a renamed copy with `rg --files | rg <file-stem>`.

Flag each commit that is already applied, and each commit that adds or modifies a missing path. Picking one of those resurrects a stale file. That happens silently when the pick applies cleanly (e.g. the commit that first added the file), because Step 4d's deletion prompt only fires on a conflict.

Display:

- Current branch (target)
- Source branch
- Number of commits to apply
- List of commits (short SHA + message), with a one-line reason beside each flagged one

Then call the **AskUserQuestion** tool to get permission to proceed. Use a single question (`multiSelect: false`) with header `Proceed`, a question naming the number of commits and the target branch (e.g. "Apply 3 commit(s) to `main`?"), and these options:

- `Drop stale`: offer it only when something was flagged, list it first, and name the flagged commits and paths in the question. Description: "Skip commits that are already applied or only touch flagged paths; apply the rest without the flagged paths."
- `Proceed` — "Apply the listed commit(s) to the current branch."
- `Cancel` — "Abort without changing anything."

If the user selects `Cancel` (or answers with their own equivalent), stop immediately without running any `git cherry-pick`.

## Step 3: Cherry-pick commits one by one

For each commit (in chronological order, oldest first):

1. Display: "Applying commit N of M: `<sha>` `<message>`"
2. Run `git cherry-pick <sha>`
3. Check the exit code:
   - **If successful (exit 0):** Report success and move to next commit.
   - **If conflicts (exit non-zero):** Proceed to Step 4.

> **Pre-commit hooks:** a clean `git cherry-pick` commits internally and does **not** run the target repo's pre-commit hook. `git cherry-pick --continue` (Step 4e) does, as does any explicit `git commit`.
>
> - Keep this pick → resolve → `--continue` flow. Using `git cherry-pick -n` followed by `git commit` pays for a hook run even on picks that would have applied cleanly.
> - If the hook is slow or noisy (e.g. a monorepo linting/testing many projects), run `git cherry-pick --continue` **bare**, with no pipe, redirect, or `echo "exit=..."` suffix, using `run_in_background: true`. The completion notice then reports git's real exit code, and the full output lands in the task's output file. Wrapping the command loses the exit code: the Bash tool's shell may be zsh, where `${PIPESTATUS[0]}` expands to nothing.
> - Search that output file with `rg` for the hook's summary or failure lines instead of `tail`-ing it, because a hook's last lines are often a later step, not the summary.
> - Never `Read` the output file whole: a verbose hook can exceed the Read size cap.

## Step 4: Resolve conflicts

When a cherry-pick produces conflicts:

### 4a: Identify all conflicting files

Run `git status` and identify files marked as "both modified", "both added", "deleted by us", "deleted by them", or any other conflict state.

### 4b: Understand the context

Run `git log --format="%B" -1 <sha-being-cherry-picked>` to read the full commit message of the commit being applied — this explains the intent of the change.

### 4c: Resolve each conflicting file

Resolve all conflict hunks in a given file in a single Read + Edit pass: read the full file once (not marker-by-marker) so you see every hunk before editing, rather than resolving one hunk and re-searching for the next.

For each conflicting file:

1. **Read the file** using the Read tool to see full contents including conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`).

2. **Understand both sides:**
   - Between `<<<<<<< HEAD` and `=======` is the current branch version.
   - Between `=======` and `>>>>>>> <sha>` is what the cherry-picked commit introduces.
   - Run `git show <sha> -- <filepath>` to see the full file after the cherry-picked commit for additional context.

3. **Resolve the conflict intelligently:**
   - If changes are in different logical sections (different functions, different import groups), keep both.
   - If changes modify the same code, understand the intent of each side:
     - The current branch version reflects the evolution of code on this branch.
     - The cherry-picked version reflects a specific fix or feature being ported.
     - Apply the cherry-picked change's intent on top of the current branch's code structure.
   - If one side adds new code and the other modifies existing code, integrate both.
   - If both sides modify the same line differently, combine if logically compatible, or prefer the cherry-picked change while preserving current-branch changes not in conflict.
   - For import statements or dependency lists: include all from both sides, removing exact duplicates.
   - **Never leave conflict markers in the file.**

4. **Edit the file** using the Edit tool to write the resolved version, removing all conflict markers.

5. **Stage the resolved file:** Run `git add <filepath>`.

### 4d: Handle special conflict types

- **"deleted by us" or "deleted by them":** If the user chose `Drop stale` in Step 2 and this file is one of the flagged paths, keep it deleted (`git rm <file>`) without asking again. Otherwise call **AskUserQuestion** (`multiSelect: false`) with header `Deletion`, naming the file in the question, and options `Keep deletion` ("Leave the file deleted") vs `Restore file` ("Restore it with the cherry-picked changes"). Act on the selection.
- **Binary file conflicts:** Do not attempt to merge. Call **AskUserQuestion** (`multiSelect: false`) with header `Binary`, naming the file in the question, and options `Keep current` ("Keep the current-branch (HEAD) version") vs `Take incoming` ("Use the cherry-picked version"). Act on the selection.

### 4e: Continue the cherry-pick

After all conflicts in the current commit are resolved and staged:

1. Run `git diff --check` to confirm no conflict markers remain — it prints `path:line: leftover conflict marker` for any it finds and exits non-zero. Run `git diff --name-only --diff-filter=U` to list any files git still considers unmerged.
2. Run `git cherry-pick --continue` to finalize the commit. This runs the target repo's pre-commit hook; if that hook is slow or verbose, run the command bare in the background and `rg` its output file (see the note in Step 3).
3. If this fails, check `git status` for remaining issues.

## Step 5: Verify the result

After all commits have been applied:

1. Run `git log --oneline -<N+3>` (where N is the number of cherry-picked commits) to show recent history.
2. Run `git diff HEAD~<N>..HEAD --stat` to show a summary of all changes introduced.
3. If any conflicts were resolved, list the files where conflicts occurred and briefly summarize how each was resolved.
4. Report final status: how many commits applied, how many had conflicts, whether all resolved successfully.

## Error handling

- If `git cherry-pick --continue` fails after resolution, run `git status` to diagnose.
- If a cherry-pick cannot be completed, call **AskUserQuestion** (`multiSelect: false`) with header `On conflict` and options `Abort` ("Run `git cherry-pick --abort` to restore the original state") vs `Keep resolving` ("Continue manual conflict resolution"). On `Abort`, run `git cherry-pick --abort`.
- If a commit is empty after cherry-pick (already applied), run `git cherry-pick --skip` and inform the user.
- If cherry-pick fails for reasons other than conflicts, report the error, then call **AskUserQuestion** (`multiSelect: false`) with header `On error` and options `Skip` ("Run `git cherry-pick --skip` to drop this commit and continue with the rest") vs `Abort` ("Run `git cherry-pick --abort` to restore the original state"). Act on the selection.
