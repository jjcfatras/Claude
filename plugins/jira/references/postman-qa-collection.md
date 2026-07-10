# Authoring a QA Postman collection

A QA collection is only worth shipping if a clean run actually proves something. Two things break that: an assertion written against a contract the API doesn't have (so it passes vacuously or the request never gets far enough to assert), and a collection that the Postman MCP silently reshaped on write (so what runs isn't what you built). This doc collects the recurring traps — the ones that cost a rediscovery each time — and the conventions that avoid them. Verify the API-specific facts against the code (phase [3/6]); the structural facts here are properties of the Postman collection format and MCP.

## Reconcile the sample against the real schema

A sample request handed to you — a saved workflow JSON, a curl someone pasted, an old export — is a _hypothesis_ about the contract, and it's usually flatter than the endpoint accepts. The common failure: the create/write endpoint reads a **nested** sub-object and rejects unknown top-level keys (`additionalProperties: false`), so pasting the flat sample `400`s.

Diff the sample against the code's actual schema before trusting it:

- Find where the handler reads the body (the DTO, the validation schema, the `*.spec.ts` that posts to it).
- Note the nesting the endpoint expects and whether extra keys are rejected.
- Rebuild the request body from the **code**, using the sample only for realistic values.

```
sample (flat, 400s):        real create body (nested):
{                           {
  "name": "...",              "name": "...",
  "definition": {...},        "version": { "version": 1, "definition": {...} }
  "status": "active"        }   // top-level "definition"/"status" rejected: additionalProperties:false
}
```

## Structure: top-level requests, not folders

Test scripts (`event` entries — the `pm.test` code) attach only to **top-level `item[]` requests**, not to folder-nested items. Since the assertions are the whole point of a QA collection, don't organize into Happy/Sad folders — you'll lose the scripts on write. Flatten to a numbered top-level sequence so the run order is unambiguous:

```
1. Login
2. Happy — create resource
3. Happy — execute & assert ACs
4. Sad — forbidden operation is rejected
5. Sad — malformed input is rejected
```

## Auth: collection Bearer, login overrides to none

Set **collection-level** auth to `Bearer {{token}}` so every request inherits it. The login request is the exception — it's a public route and must **override to No Auth**, or it sends an empty/placeholder bearer and behaves differently from production. Login's test script extracts the token into a collection variable for everything downstream.

Watch the schema asymmetry: **collection variables** take `key`/`value`/`description`/`disabled` and **no `type`**, but an **auth token value entry** _does_ take a value `type`. Getting these crossed is a silent write rejection.

## Chain state through collection variables

The collection is a stateful, top-to-bottom flow: each request's test extracts what the next needs and stashes it, and later requests read it.

```js
// in a request's test script
const json = pm.response.json();
pm.test("login returns a token", () => pm.expect(json.token).to.be.a("string"));
pm.collectionVariables.set("token", json.token); // NOT json.accessToken — confirm the field from the code
```

Use **distinct variable names** (`qa_email`, `happyVersionId`, …) so they don't shadow keys the shared environment already defines (`email`, `auth_login_token`). Depend on the selected environment for **only** the base URL; keep everything else in the collection so it's self-contained.

## Assert on the real failure signature — not the status you assumed

Intuition says a failed operation is a `4xx`. Many engines return **HTTP 200 with the failure in the body**: a `failed > 0` count, a `logs[].status: "failed"` entry, an `errors[]` array. A sad-path test that only checks `pm.response.to.have.status(400)` then goes green against a `200`-with-error is worse than no test. Find how failure surfaces from the executor code (phase [3/6]) and assert against **that**:

```js
const r = pm.response.json();
pm.test("forbidden op is rejected", () => {
  pm.expect(pm.response.code).to.eql(200); // the engine returns 200...
  pm.expect(r.failed).to.be.above(0); // ...and reports the failure in the body
});
```

## Make it re-runnable without cleanup

If there's no DELETE/teardown endpoint, a fixed resource name collides on the second run (name+version uniqueness → `400`). Suffix created-resource names with `{{$timestamp}}` (Postman's built-in dynamic variable) so each run creates fresh state instead of colliding.

## Round-trip verify the write

`createCollection` can reshape or drop what it didn't accept. After creating, call `getCollection` and confirm: every request is present, collection auth is set, the variables exist, and the reconciled (nested) bodies survived intact. A collection that saved differently than you built it is a silent failure — catch it here, not when the tester does.

## Environment lookup quirk

`getEnvironment` may reject the bare environment id and require the **team-prefixed `uid`** (as returned by `getEnvironments`) — expect a possible retry with the `uid` form.
