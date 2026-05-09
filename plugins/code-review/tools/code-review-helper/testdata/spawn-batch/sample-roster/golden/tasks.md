<!-- echo block below verbatim; strip Read line numbers -->
TaskCreate({subject: "Review for security", description: "Specialist task — write findings to $REVIEW_TMPDIR/findings/security.json then mark complete.", activeForm: "Reviewing security"})
TaskCreate({subject: "Review for quality", description: "Specialist task — write findings to $REVIEW_TMPDIR/findings/quality.json then mark complete.", activeForm: "Reviewing quality"})
TaskCreate({subject: "Review for errors", description: "Specialist task — write findings to $REVIEW_TMPDIR/findings/errors.json then mark complete.", activeForm: "Reviewing errors"})
TaskCreate({subject: "Review for perf", description: "Specialist task — write findings to $REVIEW_TMPDIR/findings/perf.json then mark complete.", activeForm: "Reviewing perf"})
TaskCreate({subject: "Review for claude-md", description: "Specialist task — write findings to $REVIEW_TMPDIR/findings/claude-md.json then mark complete.", activeForm: "Reviewing claude-md"})
