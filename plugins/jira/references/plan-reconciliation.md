# Plan Reconciliation

When a ticket proposes a solution, the easy mistake is to start from it. This doc is about resisting that — designing independently first, then merging the best of both.

## Why design your own first

Reading the ticket's proposed solution before you've formed your own anchors you. Once a concrete approach is in your head, your thinking shifts from "what's the best way to do this?" to "is this way okay?" — a narrower, more permissive question. You stop generating alternatives and start rationalizing the one you were handed. The proposed solution might be great, but you can't tell if you never seriously considered anything else.

So: set bucket (c) aside. Design from the **goal** (bucket a) and the **verified facts** (the confirmed and partial claims from the verification phase). Decide what you'd change, where, in what order, and how you'd test it, as if no one had suggested anything. Only then open the proposed solution back up.

This is the same instinct as the `debate` plugin's parallel pro/con spawn — generate competing positions independently so neither contaminates the other — just done single-threaded in your own head.

## Comparing

Once you have your plan and the ticket's, compare across these dimensions:

- **Correctness** — does it actually achieve the goal and respect the verified facts? Does it handle the edge cases the verification phase surfaced?
- **Scope** — how much does it touch? A plan that changes 3 files beats one that changes 12 for the same outcome.
- **Risk** — blast radius if it's wrong. Reversibility. Migration/data implications.
- **Simplicity** — fewer moving parts, fewer new concepts, easier to review.
- **Test strategy** — how do you prove it works? A plan you can test cheaply beats one you can only verify by hand.

Mine the proposed solution for what it knows that you don't. Ticket authors and commenters often carry domain context that isn't in the code: a constraint ("billing can't change mid-cycle"), a dead end already explored ("we tried a cache, it went stale"), a downstream consumer you'd never have found. Even if the proposed _mechanism_ is wrong, the _knowledge_ behind it can be gold.

## Synthesizing

The output isn't "pick plan A or plan B" — it's a single plan that takes the best of each. Make the reasoning visible so the user can audit it:

```
## Reconciliation
- **Structure**: mine — the ticket's middleware approach touches every route; a single guard at the service boundary is narrower (scope).
- **Edge case handling**: ticket's — the comment from @dev flagged the partial-refund case I'd have missed (domain knowledge).
- **Rollout**: mine — feature-flag it; the ticket proposed a hard cutover (risk).
```

If the proposed solution turns out to be the better plan on every dimension, say so — "the ticket's approach is sound; here it is" — and don't manufacture differences to look busy. And if you discard it entirely, state the reason plainly, because the author will want to know why their idea didn't make the cut.
