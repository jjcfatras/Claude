# Shell Command Safety Rules

These rules apply when the skill or agent runs under Claude Code's [auto permission mode](https://code.claude.com/docs/en/permission-modes), which retires the old static-analysis prompts in favor of a classifier. Earlier rules around heuristic-only patterns — `#` comments in bash, heredocs, `$()` and backtick command substitution, output redirection (`>`, `>>`), curly braces combined with quotes, adjacent quote characters, ANSI-C `$'...'` quoting, `jq -f` / `--rawfile` / `--slurpfile` flags, process substitution, and single-line-only bash — were workarounds for those prompts and have been retired. The classifier auto-approves safe-but-heuristically-flagged shell patterns; what's left below are the rules that still matter for a real reason (allowed-tools gaps, real shell-syntax bugs, RCE security, harness lifecycle, destructive ops).

If you run under default mode, expect the retired patterns to prompt — that's a function of the user's permission mode, not a skill-design rule.

## Rules

1. **No inline markdown/JSON as bash arguments** — write content to files first using the Write tool, then reference the files (e.g., `gh pr comment NUMBER -F /tmp/body.md`). Quoting reliability is real regardless of permission mode: long markdown bodies and JSON payloads embed quotes, newlines, and `$` characters that are fragile to inline.

2. **Prefer dedicated tools over shell equivalents** — use the Read tool for files, the Grep tool for search, and `jq` / `gh api --jq` for JSON. Reach for `sed`/`awk`/`grep`/`du` as bash commands only when no built-in tool covers the case. The built-ins are faster (Grep is ripgrep-backed) and avoid skill-level `allowed-tools` gaps; in skills whose frontmatter omits these commands (e.g., `code-review`, `respond-to-review`, `test-driven-fix`), the shell forms will be rejected at the skill-permission layer regardless of the harness's runtime permission mode. Typical Grep tool call in place of `grep -rn 'PATTERN' src/`: `pattern: "PATTERN", path: "src/", output_mode: "content", -n: true`. Use `output_mode: "files_with_matches"` (the default) when you only need the file list, `output_mode: "count"` for counts, and `head_limit: N` instead of piping to `head`.

3. **Fetching file contents at a specific SHA** — use the `-f` query-string form so the command is shell-agnostic (the `?ref=SHA` query-string form is interpreted as a glob pattern by zsh's nullglob and gets rejected): `gh api -X GET repos/OWNER/REPO/contents/PATH -f ref=SHA | jq -r .content | base64 --decode`. Real shell-syntax bug, not a permission heuristic.

4. **No piping to a shell interpreter (`| sh`, `| bash`, `| zsh`, `| python -`)** — fetches arbitrary code and executes it; the auto-mode classifier will (correctly) refuse this and any reasonable permission profile denies it. Download the script to a file first (e.g., `curl -o /tmp/install.sh https://...`), inspect it with the Read tool, then run it explicitly with `bash /tmp/install.sh`.

5. **Use `run_in_background: true` for backgrounded commands** — the harness owns the lifecycle and captures stdout/stderr to a file you can Read. Don't use trailing `&`, `nohup`, or `disown` — they hide the process from the harness so you lose output capture and lifecycle management. This is about how the harness manages output, not about avoiding permission prompts.

6. **Destructive git/filesystem operations need explicit user confirmation in the skill or step that runs them.** This includes `rm -rf`, `rm -r`, `git reset --hard`, `git push --force` (prefer `--force-with-lease`), `git clean -fd`, `git branch -D`, `git stash drop`, dropping database tables, and `git checkout -- <file>` against unstashed changes. Either gate these behind an `AskUserQuestion`, or sequence them after a stash or checkpoint that's reversible. Skills that need these operations should mention them in their `description` so the user sees the scope at install time. Auto mode's classifier still gates these at runtime, but skill design must too.

7. **Don't iterate `for` over a list of quoted string literals — the auto-mode classifier rejects it with `Unhandled node type: string`.** The failing shape is an explicit list of double-quoted strings as the iterable:

   ```bash
   # Rejected by the classifier — never executes
   for dir in "src/lib/utils" "src/lib" "src"; do
     path="$ROOT/$dir/CLAUDE.md"
     if [ -f "$path" ]; then echo "$path"; fi
   done
   ```

   The classifier's AST traversal does not cover the `string` node it sees in this position, so the whole command is dropped before the shell ever runs it. Real bug, not a heuristic; auto mode does not save you. Replacements in priority order:
   - **Glob tool** when you're searching for files by pattern: `pattern: "**/CLAUDE.md", path: "<repo root>"` returns the full hit list in one call. This is almost always what the for-loop above was actually trying to do.
   - **Multiple Read tool calls in parallel** when you have a fixed list of specific candidate paths. A `Read` against a missing file returns a clean error — you don't need a `[ -f ]` guard.
   - **`find`** with `-name` / `-path` for deeper traversal Glob can't express (e.g., conditional pruning of `node_modules`).

   If shell iteration is genuinely required, an unquoted glob (`for f in src/**/CLAUDE.md`) or a command-substitution feed (`for f in $(find . -name CLAUDE.md)`) parses cleanly — it is specifically the explicit quoted-string list as iterable that the classifier mishandles.
