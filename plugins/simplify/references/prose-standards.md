# Distillation rubric

Distillation is lossless compression of natural language: every sentence in the output preserves the information content of the input. Boil off redundancy; leave concentrated meaning. If a cut would drop a fact, a qualification, or a nuance, it is not distillation — do not propose it.

## Hierarchy of cuts

Apply in order. Each level removes less essential material:

1. **Redundant phrases** — the same thing said twice in different words.
2. **Filler words** — "actually", "basically", "essentially", "really", "very", "quite", "rather".
3. **Empty hedges** — "somewhat", "arguably", "it could be said that", "in a sense".
4. **Throat-clearing** — openers that delay the point ("It is worth noting that", "It should be mentioned that").
5. **Nominalizations** — noun forms where verbs are stronger ("make a decision" → "decide", "perform an analysis" → "analyze").
6. **Passive constructions** — where active is clearer and shorter without changing emphasis.
7. **Prepositional chains** — "the result of the analysis of the data" → "the data analysis result".
8. **Weak verb + adverb** — replace with one precise verb ("moved quickly" → "darted").

## What to preserve

- Technical precision and domain terminology.
- Genuine qualifications and nuance — "may", "in most cases", "unless X" often carry real meaning; only empty hedging goes.
- Logical structure and argument flow.
- Voice and character — distill the style, don't flatten it.
- Specific details: numbers, names, references, links.

## What does not count as distillation

- Tone or register changes.
- Restructuring document flow (moving or merging sections).
- Adapting for a different audience.
- Summarizing — that is lossy; distillation is lossless.

## Defer to project standards

Project-specific writing conventions in the nearest `CLAUDE.md` (then the root `CLAUDE.md`) override this rubric. A project that mandates hedged language in user-facing docs, or a fixed doc structure, wins over any cut level above.

## Examples

### Filler and hedge removal

**Before:** "It is essentially worth noting that the system actually performs quite well in basically all of the scenarios that were tested."

**After:** "The system performs well in all tested scenarios."

### Nominalization → verb

**Before:** "We performed an investigation into the cause of the failure and made a determination that the configuration was incorrect."

**After:** "We investigated the failure and determined the configuration was incorrect."

### Redundancy elimination

**Before:** "The end result of this process is that each and every individual component is tested and verified to ensure and confirm that it meets the required specifications and standards."

**After:** "This process verifies each component meets the required specifications."

### Preserved nuance

**Before:** "While the approach generally works well in most common scenarios, there are some edge cases, particularly those involving concurrent access patterns, where the current implementation may exhibit degraded performance characteristics."

**After:** "The approach works well in common scenarios but may degrade under concurrent access patterns."

Note: "may" survives — it is a genuine qualification, not a hedge.

---

Adapted from the MIT-licensed `prose-distill` skill in [laurigates/claude-plugins](https://github.com/laurigates/claude-plugins).
