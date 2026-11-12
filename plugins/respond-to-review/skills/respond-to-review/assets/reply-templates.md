# Reply templates — Step 6

Each per-item template produces the body for a single item. Inline replies post it directly. Aggregated review-body replies embed it under a per-finding heading (see the skeleton at the end).

## Placeholders

| Placeholder             | Filled from                                          |
| ----------------------- | ---------------------------------------------------- |
| `COMMIT_SHA`            | `evidence.blame_sha`                                 |
| `DATE`                  | `evidence.blame_date`                                |
| `SPECIFIC_EVIDENCE`     | `evidence.file:line` plus `evidence.quote`           |
| `DESCRIPTION_OF_CHANGE` | the Step 4 record                                    |
| `<SHORT_SHA>`           | `git rev-parse --short HEAD` after the Step 5 push   |
| `BRIEF_EXPLANATION`     | the verdict's `reasoning`, or the Step 4 record      |
| `REVIEWER`              | the item's `reviewer`                                |
| `REVIEW_ID`, `N`        | the item's `review_id` and its Step 1c finding index |

## Self-authored variant

When the item's reviewer is the current user (a self-authored review), keep the same structure but drop the second-person flourishes: write `Fixed in commit …` instead of `Good catch! Fixed in commit …`, and open the aggregated body with `Re: my earlier review summary — addressing the findings below.` rather than @-mentioning yourself. Everything else, including the HTML-comment markers, is unchanged.

## Preexisting

```text
This code predates this PR — it was introduced in commit `COMMIT_SHA` (DATE). The change in this PR doesn't modify this line.

If you'd like this addressed, I can create a follow-up issue to track it separately.
```

## False positive

```text
This is actually already handled — SPECIFIC_EVIDENCE.

BRIEF_EXPLANATION of why the current code is correct.
```

## Valid (fixed)

Used only when Step 5 ran.

```text
Good catch! Fixed in commit `<SHORT_SHA>` — DESCRIPTION_OF_CHANGE.

BRIEF_EXPLANATION of what was changed and why it addresses the concern.
```

## Valid (noted, not fixed — vague finding from Step 4)

```text
Noted — this is a valid concern but broader than what I'll resolve inside this PR. I'll address it in a follow-up.

BRIEF_ACKNOWLEDGEMENT of the core concern so the reviewer knows we heard them.
```

## Aggregated review-body reply

One per `review_id`, written to `$REVIEW_DIR/reply-review-<REVIEW_ID>.md`. One trailing marker per finding included in the reply.

```text
@REVIEWER re: your review summary — addressing your findings below.

### 1. <short handle for finding, e.g., first ~60 chars of the finding text>

> Quoted snippet of the original finding (blockquote the reviewer's words)

<per-item template body from above, matching this finding's verdict>

### 2. <short handle for next finding>

> Quoted snippet

<per-item template body>

...

<!-- respond-to-review:review-<REVIEW_ID>:finding-0 -->
<!-- respond-to-review:review-<REVIEW_ID>:finding-1 -->
```
