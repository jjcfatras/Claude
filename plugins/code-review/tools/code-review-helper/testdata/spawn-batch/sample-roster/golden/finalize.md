<!-- echo block below verbatim; strip Read line numbers -->
SendMessage({to: "security-reviewer", message: "finalize_now: all peers have finished scanning; mark your task complete"})
SendMessage({to: "quality-reviewer", message: "finalize_now: all peers have finished scanning; mark your task complete"})
SendMessage({to: "errors-reviewer", message: "finalize_now: all peers have finished scanning; mark your task complete"})
SendMessage({to: "perf-reviewer", message: "finalize_now: all peers have finished scanning; mark your task complete"})
SendMessage({to: "claude-md-reviewer", message: "finalize_now: all peers have finished scanning; mark your task complete"})
