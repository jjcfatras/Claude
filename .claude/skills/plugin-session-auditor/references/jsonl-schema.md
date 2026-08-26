# Parsed session JSON — schema reference

The parser at `tools/session-parser/main.go` produces a single JSON document per audit run. This file documents the shape so specialists can read it without re-deriving fields.

## Top level (single jsonl input)

```jsonc
{
  "source_file": "<path>",
  "session_id": "<uuid>",
  "cwd": "/Users/.../Projects/Claude",
  "git_branch": "main",
  "version": "2.1.119",                  // Claude Code version that wrote the transcript
  "first_timestamp": "ISO-8601",
  "last_timestamp": "ISO-8601",
  "plugins_used": ["code-review"],       // detected via slash commands and agent subtypes
  "plugin_scope_known": {                // map of plugin -> known commands/agents in repo
    "<plugin>": { "commands": ["..."], "agents": ["..."] }
  },
  "stats": { ... },
  "events": { ... }
}
```

When multiple jsonl files are passed, the top-level shape is `{"sessions": [<above>...]}`.

## `events`

```jsonc
{
  "tool_calls": [ToolCall],
  "tool_failures": [FailureRec],         // is_error: true AND not a permission denial
  "permission_denials": [FailureRec],    // is_error: true AND (toolDenialKind set OR error matches denial markers)
  "agent_spawns": [AgentSpawn],
  "slash_commands": [SlashCmd],
  "hook_events": [HookEvent],
  "api_errors": [APIError]
}
```

### `ToolCall`

```jsonc
{
  "uuid": "<message uuid>",
  "timestamp": "...",
  "tool_use_id": "<id from the tool_use block>",
  "name": "Bash" | "Edit" | "Agent" | "...",
  "input_preview": "<truncated stringified input, max 400 chars>",
  "is_sidechain": true | false,           // true == this call ran inside a subagent
  "parent_uuid": "...",
  "result": ToolResult | null
}
```

### `ToolResult` (embedded in `ToolCall.result`)

```jsonc
{
  "timestamp": "...",
  "is_error": true | false,
  "is_sidechain": true | false,
  "content_preview": "<truncated, max 600 chars>"
}
```

### `FailureRec`

Same shape for `tool_failures` and `permission_denials`. The parser splits them on the raw entry's `toolDenialKind` field, falling back to matching the error text against `permissionRejectMarkers` (see `main.go`) for transcripts written before that field existed.

```jsonc
{
  "timestamp": "...",
  "tool_use_id": "...",
  "tool_name": "...",
  "input_preview": "...",
  "is_sidechain": true | false,
  "error_preview": "...",
  "denial_kind": "permission-rule" | "text-match" // permission_denials only; absent on tool_failures
}
```

**Why `denial_kind` matters when reading a report.** A `PreToolUse` hook that returns `deny` writes its own prose — this repo's `enforce-builtin-tools` guardrail says "Use Edit for modifications, Read for line ranges — not `sed … <file>`" — which matches none of the harness refusal markers. Classifying on text alone therefore filed hook denials as ordinary `tool_failures` and left `permission_denial_count` at 0 even when a run hit the guardrail repeatedly. `denial_kind: "permission-rule"` is a hook or permission rule blocking the call before it ran; `"text-match"` is the legacy path and means only the prose matched.

### `AgentSpawn`

```jsonc
{
  "timestamp": "...",
  "tool_use_id": "...",
  "agent_subtype": "Explore" | "general-purpose" | "code-review-security" | "...",
  "agent_name": "<optional name=...>",
  "description": "<short description from the spawn>",
  "team_name": "<optional>",
  "is_sidechain": true | false,
  // Sidechain correlation. agent_id is taken from the Agent tool_result text.
  // Two formats appear:
  //   "agentId: a<hex>"          general-purpose spawns; one sidechain per spawn
  //   "agent_id: <name>@<team>"  team-mode spawns from TeamCreate; the same role
  //                              may have multiple sidechain transcripts across
  //                              its lifetime, all returned in `sidechains`.
  // `sidechain` is the first matching transcript for ergonomic single-file
  // consumers; iterate `sidechains` for the full picture.
  "agent_id": "<id>",
  "usage_total_tokens": 0,
  "usage_tool_uses": 0,
  "usage_duration_ms": 0,
  "sidechain_file": "<path>",
  "sidechain": Sidechain,
  "sidechains": [Sidechain, ...]
}
```

### `Sidechain`

The parsed contents of a single subagent transcript. Stats follow the same
shape as the top-level `Stats`, so consumers can analyze a specialist's work
the same way they analyze the lead. Recursive — a Sidechain's events may
contain its own `agent_spawns` with their own attached `sidechain` fields.

```jsonc
{
  "agent_type": "<from meta.json — e.g., security-reviewer>",
  "description": "<from meta.json>",
  "first_timestamp": "...",
  "last_timestamp": "...",
  "stats": Stats,
  "events": Events     // omitted in --summary-only mode
}
```

### `SlashCmd`

```jsonc
{
  "timestamp": "...",
  "name": "/code-review",
  "uuid": "...",
}
```

### `HookEvent`

```jsonc
{
  "timestamp": "...",
  "subtype": "stop_hook_summary" | "...",
  "tool_use_id": "...",
  "errors": [ ... ],                     // raw hookErrors array
  "prevented_continuation": true | false | null
}
```

### `APIError`

```jsonc
{
  "timestamp": "...",
  "summary": "<truncated message/error payload>",
}
```

## `stats`

```jsonc
{
  "total_tool_calls": 63,
  "tool_calls_by_name": { "Edit": 29, "Bash": 6, ... },
  "sidechain_tool_calls": 0,
  "main_thread_tool_calls": 63,
  "tool_failure_count": 2,
  "tool_failure_rate": 0.0317,
  "permission_denial_count": 1,
  "permission_denials_by_tool": { "Agent": 1 },
  "permission_denial_runs": [             // back-to-back denials of the same tool
    { "tool_name": "Bash", "start": "ts", "length": 3 }
  ],
  "agent_spawn_count": 1,
  "agent_spawns_by_subtype": { "Explore": 1 },
  "slash_command_count": 1,
  "slash_commands_by_name": { "/code-review": 1 },
  "hook_error_count": 4,
  "api_error_count": 0,
  "turn_count": 4,                        // distinct turn_duration system entries
  "turn_duration_ms_total": 760302,
  "turn_duration_ms_p50": 180679,
  "turn_duration_ms_p95": 217367
}
```

## Field semantics — gotchas

- **Sidechains are followed automatically.** When the main jsonl lives at `<dir>/<sessionId>.jsonl`, the parser looks for `<dir>/<sessionId>/subagents/agent-*.jsonl` and attaches each spawn's transcript to its `AgentSpawn.sidechain` (and the full set to `sidechains`). The orchestration analyzer can therefore compare what each specialist actually did — `tool_calls_by_name`, `tool_failure_count`, etc. — instead of guessing from the lead's view. `is_sidechain` on a tool call is still the lead-vs-subagent flag for the lead's transcript itself.
- **`tool_failures` vs `permission_denials`** — the parser splits these on `toolDenialKind` (see `FailureRec`). If you want both, union them. A guardrail deny lands in `permission_denials` with `denial_kind: "permission-rule"`, so a zero `permission_denial_count` genuinely means no blocked calls — including inside sidechains, which is where hook denials usually happen.
- **Which top-level counters include sidechains.** `rollUpSidechainStats` folds every attached sidechain (recursively, for subagents that spawn their own) into four counters plus one: `tool_failure_count`, `permission_denial_count`, `permission_denials_by_tool`, `hook_error_count`, `api_error_count`, and `sidechain_tool_calls`. `tool_failure_rate` is recomputed over `main_thread_tool_calls + sidechain_tool_calls`. **Everything else stays main-thread-only** — `total_tool_calls`, `main_thread_tool_calls`, `tool_calls_by_name`, `agent_spawn_count`, the slash-command counts, and every `turn_*` field describe the lead's transcript alone. So `total_tool_calls` (20) will read lower than `sidechain_tool_calls` (79) on a fan-out session; that is correct, not a bug. Until Aug 2026 none of the counters rolled up, and this file claimed otherwise — a session whose only hook denial happened inside a subagent reported a clean `0`.
  - **`events.tool_calls` is not rolled up either.** Per-subagent calls live at `agent_spawns[].sidechain.events.tool_calls`; iterate those when you need the actual call sequence.
- **`hook_error_count`** counts only system entries whose `hookErrors` array is actually populated. Empty arrays (which `stop_hook_summary` entries often carry) are excluded — they are turn-summary signals, not errors.
- **`turn_count`** counts `system / turn_duration` entries; older Claude Code versions emitted them less aggressively. Don't read it as "assistant turns" exactly — for that, count `assistant`-type entries in the raw jsonl (the parser doesn't expose this; add to the parser if you need it).
- **`plugins_used`** can be empty — the session may not have invoked any plugin. The lead should bail out early in that case.
- **`input_preview` and `content_preview`** are truncated. If a finding hinges on the full input/output, point the user back at the raw jsonl path or extend the parser.
