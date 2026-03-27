---
allowed-tools: Bash(gh pr comment:*), Bash(gh pr diff:*), Bash(gh pr view:*), Bash(gh api:*), Bash(jq:*), Bash(mktemp:*)
description: Code review a pull request
disable-model-invocation: false
model: opus
---

Provide a code review for the given pull request.

**Setup:** Run `mktemp -d /tmp/pr-review-XXXXXX` to create a unique temp directory and store the path as `$REVIEW_TMPDIR`. All temp files in this review must be written under `$REVIEW_TMPDIR/`. Create a todo list for steps 1-5. Update after each step.
**Execution:** Within each step, launch all agents in a single message (foreground, never `run_in_background`); all must complete before the next step.

**Shell Command Safety** (applies to ALL steps and ALL agents):

- **Never include `#` comments in bash commands** — use the Bash tool's `description` parameter for documentation instead. The `#` character inside shell commands desynchronizes quote tracking in the permission system, causing repeated approval prompts.
- **Never pass markdown content or JSON as inline bash arguments** — always write them to files first using the Write tool, then reference the files (e.g., `gh pr comment NUMBER -F /tmp/body.md`). Do not use `jq -f`, `--rawfile`, or `--slurpfile` as these trigger dangerous-flag security prompts — construct JSON directly with the Write tool and validate with `jq .`.
- **Never use heredocs (`<<`, `<<<`) for content that contains `#` or quote characters** — use the Write tool to create the file, then reference it.
- **Never use `sed`, `awk`, or `du`** — they are not in the allowed tools list and their syntax triggers security prompts. Use the Read tool, `jq`, or `gh api --jq` for text processing.
- **Never combine curly braces (`{`, `}`) with quote characters in the same bash command** — this triggers "expansion obfuscation" security prompts. For `gh api` calls needing `--jq` filters, pipe the output to a separate `jq` command instead of using `--jq` inline.
- **Never use `$()` command substitution in bash commands** — save intermediate results to temp files with separate commands, then reference those files.
- **Never use output redirection (`>`, `>>`) in bash commands** — use the Write tool to create files instead. Shell redirection triggers "write to arbitrary files" security prompts.
- **Never use adjacent/consecutive quote characters** (e.g., `'"`, `"'`, or `''` at word boundaries) in bash commands — these trigger "potential obfuscation" security prompts. Simplify quoting by avoiding embedded quotes (e.g., use regex wildcards `.` instead of literal characters that require escaping), or write complex expressions to a file with the Write tool first.
- **Keep every Bash command on a single line** — newlines inside a Bash command are interpreted as multiple commands and trigger security prompts. Chain with `&&` or `|` on one line.

Follow these steps precisely:

1. Launch all three Haiku agents:
   a. **CLAUDE.md Agent**: Return file paths and contents of relevant CLAUDE.md files: the root CLAUDE.md (if any) and CLAUDE.md files in directories modified by the PR.
   b. **PR Summary Agent**: View the pull request and:
   - Create a unique temp file with `mktemp /tmp/pr-<number>-XXXXXX.diff`, then write the PR diff into it with `gh pr diff <number>`. Store the path for later use.
   - Extract a **valid-line map** from the diff by parsing `diff --git` lines (for file paths) and `@@ ... +newStart,newCount @@` hunk headers (for ranges). The map is: `file path → list of [newStart, newStart+newCount-1]` ranges.
     - **Binary files**: Skip lines containing `Binary files ... differ` — these have no hunks. Include the file in the changed-files list but omit it from the valid-line map.
     - **Renamed files**: For `rename from X` / `rename to Y`, use the **new** path (the `b/` path) as the map key. If there are no hunks (pure rename with no content changes), include the file in the changed-files list but omit it from the valid-line map.
   - Return:
     (1) a summary of the change,
     (2) the **path to the diff file** (the unique `$DIFF_FILE` path),
     (3) a list of all changed file paths,
     (4) the **owner** and **repo** name (e.g., `FS-Main` and `fairsquare-ui`),
     (5) the **pull request number**,
     (6) the **full HEAD commit SHA** of the PR branch (use `gh pr view <number> --json headRefOid -q .headRefOid`),
     (7) the **valid-line map**.
     c. **Prior Reviews Agent**: Check for prior Claude Code reviews on the PR:
   - Fetch all reviews: `gh api --paginate repos/{owner}/{repo}/pulls/{number}/reviews`
   - Filter for the most recent review whose `body` contains the text `Generated with [Claude Code]`. Pipe the paginated results through multiple `jq` calls to avoid nested quotes (substitute actual owner, repo, and number values): `gh api --paginate repos/OWNER/REPO/pulls/NUMBER/reviews | jq '[.[] | select(.body | test("Generated with .Claude Code."))]' | jq 'sort_by(.submitted_at) | last'`. Do not use `jq -f` or pass jq filters through temp files.
   - If found, extract its `id`, `submitted_at`, and `commit_id`
   - Fetch that review's inline comments: `gh api --paginate repos/{owner}/{repo}/pulls/{number}/reviews/{id}/comments`
   - Extract from each comment: `path`, `line`, `start_line`, code snippet (text between first pair of triple-backtick fences in body), and first-line description
   - Write the result to a temp file as JSON using the Write tool (not bash heredocs or echo). The JSON should have the following structure:

     ```json
     {
       "last_review_date": "...",
       "last_review_commit": "...",
       "issues": [
         {
           "path": "...",
           "line": 0,
           "start_line": 0,
           "snippet": "...",
           "description": "..."
         }
       ]
     }
     ```

   - Return: the temp file path and the prior-issues data
   - If no prior Claude Code review exists, write a JSON object with `last_review_date` and `last_review_commit` set to null and an empty `issues` array, then return it along with the temp file path

2. Determine which categories apply to this PR:
   - **HAS_CLAUDE_MD**: true if CLAUDE.md files were found
   - **HAS_TYPESCRIPT**: true if any changed file ends in `.ts` or `.tsx`
   - **HAS_FRONTEND**: true if the PR modifies files in frontend directories (e.g., `src/components/`, `src/pages/`, `src/hooks/`, `app/`, or files with `.tsx`/`.jsx` extensions that contain React components)
   - **HAS_INFRASTRUCTURE**: true if any changed file matches migration, terraform, docker, or config patterns (e.g., `*.sql`, `migrations/`, `*.tf`, `*.hcl`, `docker*`, `Dockerfile*`, infrastructure or deploy directories, or files referencing `secret_manager_path`)

   Launch all applicable Sonnet agents. Pass each agent the diff file path, summary, changed file list, CLAUDE.md files, the owner/repo/HEAD SHA from step 1, and the prior-issues file path from step 1c. Each agent must read the diff from the file using the Read tool and read the prior-issues file. Agents may also read the full source of changed files if needed to verify an issue — for example, to see surrounding context, function signatures, imports, or call sites — but should avoid excessive fetching. Agents must NOT use `gh pr diff` directly.

   **Agent Shell Command Safety** — include these rules verbatim in every agent prompt:
   1. No `#` characters in bash commands — use the Bash tool's `description` parameter for documentation.
   2. No `sed`, `awk`, or `du` — use the Read tool, `jq`, or `gh api --jq` instead.
   3. No multi-line bash commands — keep every command on a single line, chain with `&&` or `|`.
   4. No heredocs (`<<`, `<<<`) — use the Write tool to create files, then reference them.
   5. No `jq -f`, `--rawfile`, or `--slurpfile` flags — these trigger dangerous-flag security prompts. Construct JSON payloads directly using the Write tool. Only use `jq .` for validation or `gh api --jq` for filtering.
   6. To fetch file contents at the HEAD SHA, use this single-line command (substitute actual values for OWNER, REPO, PATH, SHA): `gh api repos/OWNER/REPO/contents/PATH?ref=SHA --jq .content | base64 --decode`
   7. **Do NOT post reviews, comments, or any content to GitHub** — agents must only return their findings. All posting is handled in step 5 after filtering and user approval.
   8. No curly braces (`{`, `}`) combined with quote characters in the same bash command — this triggers "expansion obfuscation" security prompts. For `gh api` calls needing `--jq` filters, pipe the output to a separate `jq` command instead of using `--jq` inline. Always substitute actual values into URL paths instead of using `{placeholder}` syntax.
   9. No `$()` command substitution — run commands separately, saving intermediate results to temp files, then reference those files.
   10. No output redirection (`>`, `>>`) — use the Write tool to create files instead.
   11. No adjacent/consecutive quote characters (e.g., `'"`, `"'`, `''` at word boundaries) — simplify quoting (e.g., use regex wildcards `.` instead of escaped literals) or use the Write tool for complex expressions.

   Each agent should independently code review the change. For each issue identified, the agent must:
   1. Identify the issue and its category
   2. Perform an adversarial self-check. For each issue, answer these questions before including it:
      - **Is it pre-existing?** Check if the flagged code appears in context lines (` ` prefix) vs. added lines (`+` prefix). If it's only on context lines, score confidence 0.
      - **Does another path handle it?** Search the diff for related patterns (e.g., if flagging missing error handling, search for `catch`, `try`, or error handler in the same file's diff).
      - **Is it intentional?** Consider whether the change is directly related to the PR's purpose. A renamed variable isn't a bug; it's the point.
      - **Would CI catch it?** If a linter, type checker, or test suite would flag this, score confidence 0.
      - **Is it a false positive per the examples at the bottom of this document?**
      - **Was it already flagged?** Check the prior-issues list for an entry with the same file path AND either (a) the same or adjacent line number (within ±5 lines) OR (b) an overlapping code snippet (40+ character common substring). If a match exists AND the code at that location is unchanged (context line ` ` not added line `+`), score confidence 0. If the code HAS changed (`+` line in diff), treat as a new issue.
      - If you cannot confidently answer "no" to all of these, do not include the issue.
   3. Only after the self-check, assign all of the following:
      - **File path**: Exact relative file path from repo root, matching a path from the PR diff
      - **Line number**: Line in the NEW version where the issue occurs. For multi-line issues, provide `startLine` and `line` (end line). Must correspond to added/modified lines (`+` prefix) in the diff.
      - **Confidence score**: 0-100 (using rubric below)
      - **Severity**: Critical, Medium, or Minor (using rubric below)
      - **Brief rationale**: One-sentence justification for confidence and severity
      - **Explanation**: Detailed explanation of why this is an issue, its impact. If flagged due to CLAUDE.md, quote the relevant section.
      - **Code example**: The actual problematic code snippet from the PR
      - **Suggested fix**: An example of how to fix the issue (if applicable)

   **IMPORTANT:** File path and line number are REQUIRED for every issue. If an issue cannot be tied to a specific line, reference the most relevant changed line. Agents must cross-reference line numbers against the PR diff for accuracy.

   **Do not report issues with confidence below 50.** If an issue scores below 50 after the self-check, discard it immediately.

   For CLAUDE.md-flagged issues, double-check that the CLAUDE.md explicitly calls out that issue. If not, cap confidence at 60 unless it is also a clear functional bug.

   **Confidence scale** (0-100; include when prompting each agent): 0=false positive, pre-existing, or only on unchanged lines (do not use 25 or 50 for these — use 0). 25=plausible but unverified. 50=verified but ambiguous (another path might handle it). 75=verified and confirmed in practice, little ambiguity. 100=certain, unambiguous evidence. Use intermediate values (60, 80, 90); when unsure between levels, use the lower score.

   **Severity scale** (include when prompting each agent):
   - 🔴 Critical: Security vulnerabilities, authorization bypasses, data loss risks, crashes in common paths
   - 🟡 Medium: Missing validations, incorrect behavior in edge cases, documentation gaps for new APIs, migration issues
   - 📝 Minor: Code duplication, style inconsistencies, minor improvements, nitpicks

   **Agents (4-8 depending on conditions) — launch ALL in one message (foreground):**

   **CRITICAL — every agent prompt MUST begin with this exact text block:**

   > You are a code review agent. FORBIDDEN: Never use `sed`, `awk`, `du`, or `grep` as Bash commands — they are not in the allowed tools and will trigger permission prompts that block the review. Use the Read tool to read files, the Grep tool to search content, and `jq`/`gh api --jq` for JSON processing. No `#` comments in bash commands. No heredocs. No multi-line bash commands. No `jq -f`/`--rawfile`/`--slurpfile`. No `$()` command substitution. No curly braces with quotes in the same command — pipe to `jq` instead of `--jq` when URLs contain braces. No output redirection (`>`, `>>`) — use the Write tool. No adjacent quote characters (e.g., `'"`, `"'`) at word start — simplify quoting or use the Write tool. Do NOT post anything to GitHub.

   **Agent #1: CLAUDE.md Compliance** (if HAS_CLAUDE_MD)
   - Audit changes for CLAUDE.md compliance
   - CLAUDE.md is guidance for writing code, so not all instructions apply during review

   **Agent #2: Security, Authorization, and API Validation**
   - Security vulnerabilities, permissions, ownership checks, auth issues, input validation
   - API contract compliance: request validation (schemas, types, required fields), docs updated for new/modified routes

   **Agent #3: Infrastructure and Database** (if HAS_INFRASTRUCTURE)
   - Database migration patterns — ensure reversibility
   - New secrets must have corresponding `secret_manager_path` in terraform
   - Review configuration changes

   **Agent #4: Code Quality and Best Practices**
   - Ensure changes follow existing codebase patterns and conventions
   - Identify code duplication that should be refactored
   - Suggest improvements where appropriate

   **Agent #5: TypeScript Type Safety** (if HAS_TYPESCRIPT)
   - Type usage: `any`/`unknown` narrowing, generic constraints, null/undefined handling, discriminated unions, type assertions
   - Check that inferred types match intended behavior

   **Agent #6: Error Handling, Async, and Resilience**
   - Error handling: try/catch, error boundaries, meaningful error messages, propagation vs swallowing, graceful degradation, logging for observability
   - Async: unhandled rejections, race conditions, proper async/await, parallel vs sequential choices
   - Database: transaction handling, connection pool management

   **Agent #7: React/Frontend Patterns** (if HAS_FRONTEND)
   - Hook dependency arrays, unnecessary re-renders, memoization (`useMemo`, `useCallback`, `React.memo`), component composition, accessibility, effect cleanup

   **Agent #8: Performance Patterns**
   - N+1 queries, inefficient loops, missing pagination
   - Bundle size implications, lazy loading opportunities
   - Memory leaks (event listeners, subscriptions not cleaned up)

3. Filter issues using deduplication and a two-gate approach:

   **Pre-filter 1 — Deduplication:** Group issues by file path and line number (within ±3 lines). For each group, keep the issue with the highest confidence score. If tied, prefer the agent whose domain best matches the issue category.

   **Pre-filter 2 — Prior-review deduplication:** For each surviving issue, match against the prior-issues list from step 1c:
   1. Same file path
   2. Line within ±5 lines of a prior issue's line OR code snippet shares a 40+ character common substring with a prior issue's snippet
   3. If matched AND the line is on unchanged code (context line ` `, not added line `+` in the diff) → remove the issue (already flagged in prior review)
   4. If matched BUT the code at that location has changed (`+` line in diff) → keep the issue, append to its explanation: "_Note: This issue was flagged in a prior review but the code has since changed._"

   Log the count of issues removed by this filter for reporting in step 4.

   **Gate 1 — Confidence/Severity filter:**
   - Confidence must be at least 50 (the issue is at least verified).
   - If confidence is between 50-74, only include the issue if its severity is Critical.
   - Issues with confidence 75 or above are included regardless of severity.
     If there are no issues that meet these criteria, do not proceed.

   **Gate 2 — Diff line validation:**
   Using the valid-line map from step 1b, validate that every surviving issue targets a line within a valid hunk range for that file.

   For each issue, check its `line` (and `startLine`) against valid ranges for that file:
   - **In range**: mark as **inline-eligible**.
   - **Out of range, within 5 lines of nearest valid line**: snap to nearest valid line, prepend "_Note: This comment was placed on the nearest diff line; the issue actually occurs on line {original_line}._" Mark inline-eligible.
   - **Out of range, >5 lines away or file not in diff**: mark as **summary-only**.
   - **Multi-line** (`startLine` + `line`): if only `startLine` is out of range but `line` is valid, drop `startLine` (single-line comment). If `line` is out of range, apply snapping logic above.

   Result: two lists (inline-eligible, summary-only). If both empty, stop. If only summary-only exists, skip steps 5a and 5b.

4. Present a summary of findings to the user and ask permission to post. If issues were removed by prior-review deduplication (pre-filter 2), include a line: "Skipped N issue(s) already flagged in prior review ({last_review_date})." followed by a brief list of skipped issues (file:line — description) so the user can override if needed. If the user declines, stop.

5. Post the review using the GitHub Reviews API via `gh api` (single API call, single notification, inline comments on relevant diff lines).

   Use `owner`, `repo`, pull request `number`, and `commit_id` (full HEAD SHA) from step 1.

   **5a: Build the JSON payload** — Construct the JSON payload safely using `jq` to ensure proper escaping of special characters (quotes, newlines, backslashes, `$`) in code snippets. The target structure:

   ```json
   {
     "commit_id": "<full HEAD SHA>",
     "event": "COMMENT",
     "body": "<review summary — see format below>",
     "comments": [
       {
         "path": "relative/file/path",
         "line": 42,
         "side": "RIGHT",
         "body": "<formatted comment per ISSUE_FORMAT>"
       },
       {
         "path": "relative/file/path",
         "start_line": 45,
         "line": 50,
         "start_side": "RIGHT",
         "side": "RIGHT",
         "body": "<formatted multi-line comment per ISSUE_FORMAT>"
       }
     ]
   }
   ```

   Populate `comments` from the inline-eligible issues list. Each entry needs:
   - `path`: relative file path from repo root
   - `line`: post-Gate-2 line number (end line for multi-line issues)
   - `side`: always `"RIGHT"`
   - `body`: formatted per ISSUE_FORMAT below

   For multi-line issues, also include `start_line` and `start_side: "RIGHT"`.

   If the inline list is empty, set `comments` to `[]`.

   Build the JSON payload directly using the Write tool — do not use jq for construction (its file-reading flags `-f`, `--rawfile`, `--slurpfile` trigger dangerous-flag security prompts).

   **Shell Command Safety** (applies to all step 5 operations):
   1. No `#` characters in bash commands — use the Bash tool's `description` parameter for documentation.
   2. No `sed`, `awk`, or `du` — use the Read tool or `gh api --jq` instead.
   3. No multi-line bash commands — keep every command on a single line, chain with `&&` or `|`.
   4. No heredocs (`<<`, `<<<`) — use the Write tool to create files, then reference them.
   5. No `jq -f`, `--rawfile`, or `--slurpfile` flags — construct JSON directly with the Write tool.
   6. No curly braces (`{`, `}`) combined with quote characters in the same bash command — pipe `gh api` output to `jq` instead of using `--jq` inline.
   7. No `$()` command substitution — save intermediate results to temp files, then reference them.
   8. No output redirection (`>`, `>>`) — use the Write tool to create files instead.
   9. No adjacent/consecutive quote characters (e.g., `'"`, `"'`, `''` at word boundaries) — simplify quoting or use the Write tool.

   The approach:
   1. For each inline-eligible issue, use the Write tool to save its formatted body (per ISSUE_FORMAT) to a unique temp file (e.g., `$REVIEW_TMPDIR/comment-1.md`, `$REVIEW_TMPDIR/comment-2.md`).
   2. Use the Write tool to save the review summary body to `$REVIEW_TMPDIR/summary.md`.
   3. **Construct the entire `$REVIEW_TMPDIR/payload.json` directly using the Write tool.** The agent has all issue data in memory (paths, line numbers, formatted bodies, commit SHA). Build a valid JSON object matching the target structure above. Ensure proper JSON escaping of all string values: escape `"` as `\"`, `\` as `\\`, newlines as `\n`, tabs as `\t`, and backticks need no escaping in JSON.
   4. Validate the payload: `jq . $REVIEW_TMPDIR/payload.json`

   **5b: Post the review** — Submit via a single API call:

   `gh api repos/OWNER/REPO/pulls/NUMBER/reviews --method POST --input $REVIEW_TMPDIR/payload.json`

   Replace OWNER, REPO, and NUMBER with the actual values from step 1.

   **Fallback**: If the API call fails, write the full review summary body to a temp file using the Write tool (e.g., `$REVIEW_TMPDIR/fallback.md`), then post it with `gh pr comment NUMBER -F $REVIEW_TMPDIR/fallback.md`. Include all issues (both inline-eligible and summary-only) in the body using `ISSUE_FORMAT`, each prefixed with the file path and line number (e.g., `**src/auth.ts:42**`). Log the API error in the comment footer: "Note: Inline comments failed ({error}). All issues listed below."

   **ISSUE_FORMAT** (used for both inline comment bodies and summary-only issues):

   ````
   {severity_emoji} **{Severity}** (Confidence: {N}/100) - {brief description}

   **Explanation:** {detailed explanation. If CLAUDE.md-triggered, quote: "CLAUDE.md says: <...>"}

   **Code:**

   ```{language}
   {problematic code from PR}
   ```

   **Suggested fix:**

   ```{language}
   {corrected code}
   ```
   ````

   Severity emojis: 🔴 Critical, 🟡 Medium, 📝 Minor. For inline comments, omit the file path from the description (already attached to the line).

   **Review summary body** (the `body` field in the JSON payload) — all variants start with `### Code review` and end with:

   ```
   🤖 Generated with [Claude Code](https://claude.ai/code)

   <sub>If this code review was useful, please react with 👍. Otherwise, react with 👎.</sub>
   ```

   - **Has inline issues**: Summary table (columns: #, Severity, Confidence, File, Description) + "See inline comments for full details, code examples, and suggested fixes." If summary-only issues also exist, append `#### Additional issues (could not attach inline)` with explanation and each issue in ISSUE_FORMAT including `path:line`.
   - **Only summary-only issues**: Post with an empty `comments` array. Header: "Found N issue(s). These could not be placed as inline comments because their line numbers fall outside the diff's visible range." List each in ISSUE_FORMAT in the `body`.
   - **No issues**: Post with an empty `comments` array: "No issues found. Checked for CLAUDE.md compliance, security/authorization, API validation, infrastructure/database, and code quality."

Examples of false positives, for steps 2 and 3:

- Pre-existing issues or issues on lines the PR did not modify
- Pedantic nitpicks that a senior engineer wouldn't call out
- Issues that a linter, typechecker, or compiler would catch (e.g., missing imports, type errors, formatting). Do not run build steps — assume CI handles these.
- General code quality issues (test coverage, documentation) unless explicitly required in CLAUDE.md
- Issues called out in CLAUDE.md but explicitly silenced in code (e.g., lint ignore comments)
- Changes in functionality that are likely intentional or directly related to the broader change
- Issues identical to those already flagged in a prior Claude Code review on unchanged code

Notes:

- Use `gh` for fetching PR data. Use `gh api repos/{owner}/{repo}/pulls/{number}/reviews` for posting reviews with inline comments.
- Cite and link every issue (e.g., if referring to a CLAUDE.md, link it)
- When linking to code in inline comments, follow this format precisely: https://github.com/anthropics/claude-code/blob/c21d3c10bc8e898b7ac1a2d745bdc9bc4e423afe/package.json#L10-L15
  - Requires full git sha
  - You must provide the full sha. Commands like `https://github.com/owner/repo/blob/$(git rev-parse HEAD)/foo/bar` will not work, since your comment will be directly rendered in Markdown.
  - Repo name must match the repo you're code reviewing
  - # sign after the file name
  - Line range format is L[start]-L[end]
  - Provide at least 1 line of context before and after, centered on the line you are commenting about (eg. if you are commenting about lines 5-6, you should link to `L4-7`)
