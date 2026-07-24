---
name: create-ticket
disable-model-invocation: true
description: Generate a structured JIRA ticket (summary, acceptance criteria, QA testing steps) from a diff, PR, or description and file it in JIRA after confirmation
argument-hint: "[PR url/number | branch | freeform description] (optional — infers from context if omitted)"
allowed-tools: Bash(git *), Read, Grep, Glob, mcp__plugin_github_github__pull_request_read, mcp__claude_ai_Atlassian_Rovo__getAccessibleAtlassianResources, mcp__claude_ai_Atlassian_Rovo__getVisibleJiraProjects, mcp__claude_ai_Atlassian_Rovo__getJiraProjectIssueTypesMetadata, mcp__claude_ai_Atlassian_Rovo__createJiraIssue, mcp__claude_ai_Atlassian_Rovo__getJiraIssue, AskUserQuestion
model: sonnet
effort: medium
---

Turn a change — a git diff, a pull request, or a plain description — into a well-structured JIRA ticket, then file it in JIRA once the user approves. A good ticket here is one a developer can pick up and a QA tester can verify **without ever reading the code**, so the whole point of this command is to translate "what changed in the code" into "what someone can observe from the outside."

## Step 0: Figure out what you're working from

Look at `$ARGUMENTS` and the surrounding context to decide the input source. You don't need to ask if it's obvious:

- **Pull request** — `$ARGUMENTS` is a PR URL or `#number`. Read it with `pull_request_read` (title, body, and the file changes) so the summary reflects the actual change, not just the title.
- **Branch / working tree** — `$ARGUMENTS` names a branch, or it's empty and the current branch is ahead of its base or has uncommitted work. Use `git log` and `git diff` to see what changed. `git diff <base>...HEAD` against the likely base branch (`main`) usually captures the intent; fall back to `git diff` for uncommitted work.
- **Freeform description** — `$ARGUMENTS` reads like prose describing a change, with no code to inspect. This is the no-codebase-access path: build the ticket from what the user told you and ask for anything you genuinely need (see Step 1).

If it's genuinely ambiguous (e.g. a bare word that could be a branch or a topic), call the **AskUserQuestion** tool (`multiSelect: false`, header `Source`) with options `Pull request`, `Branch`, and `Freeform description` rather than guessing wrong and summarizing the wrong thing. Only ask when it's actually ambiguous — when the source is obvious, proceed without prompting.

## Step 1: Understand the change in user-facing terms

Whatever the source, your job is to describe **observable behavior**, not implementation. A reviewer skimming the ticket should understand what's different for a user or an API caller, not which functions moved.

From a diff or PR, read past the mechanics: a renamed variable is noise, but a new validation rule, a changed default, a new endpoint, or a different error message is signal. From a freeform description, you may have gaps — ask targeted questions only for things you can't reasonably infer and that change the acceptance criteria or test steps (e.g. "what should happen when the input is invalid?"). Don't interrogate the user; one or two sharp questions beats a checklist.

## Step 2: Draft the ticket

Use this exact structure. Show the draft to the user in the conversation **before** touching JIRA — they'll often tweak wording, and it's far cheaper to fix here than after filing. After showing the draft, call the **AskUserQuestion** tool (`multiSelect: false`, header `Draft`) with two options:

- `Looks good` — "Proceed to choosing the JIRA destination."
- `Edit wording` — "Change the draft first — I'll ask what to change."

On `Edit wording` (or a free-text reply via Other), apply the changes, show the revised draft, and ask again. Don't move to Step 3 until the user picks `Looks good`. Ask with the tool, not with a plain-text question at the end of the message.

```
## Summary
<1–3 sentences: what changes and why, in user-facing terms>

## Acceptance Criteria
- [ ] <testable, outcome-oriented statement — Given/When/Then where it sharpens it>
- [ ] <each criterion is something you can definitively call met or not met>

## QA Testing Steps (no codebase access)
1. <precondition: environment, base URL, test account or auth token, feature flag, seed data>
2. <a concrete action — a user action, OR for an API change the exact `METHOD /path` plus a full, runnable request body>
   - Expected: <observable result — on-screen text/state, or a response status code plus the specific fields/values to check>
3. ...

<!-- OR, when nothing is externally testable (see the doctrine below), the whole section is exactly one line instead of the numbered list: -->
NA: <reason — e.g. tooling-only change, no user-/API-observable surface; verified developer-side / by automated tests in CI>
```

### Why the QA section is written the way it is

**First decide whether there is anything to test at all.** Two kinds of change give a QA tester nothing to do, and in both the entire section is a single line — `NA: <reason>` — with no numbered steps: (a) the change has **no user- or API-observable surface** — build tooling, CI config, dependency bumps, dev scripts, docs, or a behavior-preserving refactor; (b) the behavior is **fully covered by automated tests that run in CI**, so a manual tester has nothing to add. Write the reason concretely — e.g. `NA: tooling-only Nx 22→23 upgrade — no runtime/UI/API surface; validated by the build/test pipeline in CI` — then stop; the `<reason>` is mandatory so a reader can see why QA was skipped and challenge it. **Never** substitute project-level scripts — `pnpm …`, `make …`, `go test`, `nx …`, "check out the branch and run …", or any repo/CLI command — as QA steps: a QA tester has no repo checkout and no toolchain, so those are exactly as unrunnable for them as "verify the `processPayment` function returns 200." If neither case applies, write real steps as below.

The tester running these steps **cannot read the code or the database**. They can only see and do what any user can: open pages, click buttons, fill inputs, read on-screen text and error messages, inspect an API response or an email, observe state changes. So write every step from that vantage point.

This rules out steps like "verify the `processPayment` function returns 200" or "check the `status` column flips to `settled`" — the tester has no way to do either. Reframe them as something observable: "Submit the payment form with a valid card → Expected: a confirmation banner reads 'Payment received' and the order moves to the Completed tab." Each step pairs an **action** with an **observable expected result**; a step with no observable outcome isn't testable and shouldn't be there.

Reference internal names (functions, tables, columns, env vars) only inside _preconditions a tester can actually set up_ (like "set the `BETA_CHECKOUT` flag to on in the admin panel") — never as the thing being verified.

### Be concrete — name the endpoints and request bodies

Vague steps are the main failure mode here. "Submit a pair of strings that previously matched" is not runnable; "Send `POST /v1/credit/exposure/business-portfolio/search` with this body, expect `200` returning the same business" is. The tester can't read the code — but **you can**: the diff or PR is in front of you, so mine it for the externally-runnable contract and hand the tester something they can fire verbatim.

When the change is reachable through an HTTP API (a new or changed endpoint, or any behavior an endpoint exercises):

- Name the exact **method and path** — `POST /v1/credit/exposure/business-portfolio/search` — and the service/base URL when more than one app is in play.
- Give a **complete, runnable request body** with realistic placeholder values, and flag which fields are **required** (and what failure looks like if one is omitted — e.g. a `400`, not a match result).
- State the **expected status code** and the **specific response fields/values** to check — not "it works."
- If the change is meant to preserve behavior (refactor, dependency swap, perf), give a **before/after baseline-diff recipe**: capture the response before deploy, send the identical request after, diff the two.

Pull these from the route definitions and request-validation schemas in the diff. An endpoint's method, path, and body are the API's public contract, not leaked internals — so they belong in the steps. The earlier rule still holds for genuinely internal names (functions, tables, columns): those appear only in preconditions, never as the thing being verified.

For a UI change, apply the same rule in UI terms: the exact screen/URL, the fields to fill with concrete values, and the on-screen text or state to observe. When the source is a freeform description that doesn't reveal the endpoint or body, ask for it (per Step 1) rather than emit a vague step.

## Step 3: Choose where it goes in JIRA

Don't hardcode or assume a project. Discover it at runtime:

1. `getAccessibleAtlassianResources` → the `cloudId`. If more than one site is returned, call the **AskUserQuestion** tool (`multiSelect: false`, header `Site`) populated with one option per site (name/URL) and use the chosen site's `cloudId`.
2. `getVisibleJiraProjects` → the projects the user can file into.
3. `getJiraProjectIssueTypesMetadata` for the chosen project → its issue types and any required fields.

Present the project and issue-type choices with `AskUserQuestion` (recommend the obvious one if there's a clear default, but let the user pick). If the issue type has required fields beyond summary/description, surface them and collect values before filing.

## Step 4: File it, only after explicit approval

Filing a ticket is an outward-facing action that other people will see, so confirm before doing it — both the ticket text and the target project/issue type. Call the **AskUserQuestion** tool (`multiSelect: false`, header `File ticket`) with four options:

- `Approve & file` — "File the ticket as shown in the chosen project."
- `Edit text` — "Change the ticket wording first — I'll ask what to change."
- `Change destination` — "Pick a different project or issue type first."
- `Cancel` — "Don't file anything."

On `Edit text` or `Change destination` (or the user's Other-field reply), apply the requested change and re-confirm. On `Cancel`, stop without filing. Once the user approves (`Approve & file`):

- Call `createJiraIssue` with the summary line (the `## Summary` distilled to one line if needed) and the full template as the description.
- Report back the created issue key and its URL. Use `getJiraIssue` to confirm/fetch the link if the create response doesn't include it.

If `createJiraIssue` fails, show the error **verbatim** and stop — don't silently retry with mangled fields. A common cause is a missing required field; if so, collect it and try once more with the user's go-ahead.
