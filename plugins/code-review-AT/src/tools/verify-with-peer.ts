import {
  createSdkMcpServer,
  query,
  tool,
  type McpSdkServerConfigWithInstance,
} from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
import type { Role } from "../types.js";
import { ROLES } from "../types.js";
import { agentsByRole } from "../agents/index.js";

const PEER_TIMEOUT_MS = 60_000;
const PEER_MAX_TURNS = 5;

const verifyWithPeerSchema = {
  target_role: z
    .enum(ROLES)
    .describe("Specialist role to verify with (must be in the roster)."),
  question: z
    .string()
    .min(10)
    .describe(
      "Focused cross-domain question for the target specialist. One topic per call. Quote diff lines you want them to evaluate.",
    ),
};

const errText = (
  message: string,
): { content: [{ type: "text"; text: string }] } => ({
  content: [{ type: "text", text: message }],
});

export const buildPeerMcpServer = (
  caller: Role,
  roster: readonly Role[],
): McpSdkServerConfigWithInstance =>
  createSdkMcpServer({
    name: "peer-verification",
    version: "1.0.0",
    tools: [
      tool(
        "verify_with_peer",
        `Ask a peer specialist a focused cross-domain question. Synchronous — blocks your turn until the peer responds (~5-15s). Use sparingly: prefer evidence from the diff and rubric over verifying with a peer.`,
        verifyWithPeerSchema,
        async ({ target_role, question }) => {
          if (target_role === caller) {
            return errText(
              `verify_with_peer: cannot verify with self (caller is ${caller}).`,
            );
          }
          if (!roster.includes(target_role)) {
            return errText(
              `verify_with_peer: role "${target_role}" is not in this PR's roster (${roster.join(", ")}).`,
            );
          }

          const targetDef = agentsByRole[target_role];
          const ac = new AbortController();
          const timer = setTimeout(() => ac.abort(), PEER_TIMEOUT_MS);

          let answer = "";
          try {
            const iter = query({
              prompt: question,
              options: {
                systemPrompt: targetDef.prompt,
                model: targetDef.model ?? "sonnet",
                maxTurns: PEER_MAX_TURNS,
                abortController: ac,
                allowedTools: ["Read", "Grep", "Bash"],
                permissionMode: "bypassPermissions",
                allowDangerouslySkipPermissions: true,
              },
            });
            for await (const msg of iter) {
              if (
                msg.type === "result" &&
                msg.subtype === "success" &&
                msg.result
              ) {
                answer = msg.result;
              }
            }
          } catch (e) {
            if (ac.signal.aborted) {
              answer = `peer_timeout: ${target_role} did not respond within ${PEER_TIMEOUT_MS / 1000}s`;
            } else {
              answer = `peer_error: ${(e as Error).message}`;
            }
          } finally {
            clearTimeout(timer);
          }

          return {
            content: [
              {
                type: "text",
                text: answer || `peer_empty: ${target_role} returned no answer`,
              },
            ],
          };
        },
      ),
    ],
  });
