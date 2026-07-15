---
name: debate
description: Adversarial debate on a claim or question. Spawns parallel pro and con subagents for opening arguments, then runs up to 5 rebuttal rounds until convergence. Renders an inline markdown report of surviving, negated, and disputed arguments.
argument-hint: <claim or question>
disable-model-invocation: true
model: opus
effort: high
allowed-tools: Bash, Read, Write, Agent
---

# /debate — adversarial pro/con debate on a user claim

You are the orchestrator for /debate. Execute the numbered steps in order. Report progress with one short line per step (e.g. `[1/6] Spawning opening arguments…`). Surface every command failure verbatim and stop — do not invent workarounds.

The user passes the claim or question as `$ARGUMENTS`. If it is empty or whitespace-only, report the error and stop.

## Variables to derive at startup

Resolve once and reuse:

- `CLAIM` — `$ARGUMENTS` (trim leading/trailing whitespace).
- `EPOCH` — `date +%s`.
- `TMP` — scratch dir at `${TMPDIR:-/tmp}/debate-${EPOCH}`. Create with `mkdir -p "$TMP"`.

All subsequent paths derive from `$TMP`. No path uses cwd.

The state model and round semantics are summarized at the bottom of this file under **State machine reference** — re-read that section if any merge step feels ambiguous.

---

## [1/6] Spawn opening arguments (parallel)

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

## [2/6] Build initial state

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

**Edge case — zero opening findings on either side.** If `opening-pro-out.json` or `opening-con-out.json` contains an empty `findings` array, skip the debate loop **and the judge** entirely. Go straight to step [5/6], render a one-sided report with an explicit warning at the top: `"⚠️ <role> produced no opening arguments — debate short-circuited."`, and set the Verdict to a walkover — the side that _did_ produce findings wins by default (`margin: decisive`), with the crux noted as "opponent forfeited". Do not error out.

---

## [3/6] Debate loop (max 3 rounds, early exit on convergence)

> **Hard constraint for everything inside `[3/6]`.** Never invoke a subprocess to read, transform, or write `state.json` — no `Bash`, `jq`, `cat`, `awk`, `sed`, `python -c`, `node -e`, or any shell pipeline. The canonical state lives in your context window: you `Write` it in `[2/6]` and re-`Write` it at the end of each round's `[3c]`. The urge to re-read it from disk is **context drift, not safety** — if you feel it, re-examine the last `Write` payload in context rather than shelling out. The only `Bash` call permitted anywhere in this section is **none**; the `rm -rf` cleanup lives in `[6/6]`, after the loop has exited. Past versions allowed this and produced stale state. A `Write` of any size — including tens of KB of JSON — is correct and expected.

Before entering the loop, initialize `pro_zero_streak = 0` and `con_zero_streak = 0`. These track consecutive rounds where one side filed no attacks and no defenses, and feed the convergence check in step [3d].

A well-matched debate usually resolves in **one or two** rounds: each side lands its strongest attacks, the other defends or concedes, and the offensive vectors run dry. Running to the cap is the exception, not the goal — it almost always means the rebuttal agents kept manufacturing marginal attacks rather than recognizing the debate was decided. Let convergence end the loop early whenever it fires; the cap is a backstop.

For `round = 1` to `3`:

### [3a] Spawn rebuttal pair in parallel

Use the **Write** tool to create both rebuttal input files at `$TMP/rebuttal-pro-r${round}.json` and `$TMP/rebuttal-con-r${round}.json`. Each contains:

```json
{
  "role": "<pro|con>",
  "round": <round>,
  "claim": "<CLAIM>",
  "state": <a pruned view of state — see "State view for rebuttal input" below>
}
```

Build each rebuttal input by constructing the embedded state object in context and passing it directly to `Write`.

❌ **Forbidden — shell pipeline that re-reads state from disk** (this is the exact form past sessions reached for):

```bash
jq -n --slurpfile s state.json \
  --arg role pro --argjson round 3 \
  '{role: $role, round: $round, claim: $s[0].claim, state: $s[0]}' \
  > rebuttal-pro-r3.json
```

Why forbidden: `--slurpfile` reads `state.json` from disk, so any in-context update not yet persisted via `Write` is silently dropped. It also bypasses the pruning step below by handing the full state object to the subagent verbatim.

✅ **Required — `Write` with the in-context pruned state object:**

```text
Write(
  file_path = "$TMP/rebuttal-pro-r3.json",
  content   = JSON({ role: "pro", round: 3, claim: "<CLAIM>", state: <pruned_state> })
)
```

`<pruned_state>` is the filtered view constructed per the next paragraph; never pass the full in-context state object here.

**Construct `<pruned_state>` before calling `Write`.** This is a required procedure, not a description of the output — do not pass the full state object to `Write`. Run these four steps in order every round:

1. Filter `state.findings` to entries with `status == "standing"` or `status == "disputed"`. Drop every `"negated"` entry along with its per-finding `attacks[]` and `defenses[]` sub-arrays.
2. Filter `state.attacks` (the top-level list) to records whose `target_finding_id` appears in the findings kept in step 1. Drop attacks targeting now-negated findings.
3. Build `pruned_state = { claim: <state.claim>, round: <state.round>, findings: <filtered findings>, attacks: <filtered attacks> }`.
4. Pass `pruned_state` (not the full state) inside the `state` field of the rebuttal input JSON when calling `Write`.

This keeps the input focused on live targets and prevents quadratic growth of subagent input as the debate progresses. The canonical state object you hold in context still carries the full history for the final report; only the on-disk rebuttal input is pruned.

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

You own the merge. Subagents never set `status`; you do, by following these rules **exactly** (re-read **State machine reference** below if anything is unclear). All work happens in-context against the canonical state object you already hold. Apply the rules directly in your reasoning and emit the updated state to disk via the **Write** tool at the end of [3c]. There is no need to `Read` `state.json` back after writing it; the file you just wrote is the file you already have in context.

The `[3/6]` hard constraint applies here too: do not shell out to merge state (e.g. `jq '.findings[…] |= ...' state.json > new && mv new state.json`). The merge logic below is easier to apply by reasoning than by transliterating into `jq`, and reading `state.json` back from disk drops any in-context update not yet persisted.

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

Then increment `state.round = current_round`. Use the **Write** tool to save the updated `$TMP/state.json` — write the full in-context state object every time. Do **not** use `Edit` to patch `state.json`: surgical edits on the structured JSON risk silently corrupting `status`/`negated_by` fields without erroring, and a full `Write` is correct and expected even when the state has grown to tens of KB. This mirrors the [3/6] hard constraint above.

### [3d] Convergence check

Compute per-side and total deltas for this round:

```
pro_new = len(pro.new_attacks) + len(pro.new_defenses)
con_new = len(con.new_attacks) + len(con.new_defenses)
total_new = pro_new + con_new
```

Then update the streak counters initialized before the loop:

- If `pro_new == 0`, increment `pro_zero_streak` by 1; otherwise reset `pro_zero_streak = 0`.
- If `con_new == 0`, increment `con_zero_streak` by 1; otherwise reset `con_zero_streak = 0`.

Exit conditions (check in order, break on the first match):

1. **Attack-drought convergence** — if `len(pro.new_attacks) == 0` AND `len(con.new_attacks) == 0` this round, neither side opened a new offensive vector. Any defenses filed this round are maintenance-only (answering the prior round's attacks) and have already been merged in [3b]/[3c], so the structural outcome is decided. Note "attack-drought convergence on round N" for the report. This check is **independent of defense counts** — it must fire even when `total_new > 0`, because the forced defenses on the previous round's attacks would otherwise keep `total_new` non-zero and mask convergence. The check is symmetric (both sides must independently file zero attacks), so a unilateral pause where one side still attacks does not trigger it.
2. **Mutual convergence** — if `total_new == 0`, the debate has converged. Note "converged on round N" for the report.
3. **One-sided exhaustion** — if `pro_zero_streak >= 2` or `con_zero_streak >= 2`, one side has been silent for two consecutive rounds and the structural outcome is decided. Note "converged on round N (one-sided exhaustion)" for the report.

Otherwise, continue to the next round (up to 3 total). The 3-round hard cap is a backstop, not a target — all three convergence paths above are the expected exits, and most debates should hit one of them on round 1 or 2.

---

## [4/6] Adjudicate — spawn the neutral judge

The loop has converged (or hit the cap). The state now records which findings survived, which were negated, and the full attack/defense history — but it does **not** say who _won_. A report that only lists surviving counts reads as a tie even when one side clearly argued better, which is the single biggest complaint about debate output. The judge fixes that: a neutral agent that reads the whole debate with fresh eyes and commits to a verdict.

Use the **Write** tool to create `$TMP/judge-in.json` from the final in-context state. Pass the **full** state here — unlike the rebuttal inputs, do **not** prune negated findings; the judge needs to see what died and how decisively, not just the survivors:

```json
{
  "claim": "<CLAIM>",
  "rounds_run": "<state.round>",
  "convergence": "<the convergence note recorded in [3d], e.g. 'attack-drought convergence on round 2' or 'hit max rounds (3)'>",
  "state": {
    "findings": "<full state.findings — every entry, all statuses included>",
    "attacks": "<full state.attacks>"
  }
}
```

Then spawn the judge — a **single** `Agent` call (not parallel; there is exactly one judge):

- `subagent_type: "judge"`
- `description: "Adjudicate debate"`
- `prompt`:

  ```
  Read the input JSON at <INPUT_PATH> and follow your instructions. Write your verdict JSON to <OUTPUT_PATH>.

  INPUT_PATH:  <TMP>/judge-in.json
  OUTPUT_PATH: <TMP>/verdict.json
  ```

  Substitute `<TMP>` with the actual value before issuing the call.

After the call returns, Read `$TMP/verdict.json`. It contains `{ "winner", "margin", "rationale", "crux", "decider" }` — hold it in context for the report. If the file is missing or malformed, render the report without a verdict block and note `_Verdict unavailable — judge did not return._` in its place rather than erroring out.

---

## [5/6] Render the report (inline, no file write)

Read the final `$TMP/state.json` and the verdict at `$TMP/verdict.json`. Render the report to chat in this exact shape (markdown). Omit empty tables — if a section has no entries, write `_(none)_` instead of an empty table.

```markdown
# Debate: <CLAIM>

_<N rounds run> — <converged on round R | hit max rounds (3)>_

## Verdict

**<Pro wins | Con wins | Genuine toss-up> (<margin>)** — <rationale>

- **Crux:** <crux>
- **What would settle it:** <decider>

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

Notes on the verdict:

- Map `winner` to the label: `pro` → `Pro wins`, `con` → `Con wins`, `toss-up` → `Genuine toss-up`.
- For a `toss-up` winner, render just `**Genuine toss-up** — <rationale>` (drop the `(<margin>)` suffix); the margin field will also read `toss-up` and is redundant in the heading.
- The Verdict is the answer the user came for — render it verbatim from `verdict.json`, do not soften a decisive call into a hedge or invent a tie the judge did not return. `Crux` and `What would settle it` are always present.

Notes on the trace:

- Group bullets by the round the **attack** landed (not the round the response was filed). One bullet per attack.
- For each attack, indicate whether it succeeded (`negated`), was defended (`disputed`), or is still pending (only possible if max rounds was hit without resolution — call that `pending`).
- Truncate `<attack text>` to roughly 240 characters if longer; signal truncation with `…`. Same for defenses.
- If the debate short-circuited because one side produced no openings, the Trace section reads `_(debate did not run — see warning above)_`.

End the orchestrator's chat output with the report. Do not narrate the merge logic or repeat per-round status lines after the report has been rendered.

---

## [6/6] Cleanup

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

Three convergence signals trigger early exit (see step [3d] for the procedural form):

1. **Attack-drought convergence** — a full round produces zero new attacks from **both** sides, regardless of defense count. Neither side has a new offensive vector; the only remaining activity is maintenance defenses on the prior round's attacks, which are merged before exiting. This is the common terminal state for a well-matched claim: attacks taper round over round until both sides run dry while still defending the last wave. Checked first because forced defenses keep `total_new > 0`, so mutual convergence (signal 2) would otherwise miss it.
2. **Mutual convergence** — a full round produces zero new attacks and zero new defenses across both sides. Nothing more to say.
3. **One-sided exhaustion** — one side produces zero attacks and zero defenses for two consecutive rounds. The other side may still be active, but the silent side has no live targets and no undefended attacks of its own to address, so the structural outcome is decided. The two-round threshold avoids false exits on a single tactical pause.

The 3-round hard cap exists as a safety net; all three convergence paths above are the expected exits, and a well-matched debate typically converges on round 1 or 2.
