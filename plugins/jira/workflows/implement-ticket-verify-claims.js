export const meta = {
  name: "implement-ticket-verify-claims",
  description:
    "Verify a JIRA ticket's codebase claims: one agent per claim cluster, each returning a structured verdict with file:line evidence",
  whenToUse:
    "Invoked by /jira:implement-ticket phase 3 when a ticket has many claims. Requires args {ticketKey, guidePath, clusters: [{name, claims}]}.",
  phases: [{ title: "Verify", model: "sonnet" }],
};

// Verdict semantics and evidence rules live in references/claim-verification.md ("Verifying"), passed as args.guidePath.
const VERDICTS = {
  type: "object",
  properties: {
    verdicts: {
      type: "array",
      items: {
        type: "object",
        properties: {
          claim: { type: "string" },
          verdict: {
            type: "string",
            enum: ["confirmed", "false", "partial", "unverifiable"],
          },
          evidence: { type: "string" },
        },
        required: ["claim", "verdict", "evidence"],
      },
    },
  },
  required: ["verdicts"],
};

if (!Array.isArray(args?.clusters)) {
  throw new Error(
    "args.clusters must be a JSON array of { name, claims } — pass an array, not a string",
  );
}

log(`${args.clusters.length} claim clusters to verify for ${args.ticketKey}`);

const results = await pipeline(args.clusters, (c) =>
  agent(
    [
      `You are verifying codebase claims from JIRA ticket ${args.ticketKey}. Procedure: Read ${args.guidePath} and apply its "Verifying" section exactly.`,
      "Check each claim below against the current code in the working tree — `rg`, Read, and LSP where available. Return exactly one verdict per claim, in the order given, with the claim text quoted verbatim.",
      "Every verdict except unverifiable cites `file:line` evidence; for unverifiable, say what you would need to check it (a log, a prod config, a running instance).",
      "Only when a claim hinges on an external library's behavior, check current docs with Context7 (MCP tools load on demand here — call ToolSearch for them first) and cite the version.",
      "Read-only: do not edit, create, or delete any file.",
      `Claims:\n${c.claims.map((claim, i) => `${i + 1}. ${claim}`).join("\n")}`,
    ].join("\n\n"),
    {
      label: `verify:${c.name}`,
      phase: "Verify",
      model: "sonnet",
      schema: VERDICTS,
    },
  ),
);

// results[i] ↔ args.clusters[i]. A dead agent or a verdict count that doesn't match the claim count
// → the whole cluster goes back to the orchestrator for inline verification, so no claim is dropped.
const ok = results.map(
  (r, i) => r && r.verdicts.length === args.clusters[i].claims.length,
);
const unverified = args.clusters.filter((_, i) => !ok[i]).map((c) => c.name);
if (unverified.length) {
  log(
    `${unverified.length} cluster(s) need inline verification: ${unverified.join(", ")}`,
  );
}

return {
  verdicts: results.filter((_, i) => ok[i]).flatMap((r) => r.verdicts),
  unverified,
};
