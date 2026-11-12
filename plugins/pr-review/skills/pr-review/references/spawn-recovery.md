# Spawn recovery

Read this from SKILL.md step [3/5] when the spawn phase goes off-script: you are resumed mid-review, or a specialist's report starts with `FINDINGS_NOT_WRITTEN:`. `$TMP`, `POST_MODE`, the [4/5] report and Cleanup are the ones SKILL.md defines.

## Resumed mid-review

**If you find yourself resumed mid-review** — an unexpected `Continue from where you left off`, or any turn where you cannot tell what landed (the orchestrator process can die while specialists are in flight) — run exactly this check, in this order, and then proceed:

1. `ls -la "$TMP/findings/"` — **disk first.** It is the only durable record.
2. `ListAgents` — which specialists are still alive.
3. **A valid findings file on disk outranks a `status: failed` task-notification.** A process exit invalidates the harness's in-process view, not the bytes a specialist already wrote. On transcript `d67f09e0` the `claude-md` notification said "did not complete… its in-process state was lost" while `findings/claude-md.json` had been on disk, valid and complete, for 71 seconds. Treat that role as **done** and do not re-spawn it — a re-spawn races a second write against a file the rubric declares immutable once written.

If `ListAgents` shows any specialist still alive, end your turn and wait for its notification, as in [3/5]. Once none is alive, continue below.

A role with **no** findings file and **no** live agent is a `missing_role`, full stop. Do not nudge it back to life: no `SendMessage`, no re-spawn, no `AskUserQuestion` asking whether to retry — that is the "invent workarounds" SKILL.md's first paragraph forbids, and it burns turns the run does not have. Continue to [4/5] with whatever landed; a partial roster still produces a useful review, and `missing_roles` reports the gap honestly. Only if _nothing_ landed, stop and jump to Cleanup.

## Finished, but its findings file was refused

This is the one carve-out from the `missing_role` rule above. A specialist whose findings Write is refused follows the rubric's step 4 failure protocol. Its report starts with a `FINDINGS_NOT_WRITTEN: <reason>` line, followed by its findings JSON. That role is not dead: its scan is complete, and the result is in your context.

The refusal is still a block you must not route around on your own. Never Write that JSON unasked: the harness tells you to refuse a subagent's request to perform its denied action and to surface it to the user instead.

**A clean payload is never worth a prompt.** If the JSON has `"findings": []` and `"scan_status": "complete"`, don't ask, in any `POST_MODE`:

- The role is a `missing_role`, and your [4/5] report says it "finished clean, not saved (<reason>)".
- Saving it would change nothing that gets posted: the review body names specialists only when no finding survives at all.
- The prompt, meanwhile, blocks the main thread while other specialists are still running.

Both refusals seen so far were empty: `dfac0a80`'s quality and `d81303f3`'s claude-md. On `d81303f3` the user dismissed this prompt after 110 s and abandoned the run.

Otherwise:

- **`interactive`** — in the turn the report arrives, call AskUserQuestion once for that role.
  - Header: `Save results`.
  - Question: "<role> finished (<N> findings) but its findings file couldn't be saved — <reason>. Save it from <role>'s report?"
  - Options: `Save` / `Treat as missing`.
  - This is not the retry question banned above. Nothing re-runs; the answer only decides whether a finished result is kept, and the decision is the user's.
  - On `Save`, Write the JSON block verbatim to `$TMP/findings/<role>.json`. That Write gets its own permission check.
  - If that Write fails too, the role is a `missing_role`. Don't ask again.
  - A dismissed or rejected question means `Treat as missing`. Don't re-ask, and don't abort the run.
- **`auto` / `dry-run`** — don't prompt. The role is a `missing_role`. In your [4/5] report, say it "finished, not saved (<reason>)" so a clean scan doesn't read as a failed one.

(On transcript `dfac0a80` quality's Write got an auto-mode classifier "no verdict", while infra's and perf's identical writes to the same dir passed. With no path for this case, the orchestrator could only offer a copy-paste shell line, and the user abandoned the run.)
