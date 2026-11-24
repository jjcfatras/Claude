# pr-review-v2 eval — proposed cases (for sign-off)

Each case is a real PR frozen at the head a reviewer saw **before** the fix landed. The runner builds a throwaway
repo cut at `head` (no descendants, so the fix commits are unreachable), serves `gh` from frozen metadata, and
runs `/pr-review-v2 <n> --dry-run`. Labels come from your own triage (`/pr-respond`, `/respond-to-review`), later
review threads, and fix commits mined from past sessions.

Label tiers:

- **must-find** — a real defect, fixed (or confirmed in-thread) after `head`. Recall is measured on these.
- **neutral** — real but deferred, preexisting, doc/wording, or below the bar; a finding that matches one is
  neither credited nor penalised.
- **must-not-flag** — a confirmed false positive; a finding here counts against precision.
- Any other finding is **unlabeled** and goes to the precision grader (Step 2).

| id             | repo#pr                    | base → head                  | size                         | must-find | neutral | must-not | why it's in                                              |
| -------------- | -------------------------- | ---------------------------- | ---------------------------- | --------- | ------- | -------- | -------------------------------------------------------- |
| fs2262         | fairsquare#2262            | a9fed09 → 388b612            | 6 files +69/-5               | 1         | 4       | 0        | v2 returned **0** here; reviewer found real gaps         |
| fs2261         | fairsquare#2261            | cdc942a → 0232b9a            | 15 files +349/-27            | 5         | 1       | 0        | one TP flaky across v2 runs; 2 v2 misses                 |
| fs2257         | fairsquare#2257            | cdc942a → fc714b3            | 15 files +754                | 10        | 1       | 0        | run 1 missed 3 defects run 2 found on unchanged lines    |
| fs2227         | fairsquare#2227            | ba733a6 → 52b4e03            | 136 files +13778/-113        | 21        | 22      | 0        | large PR; 2 reviewers found ~33 items v2 missed          |
| fs2258         | fairsquare#2258            | fe4164f → ccc4ff9            | 7 files +200/-22             | 1         | 4       | 1        | reviewer-only defect + a human false positive            |
| fs2189         | fairsquare#2189            | 8731550 → af31781            | 2 files +562                 | 4         | 3       | 0        | data migration; no AI run yet                            |
| fs2082         | fairsquare#2082            | d2ddf18 → 6b6e15b            | 20 files +573/-123           | 6         | 0       | 0        | retry/error-message regressions; no AI run yet           |
| fs2260         | fairsquare#2260            | fe4164f → d52893f            | 8 files +183/-13             | 1         | 0       | 0        | positive control (v2 caught it)                          |
| qa77           | qa-testinator#77           | 80e82b9 → a8b02bd            | 4 files +190/-55             | 6         | 0       | 0        | run 1 gated 2, missed 1; run 2 posted                    |
| qa75           | qa-testinator#75           | 8dd2ffb → 97f1815            | 10 files +413/-209           | 5         | 1       | 0        | run 1 missed 2 that run 2 caught                         |
| qa107          | qa-testinator#107          | 023c1df → 4f37729            | 10 files +97/-38             | 1         | 1       | 0        | positive control (v2 caught it)                          |
| qa92           | qa-testinator#92           | 9612eab → 5440d2a            | 13 files +264/-76            | 2         | 1       | 1        | v2's only confirmed false positive                       |
| fs2253         | fairsquare#2253            | 8bcb21a → 771f1f8            | 3 files +41                  | 0         | —       | —        | no known defect (v2 0, reviewers flagged only out-of-PR) |
| fs2263         | fairsquare#2263            | a9fed09 → 7df52af            | 9 files +152/-113            | 0         | —       | —        | no known defect (v2 0 in two runs)                       |
| fs2248         | fairsquare#2248            | d5ed9cc → cdc942a            | 13 files +328                | 0         | —       | —        | no known defect (v2 0)                                   |
| ui1213-planted | fairsquare-ui#1213 + patch | 1c70b25 → 0a32745 + 1 commit | 8 files +136/-27, +8 planted | 1         | —       | —        | **planted**: react effect cleanup                        |
| fs2248-planted | fairsquare#2248 + patch    | d5ed9cc → cdc942a + 1 commit | 13 files +328, +15 planted   | 1         | —       | —        | **planted**: claude-md rule violation                    |

Totals: 17 cases (15 real, 2 planted), **65 must-find**, 2 must-not-flag, 3 no-known-defect PRs. fs2227 runs 1
rep; every other case runs the standard rep count. Domains covered: errors/async (most), security (2260, 107, 92,
2227, 2262), typescript (75, 77), perf (75, 2227), infra/migrations (2189, 2227), test gaps (2262, 2261, 2189,
2082), react (ui1213-planted), claude-md (fs2248-planted).

## Labels per case

### fs2262 — adobe-sign permission gate

- must-find: `adobe-sign-vendor.controller.spec.ts:170` — all 403 rows send a valid body, so moving `assertPermission` below body parsing stays green (fixed 1daa68b)
- neutral: `adobe-sign-vendor.controller.ts:27` Online Checkout may now 403 (deferred); `:28` in-repo callers hit the gate as the system key (deferred); PR-description SQL error; `salesforce.route.ts:234` follow-up citation

### fs2261 — Okta/SSO local-sign-in guard

- must-find: `passkey.controller.ts:133` Okta check runs after `completeRegistration` already persisted the credential (v2 run 1 missed, run 2 found; author confirmed)
- must-find: `auth.service.ts:118` guard called only after a successful-login write
- must-find: `password.service.ts:555` duplicate of the new `assertLocalSignInAllowed` guard
- must-find: `passkey.controller.spec.ts:464` no Okta-refusal test; guard mock defaults to resolved (v2 miss, fixed 2f97332)
- must-find: `user-identity.repository.ts:39` `hasLinkForUser` always mocked, never tested (v2 miss, fixed 2f97332)
- neutral: `password.service.ts:68` helper placed between class JSDoc and decorator

### fs2257 — Okta SSO login

- must-find: `sso.controller.ts:26` gate only checks `type !== 'api'` (repo has `isTrustedServiceToken`)
- must-find: `sso.controller.ts:36` session minted with `tfa: true` on a comment's say-so (run 1 missed)
- must-find: `sso.service.ts:121` missing OKTA_* env vars not caught at startup
- must-find: `sso.service.ts:151` JWKS fetch failures land in the same catch as real signature/claim failures
- must-find: `sso.service.ts:180` OIDC discovery fetch has no timeout
- must-find: `sso.service.ts:186` discovery failure surfaces as a raw `TypeError`
- must-find: `sso.service.ts:204` `markUsed` awaited on the sign-in critical path (run 1 missed)
- must-find: `sso.service.ts:215` untyped discovery body read (run 2 dropped it as a "dismissed" prior issue; fixed 52d87b2)
- must-find: `sso.service.ts:219` `createEmployee` + `link` not in one transaction
- must-find: `sso.service.spec.ts:189` role/accountType fixtures can't tell the link and reject paths apart
- neutral: nonce note (below-gate in both runs)

### fs2227 — lender offers (large)

- must-find (runtime): `giggle-offer.service.ts:236` TLS verification off for SSN/DOB; `sbl-lead-offer.repository.ts:260` raw request with PII persisted; `lead-offer-submit.service.ts:362` raw exception text returned to guests; `lead-offer-result.service.ts:151` lost compare-and-set answered 200; `lead-offer-result.service.ts:29` CLOSED_STATUSES omits EXPIRED; `sbl-lead-offer.repository.ts:329` `markAccepted` without compare-and-set; `lead-offer-submit.service.ts:478` outcome-write failure makes the sweep call the lender again; `2026-11-16-sbl-lead-offer-expired-status.ts:56` `down()` fails with 23505; `lead-submission.service.ts:598` request-path expiry never reaches Salesforce; `service-token.ts:19` external-API deny-list rollout gate; `fundbox-offer.service.ts:79` Fundbox auth with no timeout and default retries; `lead-offer-submit.service.ts:633` pre-check swallows every failure; `lead-offer-sweep.service.ts:129` module init awaits the full sweep; `lead-offer-sweep.service.ts:253` status-update errors logged and dropped; `lead-offer-sweep.service.ts:154` slow vendor-api stalls lost-submit re-send; `sbl-lead-offer.repository.ts:207` no index for the CTA lookup; `main.tf:335` Giggle still in LEADS_NON_API_LENDERS
- must-find (test gaps hiding a real bug): `customer.permission-set.ts:11` offers-* grants untested (exposed a real customer offers-read bug); `lead-offer-waiter.service.ts:18` PENDING removable from OPEN_OFFER_STATUSES with tests green; `lead-offer-result.service.ts:132` CAS tested only on REQUESTED rows; `lead-offer-result.service.spec.ts:242` lenderRef precedence untestable
- neutral (22): doc/comment contradictions (`lender-offer.type.ts:22`, `2026-11-15-sbl-lead-offer.ts:15`, `lead-offer-submit.service.ts:575`, `sbl-lead-offer.model.ts:165`, `lead-lender-matches.service.ts:98`, `lead-offer-sweep.service.ts:72`, `fundbox.type.ts:131`, `lead-offers-read.routes.ts:24`, `main.tf:332`, `lead-offer-waiter.service.ts:38`, `lead-offer-submit.service.ts:402`), duplication (`lead-offer-result.service.ts:34`, `lead-submission.service.ts:598` twice-written sequence, `lead-offer-sweep.service.ts:65`, `:305`, `lead-offer-waiter.service.ts:21`), typing (`giggle-offer.service.ts:211`, `:360`), weak test pins (`sbl-lead-offer.dal.spec.ts:245`, `lead-submission.service.spec.ts:1850`, `lead-offer-result.service.spec.ts:235`), `lead-offer-submit.service.ts:54` hard-coded lender input
- excluded: jquirossoto F-001…F-007 (posted at a later head; may not exist at 52b4e03)

### fs2258 — logger request bodies

- must-find: `log-body.utils.ts:10` `in` on an unvalidated body: null/scalar throws `TypeError` instead of `INVALID_INPUT` (fixed aa11c76)
- neutral: `request-log.service.ts:39` (preexisting), `vendor.service.ts:74` 204 body shape (deferred), `logger-routes.spec.ts:529` docblock, missing ticket ref
- must-not-flag: `local.tf:22` "array body encrypted before AJV" — logger encryption doesn't apply there

### fs2189 — Plaid import → asset report migration

- must-find: `…plaid-import-connections-to-asset-report.ts:44` copies a failed generation's token into the READY row
- must-find: `…:61` hardcodes `days_requested = 730` where #2053 labels the same token 153
- must-find: `plaid-import-conversion-migration.spec.ts:153` `days_requested` never asserted
- must-find: `plaid-import-conversion-migration.spec.ts:347` safety table covers one shape only
- neutral: `…:33` ciphertext equality breaks after key rotation (deferred, documented); `asset-report-pending-index.spec.ts:39` docblock; Jira QA steps

### fs2082 — Salesforce 503 mapping / maxRetries

- must-find: `user-preference-sync.service.ts:563` 503 mapping makes Connector retries replay Salesforce calls 3× under the advisory lock
- must-find: `salesforce.service.ts:206` token-fetch errors regress to bare `invalid_grant` (SFMC catches too)
- must-find: `lead-opportunity.service.ts:362` only 3 of 10 `maxRetries: 0` creates asserted
- must-find: `salesforce.client.ts:197` `linkExternalFile` `maxRetries: 0` untested
- must-find: `user-preference-sync.service.ts:563` Preference_Profile `maxRetries: 0` not asserted
- must-find: `marketing-cloud-vendor.service.spec.ts:296` SFMC status mapping pinned by one row

### fs2260 — pnpm enforcement

- must-find: `package.json:7` unpinned `npx only-allow pnpm` runs latest npm version on every install

### qa77 — plaid-50 import polling

- must-find: `plaid-import.ts:199` any non-ok getAssetReport (even 429/5xx) permanently rules out the suite's connection
- must-find: `plaid-50/api.test.ts:148` `Promise.all` rejects while B keeps polling → leaked connection
- must-find: `plaid-50/api.test.ts:128` `resolve(imports.a)` throw leaks c and m
- must-find: `plaid-import.ts:171` one non-2xx from vendorLogs aborts the 8-minute poll
- must-find: `plaid-import.ts:204` `as` cast + `?? []` turn a malformed report into an empty list
- must-find: `plaid-import.ts:142` private ImportRow duplicates sibling Generation types

### qa75 — DLP tokens

- must-find: `dfp-265/api.test.ts:45` eager mint in `beforeAll` fails all 11 sad-path tests
- must-find: `dfp-265/api.test.ts:85` `as Channel[]` lets a typo compile
- must-find: `dfp-265/api.test.ts:256` `Record<string, DlpToken>` instead of `Record<Channel, …>`
- must-find: `dlp-token.ts:66` `pick()` claims "none found" when candidates all failed the filter
- must-find: `dlp-token.ts:157` six independent mints awaited serially
- neutral: `dlp-token.ts:122` minted tokens printed to CI logs (untriaged)

### qa107 — postgres MCP

- must-find: `scripts/postgres-mcp.sh:45` caller `connectionString` overrides the hardened one; PG* password follows it
- neutral: `CLAUDE.md:88` says three MCP servers while README.md (outside the diff) still says two

### qa92 — sf-login / shared soql

- must-find: `scripts/sf-login.mjs:34` token fetch has no catch; one org's network error kills the rest
- must-find: `scripts/sf-login.mjs:48` unpinned `npx -y @salesforce/cli` inherits env
- neutral: `claude-md-1` (skipped, reason unknown)
- must-not-flag: `dfp-237/api.test.ts:245` dropped x-brand argument — vendor-api never reads it

### fs2253, fs2263, fs2248 — no known defect

No in-PR defect was flagged by v2 or by reviewers. These measure how many findings v2 emits on code nobody
objected to; every finding here is unlabeled and goes to the precision grader.

### ui1213-planted — fairsquare-ui#1213 + `planted/ui1213-react-effect-cleanup.patch`

The patch adds an auto-dismiss effect to `document-slot-card.tsx`, one commit on top of the real head.

- must-find: `document-slot-card.tsx:34` — the `setTimeout` is never cleared. An earlier error batch's timer
  wipes a newer batch early, and `setErrors` fires after unmount (react: effect cleanup).
- Everything else in the real PR is unlabeled.

### fs2248-planted — fairsquare#2248 + `planted/fs2248-claude-md-swc-spyon.patch`

The patch adds a bare `export function normalizeIssuer` (`user-identity.repository.ts:7`) and a spec that spies
on it.

- must-find: `user-identity.repository.spec.ts:32` — `jest.spyOn(repositoryModule, 'normalizeIssuer')` on a bare
  named export. The fairsquare rule `.claude/rules/testing-gotchas.md` (scoped to `**/*.spec.ts`) says this throws
  `TypeError: Cannot redefine property` under `@swc/jest`. The fix is to export an object and call through it. A
  finding anchored at `user-identity.repository.ts:7` that names the same problem also counts.

## Notes

- **Line drift:** some labels come from a comment posted at a later head. Before the pilot, the fixture builder
  checks that every must-find `file:line` exists at `head` and lists any that don't.
- **Fixtures are identifiers, not code:** each case stores the repo, `base` and `head` SHAs, and an optional patch.
  The runner builds a throwaway clone from your local checkout (`git clone --no-local --single-branch` cut at
  `head`), so no fairsquare source is committed here except the two planted patches.
