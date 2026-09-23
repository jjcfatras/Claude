# Triage procedure — one verdict per review item

Shared by both analysis paths in `/respond-to-review` Step 2: the orchestrator applies it inline for three or fewer items, and each `analyze:*` agent in `triage-workflow.js` applies it to its single item. Whichever path runs, the output is the verdict object described at the end, so downstream steps never re-parse prose.

Work from these item fields, by `source_type`:

- **Inline comment** (`inline-comment`): `id`, `path`, `line`, `original_line`, `diff_hunk`, `body`, `reviewer`.
- **Review-body finding** (`review-body-finding`): `id`, `review_id`, `reviewer`, `body` (the finding text), and optional `path` / `line` when the finding named a location.

You also have the PR's head SHA, base SHA, and full commit list, plus the path to the saved PR diff.

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
   - If settling a claim needs a **throwaway script that imports the project's own dependencies**, do not put it in the temp dir — `/tmp` sits outside the repo and cannot resolve its `node_modules` (or its venv, or its repo-local binaries). Run the source with the repo root as cwd instead: `node --input-type=module -e "$(cat "$TMPDIR/probe.mjs")"`. Keep the script itself in the temp dir; only the _execution_ has to happen from the repo.
5. If you can **definitively** demonstrate the concern is unfounded with concrete evidence → verdict `false-positive`, with the evidence recorded per the next section.
6. If there is any reasonable chance the reviewer is correct → verdict `valid`.

**Important**: Err on the side of treating issues as valid. Only categorize as false positive when you have clear, concrete evidence. When in doubt, it is a valid issue.

## Reproduce before you record

A `preexisting` or `false-positive` verdict is a claim that will be posted to the PR as a dismissal, so it must be backed by evidence you observed in this run:

- `preexisting` → you ran the blame command above and are copying its SHA and date into `blame_sha` / `blame_date`.
- `false-positive` → you `Read` the exact file and lines you are citing, and `quote` is the verbatim text you saw there (or the verbatim doc passage from Context7 for `kind: "library-docs"`). Not a paraphrase, not a memory of what the file "usually" contains.

If you cannot reproduce the evidence, the verdict is `valid`. A dismissal with `kind: "none"` evidence is not a dismissal; in the workflow path the verifier overturns it.

## The verdict object

Return exactly these fields. The workflow enforces them with a JSON schema; the inline path records the same shape.

- `id` — the item's `id` (inline comment ID or synthetic `review-<reviewId>-finding-<N>`).
- `verdict` — `preexisting` | `false-positive` | `valid`.
- `reasoning` — 2–3 sentences explaining why.
- `evidence` — object with `kind` (`blame` | `code` | `diff` | `library-docs` | `none`) and, as applicable, `file`, `line`, `quote`, `blame_sha`, `blame_date`. Step 6 fills the reply templates from these fields verbatim.
- `fix_plan` — `valid` only: a brief description of what needs to change.
- `vague` — boolean. `true` when the item is a valid concern but too abstract to fix mechanically (e.g., "consider whether this module is doing too much"); Step 4 then marks it `noted` instead of attempting a speculative edit. Otherwise `false`.
