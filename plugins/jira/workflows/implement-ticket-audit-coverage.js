export const meta = {
  name: "implement-ticket-audit-coverage",
  description:
    "Audit a finished changeset for test coverage: one agent per changed file, one verdict per changed behavior",
  whenToUse:
    "Invoked by /jira:implement-ticket phase 7 when the diff spans many files. Requires args {worktree, diffBase, files: [repo-relative paths]}.",
  phases: [{ title: "Audit", model: "sonnet" }],
};

// Verdict definitions mirror skills/implement-ticket/SKILL.md phase [7/7] — when editing either, edit both.
const AUDIT = {
  type: "object",
  properties: {
    behaviors: {
      type: "array",
      items: {
        type: "object",
        properties: {
          file: { type: "string" },
          behavior: { type: "string" },
          verdict: {
            type: "string",
            enum: ["covered", "uncovered", "not-testable"],
          },
          coveringTest: { type: "string" },
        },
        required: ["file", "behavior", "verdict"],
      },
    },
  },
  required: ["behaviors"],
};

if (!Array.isArray(args?.files)) {
  throw new Error(
    "args.files must be a JSON array of repo-relative paths — pass an array, not a string",
  );
}
if (typeof args.worktree !== "string" || !args.worktree) {
  throw new Error(
    "args.worktree must be the absolute path of the ticket's worktree",
  );
}

log(`${args.files.length} changed files to audit against ${args.diffBase}`);

const results = await pipeline(args.files, (file) =>
  agent(
    [
      `You are auditing test coverage for the changes to \`${file}\`. Run \`git -C ${args.worktree} diff ${args.diffBase} -- ${file}\` to see them; if that prints nothing because the file is untracked, Read \`${args.worktree}/${file}\` whole — every behavior in it is new. Resolve every repo-relative path against ${args.worktree}.`,
      "Go hunk by hunk and name each behavior a hunk introduces or changes — the behavior, not the line, is the unit. Give each one a verdict:",
      "- covered — name the test (file and test name) that asserts the new behavior, such that reverting the hunk would turn that test red. A test that merely executes the changed line does not count.\n- uncovered — the behavior changed and no test would notice if it broke.\n- not-testable — config, docs, copy, dependency bumps, pure formatting.",
      "Audit only: do not edit files, write tests, run the test suite, or run mutation checks — the orchestrator does all of that after this audit.",
    ].join("\n\n"),
    { label: `audit:${file}`, phase: "Audit", model: "sonnet", schema: AUDIT },
  ),
);

// results[i] ↔ args.files[i]. A dead agent → the orchestrator audits that file inline.
const unaudited = args.files.filter((_, i) => !results[i]);
if (unaudited.length) {
  log(`${unaudited.length} file(s) need inline audit: ${unaudited.join(", ")}`);
}

return {
  behaviors: results.filter(Boolean).flatMap((r) => r.behaviors),
  unaudited,
};
