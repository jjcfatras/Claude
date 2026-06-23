---
description: Take a JIRA ticket by key, understand its intent, verify every claim it makes about the codebase to catch false positives, design an implementation plan (reconciling any solution the ticket proposed with one designed independently), present it for approval, then implement. Use whenever the user wants to pick up, start, action, or implement a JIRA ticket/issue by key (e.g. "implement PROJ-1234", "pick up ABC-42", "let's action this ticket").
argument-hint: <JIRA-key> (e.g. PROJ-1234)
model: opus
effort: xhigh
allowed-tools: Bash, Read, Write, Grep, Glob, Edit, Agent, AskUserQuestion, mcp__claude_ai_Atlassian_Rovo__getJiraIssue, mcp__claude_ai_Atlassian_Rovo__getAccessibleAtlassianResources, mcp__plugin_context7_context7__resolve-library-id, mcp__plugin_context7_context7__query-docs
---

Take a JIRA ticket and carry it all the way to working code — but with a healthy skepticism that separates this from "just do what the ticket says." Tickets are written by people who may be looking at stale code, half-remembering how a system works, or proposing a fix they haven't validated. So before you build anything, you find out what the ticket is _really_ asking for, check whether its claims about the codebase are actually true, and design your own approach rather than reflexively implementing whatever solution the ticket sketched. The whole point is to catch a wrong premise on day one instead of discovering it three commits deep.

Work through the phases in order. Don't skip the verification phase even when the ticket looks obviously correct — that's exactly when stale claims slip through.

## Use context7 for external-library facts

Whenever a step turns on how an _external_ library, framework, SDK, or API actually behaves — not how this repo's own code behaves — verify it against current docs with context7 instead of trusting the ticket or your own memory (which may be stale): call `mcp__plugin_context7_context7__resolve-library-id`, then `mcp__plugin_context7_context7__query-docs` with the returned ID. This is the same skepticism the phases below aim at the ticket, pointed at the libraries you depend on. It matters most in three places:

- **[3/6] Verify claims** — a bucket (b) claim that hinges on library behavior ("the SDK already retries on 5xx", "`axios` throws on 4xx") is _checkable_ against docs, not "unverifiable". Confirm or refute it from the docs and cite the version.
- **[4/6] Design** — confirm the API you intend to use exists with the signature and capability you're assuming, so the plan isn't built on a stale or imagined API.
- **[6/6] Implement** — get current API names and signatures before writing the call, so the code isn't stale on arrival.

Skip context7 for project-internal logic, general programming patterns, or anything verifiable from the code in front of you — don't burn calls on claims that don't depend on external library behavior.

## Variables to derive at startup

- **Ticket key** — from `$ARGUMENTS` (e.g. `PROJ-1234`). If it's missing or doesn't look like a JIRA key, ask the user for it before doing anything else.
- **cloudId** — `getAccessibleAtlassianResources` returns the Atlassian site(s). If there's exactly one, use it; if more than one, call the **AskUserQuestion** tool (`multiSelect: false`, header `Site`) populated with one option per site (name/URL) and use the chosen site's `cloudId`.
- **Scratch dir** — `${TMPDIR:-/tmp}/jira-implement-ticket-$EPOCH` (one `date +%s` call). Use it only if you need to spawn investigator subagents that write findings to files; otherwise you may not need it at all.

## [1/6] Fetch the ticket

Call `getJiraIssue` with the key and cloudId. Read **everything**, not just the description:

- **Description** — the primary statement of the problem and (often) a proposed solution.
- **Comments** — this is where the real story usually lives: clarifications, a reviewer poking holes, "actually it turned out to be X", a second proposed approach. Read them all and weight later comments over the original description when they conflict.
- **Linked issues / parent epic** — context on why this matters and what it depends on.
- **Attachments / acceptance criteria fields** — concrete expected behavior.

If `getJiraIssue` fails, show the error **verbatim** and stop — don't guess at the ticket contents from the key alone.

## [2/6] Understand intent

Distill what the ticket is actually trying to achieve — the outcome a person or system should get — not just its literal wording. A ticket titled "add a retry" might really be about "stop dropping orders when the payment provider blips." Aim for the underlying goal, because that's what your plan has to satisfy.

Then sort everything the ticket says into three buckets. Keeping these separate is what makes the next phases work — you verify the second bucket, and you treat the third as _a_ proposal rather than _the_ answer:

- **(a) Goal / problem** — what outcome is wanted, and why. The thing that's true regardless of how it's solved.
- **(b) Claims about the codebase** — every factual assertion about how the code currently behaves: "there's no validation on the login form", "`processRefund` doesn't check the account balance", "the endpoint returns 500 on empty input", "we don't log auth failures". These are checkable, and some will be wrong.
- **(c) Proposed solution** — any approach the ticket (or a comment) suggests: "we should add a middleware", "just wrap it in a try/catch", a code snippet, a list of files to change.

Some tickets have no bucket (c) at all — that's fine and common. Don't invent one.

## [3/6] Verify every codebase claim

This is the phase that earns its keep. Go through bucket (b) and check each claim against the **actual current code** — Grep, Glob, Read, and the LSP tools if available. Don't trust the ticket; trust the code in front of you.

Classify each claim, and require concrete `file:line` evidence for the verdict:

- **Confirmed** — the code matches the claim. Cite where.
- **False** — the code contradicts the claim (e.g. the validation the ticket says is missing is right there at `auth.js:42`). This is a false positive, and catching it now is the highest-value thing this command does.
- **Partial** — true in some paths, not others (e.g. validated on the API route but not the batch importer).
- **Unverifiable** — depends on runtime/config/external state you can't inspect statically. Say so rather than guessing.

When there are many independent claims, spawn parallel investigator subagents (one `Agent` message, several calls) so verification doesn't serialize — give each a cluster of claims and a findings file to write to, then merge their verdicts. When there are only a few, just verify inline.

**If a false claim knocks out the ticket's premise, stop here.** If the ticket asks you to "add the missing validation" and the validation already exists, building anything is wrong — surface the finding and show the user the evidence, then call the **AskUserQuestion** tool (`multiSelect: false`, header `Premise`) with three options:

- `Bug is elsewhere` — "Validation exists but is buggy; the real bug is in a different spot."
- `Stale — close it` — "The ticket is stale and closeable; stop here."
- `Different path` — "The ticket meant a different code path — I'll point you to it."

Act on the selection, or take the user's redirect from the Other field — don't pick one and plow ahead. Don't plan around a phantom. See `${CLAUDE_PLUGIN_ROOT}/references/claim-verification.md` for how to extract atomic claims and handle the premise-invalidation case.

## [4/6] Design the plan

**Design your own plan first, before you let the ticket's proposed solution anchor you.** This ordering is deliberate: if you read bucket (c) and start from there, you inherit its blind spots and your search for alternatives narrows to "is this approach okay?" instead of "what's the best approach?". So set the proposed solution aside and design from the goal (bucket a) and the verified facts (the confirmed/partial claims from phase 3): what changes, where, in what order, what the tests look like, what could break.

Then, **if** the ticket had a proposed solution (bucket c), compare it against yours across these dimensions — correctness, scope, risk, simplicity, test strategy — and synthesize the best of both. The proposed solution often carries domain knowledge you lack (a gotcha, a constraint, the "we tried X and it didn't work" from a comment), so don't dismiss it; mine it. Record where each plan won and why, so the user can see the reasoning. See `${CLAUDE_PLUGIN_ROOT}/references/plan-reconciliation.md` for the comparison method and synthesis format.

If there was no proposed solution, your independent plan _is_ the plan — skip the comparison.

## [5/6] Present for approval

Show the user, in the conversation, before touching any file:

1. **Intent** — one or two sentences on what the ticket really wants (bucket a).
2. **Claim verification** — a short table of each claim and its verdict (confirmed / false / partial / unverifiable) with evidence. Put any **false** claims at the top — they're the headline.
3. **The plan** — the synthesized implementation plan: files to change, sequence, test approach.
4. **Reconciliation rationale** — if you compared against a proposed solution, a few lines on what you took from each and why. If you discarded the proposed approach, say so plainly with the reason.

Then gate with `AskUserQuestion`: implement this plan, adjust it, or stop. **Do not edit any file before the user approves** — this command's value is front-loaded into thinking, and the user needs a chance to catch anything you missed about their system.

## [6/6] Implement

Only after approval. Work through the plan, letting the nature of each change decide how careful to be.

**Testable changes** (logic, validation, data transforms, bug fixes) — drive them with red/green TDD. The cycle, and why each step matters:

1. **Write the smallest test** that pins the behavior the ticket wants — one case, named for what it asserts.
2. **Run it and watch it fail (RED).** This is the step it's tempting to skip, and the one that does the work. A test you've never seen fail might be passing for the wrong reason — asserting nothing, exercising a typo'd path, or already satisfied — in which case it guards nothing. Seeing the _expected_ failure (the feature is missing, not a syntax error) is what proves the test actually grips the gap you're about to close.
3. **Write the minimal code** to make it pass — no extra features, no speculative refactoring.
4. **Run it again and watch it pass (GREEN)**, and confirm the rest of the suite is still green so you didn't fix one thing by breaking another.
5. **Refactor under green** — tidy names, remove duplication, no behavior change — then loop back for the next case.

This is the same skepticism the earlier phases aim at the ticket, turned on your own code: don't trust that it works because you wrote it — prove it by watching the test go red, then green. See `${CLAUDE_PLUGIN_ROOT}/references/red-green-tdd.md` for the full cycle and the edge cases. If the `superpowers:test-driven-development` skill is installed it's the canonical, stricter form of this loop — lean on it; the discipline above stands on its own when it isn't.

**Non-testable changes** (config, docs, copy, scaffolding) — implement directly; a forced test here is theater. And if a change _would_ be testable but you genuinely can't run the tests (no runner wired up, behavior hangs on live external state), say so plainly rather than claim a green you never saw.

After implementing, run the project's existing test suite and report results honestly — if something fails, say so with the output. Then summarize what you changed, file by file, and note anything you deferred or that needs follow-up. If you hit something that contradicts the approved plan (a verified fact turns out wrong once you're in the code), stop and re-surface rather than improvising around it.

## Cleanup

If you created the scratch dir, remove it — but defend the `rm` so a bad variable can't widen it: confirm the path is non-empty and under the temp root before `rm -rf "$SCRATCH"`. If you never created it, there's nothing to clean up.
