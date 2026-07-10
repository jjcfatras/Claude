# Red/Green TDD

The implement phase writes code, and code you wrote always _looks_ right to you — that's exactly the
blind spot the earlier phases trained against on the ticket. Test-driven development turns the same
skepticism on your own work: instead of trusting that the change works because you intended it to,
you prove it by watching a test fail for the right reason, then pass. This doc is the full version of
the loop phase 6 sketches.

## Why see red first

The load-bearing step is watching the test **fail before** you write the code. It's tempting to skip
— you're confident, the fix is obvious — but skipping it quietly defeats the whole exercise. A test
you've never seen fail might be passing for a reason that has nothing to do with the behavior you
care about: it asserts nothing meaningful, it exercises a path your typo never reaches, the
expectation is already satisfied by accident. A test written _after_ the code almost always passes on
the first run, and a green you've only ever seen green tells you nothing — it could be inert.

Seeing the _expected_ red — "fails because the feature isn't there yet", not "fails because of a
syntax error in the test" — is the evidence that the test actually grips the gap you're closing. That
one observation is what converts a test from decoration into a guard.

## The cycle

1. **RED — write the smallest test.** One behavior, named for what it asserts
   (`returns categories for category`, not `test1`). Use real inputs, not mocks of the thing under
   test.
2. **Verify RED — run it, watch it fail.** Confirm it fails, and fails for the right reason. If it
   errors instead of failing, or fails on something incidental, fix the test first.
3. **GREEN — write the minimal code** that makes it pass. Resist adding the next three features you
   can already see; they get their own red first.
4. **Verify GREEN — run it again.** The new test passes _and_ the rest of the suite is still green,
   so you didn't trade one bug for another.
5. **REFACTOR under green.** Tidy names, pull out duplication, with no behavior change — the tests
   stay green throughout. Then loop back to RED for the next case.

**Worked example.** Ticket: `pluralize('category', 2)` returns `'categorys'`, should be
`'categories'`. RED: add a test asserting `pluralize('category', 2) === 'categories'`; run the suite
and watch _only_ that test fail (the existing cases stay green — proof the test isolates the gap).
GREEN: add the consonant-plus-`y` → `ies` rule; run again, the new test passes and nothing else
broke. REFACTOR: if the rule duplicates an existing branch, fold them — tests still green.

## What counts as testable

Drive with red/green anything whose correctness you can pin with an assertion: pure functions, data
transforms, validation, parsing, bug fixes with a concrete reproduction. These are where a failing
test sharpens the work and a passing one earns trust.

Some changes aren't testable in any honest way, and forcing a test onto them is theater that wastes
time and gives false confidence: config and dependency bumps, documentation and copy, scaffolding,
pure formatting. Implement those directly.

The honest middle case: a change that _would_ be testable, but you can't actually run the tests here
— no runner is wired up, or the behavior depends on live external state (a real network call, prod
data). Say so plainly. "This needs a test for X but the suite can't run in this environment; here's
the test I'd write" is worth far more than asserting a green you never observed.

## Picking the test that sharpens the work

When several tests are possible, write the one that pins the ticket's actual acceptance behavior
first — the case a reviewer would check to believe the ticket is done. Incidental coverage (extra
edge cases, defensive branches) can follow once the headline behavior is green. A test aimed at what
the ticket promises keeps the loop honest; a pile of tangential tests can be green while the real
requirement is still unmet.

## Honest reporting

Never claim green without having run it. Quote the real test output — the failing run and the passing
run both — rather than narrating what you expect them to say. If the existing suite was already red
before you touched anything, surface that instead of folding it into your change. The value of TDD is
entirely in the observations being real; a fabricated green is worse than no test, because someone
will trust it.

## Relation to `superpowers:test-driven-development`

Phase 6 of `implement-ticket` now drives testable changes by invoking that skill directly — it's a
declared dependency of this plugin, so it should be present. The skill is the canonical, stricter form
of this loop — the same RED → GREEN → REFACTOR with a harder line ("no production code without a
failing test first; if you wrote code first, delete it and start over"). This doc is the self-contained
version of the same discipline, kept as the fallback for anyone running the plugin with the skill
disabled or unavailable.
