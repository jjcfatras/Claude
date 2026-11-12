# Triage procedure — one verdict per review item

Shared by both analysis paths in `/respond-to-review` Step 2: the orchestrator applies it inline for three or fewer items, and each `analyze:*` agent in `triage-workflow.js` applies it to its single item. Whichever path runs, the output is the verdict object described at the end, so downstream steps never re-parse prose.

Work from these item fields, by `source_type`:

- **Inline comment** (`inline-comment`): `id`, `path`, `line`, `original_line`, `diff_hunk`, `body`, `reviewer`.
- **Review-body finding** (`review-body-finding`): `id`, `review_id`, `reviewer`, `body` (the finding text), and optional `path` / `line` when the finding named a location.
- **Review response** (`review-response`): a thread the current user started that someone has answered — the inline-comment fields plus `thread_id` and `responses` (every reply, oldest first). Skip Checks 1 and 2 and apply **Check 3** only: the question is whether the response holds, not whether the original concern was valid.

You also have the PR's head SHA, base SHA, and full commit list, the path to the saved PR diff, and the worktree where the PR head is checked out — resolve every repo-relative path against it.

## Check 1 — Is this preexisting?

Determine whether the code the item flags was introduced by this PR or existed before. Branch on `source_type`:

**Inline comments:**

1. **Parse the `diff_hunk`**: Look at the last line(s) of the `diff_hunk` (which correspond to the commented line). If the line starts with ` ` (space, context line) rather than `+` (added line), the code is **preexisting** — it was not added or modified by this PR.
2. **Confirm with git blame at the head SHA**: run `git blame -L LINE,LINE HEAD_SHA -- PATH`. Pinning the SHA keeps a dirty working tree out of the answer. If the blame SHA is **not in the PR's commit list**, the code predates this PR.
3. If confirmed preexisting → verdict `preexisting`, with `evidence.kind: "blame"` and the blame SHA and date copied into `blame_sha` / `blame_date`.

**Review-body findings:**

1. If the finding has both a `path` and a `line`, run the same blame check and apply the same rule.
2. If the finding has only a `path` (no line), skim the saved PR diff for that path. If the finding clearly describes code that isn't in the PR's added lines for that file (e.g., the reviewer says "the existing handler in `foo.ts` does X" and the diff shows no touching of that handler), verdict `preexisting` with `evidence.kind: "diff"`, quoting the hunk header (or the absence) you relied on.
3. If the finding has neither a path nor a line, or you cannot confidently localize it in the diff, **skip the preexisting verdict** and proceed to Check 2. This is a feature, not a gap — vague architectural concerns typically aren't about preexisting code.

## Check 2 — Is this a false positive?

If the item is not preexisting, analyze whether the reviewer's concern is valid. The check works the same way for both source types — the only difference is what context you read.

1. **Read the relevant file(s)** to understand context (surrounding functions, imports, types, error handling). For inline comments, that's the commented path. For review-body findings, follow any file references in the finding text; if the finding is cross-cutting, read the most likely affected files based on the PR diff.
2. **Understand the reviewer's concern** by carefully parsing the comment / finding text.
3. **Evaluate the concern** against the actual code:
   - Does the code already handle the scenario the reviewer is worried about?
   - Is there handling elsewhere in the codebase (e.g., middleware, base class, framework feature) that addresses the concern?
   - Is the reviewer misreading the code or missing context?
   - Is the concern about style/preference rather than correctness?
   - Would the suggested change actually be incorrect or unnecessary given the types/constraints?
4. Use `rg` to search for relevant patterns if needed (e.g., if the reviewer asks about error handling, search for try/catch or error middleware in related files). If the reviewer's concern hinges on external library behavior (React hooks, Prisma, Next.js, AWS SDK, etc.), verify the claim with Context7 (`mcp__plugin_context7_context7__resolve-library-id` then `mcp__plugin_context7_context7__query-docs`) before deciding the verdict.
   - If settling a claim needs a **throwaway script that imports the project's own dependencies**, do not put it in the temp dir — `/tmp` sits outside the repo and cannot resolve its `node_modules` (or its venv, or its repo-local binaries). Run the source with the worktree root as cwd instead: `node --input-type=module -e "$(cat "$REVIEW_DIR/probe.mjs")"`. Keep the script itself in the temp dir; only the _execution_ has to happen from the worktree.
5. If you can **definitively** demonstrate the concern is unfounded with concrete evidence → verdict `false-positive`, with the evidence recorded per the next section.
6. If there is any reasonable chance the reviewer is correct → verdict `valid`.

**Important**: Err on the side of treating issues as valid. Only categorize as false positive when you have clear, concrete evidence. When in doubt, it is a valid issue.

## Check 3 — Does the response hold? (`review-response` only)

The original comment (`body`) is the concern; the **latest** entry in `responses` is the answer to judge. Earlier replies ending in `<!-- respond-to-review:verify -->` are this skill's previous verifications — read them as context for what was already argued, not as the answer.

1. **Classify the response**: a **fix** (cites a commit or describes a code change), a **deferral** (preexisting, follow-up, out of scope), a **rebuttal** (the concern is a false positive), or **no real response** (a "+1", a question, an acknowledgement with no action).
2. **Fix**:
   - If it cites a commit, confirm the commit is in the PR: `git merge-base --is-ancestor <sha> HEAD_SHA` must exit 0. Record the SHA as `evidence.fix_sha`.
   - See what changed: `git show <sha> -- <path>`.
   - `Read` the code at the head SHA where the concern lives. For an outdated thread (`line` is null), locate it from `original_line`, the `diff_hunk`, and `rg` — the code may have moved.
   - `accepted` only when the **original** concern no longer reproduces at head and the change adds no new defect in the lines it touches. Matching what the response says it did is not enough. When the response reports its own test ("tested locally: …"), re-run that probe if it is cheap and has no side effects.
3. **Deferral**: `accepted` when it is logical — the code predates the PR (run Check 1's blame to confirm it), or the concern is broader than the PR and is not a defect in lines the PR adds. Deferring a correctness or security defect that this PR introduces is not logical → `disputed`.
4. **Rebuttal**: re-check the response's cited evidence the way Check 2 checks a false positive — `Read` the code it points at, query Context7 for a library claim. `accepted` if the evidence holds and answers the concern.
5. **No real response** → `disputed`.

Otherwise the verdict is `disputed`, and `reasoning` says concretely what is still wrong and what would resolve it — that text becomes the pushback reply. **When in doubt, `disputed`**: an `accepted` verdict resolves the thread and hides the discussion, so a wrong one buries a real bug, while a wrong `disputed` costs one extra reply.

## Reproduce before you record

A `preexisting` or `false-positive` verdict is a claim that will be posted to the PR as a dismissal, and an `accepted` verdict resolves a thread, so each must be backed by evidence you observed in this run:

- `preexisting` → you ran the blame command above and are copying its SHA and date into `blame_sha` / `blame_date`.
- `false-positive` → you `Read` the exact file and lines you are citing, and `quote` is the verbatim text you saw there (or the verbatim doc passage from Context7 for `kind: "library-docs"`). Not a paraphrase, not a memory of what the file "usually" contains.
- `accepted` → the same evidence rules, by what made the response hold: the code at head that resolves the concern (`kind: "code"`, plus `fix_sha` for a fix), the blame that shows a deferral targets preexisting code (`kind: "blame"`), or the code or docs that back a rebuttal.

If you cannot reproduce the evidence, the verdict is `valid` (or `disputed` for a review response). A dismissal or acceptance with `kind: "none"` evidence does not count; in the workflow path the verifier overturns it.

## The verdict object

Return exactly these fields. The workflow enforces them with a JSON schema; the inline path records the same shape.

- `id` — the item's `id` (inline comment ID or synthetic `review-<reviewId>-finding-<N>`).
- `verdict` — `preexisting` | `false-positive` | `valid`; for a `review-response` item, `accepted` | `disputed`.
- `reasoning` — 2–3 sentences explaining why. For `disputed`, what is still wrong and what would resolve it.
- `evidence` — object with `kind` (`blame` | `code` | `diff` | `library-docs` | `none`) and, as applicable, `file`, `line`, `quote`, `blame_sha`, `blame_date`, `fix_sha` (the response's fix commit, review responses only). Step 6 fills the reply templates from these fields verbatim.
- `fix_plan` — `valid` only: a brief description of what needs to change.
- `vague` — boolean. `true` when the item is a valid concern but too abstract to fix mechanically (e.g., "consider whether this module is doing too much"); Step 4 then marks it `noted` instead of attempting a speculative edit. Otherwise `false`.
