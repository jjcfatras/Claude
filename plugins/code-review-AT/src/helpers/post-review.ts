import { spawnSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import type { ReviewContext } from "../context.js";

export class PostError extends Error {
  constructor(
    message: string,
    public tier: 1 | 2 | 3,
  ) {
    super(message);
    this.name = "PostError";
  }
}

type GhResult = {
  status: number | null;
  stdout: string;
  stderr: string;
};

const gh = (argv: string[]): GhResult => {
  const r = spawnSync("gh", argv, {
    stdio: ["ignore", "pipe", "pipe"],
    encoding: "utf8",
  });
  return {
    status: r.status,
    stdout: r.stdout ?? "",
    stderr: r.stderr ?? "",
  };
};

const extractHttpStatus = (stderr: string): number | null => {
  const m = stderr.match(/HTTP\s+(\d{3})/i);
  return m ? Number(m[1]) : null;
};

const tier1 = (ctx: ReviewContext, payloadPath: string): GhResult =>
  gh([
    "api",
    `repos/${ctx.baseOwner}/${ctx.baseRepo}/pulls/${ctx.prNumber}/reviews`,
    "--method",
    "POST",
    "--input",
    payloadPath,
  ]);

const tier2CreatePending = (
  ctx: ReviewContext,
  pendingPayloadPath: string,
): GhResult =>
  gh([
    "api",
    `repos/${ctx.baseOwner}/${ctx.baseRepo}/pulls/${ctx.prNumber}/reviews`,
    "--method",
    "POST",
    "--input",
    pendingPayloadPath,
    "--jq",
    ".id",
  ]);

const tier2Submit = (
  ctx: ReviewContext,
  reviewId: string,
  bodyPayloadPath: string,
): GhResult =>
  gh([
    "api",
    `repos/${ctx.baseOwner}/${ctx.baseRepo}/pulls/${ctx.prNumber}/reviews/${reviewId}/events`,
    "--method",
    "POST",
    "--input",
    bodyPayloadPath,
    "-f",
    "event=COMMENT",
  ]);

const tier3Fallback = (ctx: ReviewContext, fallbackPath: string): GhResult =>
  gh(["pr", "comment", String(ctx.prNumber), "-F", fallbackPath]);

export const postReview = (ctx: ReviewContext): { tier: 1 | 2 | 3 } => {
  const payload = path.join(ctx.tmpdir, "payload.json");
  const pendingPayload = path.join(ctx.tmpdir, "payload-pending.json");
  const bodyPayload = path.join(ctx.tmpdir, "payload-body.json");
  const fallback = path.join(ctx.tmpdir, "fallback.md");

  process.stdout.write("Posting review (tier 1 → batched)…\n");
  const r1 = tier1(ctx, payload);
  if (r1.status === 0) return { tier: 1 };
  const httpStatus = extractHttpStatus(r1.stderr);
  process.stdout.write(`  tier 1 failed: ${r1.stderr.trim()}\n`);

  if (httpStatus !== 422) {
    return finalFallback(ctx, fallback, `tier 1 returned ${r1.stderr.trim()}`);
  }

  process.stdout.write("Posting review (tier 2 → pending + submit)…\n");
  const r2create = tier2CreatePending(ctx, pendingPayload);
  if (r2create.status !== 0) {
    process.stdout.write(`  tier 2 create failed: ${r2create.stderr.trim()}\n`);
    return finalFallback(
      ctx,
      fallback,
      `tier 2 create returned ${r2create.stderr.trim()}`,
    );
  }
  const reviewId = r2create.stdout.trim();
  const r2submit = tier2Submit(ctx, reviewId, bodyPayload);
  if (r2submit.status === 0) return { tier: 2 };

  process.stderr.write(
    `WARNING: pending review ${reviewId} is dangling. Delete with:\n` +
      `  gh api repos/${ctx.baseOwner}/${ctx.baseRepo}/pulls/${ctx.prNumber}/reviews/${reviewId} --method DELETE\n`,
  );
  return finalFallback(
    ctx,
    fallback,
    `tier 2 submit returned ${r2submit.stderr.trim()}`,
  );
};

const finalFallback = (
  ctx: ReviewContext,
  fallbackPath: string,
  apiError: string,
): { tier: 3 } => {
  process.stdout.write("Posting review (tier 3 → fallback issue comment)…\n");
  const body = readFileSync(fallbackPath, "utf8").replace(
    "{API_ERROR}",
    apiError,
  );
  const patchedPath = `${fallbackPath}.patched`;
  writeFileSync(patchedPath, body);
  const r = tier3Fallback(ctx, patchedPath);
  if (r.status !== 0) {
    throw new PostError(
      `All three posting tiers failed. Last error: ${r.stderr.trim()}`,
      3,
    );
  }
  return { tier: 3 };
};
