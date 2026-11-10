# Posting fallback (tiers 2–3)

Read this from SKILL.md step [5/5] when tier 1 fails: start at tier 2 after `HTTP 422`, at tier 3 after any other failure. Tier 1's stderr is already in `$TMP/tier1.err`. Run each block below as **one** Bash call: shell variables don't survive between calls.

**Tier 2 — create pending review then submit:**

```bash
REVIEW_ID=$(gh api "repos/$OWNER/$REPO/pulls/$PR_NUMBER/reviews" --method POST --input "$TMP/payload-pending.json" --jq '.id' 2> "$TMP/tier2.err")
if [ -z "$REVIEW_ID" ]; then
  echo "tier 2 create failed — falling to tier 3"
elif jq '. + {event: "COMMENT"}' "$TMP/payload-body.json" \
  | gh api "repos/$OWNER/$REPO/pulls/$PR_NUMBER/reviews/$REVIEW_ID/events" --method POST --input - > /dev/null 2> "$TMP/tier2.err"; then
  echo "posted via tier 2 (review $REVIEW_ID)"
else
  echo "tier 2 submit failed — pending review $REVIEW_ID is dangling; falling to tier 3"
fi
```

`event` goes into the request body through `jq`, never as `-f event=COMMENT`: with `--input`, `gh` sends field flags as URL query parameters, and `payload-body.json` holds only `body`. Whichever step fails leaves its stderr in `$TMP/tier2.err` for the tier-3 patch; don't `cat` it.

- `posted via tier 2` → report it and go to SKILL.md's Cleanup.
- `tier 2 create failed` → continue at tier 3.
- `tier 2 submit failed` → warn the user that the pending review is dangling and give them the command that removes it, with the real values filled in: `gh api --method DELETE "repos/<OWNER>/<REPO>/pulls/<PR_NUMBER>/reviews/<REVIEW_ID>"`. Then continue at tier 3.

**Tier 3 — fallback issue comment:**

Patch the `{API_ERROR}` placeholder in `$TMP/fallback.md` with the captured stderr from the last failing tier — guardrail-safe (no `sed -i`, no `cat`), writing to the `.patched` copy:

```bash
ERRFILE="$TMP/tier1.err"
[ -s "$TMP/tier2.err" ] && ERRFILE="$TMP/tier2.err"
python3 -c "import pathlib,sys; src=pathlib.Path('$TMP/fallback.md').read_text(); pathlib.Path('$TMP/fallback.md.patched').write_text(src.replace('{API_ERROR}', sys.stdin.read()))" < "$ERRFILE"
gh pr comment "$PR_NUMBER" -F "$TMP/fallback.md.patched"
```

Report `posted via tier 3` on success, then go to SKILL.md's Cleanup. If tier 3 also fails, surface the full error and stop: that is a fatal stop, so run Cleanup and end with a `failed:` marker.
