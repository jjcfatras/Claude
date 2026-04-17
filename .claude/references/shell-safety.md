# Shell Command Safety Rules

Claude Code's permission system flags certain shell patterns as potentially dangerous. These rules prevent repeated approval prompts.

## Rules

1. **No `#` comments in bash commands** — the `#` character desynchronizes quote tracking in the permission system. Use the Bash tool's `description` parameter for documentation instead.

2. **No inline markdown/JSON as bash arguments** — write content to files first using the Write tool, then reference the files (e.g., `gh pr comment NUMBER -F /tmp/body.md`).

3. **No heredocs (`<<`, `<<<`)** — especially with `#` or quote characters inside. Use the Write tool to create files, then reference them.

4. **No `sed`, `awk`, `du`, or `grep` as bash commands** — they are not in the allowed-tools list. Use the Read tool for files, the Grep tool for search, `jq` for JSON processing, and `gh api --jq` for API filtering.

5. **No curly braces (`{`, `}`) combined with quote characters in the same command** — triggers "expansion obfuscation" prompts. For `gh api` calls needing `--jq` filters, pipe the output to a separate `jq` command instead. Always substitute actual values into URL paths instead of using `{placeholder}` syntax.

6. **No `$()` command substitution** — save intermediate results to temp files with separate commands, then reference those files.

7. **No output redirection (`>`, `>>`)** — triggers "write to arbitrary files" prompts. Use the Write tool to create files instead.

8. **No adjacent/consecutive quote characters** (e.g., `'"`, `"'`, `''` at word boundaries) — triggers "potential obfuscation" prompts. Simplify quoting by using regex wildcards `.` instead of literal characters that require escaping, or write complex expressions to a file with the Write tool first.

9. **Keep every bash command on a single line** — newlines inside a command are interpreted as multiple commands and trigger security prompts. Chain with `&&` or `|` on one line.

10. **No ANSI-C quoting (`$'...'`)** — triggers "ANSI-C quoting which can hide characters" prompts. Avoid placing `$` immediately before a single quote (e.g., `$VAR'suffix'`). Use double quotes or separate the variable from single-quoted strings with a space.

11. **No `jq -f`, `--rawfile`, or `--slurpfile` flags** — these trigger dangerous-flag security prompts. Construct JSON payloads directly using the Write tool. Only use `jq .` for validation or `gh api --jq` for filtering.

12. **Fetching file contents at a specific SHA** — use this single-line pattern (substitute actual values): `gh api repos/OWNER/REPO/contents/PATH?ref=SHA | jq -r .content | base64 --decode`
