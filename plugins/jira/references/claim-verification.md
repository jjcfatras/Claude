# Claim Verification

Tickets describe the codebase in prose, and prose blurs together facts, assumptions, and guesses. The job here is to pull out the **checkable factual claims**, test each against the actual code, and notice when one of them is wrong — because a ticket built on a false claim asks you to build the wrong thing.

## Extracting atomic claims

Read the description and comments and pull out every assertion about how the code _currently_ behaves. Split compound statements into atomic claims — one verdict per claim, so a half-true sentence doesn't get a single muddy grade.

Claims usually take one of these shapes:

- **Absence** — "there's no validation on X", "we don't log auth failures", "nothing handles the empty-list case". These are the most common false positives: the thing often exists, just somewhere the ticket author didn't look.
- **Presence / behavior** — "`processRefund` checks the balance", "the endpoint returns 500 on bad input", "the cron runs hourly".
- **Location** — "the bug is in `auth.js`", "this all lives in the `billing` module".
- **Causation** — "the timeout is because we open a new connection per request". Causal claims are the hardest to verify statically; often the best you can do is confirm the mechanism exists, not that it's _the_ cause.

What is _not_ a claim to verify: the goal, the proposed solution, opinions ("this is messy"), and anything about runtime/production state you can't see from the source.

## Verifying

For each claim, go to the code — Grep for the symbol, Glob for the file, Read the region, use LSP `findReferences` / `goToDefinition` when chasing a symbol. Verdicts:

| Verdict          | Meaning                                  | Evidence required                                                                                    |
| ---------------- | ---------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| **Confirmed**    | Code matches the claim                   | `file:line` showing it                                                                               |
| **False**        | Code contradicts the claim               | `file:line` of the thing the ticket said wasn't there, or proof the asserted behavior doesn't happen |
| **Partial**      | True on some paths, not others           | `file:line` for both the covered and uncovered path                                                  |
| **Unverifiable** | Depends on runtime/config/external state | what you'd need to check it (a log, a prod config, a running instance)                               |

Always cite `file:line`. "I checked and it's fine" is not a verdict; "validation exists at `auth.js:42-58`, so the claim that it's missing is false" is.

## The premise-invalidation rule

A false claim is not always fatal — a ticket can be slightly wrong in a detail and still want the right thing. The question is whether the false claim is **load-bearing**: does the ticket's ask depend on it?

- **Load-bearing false claim** → stop and surface. "Add the missing input validation" + validation already exists = there's nothing to add. Building anything here is wrong. Show the evidence and ask the user how to proceed: the real bug may be elsewhere (validation exists but is buggy), the ticket may be stale and closeable, or the author may have meant a different code path. Let them redirect — don't pick one and plow ahead.
- **Incidental false claim** → note it, correct the record, keep going. If the ticket says the bug is in `auth.js` but it's actually in `session.js`, the goal (fix the bug) still stands — just plan against the real location.

The reason to stop on a load-bearing false claim is leverage: it costs one message to catch now and a wasted implementation to catch later. Surfacing early is the highest-value move this whole workflow makes.
