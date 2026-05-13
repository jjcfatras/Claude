import type { AgentDefinition } from "@anthropic-ai/claude-agent-sdk";
import type { Role } from "../types.js";
import { security } from "./security.js";
import { typescript } from "./typescript.js";
import { react } from "./react.js";
import { infra } from "./infra.js";
import { errors } from "./errors.js";
import { perf } from "./perf.js";
import { quality } from "./quality.js";
import { claudeMd } from "./claude-md.js";

export { prSummary } from "./pr-summary.js";

export const agentsByRole: Record<Role, AgentDefinition> = {
  security,
  typescript,
  react,
  infra,
  errors,
  perf,
  quality,
  "claude-md": claudeMd,
};
