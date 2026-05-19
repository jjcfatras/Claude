---
description: Adversarial debate on a claim or question. Spawns parallel pro and con subagents for opening arguments, then runs up to 5 rebuttal rounds until convergence. Renders an inline markdown report of surviving, negated, and disputed arguments.
argument-hint: <claim or question>
model: opus
effort: high
allowed-tools: Bash, Read, Write, Agent
---

# /debate — adversarial pro/con debate on a user claim

You are the orchestrator for /debate. Execute the numbered steps in order. Report progress with one short line per step (e.g. `[1/5] Spawning opening arguments…`). Surface every command failure verbatim and stop — do not invent workarounds.

The user passes the claim or question as `$ARGUMENTS`. If it is empty or whitespace-only, report the error and stop.

## Variables to derive at startup

Resolve once and reuse:

- `CLAIM` — `$ARGUMENTS` (trim leading/trailing whitespace).
- `EPOCH` — `date +%s`.
- `TMP` — scratch dir at `${TMPDIR:-/tmp}/debate-${EPOCH}`. Create with `mkdir -p "$TMP"`.

All subsequent paths derive from `$TMP`. No path uses cwd.

The state model and round semantics are summarized at the bottom of this file under **State machine reference** — re-read that section if any merge step feels ambiguous.

---

## [1/5] Spawn opening arguments (parallel)

Use the **Write** tool to create the two opening-input files at `$TMP/opening-pro.json` and `$TMP/opening-con.json`. Each file contains:

```json
{
  "role": "<pro|con>",
  "claim": "<CLAIM>",
  "min_findings": 3,
  "max_findings": 7
}
```

Substitute `<CLAIM>` with the actual claim string verbatim. Substitute the role for each file.

Then spawn both opening agents **in parallel** — emit **one single message** that contains two `Agent` tool calls (pro + con). For each:

- `subagent_type: "opening"`
- `description: "Opening arguments — <role>"`
- `prompt`:

  ```
  Read the input JSON at <INPUT_PATH> and follow your instructions. Write your findings JSON to <OUTPUT_PATH>.

  INPUT_PATH:  <TMP>/opening-<role>.json
  OUTPUT_PATH: <TMP>/opening-<role>-out.json
  ```

  Substitute `<TMP>` and `<role>` with the actual values before issuing the call.

After both Agent calls return, Read both output files. Each should contain `{ "role": "<role>", "findings": [ { "id": "<role>-N", "text": "..." } ] }`.

---

## [2/5] Build initial state

Re-prefix IDs so they cannot collide across the two parallel openings: the pro side keeps `pro-1`, `pro-2`, … in original order; the con side keeps `con-1`, `con-2`, …. (The subagents already follow this convention but you must verify — if either side emitted any non-conforming id, rewrite it to the correct `<role>-<n>` form during this step.)

Use the **Write** tool to create `$TMP/state.json`:

```json
{
  "claim": "<CLAIM>",
  "round": 0,
  "findings": [
    {
      "id": "pro-1",
      "side": "pro",
      "text": "<text from opening-pro-out.json>",
      "status": "standing",
      "attacks": [],
      "defenses": [],
      "negated_by": null
    }
    // ...one entry per pro finding, then all con findings in the same shape
  ],
  "attacks": []
}
```

Notes:

- `attacks` on the **finding** is the per-finding list of attack records targeting that finding. `attacks` at the top level is the flat list of all attack records across the debate (same records, indexed both ways for convenience).
- Every opening finding starts `status: "standing"`, `attacks: []`, `defenses: []`, `negated_by: null`.

**Edge case — zero opening findings on either side.** If `opening-pro-out.json` or `opening-con-out.json` contains an empty `findings` array, skip the debate loop entirely. Go straight to step [4/5] and render a one-sided report with an explicit warning at the top: `"⚠️ <role> produced no opening arguments — debate short-circuited."`. Do not error out.

---

## [3/5] Debate loop (max 5 rounds, early exit on convergence)

For `round = 1` to `5`:

### [3a] Spawn rebuttal pair in parallel

Read the current `$TMP/state.json`. Use the **Write** tool to create both rebuttal input files at `$TMP/rebuttal-pro-r${round}.json` and `$TMP/rebuttal-con-r${round}.json`. Each contains:

```json
{
  "role": "<pro|con>",
  "round": <round>,
  "claim": "<CLAIM>",
  "state": <the full contents of $TMP/state.json, embedded inline>
}
```

Spawn both rebuttal agents **in parallel** — one single message, two `Agent` tool calls. For each:

- `subagent_type: "rebuttal"`
- `description: "Round <round> rebuttal — <role>"`
- `prompt`:

  ```
  Read the input JSON at <INPUT_PATH> and follow your instructions. Write your deltas JSON to <OUTPUT_PATH>.

  INPUT_PATH:  <TMP>/rebuttal-<role>-r<round>.json
  OUTPUT_PATH: <TMP>/rebuttal-<role>-r<round>-out.json
  ```

After both return, Read both output files.

### [3b] Merge deltas into state

You own the merge. Subagents never set `status`; you do, by following these rules **exactly** (re-read **State machine reference** below if anything is unclear). All work happens against the in-memory state you just Read.

For each `new_attacks` entry across both rebuttal outputs (process pro's attacks first, then con's — order does not matter semantically, just be consistent):

1. Look up the target finding by `target_finding_id` in `findings`.
2. If the target does not exist, or its `side` matches the attacker's side (attacking own finding — illegal), or its `status == "negated"`, **discard the attack silently** (stale or invalid). Do not log; do not surface.
3. Otherwise, build the attack record:
   ```json
   {
     "id": "<attack id from subagent>",
     "round": <current round>,
     "attacker": "<pro|con>",
     "target_finding_id": "<id>",
     "text": "<attack text>"
   }
   ```
4. Append the record to both `findings[i].attacks` (on the target) and the top-level `attacks` list.

For each `new_defenses` entry across both rebuttal outputs:

1. Look up the attack by `target_attack_id` in the top-level `attacks` list.
2. If the attack does not exist, or the defended finding's `side` does not match the defender's side, **discard silently**.
3. Otherwise, build the defense record:
   ```json
   {
     "target_attack_id": "<id>",
     "round": <current round>,
     "text": "<defense text>"
   }
   ```
4. Append the record to `findings[i].defenses` on the defended finding.

### [3c] Resolution sweep

After all attacks and defenses are merged for this round, walk every finding `F` whose `status != "negated"`:

1. For each `a` in `F.attacks` where `a.round < current_round`:
   - Search `F.defenses` for any entry where `defense.target_attack_id == a.id`.
   - If no such defense exists, set `F.status = "negated"` and `F.negated_by = a.id`, then break out of the per-attack loop for `F`.
2. If `F.status` was not flipped to `"negated"` in step 1 AND `F.attacks` is non-empty:
   - Set `F.status = "disputed"`. (Every attack landed has been countered — at least so far.)
3. Otherwise (no attacks at all on `F`): leave `F.status = "standing"`.

Then increment `state.round = current_round`. Use the **Write** tool to save the updated `$TMP/state.json`.

### [3d] Convergence check

Compute:

```
total_new = len(pro.new_attacks) + len(pro.new_defenses)
          + len(con.new_attacks) + len(con.new_defenses)
```

If `total_new == 0`, break out of the loop — the debate has converged. Note which round converged for the report.

Otherwise, continue to the next round (up to 5 total).

---

## [4/5] Render the report (inline, no file write)

Read the final `$TMP/state.json`. Render the report to chat in this exact shape (markdown). Omit empty tables — if a section has no entries, write `_(none)_` instead of an empty table.

```markdown
# Debate: <CLAIM>

_<N rounds run> — <converged on round R | hit max rounds (5)>_

## Summary

Pro surviving: **<A>** · Con surviving: **<B>** · Negated: **<K>** · Disputed: **<D>**

> _Surviving_ = `standing` or `disputed`. _Disputed_ findings are a subset of surviving — the opposing side attacked them but did not negate them. _Negated_ = attacked without a successful defense.

## Pro — Surviving Arguments

| ID    | Argument    | Status            |
| ----- | ----------- | ----------------- |
| pro-1 | <full text> | standing/disputed |

## Con — Surviving Arguments

| ID    | Argument    | Status            |
| ----- | ----------- | ----------------- |
| con-1 | <full text> | standing/disputed |

## Negated Arguments

| ID    | Side | Argument    | Negated by                             |
| ----- | ---- | ----------- | -------------------------------------- |
| pro-3 | pro  | <full text> | <attack-id> (round N): "<attack text>" |

## Debate Trace

**Round 1**

- <attacker> attacks <target-id> → "<attack text>"
  → <defender defended in round N+1 → disputed | not defended → negated | counter-attacked in round N+k>
- ...

**Round 2**

- ...
```

Notes on the trace:

- Group bullets by the round the **attack** landed (not the round the response was filed). One bullet per attack.
- For each attack, indicate whether it succeeded (`negated`), was defended (`disputed`), or is still pending (only possible if max rounds was hit without resolution — call that `pending`).
- Truncate `<attack text>` to roughly 240 characters if longer; signal truncation with `…`. Same for defenses.
- If the debate short-circuited because one side produced no openings, the Trace section reads `_(debate did not run — see warning above)_`.

End the orchestrator's chat output with the report. Do not narrate the merge logic or repeat per-round status lines after the report has been rendered.

---

## [5/5] Cleanup

After the report is rendered, remove the scratch dir. Defensive check: only `rm -rf` paths whose basename starts with `debate-` (the prefix you created):

```bash
case "$(basename "$TMP")" in
  debate-*) rm -rf "$TMP" ;;
  *) echo "refusing to remove $TMP (unexpected prefix)" ;;
esac
```

Then stop. Do not print any post-cleanup status line — the report is the final user-facing output.

---

## State machine reference

Single source of truth for the rules used in step [3b]/[3c]. Read this whenever a merge feels ambiguous.

### Finding status

Three terminal-ish values: `standing`, `disputed`, `negated`. Status is **derived** at the end of every round from each finding's `attacks` and `defenses` history — you do not track intermediate states.

- `standing` — `attacks == []`. No one ever attacked this finding.
- `disputed` — `attacks` is non-empty AND every attack from a **previous** round has a matching defense (matched by `defense.target_attack_id == attack.id`). Attacks landed this current round do **not** yet require a defense — defender's chance is next round.
- `negated` — there is at least one attack from a previous round whose `id` does not appear in any defense's `target_attack_id`. Terminal.

`negated` is terminal. A finding never transitions out of `negated` even if a later subagent emits a stale defense for it (such defenses are silently discarded during merge).

### Defense window

A defense filed in round `N` must reference an attack from round `N-1` or earlier (typically `N-1`). Defenders see the post-round-(N-1) state at the start of their round-N invocation, so any attack landing in round `N` is invisible to them until round `N+1`. This is intentional — the one-round defense window is what makes the debate terminate.

### Stale attacks and defenses

Subagents have no memory across rounds and may emit:

- An attack against an already-negated finding → discard silently.
- An attack against a finding on the attacker's own side → discard silently (illegal).
- A defense for an attack `id` that doesn't exist or doesn't target the defender's side → discard silently.

Discards are not surfaced to the user. They are normal artifacts of stateless subagents.

### Convergence

If a full round produces zero new attacks AND zero new defenses across both sides, the debate has converged — break early. The 5-round hard cap exists as a safety net; convergence is the expected exit path.
