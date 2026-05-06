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
  "permission_denials": [FailureRec],    // is_error: true AND error matches denial markers
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
Same shape for `tool_failures` and `permission_denials`. The parser splits them based on the error text (see `permissionRejectMarkers` in `main.go`).
```jsonc
{
  "timestamp": "...",
  "tool_use_id": "...",
  "tool_name": "...",
  "input_preview": "...",
  "is_sidechain": true | false,
  "error_preview": "..."
}
```

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
  "uuid": "..."
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
  "summary": "<truncated message/error payload>"
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
- **`tool_failures` vs `permission_denials`** — the parser splits these. If you want both, union them.
- **`hook_error_count`** counts only system entries whose `hookErrors` array is actually populated. Empty arrays (which `stop_hook_summary` entries often carry) are excluded — they are turn-summary signals, not errors.
- **`turn_count`** counts `system / turn_duration` entries; older Claude Code versions emitted them less aggressively. Don't read it as "assistant turns" exactly — for that, count `assistant`-type entries in the raw jsonl (the parser doesn't expose this; add to the parser if you need it).
- **`plugins_used`** can be empty — the session may not have invoked any plugin. The lead should bail out early in that case.
- **`input_preview` and `content_preview`** are truncated. If a finding hinges on the full input/output, point the user back at the raw jsonl path or extend the parser.
