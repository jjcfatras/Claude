# Accounting mismatch

Read this from SKILL.md step [4/5] only when the `Accounted:` line reports a mismatch. The raw `findings/*.json` files are then the **only** surviving copy of each orphaned finding, so preserving them comes before anything else.

1. Report the gap in **at most 10 lines**, in exactly this shape — no prose beyond it, no finding bodies reproduced, no teaching asides. The bound matters: this message is the user's only notice, and an unbounded explanation buries it.

   ```
   ⚠ Accounting gap: N finding(s) entered the pipeline and appear in no bucket.
     <role>-<id> | <severity> conf=<n> | <file>:<line> | <one-line rationale>
     … one line per orphan …
   Raw findings preserved at $TMP/findings/ (Cleanup skipped for this run).
   Re-run by hand or file this against the helper; remove the dir with: rm -rf "$TMP"
   ```

   With more than 7 orphans, list the first 6 and replace the rest with one `  … and K more` line, so the report stays within 10 lines. Write `$TMP` out as its literal path: the user can't expand it.

   Take each orphan's severity, confidence, file, line and rationale from the specialist's own `findings/<role>.json` with this exact `jq`, in one Bash call. Pass the orphan IDs the summary printed. This is the single exception to SKILL.md's do-not-re-read rule in [4/5], and it exists because the summary provably does _not_ have this information:

   ```bash
   for f in "$TMP/findings"/*.json; do
     jq -r --argjson orphans '["<id>","<id>"]' '
       .specialist as $role | .findings[]
       | ($role + "-" + (.id | sub("^f-";"") | sub("^" + $role + "-";""))) as $ns
       | select($orphans | index($ns))
       | "  \($ns) | \(.severity) conf=\(.confidence) | \(.file):\(.line) | \(.rationale)"
     ' "$f" 2> /dev/null || echo "  (unreadable, inspect by hand: $f)"
   done
   ```

   **The loop is load-bearing — do not collapse it back to `jq … "$TMP/findings"/*.json`.** `jq` treats a file glob as one concatenated stream, so a single malformed findings file aborts the whole invocation and every file that sorts after it prints nothing at all, including valid files holding orphans. That is silent, order-dependent data loss at the exact moment these files are the only surviving copy of a lost finding — and a malformed file is not hypothetical here, since a specialist writing bad JSON is precisely what `unreadable_roles` exists to record. Per-file, a bad file costs only its own line.

   (The `sub` calls reproduce the helper's ID namespacing: a specialist numbers its findings locally as `f-1` or `<role>-1`, and the loader rewrites both to `<role>-1`. The filter prints that namespaced `$ns`, so each line's ID matches the orphan list.)

2. **Skip Cleanup's `rm -rf` for this run** (see SKILL.md › Cleanup) so the raw findings survive, and make sure the preservation line above names the path. The git-ref deletion still runs. This is Cleanup's `rm -rf` carve-out.
3. Continue to SKILL.md's `POST_MODE` branch as normal — the findings that _did_ reconcile are still valid and worth posting.

This procedure runs in **every** `POST_MODE`, including `auto` and `dry-run`. An unattended run is exactly where a silently-dropped finding would otherwise vanish without anyone seeing it.
