# Triage summary — Step 3 table

Render this in the Step 3 message, split by source within each verdict category so the user can see at a glance where each concern came from:

```text
## Review Item Triage — PR #NUMBER

### Preexisting (N)
  Inline comments:
    - **file.ts:42** (@reviewer): "Their comment..." → Code predates PR (blame: abc1234, 2025-01-15)
  Review-body findings (@reviewer on review #12345):
    - "Their finding..." → Targets existing handler not modified by this PR

### False Positives (N)
  Inline comments:
    - **file.ts:88** (@reviewer): "Their comment..." → Already handled by XYZ middleware
  Review-body findings (@reviewer on review #12345):
    - "Their finding..." → Current architecture already addresses this via EVIDENCE

### Valid Issues (N)
  Inline comments:
    - **file.ts:120** (@reviewer): "Their comment..." → Plan: add null check before accessing .property
    - **file.ts:7** (@reviewer): "Their comment..." → verifier could not reproduce the false-positive evidence: cited guard is not at handler.ts:88 → Plan: add the guard
  Review-body findings (@reviewer on review #12345):
    - "Their finding..." → Plan: add error boundary around the X call site
```

## Shape rules

- Omit a sub-section when it is empty (don't show an "Inline comments:" header with nothing under it). Omit the entire verdict category if its count is zero.
- Mark each dismissal the workflow's verifier upheld (`verified: true`) with a `(verified)` suffix.
- A verdict the verifier overturned (`overturned` set) lands under **Valid Issues** with the note `→ verifier could not reproduce the <original verdict> evidence: <reason>`, so the user sees the flip and can use `Override some` to restore the original if they disagree.
- Inline-path dismissals carry no suffix — the orchestrator reproduced them itself, so there was no second reader.
