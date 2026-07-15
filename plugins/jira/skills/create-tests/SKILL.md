---
name: create-tests
disable-model-invocation: true
description: Generate a runnable Postman QA collection from a JIRA ticket — mine its acceptance criteria for happy-path and containment/negative scenarios, pin the real API contract (endpoints, auth, request/response shapes, failure semantics) from the codebase, then author a chained, self-verifying collection into a Postman workspace via the Postman MCP after approval. Use whenever the user wants QA tests, a Postman/API test collection, or executable test cases from a ticket (e.g. "create tests for BAU-1810", "make a QA collection for PROJ-42").
argument-hint: <JIRA-key> (e.g. PROJ-1234); optionally a sample request/workflow file to seed from
model: opus
effort: xhigh
allowed-tools: Bash, Read, Grep, Glob, Agent, AskUserQuestion, mcp__claude_ai_Atlassian_Rovo__getJiraIssue, mcp__claude_ai_Atlassian_Rovo__getAccessibleAtlassianResources, mcp__plugin_postman_postman__getWorkspaces, mcp__plugin_postman_postman__getWorkspace, mcp__plugin_postman_postman__getEnvironments, mcp__plugin_postman_postman__getEnvironment, mcp__plugin_postman_postman__createCollection, mcp__plugin_postman_postman__getCollection, mcp__plugin_postman_postman__updateCollection, mcp__plugin_context7_context7__resolve-library-id, mcp__plugin_context7_context7__query-docs
---

Take a JIRA ticket and turn it into a runnable, self-verifying Postman QA collection — the kind a manual tester runs top-to-bottom against a live deployment, where a clean pass _is_ the proof the ticket's acceptance criteria hold. What separates this from "translate the ticket into requests" is the same skepticism `implement-ticket` aims at a ticket's claims, pointed here at the API contract: a ticket, and especially a sample request someone pasted in, describes what the author _thinks_ the endpoint wants. The engine may want something different — a nested body where the sample is flat, a `200` with `failed > 0` where you expected a `4xx`. Write an assertion against the imagined contract and the collection is green against nothing. So before you author a single request, you find out what the API _actually_ does from its own code, and you make the tests grip the real behavior.

Work through the phases in order. Don't skip the contract phase even when a sample or the ticket looks obviously right — that's exactly where the mismatch hides.

## Use context7 for external-library facts

Whenever a step turns on how an _external_ library, framework, or API actually behaves — not how this repo's own code behaves — verify it against current docs with context7 instead of trusting the ticket or your memory: call `mcp__plugin_context7_context7__resolve-library-id`, then `mcp__plugin_context7_context7__query-docs` with the returned ID. It matters most in two places:

- **[3/6] Pin the contract** — when a request/response shape is defined by a framework's conventions (a validation library's error envelope, an auth SDK's token response) rather than by code you can read here.
- **[6/6] Author** — when a Postman test-script or collection-schema detail (a `pm.*` API, a collection-format field) is one you're unsure of; confirm it rather than emit a script that silently no-ops.

Skip context7 for the repo's own endpoints and business logic — those you verify from the code in front of you, not from docs.

## Variables to derive at startup

- **Ticket key** — from `$ARGUMENTS` (e.g. `PROJ-1234`). If it's missing or doesn't look like a JIRA key, ask the user for it before doing anything else.
- **Seed file** — `$ARGUMENTS` may also name a sample request/workflow/spec file (a `*.json`, an OpenAPI fragment, a saved request). If given, treat it as a _starting hypothesis_ about the contract, not the truth — it gets reconciled against the code in phase [3/6].
- **cloudId** — `getAccessibleAtlassianResources` returns the Atlassian site(s). If there's exactly one, use it; if more than one, call the **AskUserQuestion** tool (`multiSelect: false`, header `Site`) populated with one option per site (name/URL) and use the chosen site's `cloudId`.

## [1/6] Fetch the ticket

Call `getJiraIssue` with the key and cloudId. Read **everything**, not just the description:

- **Description** — the feature or change under test and why it matters.
- **Acceptance criteria** — the primary source of happy-path scenarios: each AC is something a passing test must demonstrate.
- **Testing notes / QA steps** — often spell out the exact setup ("create a workflow with two script nodes, one with X and one without") that shapes how requests are sequenced.
- **Containment / negative rules** — any "must remain unavailable", "other cases must be rejected", "no cross-tenant leakage" phrasing. These are the sad-path scenarios.
- **Comments and linked issues** — clarifications and edge cases that didn't make the description; weight later comments over the original when they conflict.

If `getJiraIssue` fails, show the error **verbatim** and stop — don't guess the ticket contents from the key alone.

## [2/6] Derive the test scenarios

Turn the ticket into an explicit, written list of scenarios _before_ you touch the API — so the tests are driven by what the ticket wants proven, not by whatever the endpoints happen to make easy. Sort every scenario into one of two buckets and tag it with the AC (or rule) it proves:

- **Happy path** — the behaviour each acceptance criterion asserts _should_ work. One scenario per AC where you can; name it for what it demonstrates.
- **Sad path** — what should be **rejected or contained**: the negative rules, the "unspecified cases stay unavailable", the malformed input. Containment criteria (AC says feature X is exposed → the sad path proves feature Y is _not_) are the highest-value negative tests and the easiest to forget.

Keep _what to prove_ (this phase) separate from _how the API does it_ (next phase). A scenario like "an allowlisted crypto fn is callable; a non-allowlisted one is not" is complete here without yet knowing the endpoint — that's the point.

## [3/6] Pin the API contract from the code

This is the phase that earns its keep. Every scenario becomes one or more HTTP requests, and each request needs the _real_ method, path, headers, body shape, and success/failure signature — pulled from the code, with `file:line` evidence, never assumed.

For each endpoint a scenario touches, establish:

- **Base URL / host convention** — how the running service's address is expressed (an env var, a gateway host, path-routing across services). This becomes a collection/environment variable, not a literal.
- **Auth flow** — the login (or token) endpoint, its **required headers** (a brand/tenant header, content-type), the request body, and — critically — the **exact field name** the token comes back in (`token` vs `accessToken` vs `jwt`) and how it's sent on later calls (`Authorization: Bearer …`). Guessing the token field is the most common way the whole chain fails on request two.
- **Per-endpoint shape** — method, exact path (trailing slashes matter), request-body shape, and response shape. Trust the repo's own tests (`*.spec.ts`, `*_test.go`, etc.) for body shapes over prose or a sample — a passing test is executable proof of the contract.
- **How failures surface** — before writing any sad-path assertion, find out what a rejected/failed call actually returns. Many engines return **HTTP 200 with an error in the body** (a `failed > 0` count, a `status: "failed"` log entry) rather than a `4xx`. Assert against whatever the code actually does.

When several endpoints are involved, spawn parallel `Agent` investigators (one message, several calls) — give each a cluster of endpoints and demand exact paths, field names, and quoted code with `file:line`, because those strings go straight into the requests and assertions. Merge their findings. When it's one or two endpoints, verify inline.

**Reconcile any seed file against the real create/write schema.** A sample the user hands you is frequently _flatter_ than the endpoint accepts — the endpoint reads a nested sub-object and rejects unknown top-level keys (`additionalProperties: false`), so a naive paste `400`s. Diff the sample against the code's actual schema and build the request from the code. See `${CLAUDE_PLUGIN_ROOT}/references/postman-qa-collection.md` for the reconciliation method and the recurring authoring traps.

## [4/6] Ask only the genuinely-unknowable

Some facts aren't in the repo. Batch them into **one** AskUserQuestion (`multiSelect: false`) rather than dribbling out questions — ask only what you genuinely couldn't determine:

- **Target Postman workspace** — call `getWorkspaces` and offer the results as options; the collection is created there.
- **Target environment / base URL** — call `getEnvironments`/`getEnvironment` first and see if the host is discoverable (note the tool may want the team-prefixed `uid`, not the bare id). If the repo only has localhost defaults and no dev/QA host, ask which environment to target and which variable carries the base URL.
- **Sad-path intent** — when the ticket leaves the negative scenario open-ended, offer the candidate(s) you derived and let the user pick or redirect (handle the Other field).

Surface, don't ask, the **prerequisites** the tester will need: which credentials/permissions the login user must hold (e.g. the abilities the protected routes require), since a collection can't supply those and requests will `403` without them. These go in the handoff, not in a question.

## [5/6] Present the collection plan for approval

Show the user, in the conversation, **before creating anything**:

1. **Scenarios → ACs** — the happy and sad scenarios from phase [2/6], each tagged to the acceptance criterion it proves, so the user can see the coverage.
2. **Request sequence** — the chained, stateful flow (login → set up state → exercise → assert), with the variables each step sets and the ones it consumes.
3. **Reconciled request bodies** — the actual bodies you'll send, called out where they differ from any seed file (and why a flat paste would have failed).
4. **Structural constraints** — the Postman-MCP realities that shape the deliverable, stated up front so the plan doesn't promise something the build can't produce: test scripts live only on top-level requests (so it's a numbered top-level sequence, not happy/sad folders); the login request overrides the collection's Bearer auth to No Auth; names carry a `{{$timestamp}}` suffix so the collection re-runs without colliding when there's no cleanup endpoint. See `${CLAUDE_PLUGIN_ROOT}/references/postman-qa-collection.md`.

Then gate with `AskUserQuestion`: build this collection, adjust it, or stop. **Do not call `createCollection` before the user approves** — the value here is front-loaded into getting the contract and coverage right, and the user may know a constraint you don't.

## [6/6] Author and verify

Only after approval. Build the collection with a single `createCollection` into the chosen workspace, following the structure and traps in `${CLAUDE_PLUGIN_ROOT}/references/postman-qa-collection.md`: numbered top-level requests, collection-level Bearer auth with the login override, per-request `pm.test` assertions that extract state into collection variables and assert status **and** body per the real failure signature, distinct variable names that won't shadow the environment.

Then **round-trip verify**: call `getCollection` and confirm what persisted — every request is present, the collection auth is set, the variables exist, and the reconciled (e.g. nested) bodies survived the write intact. A collection that didn't save the way you built it is a silent failure.

Report the handoff: how the tester runs it (select the environment, provide credentials, run the collection top-to-bottom), the documented prerequisites (login abilities/permissions, which environment), and what each scenario proves. State plainly that it wasn't executed here — with no credentials and no live API in-session, the `pm.test` assertions _are_ the deliverable; the tester's run is what turns them green. If `createCollection` or `getCollection` fails, show the error **verbatim** and stop rather than reporting a success you didn't confirm.
