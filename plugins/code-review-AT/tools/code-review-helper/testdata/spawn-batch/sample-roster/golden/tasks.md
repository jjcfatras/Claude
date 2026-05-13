<!-- BATCH-EMIT CONTRACT — read before echoing -->
<!-- Emit every tool_use line below in ONE assistant message, in order, with nothing between them. -->
<!-- No prose, no thinking text, no narration between calls. No whitespace edits. Strip `cat -n` line-number prefix per line. -->
<!-- If you find yourself about to emit fewer tool_uses than lines below, STOP and re-batch in a single message. -->
<!-- echo block below verbatim -->
TaskCreate({subject: "Review for security", description: "Specialist task — write findings to $REVIEW_TMPDIR/findings/security.json then mark complete.", activeForm: "Reviewing security"})
TaskCreate({subject: "Review for quality", description: "Specialist task — write findings to $REVIEW_TMPDIR/findings/quality.json then mark complete.", activeForm: "Reviewing quality"})
TaskCreate({subject: "Review for errors", description: "Specialist task — write findings to $REVIEW_TMPDIR/findings/errors.json then mark complete.", activeForm: "Reviewing errors"})
TaskCreate({subject: "Review for perf", description: "Specialist task — write findings to $REVIEW_TMPDIR/findings/perf.json then mark complete.", activeForm: "Reviewing perf"})
TaskCreate({subject: "Review for claude-md", description: "Specialist task — write findings to $REVIEW_TMPDIR/findings/claude-md.json then mark complete.", activeForm: "Reviewing claude-md"})
