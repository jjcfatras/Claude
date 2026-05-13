import { z } from "zod";
import { SEVERITIES, SCAN_STATUSES, ROLES } from "../types.js";

export const VerificationSchema = z.object({
  asked: z.string(),
  verdict: z.enum([
    "confirmed",
    "false_positive",
    "out_of_scope",
    "peer_timeout",
    "peer_unavailable",
  ]),
  note: z.string(),
  applied_adjustment: z.number().int(),
});

export const FindingSchema = z.object({
  id: z.string(),
  category: z.string(),
  file: z.string(),
  line: z.number().int().nonnegative(),
  startLine: z.number().int().nonnegative().nullable().optional(),
  confidence: z.number().int().min(0).max(100),
  severity: z.enum(SEVERITIES),
  rationale: z.string(),
  explanation: z.string(),
  code: z.string(),
  suggested_fix: z.string().nullable().optional(),
  language: z.string(),
  verifications: z.array(VerificationSchema).default([]),
});

export type Finding = z.infer<typeof FindingSchema>;

export const FindingsOutputSchema = z.object({
  specialist: z.enum(ROLES),
  scan_status: z.enum(SCAN_STATUSES),
  findings: z.array(FindingSchema),
});

export type FindingsOutput = z.infer<typeof FindingsOutputSchema>;

export const findingsJsonSchema = z.toJSONSchema(FindingsOutputSchema);
