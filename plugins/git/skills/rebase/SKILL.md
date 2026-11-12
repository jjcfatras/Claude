---
name: rebase
description: Rebase the current branch onto another branch with conflict resolution; offers a force-with-lease push when the rebase rewrote already-pushed commits. Use when the user asks to "rebase onto main", "rebase this branch", "rebase on top of X", or "replay my commits onto another branch". Not for interactive rebase, squash, reword, or --onto.
argument-hint: <base-branch>
allowed-tools: Bash(git *), Bash(rg *), Read, Edit, AskUserQuestion
model: opus
effort: high
---

Rebase the current branch onto a base branch: replay its commits on top of the base, automatically resolving any conflicts. Plain `git rebase <base>` only — no `--onto`, no squash or reorder.

Arguments: `$ARGUMENTS`. The first whitespace-separated token is the **base branch**, written `<base>` below. If there is none, ask which branch to rebase onto. If it starts with `-`, stop: this skill takes a branch name, not rebase flags.

Run shell work as `git` or `rg` commands and view files with `Read` (not `cat`, `sed -n`, or `ls`) — those are what `allowed-tools` pre-approves.

## Step 0: Validate preconditions

1. Run `git status` (long form, before anything else — a stopped rebase looks like a dirty tree or a detached HEAD to the checks below):
   - If it says `You are currently rebasing`, call **AskUserQuestion** (`multiSelect: false`) with header `In progress` and options `Continue it` ("Resume the stopped rebase") vs `Abort it` ("Run `git rebase --abort`, then start this rebase").
     - On `Abort it`, run `git rebase --abort` and restart Step 0.
     - On `Continue it`, recover the run's values: get the paths with `git rev-parse --git-path rebase-merge/head-name` (and `rebase-merge/orig-head`, `rebase-merge/onto`; use `rebase-apply/` instead if `rebase-merge/` doesn't exist) and `Read` each. `head-name` is `refs/heads/<current>`, `orig-head` is `<old-tip>`, `onto` is `<base-ref>`. Then go to Step 4 if `git diff --name-only --diff-filter=U` lists files, else to Step 4e.2.
   - If a merge or cherry-pick is in progress, **stop** and tell the user to finish or abort it first.
2. Run `git status --porcelain` to check for uncommitted changes. If the working tree is dirty, **stop** and tell the user to commit or stash changes first.
3. Run `git branch --show-current` and store it as `<current>`. If it prints nothing, HEAD is detached: **stop** and tell the user to switch to a branch first.
4. Run `git fetch origin <name>`, where `<name>` is `<base>` without a leading `origin/`, so the remote-tracking ref is fresh. If `<name>` is not `<current>`, also run `git fetch origin <current>` as a **separate** command (a two-ref fetch fails entirely when one ref is missing, and `<current>` may never have been pushed); ignore its failure. If the base fetch fails (no `origin`, no such remote branch, offline), say so and continue with local refs.
5. Resolve the **base ref** (`<base-ref>` below):
   - If `<base>` is `<current>` (e.g. `/git:rebase main` while on `main`), use `origin/<base>` — the user means "replay my local commits on top of the remote". If `origin/<base>` doesn't exist, stop: there is nothing to rebase onto.
   - Otherwise use `<base>` if `git rev-parse --verify <base>` succeeds, else `origin/<base>`. If neither exists, stop and report the error.
6. **Stale check:** if `<base-ref>` is a local branch and `origin/<base>` also exists, run `git rev-list --count <base>..origin/<base>`. If it is non-zero, local `<base>` is missing that many commits from the remote — and a branch cut from it would otherwise look "already up to date". Call **AskUserQuestion** (`multiSelect: false`) with header `Base`, naming the count in the question, and options `Use origin/<base>` ("Rebase onto the remote branch, which has the N missing commit(s)"), `Use local <base>` ("Rebase onto local `<base>` as it is"), and `Cancel` ("Stop without changing anything"). Set `<base-ref>` from the answer, or stop on `Cancel`.

## Step 1: Determine what will be replayed

1. Record `<old-tip>` (`git rev-parse HEAD`). Step 5 uses it.
2. Run `git log --oneline --reverse --cherry <base-ref>...HEAD` to list the branch's commits, merges excluded. `+` lines will be replayed (N below is their count); `=` lines are already in the base by patch and rebase will drop them. Run `git rev-list --count HEAD..<base-ref>` for how many base commits the branch will land on top of.
3. Classify the rebase:
   - **Already up to date:** `git merge-base --is-ancestor <base-ref> HEAD` succeeds. Report this and stop — nothing to do.
   - **Fast-forward:** HEAD is an ancestor of `<base-ref>` (no commits to replay). The rebase only moves the branch pointer.
   - **Replay:** both sides have unique commits. The `+` commits get new SHAs.
4. Run `git log --merges --oneline <base-ref>..HEAD`. A plain rebase flattens merge commits: it drops each merge and replays only the non-merge commits. That is usually what the user wants (e.g. "merged main into my branch" commits), so just flag them for Step 2.
5. **Remote checks** (only when `git rev-parse --verify --quiet origin/<current>` succeeds and `<base>` is not `<current>`):
   - If `git rev-list --count <base-ref>..origin/<current>` is non-zero, the rebase rewrites commits already pushed to `origin/<current>`. Flag it: Step 6 offers the push.
   - If `git rev-list --count HEAD..origin/<current>` is non-zero, `origin/<current>` has commits this branch lacks (a push from elsewhere). Flag it: the user should pull them first, because Step 6's push will be refused.

## Step 2: Show summary before proceeding

Display:

- Current branch (the one being rebased)
- Base ref (and whether it resolved to local `<base>` or `origin/<base>`)
- Rebase type (fast-forward or replay) and how many base commits the branch will land on top of
- List of commits from Step 1 (short SHA + message), marking the `=` ones as "already in base, will be dropped"
- Each flag from Step 1, one line each: flattened merge commits, rewritten pushed commits, remote commits this branch lacks

Then call the **AskUserQuestion** tool to get permission to proceed. Use a single question (`multiSelect: false`) with header `Proceed`, a question naming N, the branch, and the base ref (e.g. "Rebase 3 commit(s) of `feature` onto `main`?"), and two options:

- `Proceed` — "Rebase onto `<base-ref>`."
- `Cancel` — "Abort without changing anything."

If the user selects `Cancel` (or answers with their own equivalent), stop immediately without running `git rebase`.

## Step 3: Run the rebase

Run `git rebase --no-rebase-merges --no-update-refs <base-ref>`. The two flags override any `rebase.rebaseMerges` / `rebase.updateRefs` config, so merges flatten as Step 2 said and no other local branch moves.

Check the exit code:

- **If successful (exit 0):** Report success. Skip to Step 5.
- **If non-zero:** if `git diff --name-only --diff-filter=U` lists files, proceed to Step 4. Otherwise the rebase failed for another reason — see Error handling.

> **No pre-commit hooks:** `git rebase` does not run the repo's pre-commit hook — not for clean picks, and not on `git rebase --continue`. Conflict resolutions are therefore never linted or tested by the hook. Step 5 says so in the final report.

## Step 4: Resolve conflicts

When a commit fails to replay:

Resolve every conflicting file directly in this context using the `Read` and `Edit` tools. Do not spawn a subagent (the `Agent` tool is intentionally not in this skill's `allowed-tools`) — delegating conflict resolution breaks the per-context Read-before-Edit guard and causes cascading `File has not been read yet` errors when the main thread later edits files the subagent touched.

**Ours and theirs are inverted in a rebase.** Git replays each commit on top of the base, so:

- `HEAD`, "ours", and `--ours` are the **base** plus the commits replayed so far.
- "theirs" and `--theirs` are the **branch's own commit** being replayed, named by `REBASE_HEAD`.
- "deleted by us" means the file was deleted on the base; "deleted by them" means the replayed commit deletes it.

### 4a: Identify all conflicting files

Run `git status` and identify files marked as "both modified", "both added", "deleted by us", "deleted by them", or any other conflict state. It also lists the commands done and the ones remaining.

### 4b: Understand the context

Run `git log -1 --format=%B REBASE_HEAD` to read the full message of the commit being replayed — this explains the intent of the change.

### 4c: Resolve each conflicting file

Resolve all conflict hunks in a given file in a single Read + Edit pass: read the full file once (not marker-by-marker) so you see every hunk before editing, rather than resolving one hunk and re-searching for the next.

For each conflicting file:

1. **Read the file** using the Read tool to see full contents including conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`).

2. **Understand both sides:**
   - Between `<<<<<<< HEAD` and `=======` is the base side (the base branch plus the commits already replayed).
   - Between `=======` and `>>>>>>> <sha> (<subject>)` is what the branch's commit being replayed introduces.
   - Run `git show REBASE_HEAD -- <filepath>` to see exactly what that commit changed in this file.

3. **Resolve the conflict intelligently:**
   - Resolve to what **this** commit intends at this point in history. Don't fold in edits from later commits on the branch — they replay next, and pre-applying them makes those commits conflict again or drop as empty.
   - If changes are in different logical sections (different functions, different import groups), keep both.
   - If changes modify the same code, understand the intent of each side:
     - The base side is the code the branch is being moved onto — treat its structure as the starting point.
     - The replayed side is the branch's own change.
     - Apply the replayed commit's intent on top of the base's code structure.
   - If one side adds new code and the other modifies existing code, integrate both.
   - If both sides modify the same line differently, combine if logically compatible, or prefer the replayed change while preserving base-side changes not in conflict.
   - For import statements or dependency lists: include all from both sides, removing exact duplicates.
   - **Never leave conflict markers in the file.**

4. **Edit the file** using the Edit tool to write the resolved version, removing all conflict markers.

5. **Stage the resolved file:** Run `git add <filepath>`.

### 4d: Handle special conflict types

- **"deleted by us" or "deleted by them":** If the user already answered this question for the same path earlier in this rebase, apply that answer again without asking. Otherwise call **AskUserQuestion** (`multiSelect: false`) with header `Deletion`, naming the file and which side deleted it (base or the replayed commit) in the question, and options `Keep deletion` ("Leave the file deleted — `git rm <file>`") vs `Restore file` ("Keep the file with the surviving side's changes — `git add <file>`"). Act on the selection.
- **Binary file conflicts:** Do not attempt to merge. Call **AskUserQuestion** (`multiSelect: false`) with header `Binary`, naming the file in the question, and options `Keep base` ("Use the base version via `git checkout --ours <file>`") vs `Keep mine` ("Use the replayed commit's version via `git checkout --theirs <file>`"). Act on the selection, then `git add <file>`.

### 4e: Continue the rebase

After all conflicts in the current commit are resolved and staged:

1. Run `git diff --cached --check` to confirm no conflict markers remain. It compares the index with HEAD, so it sees markers in files already `git add`ed — plain `git diff --check` does not. Only `leftover conflict marker` lines matter; whitespace warnings from the commit itself don't block. Then run `git diff --name-only --diff-filter=U` to list any files git still considers unmerged.
2. Run `git -c core.editor=true rebase --continue`. The `-c` keeps the commit's original message without opening an editor (an exported `GIT_EDITOR` takes precedence; Claude Code's shell sets it to `true`). Exit 0 with `Successfully rebased` means the rebase is done — go to Step 5.
3. If a later commit conflicts, repeat Step 4 for it. Keep a running list of every file resolved and how.
4. If git reports the resolved commit is now empty (its change is already in the base), run `git rebase --skip` and tell the user which commit was dropped.

## Step 5: Verify the result

After the rebase completes:

1. Run `git log --oneline --graph -<N+3>` to show recent history.
2. Run `git range-diff --creation-factor=95 <base-ref> <old-tip> HEAD` to pair each original commit with its rebased version: `=` means the patch is unchanged, `!` means it changed (a conflict resolution), and `<` with no partner means it was dropped. Without `--creation-factor=95`, a small commit changed by a resolution shows as dropped-and-added instead of `!`.
3. If any conflicts were resolved, list the files where conflicts occurred, briefly summarize how each was resolved, and note that no pre-commit hook checked them — suggest running the project's tests.
4. Report final status: branch, base ref, number of commits replayed, how many had conflicts, how many were dropped (`=` lines from Step 1, `<` entries above, and `--skip`s). Give the undo: `git reset --hard <old-tip>`.

## Step 6: Offer to push rewritten history

Skip this step unless `origin/<current>` exists and `git merge-base --is-ancestor origin/<current> HEAD` fails — that is exactly when a plain `git push` will be rejected as non-fast-forward (the `push` skills stop there and never force-push).

Call **AskUserQuestion** (`multiSelect: false`) with header `Push`, a question naming the branch (e.g. "`feature` was rewritten; force-push it to `origin`?"), and two options:

- `Force-push` — "Run `git push --force-with-lease --force-if-includes origin HEAD`. Refuses if someone else pushed to the branch since you last pulled it."
- `Leave it` — "Don't push. The next push of this branch must be a force-push."

On `Force-push`, run the command exactly as given. On success, note that the Step 5 undo now also needs a force-push to restore the remote. If it is rejected (`stale info` or `remote ref updated since checkout`), **stop and report**: someone else pushed to `origin/<current>`, and forcing would discard their work. Never retry with `--force` or drop either flag.

## Error handling

- If `git rebase --continue` fails after resolution, run `git status` to diagnose remaining conflicts or unstaged changes.
- If the rebase cannot be completed, call **AskUserQuestion** (`multiSelect: false`) with header `On conflict` and options `Abort` ("Run `git rebase --abort` to restore the original state") vs `Keep resolving` ("Continue manual conflict resolution"). On `Abort`, run `git rebase --abort`.
- If the rebase fails for any other reason (e.g. a `pre-rebase` hook refuses), report the error. Never pass `--no-verify`, `--force`, or other overrides without confirmation.
