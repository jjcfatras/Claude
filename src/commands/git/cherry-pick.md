---
description: Cherry-pick commits from another branch with conflict resolution
argument-hint: <source-branch> [commit-sha or sha1..sha2]
allowed-tools: Bash(git *), Read, Edit, Grep
model: sonnet
effort: high
---

Cherry-pick one or more commits from a source branch into the current branch, automatically resolving any merge conflicts.

## Step 0: Validate preconditions

1. Run `git status --porcelain` to check for uncommitted changes. If the working tree is dirty, **stop** and tell the user to commit or stash changes first.
2. Run `git branch --show-current` to confirm the current branch. Display it and state that cherry-picked commits will be applied here.
3. Verify the source branch `$1` exists by running `git rev-parse --verify $1`. If it does not exist, try `origin/$1`. If neither exists, stop and report the error.

## Step 1: Determine which commits to cherry-pick

**If a specific commit or range is provided (`$2` is non-empty):**

- If `$2` contains `..` (e.g., `abc123..def456`), it is a range. Run `git log --oneline --reverse $2` to list the commits in order.
- If multiple space-separated SHAs are provided, collect all of them. Verify each exists with `git rev-parse --verify`.
- If a single SHA is provided, verify it exists and use it.

**If no specific commits are provided (only `$1` given):**

- Run `git log --oneline -15 $1` to show the 15 most recent commits on the source branch.
- Present the list and ask which commits to cherry-pick. Accept commit SHAs, a range, or a count from the top (e.g., "the last 3").

## Step 2: Show summary before proceeding

For each commit to be cherry-picked, run `git log --format="%h %s" -1 <sha>`.

Display:

- Current branch (target)
- Source branch
- Number of commits to apply
- List of commits (short SHA + message)

Ask the user to confirm before proceeding.

## Step 3: Cherry-pick commits one by one

For each commit (in chronological order, oldest first):

1. Display: "Applying commit N of M: `<sha>` `<message>`"
2. Run `git cherry-pick <sha>`
3. Check the exit code:
   - **If successful (exit 0):** Report success and move to next commit.
   - **If conflicts (exit non-zero):** Proceed to Step 4.

## Step 4: Resolve conflicts

When a cherry-pick produces conflicts:

### 4a: Identify all conflicting files

Run `git status` and identify files marked as "both modified", "both added", "deleted by us", "deleted by them", or any other conflict state.

### 4b: Understand the context

Run `git log --format="%B" -1 <sha-being-cherry-picked>` to read the full commit message of the commit being applied — this explains the intent of the change.

### 4c: Resolve each conflicting file

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

- **"deleted by us" or "deleted by them":** Ask the user whether to keep the deletion or restore the file with the cherry-picked changes.
- **Binary file conflicts:** Do not attempt to merge. Ask the user which version to keep.

### 4e: Continue the cherry-pick

After all conflicts in the current commit are resolved and staged:

1. Use the Grep tool to search for `<<<<<<<` across the repository to verify no conflict markers remain.
2. Run `git cherry-pick --continue` to finalize the commit.
3. If this fails, check `git status` for remaining issues.

## Step 5: Verify the result

After all commits have been applied:

1. Run `git log --oneline -<N+3>` (where N is the number of cherry-picked commits) to show recent history.
2. Run `git diff HEAD~<N>..HEAD --stat` to show a summary of all changes introduced.
3. If any conflicts were resolved, list the files where conflicts occurred and briefly summarize how each was resolved.
4. Report final status: how many commits applied, how many had conflicts, whether all resolved successfully.

## Error handling

- If `git cherry-pick --continue` fails after resolution, run `git status` to diagnose.
- If the user wants to abort, run `git cherry-pick --abort` to restore the original state.
- If a commit is empty after cherry-pick (already applied), run `git cherry-pick --skip` and inform the user.
- If cherry-pick fails for reasons other than conflicts, report the error and ask whether to skip or abort.
