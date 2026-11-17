---
name: transcript
description: Print the filepath of the current Claude Code session's .jsonl transcript. Use when the user asks where this session's transcript / session log / .jsonl file is, asks for "the transcript path", or when you need the current session's transcript file to pass to another tool such as the plugin-session-auditor. Only for locating the current session's file — not for reading or analyzing transcript contents.
allowed-tools: Bash(${CLAUDE_SKILL_DIR}/scripts/transcript-path.sh *)
effort: low
---

Run `${CLAUDE_SKILL_DIR}/scripts/transcript-path.sh` with Bash exactly once, exactly as written (no quotes, no arguments), and print only its stdout. No preamble, no commentary, no markdown formatting around the result.
