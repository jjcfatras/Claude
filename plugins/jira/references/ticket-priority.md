# Ticket Priority

A priority level is only meaningful relative to how _this team_ uses the field. Grading a new ticket against a personal severity scale produces a number that reads fine and means nothing on the board it lands on. So the method here is: **derive the bar from the project's own gradings, then default to the floor unless you can name the concrete harm.**

## Calibrate against the project, not a personal scale

Once the destination is known — the epic when there is one, else the project — look at what actually earns the upper bands there:

```
parent = <EPIC-KEY> AND priority in (High, Highest) AND created > -180d ORDER BY created DESC
```

Read the **summaries** and note what they have in common — that's the bar. Scope to the epic whenever the ticket has one: a maintenance queue and a product board grade the same defect differently, and the epic is the closer comparison. With no epic, swap the scope to `project = <KEY> AND …`.

Two things the calibration tells you that a static ladder can't. First, whether the upper bands are used at all — a project where nothing is above Medium is telling you its High means "page someone". Second, where the line actually sits: a team whose Highs cite tens of thousands of failed events has a very different bar from one whose most recent High cites 72.

> **Trap:** `searchJiraIssuesUsingJql` does **not** cap itself — pass `maxResults: 5` and `fields: ["summary","priority","issuetype"]` explicitly, or an active upper band returns 50+ issues at ~900 characters each and overflows into a saved file. One page of the upper band is enough to calibrate — do not enumerate the project.

## Fallback ladder

When calibration isn't available — freeform input with no project chosen yet, the MCP unavailable, a brand-new project with no history — grade against this, and replace it with observed gradings the moment they exist:

- **High** — a real defect with demonstrated impact, **or** a security/authorization gap. Not "production emergency"; a live authz hole with zero observed exploitation still clears this bar.
- **Medium** — a real defect or meaningful risk, but bounded blast radius, no live exposure, or purely internal/diagnostic impact.
- **Low** — build tooling, lint hygiene, deduplication, type narrowing with no live defect, cosmetic warnings, docs. Anything whose own body says _"nothing is broken today"_ belongs here.
- **Highest / Lowest** — leave alone unless the project demonstrably uses them. Most schemes live in the middle three, and reaching for an outer band you haven't seen used is the loudest possible miscalibration.

## Anti-inflation discipline

**Default to Low.** Most tickets are correctly Low, and a queue is supposed to be mostly Low. Raising everything carries exactly as much information as raising nothing — it destroys the signal the priority field carries.

Grade above the floor only when you can name the **concrete harm**. These are not concrete harm:

- "it would be nice to fix"
- "it's good hygiene"
- "it blocks a future upgrade"

Nor is the urgency in how the change was described to you. A tooling chore filed with "we really need to sort this out" is still a tooling chore; grade the change, not the framing.

Before settling on a level, sanity-check the distribution it implies: if this ticket is Medium, would the majority of that project's queue be Medium+? A board where most things are above the floor carries no more information than one where everything sits on it.

## Observed vs. speculative — the High/Medium line

The primary discriminator is whether the harm is **demonstrated** or **theoretical**. Real event counts, a live exposure, a reproduced failure → the case for High is open. "A future caller could…", "if someone were to…", "this is latent" → cap at Medium, unless it's an exploitable security boundary, which clears High on its own.

The secondary discriminator is **blast radius and egress**. A worked example: a request-log sanitizer that misses one header name, so a live capability credential is written unmasked into a durable store — a real defect, happening today. It was still graded **Medium**, not High, because the store is encrypted at rest and the field never reaches the downstream log index. Real defect, bounded blast radius, no live egress is textbook Medium.

One caveat inherited from the rest of this workflow: if the grade rests on a factual claim about the code, verify the claim before grading on it (see `claim-verification.md`). A raise that depends on a claim the code no longer supports is not a raise.

## Justify in one sentence

State the level with the harm that earned it, citing what it's calibrated against:

- `High — endpoints enforce authentication but no authorization on a PII-bearing store; matches the project's existing security-gap Highs.`
- `Medium — real defect, but the credential is encrypted at rest and never egresses; bounded blast radius.`
- `Low — build-tooling only, no runtime or API surface; nothing is broken today.`

A level with no justification is the default stamp wearing a costume.

---

## Worked example — how a real calibration ran

_Illustration, not a rule. The numbers below belong to one project; the method is what ports._

An epic serving as a monorepo maintenance queue had **45 children: 28 Low / 6 Medium / 11 High+** — the upper bands were actively used, so a raise wasn't an exotic act. Its five most recent team-graded Highs:

| What earned it High                                                                |
| ---------------------------------------------------------------------------------- |
| Poison-record loop — 20,669 × 400 over 32 days, 100% failure rate, undetected      |
| 3,364 × 500 upstream errors mis-typed as ours, then retried 3× (amplification)     |
| A daily workflow that had **never** executed its payload (silent business failure) |
| Vendor outcomes all flattened to 500 — only **72** observed events                 |
| Logger endpoints enforcing authn but **no authorization** (zero observed exploit)  |

From those five: High here means _"a real defect with demonstrated impact, or a security/authorization gap"_ — not "emergency". Two entries pin the bar precisely — one cleared it on 72 events, another with no exploitation at all.

**Why grading at creation matters.** In that same epic, two tickets described the identical defect — same routes, same commented-out permission checks — and were filed **four minutes apart**. One was stamped High with a `security` label; the other Low. Nothing distinguished them but the moment of filing. An ungraded default is not a neutral placeholder; it is a wrong answer that looks like a considered one.
