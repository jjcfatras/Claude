# Shell Command Safety Rules

Claude Code's permission system flags certain shell patterns as potentially dangerous. These rules prevent repeated approval prompts.

## Rules

1. **No `#` comments in bash commands** — the `#` character desynchronizes quote tracking in the permission system. Use the Bash tool's `description` parameter for documentation instead.

2. **No inline markdown/JSON as bash arguments** — write content to files first using the Write tool, then reference the files (e.g., `gh pr comment NUMBER -F /tmp/body.md`).

3. **No heredocs (`<<`, `<<<`)** — especially with `#` or quote characters inside. Use the Write tool to create files, then reference them.

4. **Prefer the dedicated tools over shell equivalents** — use the Read tool for files, the Grep tool for search, and `jq` / `gh api --jq` for JSON. Reach for `sed`/`awk`/`grep`/`du` as bash commands only when no built-in tool covers the case. The built-ins are faster (Grep is ripgrep-backed) and avoid skill-level `allowed-tools` gaps; in skills whose frontmatter omits these commands (e.g., `code-review`, `respond-to-review`, `test-driven-fix`), the shell forms will be rejected regardless of project allowlist. Typical Grep tool call in place of `grep -rn 'PATTERN' src/`: `pattern: "PATTERN", path: "src/", output_mode: "content", -n: true`. Use `output_mode: "files_with_matches"` (the default) when you only need the file list, `output_mode: "count"` for counts, and `head_limit: N` instead of piping to `head`.

5. **No curly braces (`{`, `}`) combined with quote characters in the same command** — triggers "expansion obfuscation" prompts. This covers three common patterns:
   - **jq object construction**: `jq '{path: .path, line: .line}'` is prohibited. Extract fields individually with separate `jq -r .field` calls, or write the raw JSON to a file with the Write tool and let the agent build the final object.
   - **`gh api --jq` filters containing `{...}`**: pipe the raw response into a separate `jq` command instead of passing a brace-bearing filter through `--jq`.
   - **URL path placeholders**: always substitute actual values into URL paths (e.g., `repos/OWNER/REPO/pulls/NUMBER`) rather than `{owner}`/`{repo}`/`{number}` templates.

6. **No `$()` command substitution** — save intermediate results to temp files with separate commands, then reference those files.

7. **No output redirection (`>`, `>>`)** — triggers "write to arbitrary files" prompts. This includes capturing command output to a file (e.g., `gh pr diff NUM > pr.diff`, `command > out.txt`, `cmd | tee file`) and appending (`>>`). For small output: run the command without redirection, then call the Write tool with the captured output. For output too large to pull into context (big diffs, long logs, full API responses), invoke the Bash tool with `run_in_background: true` — it persists stdout/stderr to a file automatically, and you Read that file instead of redirecting.

8. **No adjacent/consecutive quote characters** (e.g., `'"`, `"'`, `''` at word boundaries) — triggers "potential obfuscation" prompts. Simplify quoting by using regex wildcards `.` instead of literal characters that require escaping, or write complex expressions to a file with the Write tool first.

9. **Keep every bash command on a single line** — newlines inside a command are interpreted as multiple commands and trigger security prompts. Chain with `&&` or `|` on one line.

10. **No ANSI-C quoting (`$'...'`)** — triggers "ANSI-C quoting which can hide characters" prompts. Avoid placing `$` immediately before a single quote (e.g., `$VAR'suffix'`). Use double quotes or separate the variable from single-quoted strings with a space.

11. **No `jq -f`, `--rawfile`, or `--slurpfile` flags** — these trigger dangerous-flag security prompts. Construct JSON payloads directly using the Write tool. Only use `jq .` for validation or `gh api --jq` for filtering.

12. **Fetching file contents at a specific SHA** — use this single-line pattern (substitute actual values): `gh api repos/OWNER/REPO/contents/PATH?ref=SHA | jq -r .content | base64 --decode`

13. **No backtick command substitution (`` `cmd` ``)** — same family as `$()` (rule 6). The permission system flags both as expansion obfuscation. Save intermediate results to temp files with separate commands, then reference those files.

14. **No process substitution (`<(cmd)`, `>(cmd)`)** — runs each side as a subshell exposed as a virtual file (`/dev/fd/...`), which the permission system cannot statically inspect. Run each command separately, write its output via the Write tool, then diff or compare the resulting files.

15. **No piping to a shell interpreter (`| sh`, `| bash`, `| zsh`, `| python -`)** — fetches arbitrary code and executes it; will be denied in any reasonable permission profile. Download the script to a file first (e.g., `curl -o /tmp/install.sh https://...`), inspect it with the Read tool, then run it explicitly with `bash /tmp/install.sh`.

16. **No trailing `&` to background a command, and no `nohup` / `disown`** — these hide a process from the harness and trigger lifecycle/permission prompts. Use the Bash tool's `run_in_background: true` parameter instead. It captures stdout/stderr to a file the harness manages, and you Read that file rather than redirecting (which also satisfies rule 7).

17. **Destructive git/filesystem operations need explicit user confirmation in the skill or step that runs them.** This includes `rm -rf`, `rm -r`, `git reset --hard`, `git push --force` (prefer `--force-with-lease`), `git clean -fd`, `git branch -D`, `git stash drop`, dropping database tables, and `git checkout -- <file>` against unstashed changes. Either gate these behind an `AskUserQuestion`, or sequence them after a stash or checkpoint that's reversible. Skills that need these operations should mention them in their `description` so the user sees the scope at install time.
