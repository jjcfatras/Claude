---
name: implement-ticket
disable-model-invocation: true
description: Take a JIRA ticket by key, understand its intent, verify every claim it makes about the codebase to catch false positives, design an implementation plan (reconciling any solution the ticket proposed with one designed independently), present it for approval, then implement in an isolated worktree — and finish by sweeping the final diff to confirm every change it made is covered by a test, retrofitting the missing ones, then committing and pushing the ticket branch. Use whenever the user wants to pick up, start, action, or implement a JIRA ticket/issue by key (e.g. "implement PROJ-1234", "pick up ABC-42", "let's action this ticket").
argument-hint: <JIRA-key> (e.g. PROJ-1234)
model: opus
effort: xhigh
allowed-tools: Bash, Read, Write, Grep, Glob, Edit, Agent, Workflow, Skill, AskUserQuestion, EnterWorktree, ExitWorktree, mcp__claude_ai_Atlassian_Rovo__getJiraIssue, mcp__claude_ai_Atlassian_Rovo__getAccessibleAtlassianResources, mcp__plugin_context7_context7__resolve-library-id, mcp__plugin_context7_context7__query-docs
---

Take a JIRA ticket and carry it all the way to working code — but with a healthy skepticism that separates this from "just do what the ticket says." Tickets are written by people who may be looking at stale code, half-remembering how a system works, or proposing a fix they haven't validated. So before you build anything, you find out what the ticket is _really_ asking for, check whether its claims about the codebase are actually true, and design your own approach rather than reflexively implementing whatever solution the ticket sketched. The whole point is to catch a wrong premise on day one instead of discovering it three commits deep.

Work through the phases in order. Don't skip the verification phase even when the ticket looks obviously correct — that's exactly when stale claims slip through.

Everything after the fetch happens in a worktree on a fresh branch for the ticket (see Worktree setup), never in the checkout the skill was invoked from — that one may be on another branch or carry uncommitted work, and it stays untouched.

## Use context7 for external-library facts

Whenever a step turns on how an _external_ library, framework, SDK, or API actually behaves — not how this repo's own code behaves — verify it against current docs with context7 instead of trusting the ticket or your own memory (which may be stale): call `mcp__plugin_context7_context7__resolve-library-id`, then `mcp__plugin_context7_context7__query-docs` with the returned ID. This is the same skepticism the phases below aim at the ticket, pointed at the libraries you depend on. It matters most in three places:

- **[3/7] Verify claims** — a bucket (b) claim that hinges on library behavior ("the SDK already retries on 5xx", "`axios` throws on 4xx") is _checkable_ against docs, not "unverifiable". Confirm or refute it from the docs and cite the version.
- **[4/7] Design** — confirm the API you intend to use exists with the signature and capability you're assuming, so the plan isn't built on a stale or imagined API.
- **[6/7] Implement** — get current API names and signatures before writing the call, so the code isn't stale on arrival.
- **[7/7] Verify coverage** — when a gap sits on library behavior you'd have to mock or assert against (a framework's test utilities, an SDK's error shape), confirm the current API from docs before writing the retrofit test.

Skip context7 for project-internal logic, general programming patterns, or anything verifiable from the code in front of you — don't burn calls on claims that don't depend on external library behavior.

## Variables to derive at startup

- **Ticket key** — from `$ARGUMENTS` (e.g. `PROJ-1234`). If it's missing or doesn't look like a JIRA key, ask the user for it before doing anything else.
- **cloudId** — `getAccessibleAtlassianResources` returns the Atlassian site(s). If there's exactly one, use it; if more than one, call the **AskUserQuestion** tool (`multiSelect: false`, header `Site`) populated with one option per site (name/URL) and use the chosen site's `cloudId`.
- **Scratch dir** — `${TMPDIR:-/tmp}/jira-implement-ticket-$EPOCH` (one `date +%s` call). Use it for investigator subagents' findings files and Worktree teardown's commit log; create it the first time either needs it.

## [1/7] Fetch the ticket

Call `getJiraIssue` with the key and cloudId. Read **everything**, not just the description:

- **Description** — the primary statement of the problem and (often) a proposed solution.
- **Comments** — this is where the real story usually lives: clarifications, a reviewer poking holes, "actually it turned out to be X", a second proposed approach. Read them all and weight later comments over the original description when they conflict.
- **Linked issues / parent epic** — context on why this matters and what it depends on.
- **Attachments / acceptance criteria fields** — concrete expected behavior.

If `getJiraIssue` fails, show the error **verbatim** and stop — don't guess at the ticket contents from the key alone.

## Worktree setup

Set up the worktree now: after the fetch, so a failed fetch leaves nothing behind, and before verification, so claims are checked against the same base you'll build on.

1. Invoke the **`superpowers:using-git-worktrees` skill** via the `Skill` tool, the same declared dependency phase 6 leans on. This skill's instructions are the declared preference for a worktree, so don't ask for consent; name the worktree after the ticket key. If the skill is unavailable, call **EnterWorktree** with `name: "<KEY>"`.
2. If that skill reported the session was **already in a linked worktree**, it created nothing: note that, work there, and skip Worktree teardown at the end — never remove a worktree this run didn't create.
3. **Pick the base and cut the ticket branch** (skip if step 2 applied). The worktree starts on an auto-named branch cut from the invoking checkout's `HEAD`, which may not be the branch the PR targets. Run `git symbolic-ref --short refs/remotes/origin/HEAD` and strip `origin/` (fall back to `main`) for the default branch, then call **AskUserQuestion** (`multiSelect: false`, header `PR base`) with the default branch first; the Other field takes any other base, such as `dev`. Record the answer as `$PR_BASE` — Worktree teardown reuses it, so it's asked only once. Then, each as its own call: `git fetch origin <PR_BASE>`, `git checkout -b <type>/<KEY>-<short-slug> origin/<PR_BASE>` (`type` is `fix`, `feat` or `chore`, from the ticket's type), and `git branch -D <auto-branch>` (the branch name `EnterWorktree` reported), so the worktree has one branch and `ExitWorktree` later names the right one.
4. Record `$WORKTREE` (`git rev-parse --show-toplevel`) and `$BASE` (`git rev-parse HEAD`) — `$BASE` is the pre-implementation state phase 7 diffs against. Both are placeholders: write the literal values into later commands, since Bash state doesn't persist between calls.
5. If dependencies weren't installed by the skill, install them from the lockfile: `pnpm-lock.yaml` → `pnpm install --frozen-lockfile`, `package-lock.json` → `npm ci`, `yarn.lock` → `yarn install --immutable`. A fresh worktree has no `node_modules`, so tests would fail or silently skip. Other toolchains (Go modules, cargo) fetch on first build — skip them.

**From here until Worktree teardown, work only in `$WORKTREE`.** Read and Edit `$WORKTREE/<repo-relative path>`, never the invoking checkout's copy. The harness refuses Bash calls it can't prove keep git inside the worktree, so run each `git` command as its own plain call — no `;`/`&&`/pipe chain around it. It likewise refuses a command that runs a string through `source`, such as `source ~/.nvm/nvm.sh; nvm use; pnpm install`: check `node --version` against the repo's `.nvmrc` first, and run the install as a plain call. If a call is refused, split it; don't rephrase the chain.

## [2/7] Understand intent

Distill what the ticket is actually trying to achieve — the outcome a person or system should get — not just its literal wording. A ticket titled "add a retry" might really be about "stop dropping orders when the payment provider blips." Aim for the underlying goal, because that's what your plan has to satisfy.

Then sort everything the ticket says into three buckets. Keeping these separate is what makes the next phases work — you verify the second bucket, and you treat the third as _a_ proposal rather than _the_ answer:

- **(a) Goal / problem** — what outcome is wanted, and why. The thing that's true regardless of how it's solved.
- **(b) Claims about the codebase** — every factual assertion about how the code currently behaves: "there's no validation on the login form", "`processRefund` doesn't check the account balance", "the endpoint returns 500 on empty input", "we don't log auth failures". These are checkable, and some will be wrong.
- **(c) Proposed solution** — any approach the ticket (or a comment) suggests: "we should add a middleware", "just wrap it in a try/catch", a code snippet, a list of files to change.

Some tickets have no bucket (c) at all — that's fine and common. Don't invent one.

## [3/7] Verify every codebase claim

This is the phase that earns its keep. Go through bucket (b) and check each claim against the **actual current code** — `rg`, `Read`, and the LSP tools if available. Don't trust the ticket; trust the code in front of you.

Classify each claim, and require concrete `file:line` evidence for the verdict:

- **Confirmed** — the code matches the claim. Cite where.
- **False** — the code contradicts the claim (e.g. the validation the ticket says is missing is right there at `auth.js:42`). This is a false positive, and catching it now is the highest-value thing this command does.
- **Partial** — true in some paths, not others (e.g. validated on the API route but not the batch importer).
- **Unverifiable** — depends on runtime/config/external state you can't inspect statically. Say so rather than guessing.

Scale how you verify to how many claims there are — don't reach for machinery a three-claim ticket doesn't need:

- **A few claims** — verify inline.
- **Several** — spawn parallel investigator subagents (one `Agent` message, several calls, each with `model: "sonnet"`) so verification doesn't serialize; give each a cluster of claims, `$WORKTREE` as the root to read from, and a findings file to write to, then merge their verdicts.
- **Many independent claims** (roughly 8+, or enough that verifying them inline would flood your context) — call the **Workflow** tool. The user invoking this skill _is_ the explicit opt-in Workflow requires, so you don't need to ask for it again. Pass the shipped script, never a hand-written one:
  - `name`: `"jira:implement-ticket-verify-claims"`, the workflow this plugin ships. If the tool doesn't know that name (an older Claude Code build), pass `script` instead: the exact contents of `${CLAUDE_PLUGIN_ROOT}/workflows/implement-ticket-verify-claims.js`, Read and passed verbatim. Never `scriptPath`: Workflow only loads paths under the working directory or an added directory.
  - `args` (a real JSON object, not a string): `{ ticketKey, worktree, guidePath, clusters }` — `worktree` is `$WORKTREE`; `guidePath` is `${CLAUDE_PLUGIN_ROOT}/references/claim-verification.md`; `clusters` is `[{ name, claims: ["<atomic claim>", …] }]`, grouping related claims so the fan-out stays coarse (one agent per cluster, under ~15 clusters).

  The script forces each cluster's agent to return `{ claim, verdict, evidence }` objects, `verdict` one of `confirmed | false | partial | unverifiable` — verdict objects instead of prose you'd have to re-parse is the whole reason to reach for Workflow here. Wait for its completion notification; it returns `{ verdicts, unverified }`. Verify every cluster named in `unverified` (its agent died or returned the wrong number of verdicts) inline — never drop a claim.

**A workflow returns verdicts and nothing else.** Judging whether a false claim is load-bearing, and the `AskUserQuestion` gate below, stay in this session — a workflow subagent runs in a background runtime and can't prompt the user, so a gate moved inside one is a gate silently skipped.

**Any other subagent (release-notes scans, docs lookups) — brief it with what you already know, and keep it in the foreground.** State the facts you've already established (files read, versions found) and say whether it may read the repo or should return open questions instead; otherwise it re-reads what you just checked. Don't pass `run_in_background`: a background hand-back starts a new turn, and the `model`/`effort` pin above applies only to the turn this skill started in, so the rest of the run silently drops to the session model.

**If a false claim knocks out the ticket's premise, stop here.** If the ticket asks you to "add the missing validation" and the validation already exists, building anything is wrong — surface the finding and show the user the evidence, then call the **AskUserQuestion** tool (`multiSelect: false`, header `Premise`) with three options:

- `Bug is elsewhere` — "Validation exists but is buggy; the real bug is in a different spot."
- `Stale — close it` — "The ticket is stale and closeable; stop here."
- `Different path` — "The ticket meant a different code path — I'll point you to it."

Act on the selection, or take the user's redirect from the Other field — don't pick one and plow ahead; on `Stale — close it`, run Worktree teardown before stopping. Don't plan around a phantom. See `${CLAUDE_PLUGIN_ROOT}/references/claim-verification.md` for how to extract atomic claims and handle the premise-invalidation case.

## [4/7] Design the plan

**Design your own plan first, before you let the ticket's proposed solution anchor you.** This ordering is deliberate: if you read bucket (c) and start from there, you inherit its blind spots and your search for alternatives narrows to "is this approach okay?" instead of "what's the best approach?". So set the proposed solution aside and design from the goal (bucket a) and the verified facts (the confirmed/partial claims from phase 3): what changes, where, in what order, what the tests look like, what could break.

Then, **if** the ticket had a proposed solution (bucket c), compare it against yours across these dimensions — correctness, scope, risk, simplicity, test strategy — and synthesize the best of both. The proposed solution often carries domain knowledge you lack (a gotcha, a constraint, the "we tried X and it didn't work" from a comment), so don't dismiss it; mine it. Record where each plan won and why, so the user can see the reasoning. See `${CLAUDE_PLUGIN_ROOT}/references/plan-reconciliation.md` for the comparison method and synthesis format.

If there was no proposed solution, your independent plan _is_ the plan — skip the comparison.

## [5/7] Present for approval

Show the user, in the conversation, before touching any file:

1. **Intent** — one or two sentences on what the ticket really wants (bucket a).
2. **Claim verification** — a short table of each claim and its verdict (confirmed / false / partial / unverifiable) with evidence. Put any **false** claims at the top — they're the headline.
3. **The plan** — the synthesized implementation plan: files to change, sequence, test approach.
4. **Reconciliation rationale** — if you compared against a proposed solution, a few lines on what you took from each and why. If you discarded the proposed approach, say so plainly with the reason.

Then gate with `AskUserQuestion`: implement this plan, adjust it, or stop (on stop, run Worktree teardown). **Do not edit any file before the user approves** — this command's value is front-loaded into thinking, and the user needs a chance to catch anything you missed about their system.

## [6/7] Implement

Only after approval. Work through the plan, letting the nature of each change decide how careful to be.

**Testable changes** (logic, validation, data transforms, bug fixes) — drive them with the **`superpowers:test-driven-development` skill**: invoke it via the `Skill` tool _before_ you write implementation code, then follow the RED → GREEN → REFACTOR loop it enforces. It's a declared dependency of this plugin, so it should be available. The step that does the work is watching each new test **fail for the right reason** before you write the code: a test you've never seen red may be passing for the wrong reason, and then it guards nothing. This is the same skepticism the earlier phases aim at the ticket, turned on your own code. If the skill is unavailable (the dependency was disabled), Read `${CLAUDE_PLUGIN_ROOT}/references/red-green-tdd.md` and follow its cycle — it's the self-contained fallback.

**Non-testable changes** (config, docs, copy, scaffolding) — implement directly; a forced test here is theater. And if a change _would_ be testable but you genuinely can't run the tests (no runner wired up, behavior hangs on live external state), say so plainly rather than claim a green you never saw.

After implementing, run the specs for the code you touched and report results honestly — if something fails, say so with the output. Save the full suite (type-check, lint, all tests) for phase 7's single fresh run, so it isn't paid for twice. If you hit something that contradicts the approved plan (a verified fact turns out wrong once you're in the code), stop and re-surface rather than improvising around it — and if the run ends there, follow Worktree teardown's stop-after-edits rule. Don't write the final summary here — phase 7 sweeps the changeset first, so the wrap-up belongs there once coverage is settled.

## [7/7] Verify every change has test coverage

Phase 6 tested what the plan said to test. This phase tests what you actually changed — and the two are never quite the same set. Implementation always drags in edits the plan didn't anticipate: a caller you had to adjust, a formatter that needed a new branch, a second code path you only found once you were in the file. None of those were in the plan's test list, so the red/green loop never pinned them, and they ride out on a green suite because nothing asserts them. Find them now, while the context is still in your head.

**Build the inventory from what you changed, not from what you planned.** Diff against `$BASE` from Worktree setup — `git diff $BASE` (plus `git diff --stat $BASE` for the file list), and `git status --short` for new untracked files, which `git diff` doesn't show. If the working tree isn't a git repo, or your changes live outside it, fall back to your own record of every file you edited this run — do _not_ report full coverage just because `git diff` came back empty.

Then go hunk by hunk and name the **behavior** each one introduces or changes, because behavior is the unit of coverage, not the line. Give every behavior one of three verdicts:

- **Covered** — name the test that pins it. A test that merely _executes_ the changed line doesn't count; it has to assert the new behavior, such that reverting your change turns that test red.
- **Uncovered** — the behavior changed and no test would notice if it broke. This is the phase's whole reason to exist.
- **Not meaningfully testable** — config, docs, copy, dependency bumps, pure formatting. Say which and move on; a forced test here is theater (see `${CLAUDE_PLUGIN_ROOT}/references/red-green-tdd.md`).

When the diff spans enough files that auditing every hunk inline would flood your context, run **the audit** through the **Workflow** tool, running the shipped script, never a hand-written one: `name` is `"jira:implement-ticket-audit-coverage"` (if the tool doesn't know that name, pass `script` instead, the exact contents of `${CLAUDE_PLUGIN_ROOT}/workflows/implement-ticket-audit-coverage.js`, Read and passed verbatim, never as `scriptPath`), and `args` is a real JSON object `{ worktree, diffBase, files }` — `worktree` is `$WORKTREE`, `diffBase` is `$BASE`, `files` the changed paths. It runs one agent per file and returns `{ behaviors, unaudited }`, each behavior a `{ file, behavior, verdict, coveringTest }` object with `verdict` one of `covered | uncovered | not-testable`; audit every `unaudited` file inline. The script diffs with git, so with no git working tree, audit inline. For a handful of files, skip the workflow and audit inline. **The workflow audits only** — writing the retrofit tests, running the mutation check, and the `AskUserQuestion` gate below all stay in this session, for two reasons: parallel agents editing the same working tree collide, and a background subagent can't prompt the user.

If the project has its own coverage command (a `test:coverage` script, `go test -cover`, `pytest --cov`), run it as a **hint about what's definitely uncovered** — but a hunk it reports as covered can still be covered only by a test that asserts nothing about your change. Trust the per-behavior audit over the percentage.

**Close each uncovered gap** with the same red/green loop, adjusted for one problem: you can't watch a test fail before code that already exists. So write the test, confirm it passes, then **break the change it covers** — revert that hunk, flip the condition, remove the guard — and re-run to confirm _that_ test fails, for the right reason. Restore the change and confirm green again. That manufactured red is your evidence the test grips something real; skip it and you've added a test you've only ever seen green, which is exactly the inert test `${CLAUDE_PLUGIN_ROOT}/references/red-green-tdd.md` warns about.

Fill gaps that fit the project's existing test setup without asking — the plan was approved in phase 5 and this is finishing it. **Stop and ask** when closing a gap would need test infrastructure the repo doesn't have (a new fixture or factory, a harness, a mock server, a new test dependency): that's scope the user didn't approve. Call the **AskUserQuestion** tool (`multiSelect: false`, header `Coverage`) with three options:

- `Build the harness` — "Add the missing test infrastructure and cover it properly."
- `Ship uncovered` — "Leave it uncovered; note the gap as a follow-up."
- `Narrow the change` — "Drop or shrink the change so it doesn't need new infrastructure."

Act on the selection or take the user's redirect from the Other field. And **don't widen this into a coverage crusade** — the scope is the changes this run made. Pre-existing untested code next door stays untested; note it as a follow-up if it's egregious, don't fix it here.

Before claiming done, invoke the **`superpowers:verification-before-completion` skill** via the `Skill` tool — same declared dependency phase 6 leans on, and its whole job is refusing to assert a green you haven't observed. Run the project's full suite once, fresh (bypass the build cache), as that skill requires. Then run Worktree teardown, and report:

1. **Coverage table** — every changed behavior, its verdict, and the test that covers it.
2. **Retrofitted tests** — what you added, with the real mutation-check output (the manufactured red _and_ the restored green), quoted rather than narrated.
3. **File-by-file summary** of everything the run changed.
4. **Gaps left open** — anything still uncovered and why (not testable, infrastructure the user declined, suite can't run here), plus any follow-ups.
5. **Branch** — the pushed branch name and commit SHA, the PR URL (or why none was opened), and the worktree path, which stays in place (remove it with `git worktree remove <path>` once the PR merges).

If a change is genuinely testable but you couldn't run the suite, say so plainly and include the test you would have written. An honest "uncovered, here's the test I'd add" is worth more than a green you never saw.

For API-level behavior, `/jira:create-tests` builds a runnable Postman QA collection from the same ticket — worth mentioning to the user as an optional next step, but it's user-invoked only, so don't try to call it yourself.

## Worktree teardown

Run this on every exit after Worktree setup — skip it entirely if setup found the session already in a linked worktree. Which branch applies depends on whether any file was edited:

**Stopped before any edit** (premise stop in phase 3, stop at the phase 5 gate): call **ExitWorktree** with `action: "remove"` — it deletes the worktree and its empty branch. If it refuses because of changes, show them and ask before re-invoking with `discard_changes: true`; never set that on your own.

**Finished (end of phase 7)** — commit, push, open the PR, then return to the invoking checkout and leave the worktree in place for follow-up work. Run each command as its own plain call:

1. `git branch --show-current` — record the branch.
2. `git status --short`, then stage the changed files by name with `git add <paths>`. Never stage files that look like secrets (`.env`, `.env.*`, `credentials.json`, `*.pem`, `id_rsa`, `*.key`, token/password files).
3. `git commit -m "<KEY>: <one-line summary>"`, with `run_in_background: true` and output captured as `> "$SCRATCH/commit.log" 2>&1` (create the scratch dir first if you haven't): a repo's pre-commit hooks can outrun the 120 s Bash timeout. End your turn and wait for the completion notification — never `sleep` or poll the log. On a non-zero exit, `rg` the log for the cause, then stop: keep the worktree and report its path. Never `--no-verify`.
4. `git push -u origin HEAD`. If it fails, stop: keep the worktree and report its path with the error. Never force-push.
5. **PR base.** Use `$PR_BASE` from Worktree setup step 3. If setup found the session already in a linked worktree, that step was skipped: run its default-branch lookup and `AskUserQuestion` here instead. `/git:pr` always targets the default branch unless told otherwise, so never guess.
6. **Open the PR** while still in the worktree, so the current branch is the pushed ticket branch: invoke the `git:pr` skill via the `Skill` tool with `args: "--base <PR_BASE>"`. A failure here never stops teardown, since the branch is already pushed: carry on to step 7 and report the error plus `gh pr create --base <PR_BASE>` for the user to run by hand. The same goes if the `git` plugin isn't installed.
7. Call **ExitWorktree** with `action: "keep"`. This returns the session to the invoking checkout and leaves the worktree and its branch on disk; `remove` would delete both.

**Stopped after an edit** (a contradicted plan in phase 6, a failed commit or push): call **ExitWorktree** with `action: "keep"` only, never remove the worktree, and report its path and branch so the user can finish by hand. Removing it would discard the uncommitted work.

## Cleanup

If you created the scratch dir, remove it — but defend the `rm` so a bad variable can't widen it: confirm the path is non-empty and under the temp root before `rm -rf "$SCRATCH"`. If you never created it, there's nothing to clean up.
