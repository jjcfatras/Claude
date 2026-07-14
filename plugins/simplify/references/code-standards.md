# Simplification rubric

Four pillars governing every proposed change. They are independent constraints, not a checklist — a change must satisfy all four to be worth proposing.

## 1. Preserve functionality

Never change what the code does, only how it does it. All original behavior, outputs, error semantics, side effects, and observable invariants must remain intact.

- No new public API. No removed public API. No silently changed argument order or return shape.
- Same error-handling contract — if the original swallowed an error, the simplified version still swallows it (flag it as a follow-up, do not "fix" it here).
- Same async semantics — do not promote sync to async or vice versa.
- Same thrown exception types. Same logging side effects unless the log line itself is the noise being removed and you call it out.

If you cannot convince yourself a hunk preserves behavior, do not propose it.

## 2. Defer to project standards

Project-specific conventions always override defaults in this file. Sources, in order of precedence:

1. The nearest `CLAUDE.md` ancestor of the file being edited.
2. The root `CLAUDE.md`.
3. Idiomatic conventions for the language already visible in the file (naming, import style, error-handling pattern).
4. This rubric, as a last resort.

This file deliberately hardcodes no language conventions — they belong in `CLAUDE.md`, where the project owner controls them.

## 3. Enhance clarity

Worthwhile simplifications usually take one of these shapes. Skip the change if none of them apply.

- Reduce nesting (early return, guard clause, flatten else-if chain).
- Eliminate dead code, unreachable branches, unused parameters, and stale imports.
- Replace a nested ternary with a switch, if/else chain, or lookup table.
- Collapse near-duplicate blocks into a single helper — only when the duplication is true semantic duplication, not coincidental similarity.
- Rename a variable or function whose current name actively misleads the reader.
- Remove comments that restate the code below them.

What does not count as clarity:

- Reformatting (the project's formatter handles this on save).
- Style-only rewrites that swap one idiom for another with no readability gain.
- Comment additions describing what code does (the code already says that).

## 4. Maintain balance

Simplification has diminishing returns and a worse-case downside. Stop when any of these become true:

- The change makes the code harder to debug, harder to extend, or harder to read at a glance.
- The change collapses an abstraction that exists for a non-obvious reason (cross-module reuse, testability, a name that documents intent).
- The change combines unrelated concerns into a single function or component.
- The change is clever — saves lines at the cost of someone having to think about it for thirty seconds. Lines are cheap; reader time is not.

A good simplification is one a colleague would read and immediately agree with. If you would need a paragraph to defend the change, do not propose it.
