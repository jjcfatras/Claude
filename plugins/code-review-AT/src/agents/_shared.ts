import type { AgentDefinition } from "@anthropic-ai/claude-agent-sdk";

export type SpecialistTools = string[];

export const SPECIALIST_TOOLS: SpecialistTools = [
  "Read",
  "Grep",
  "Glob",
  "Bash",
  "mcp__plugin_github_github__get_file_contents",
  "mcp__plugin_context7_context7__resolve-library-id",
  "mcp__plugin_context7_context7__query-docs",
  "mcp__peer-verification__verify_with_peer",
];

export const OUTPUT_INSTRUCTIONS = `
## Output

Return your findings as structured JSON matching the findings schema. The orchestrator collects \`structured_output\` from your terminal message — do NOT write any files, do NOT print the JSON to the assistant text, do NOT call any \`Write\`/\`Bash\` heredoc. Use the \`outputFormat\` channel.

Required fields per finding: \`id\` (your specialist prefix + sequence, e.g. \`sec-1\`), \`category\`, \`file\` (repo-relative path), \`line\` (1-based new-file line number), \`confidence\` (0-100), \`severity\` (\`Critical\`/\`Medium\`/\`Minor\`), \`rationale\` (one sentence), \`explanation\` (full reasoning, may quote source), \`code\` (the offending snippet), \`language\` (e.g. \`ts\`, \`tsx\`, \`go\`, \`sql\`), \`verifications\` (default \`[]\`).

Set top-level \`specialist\` to your role name and \`scan_status\` to \`complete\` on success. If you ran out of time or hit a tool limit, set \`scan_status\` to \`timed_out\` and emit what you have.

## Peer verification

You may call the \`verify_with_peer\` tool to ask another specialist a focused cross-domain question. Use it sparingly — each call is a synchronous SDK side-query that blocks your turn ~5-15s. Target role must be in the roster. Only ask questions inside the target's domain. The tool returns the peer's freeform answer text.
`;

export const buildAgent = (params: {
  description: string;
  prompt: string;
  tools?: SpecialistTools;
  model?: string;
}): AgentDefinition => ({
  description: params.description,
  prompt: params.prompt + OUTPUT_INSTRUCTIONS,
  tools: params.tools ?? SPECIALIST_TOOLS,
  model: params.model ?? "sonnet",
});
