export const meta = {
  name: "respond-to-review-triage",
  description:
    "Triage PR review items: one verdict per item, then adversarially verify every dismissal and every accepted response",
  phases: [{ title: "Analyze" }, { title: "Verify" }],
};

// Keep in sync with assets/agent-preamble.md in this skill (SKILL.md Step 1c copies it into parsing-agent prompts).
const PREAMBLE = [
  "You are a review-response analysis agent. Use the Read/Write tools and `rg` for searching, rather than `sed`/`awk`/`du` — those aren't in this skill's `allowed-tools`, so they prompt outside auto permission mode. Don't pipe untrusted data into `sh`/`bash`. Don't run `rm -rf` or destructive git operations without confirmation. Do NOT post anything to GitHub — only the main skill posts replies in Step 6.",
  "**Verify library claims with Context7.** When evaluating a reviewer's concern that hinges on a specific library, framework, or external API (React hooks, Prisma, Next.js routing, AWS SDK, etc.), verify the claim against current docs before deciding: call `mcp__plugin_context7_context7__resolve-library-id`, then `mcp__plugin_context7_context7__query-docs` with the returned ID. Use this to confirm a **false-positive** verdict (the library really does handle the concern) or a **valid** verdict (the library really does behave as the reviewer says). Skip Context7 for general programming patterns, project-internal logic, or anything verifiable from the diff alone — don't burn calls on claims that don't depend on external library behavior.",
].join("\n\n");

// Field semantics live in references/triage-checks.md ("The verdict object").
const VERDICT = {
  type: "object",
  properties: {
    id: { type: "string" },
    verdict: {
      type: "string",
      enum: ["preexisting", "false-positive", "valid", "accepted", "disputed"],
    },
    reasoning: { type: "string" },
    evidence: {
      type: "object",
      properties: {
        kind: {
          type: "string",
          enum: ["blame", "code", "diff", "library-docs", "none"],
        },
        file: { type: "string" },
        line: { type: "integer" },
        quote: { type: "string" },
        blame_sha: { type: "string" },
        blame_date: { type: "string" },
        fix_sha: { type: "string" },
      },
      required: ["kind"],
    },
    fix_plan: { type: "string" },
    vague: { type: "boolean" },
  },
  required: ["id", "verdict", "reasoning", "evidence", "vague"],
};

const CHECK = {
  type: "object",
  properties: {
    reproduced: { type: "boolean" },
    reason: { type: "string" },
  },
  required: ["reproduced", "reason"],
};

if (!Array.isArray(args.items)) {
  throw new Error(
    "args.items must be a JSON array — pass the parsed pending-items list, not a string",
  );
}

const prCommits = (args.prCommits || []).join(", ") || "(none listed)";
const ctx = [
  `PR #${args.pr}. Head ${args.headSha}, base ${args.baseSha}. PR commits: ${prCommits}.`,
  `The review dir ($REVIEW_DIR) is ${args.reviewDir}; the PR diff is saved there as pr.diff.`,
  `The PR head is checked out at ${args.worktree} — resolve every repo-relative path against it.`,
  `Procedure: Read ${args.checksPath} and apply it exactly, including its "Reproduce before you record" rule.`,
  "MCP tools (Context7) load on demand here — call ToolSearch for them before use.",
].join(" ");

log(`${args.items.length} review items to triage`);

const verdicts = await pipeline(
  args.items,
  (item) =>
    agent(
      `${PREAMBLE}\n\n${ctx}\n\nAnalyze exactly this one review item and return a verdict object.\n\n${JSON.stringify(item, null, 2)}`,
      { label: `analyze:${item.id}`, phase: "Analyze", schema: VERDICT },
    ),
  async (v, item) => {
    // Analysis agent was skipped or died: hand the item back so the orchestrator analyzes it inline.
    if (!v) return { id: item.id, verdict: null };
    // Valid and disputed are already the safe defaults and the user gates them in Step 3; only verdicts that close a concern get a second reader.
    if (v.verdict === "valid" || v.verdict === "disputed") return v;
    const accepted = v.verdict === "accepted";
    const claim = accepted
      ? `accepted the latest response in this review thread as resolving the original comment (the thread will be resolved), citing the evidence below. Your job is to REFUTE that acceptance. If fix_sha is set, \`git merge-base --is-ancestor <fix_sha> ${args.headSha}\` must exit 0. The cited code must resolve the ORIGINAL comment's concern at the head SHA, not merely match what the response says it changed.`
      : `dismissed this review item as "${v.verdict}" citing the evidence below. Your job is to REFUTE that dismissal.`;
    const check = await agent(
      `${PREAMBLE}\n\n${ctx}\n\nA prior agent ${claim} Independently reproduce every cited field, by evidence kind:\n- blame: run \`git blame -L <line>,<line> ${args.headSha} -- <file>\`; the SHA must equal blame_sha, must not be one of the PR commits, and the date must match.\n- code / diff: Read the cited file at the cited lines (or the saved diff); the quote must appear there verbatim AND must actually address the reviewer's concern.\n- library-docs: re-query Context7 for the claim.\n- none: there is nothing to reproduce — set reproduced=false.\nIf any field is absent, different, or does not address the concern, set reproduced=false and say which. Default to reproduced=false when uncertain.\n\nItem:\n${JSON.stringify(item, null, 2)}\n\nClaimed verdict:\n${JSON.stringify(v, null, 2)}`,
      { label: `verify:${item.id}`, phase: "Verify", schema: CHECK },
    );
    if (check && check.reproduced) return { ...v, verified: true };
    return {
      ...v,
      verdict: accepted ? "disputed" : "valid",
      overturned: v.verdict,
      reasoning: `Verifier could not reproduce the ${v.verdict} evidence: ${check ? check.reason : "verifier unavailable"}. Original reasoning: ${v.reasoning}`,
      evidence: { kind: "none" },
    };
  },
);

const overturned = verdicts.filter((v) => v && v.overturned).length;
const unanalyzed = verdicts.filter((v) => !v || v.verdict === null).length;
log(
  `${verdicts.length} verdicts — ${overturned} overturned, ${unanalyzed} need inline analysis`,
);

// verdicts[i] corresponds to args.items[i]. null or verdict:null → the orchestrator analyzes that item inline.
return { verdicts };
