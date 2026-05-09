<!-- echo block below verbatim; strip Read line numbers -->
SendMessage({to: "security-reviewer", message: {"type": "shutdown_request", "reason": "review complete, team teardown"}})
SendMessage({to: "quality-reviewer", message: {"type": "shutdown_request", "reason": "review complete, team teardown"}})
SendMessage({to: "errors-reviewer", message: {"type": "shutdown_request", "reason": "review complete, team teardown"}})
SendMessage({to: "perf-reviewer", message: {"type": "shutdown_request", "reason": "review complete, team teardown"}})
SendMessage({to: "claude-md-reviewer", message: {"type": "shutdown_request", "reason": "review complete, team teardown"}})
