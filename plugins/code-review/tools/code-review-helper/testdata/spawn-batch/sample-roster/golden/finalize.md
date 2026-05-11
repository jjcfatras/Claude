<!-- BATCH-EMIT CONTRACT — read before echoing -->
<!-- Emit every tool_use line below in ONE assistant message, in order, with nothing between them. -->
<!-- No prose, no thinking text, no narration between calls. No whitespace edits. Strip `cat -n` line-number prefix per line. -->
<!-- If you find yourself about to emit fewer tool_uses than lines below, STOP and re-batch in a single message. -->
<!-- echo block below verbatim -->
SendMessage({to: "security-reviewer", message: "finalize_now: all peers have finished scanning; mark your task complete"})
SendMessage({to: "quality-reviewer", message: "finalize_now: all peers have finished scanning; mark your task complete"})
SendMessage({to: "errors-reviewer", message: "finalize_now: all peers have finished scanning; mark your task complete"})
SendMessage({to: "perf-reviewer", message: "finalize_now: all peers have finished scanning; mark your task complete"})
SendMessage({to: "claude-md-reviewer", message: "finalize_now: all peers have finished scanning; mark your task complete"})
