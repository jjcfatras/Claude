<!-- BATCH-EMIT CONTRACT — read before echoing -->
<!-- Emit every tool_use line below in ONE assistant message, in order, with nothing between them. -->
<!-- No prose, no thinking text, no narration between calls. No whitespace edits. Strip `cat -n` line-number prefix per line. -->
<!-- If you find yourself about to emit fewer tool_uses than lines below, STOP and re-batch in a single message. -->
<!-- echo block below verbatim -->
SendMessage({to: "security-reviewer", message: {"type": "shutdown_request", "reason": "review complete, team teardown"}})
SendMessage({to: "quality-reviewer", message: {"type": "shutdown_request", "reason": "review complete, team teardown"}})
SendMessage({to: "errors-reviewer", message: {"type": "shutdown_request", "reason": "review complete, team teardown"}})
SendMessage({to: "perf-reviewer", message: {"type": "shutdown_request", "reason": "review complete, team teardown"}})
SendMessage({to: "claude-md-reviewer", message: {"type": "shutdown_request", "reason": "review complete, team teardown"}})
