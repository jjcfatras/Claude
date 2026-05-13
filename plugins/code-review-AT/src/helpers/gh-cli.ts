import { spawnSync } from "node:child_process";
import { writeFileSync } from "node:fs";
import type { PrMeta, PriorIssue, PriorIssues } from "../types.js";

export class GhCliError extends Error {
  constructor(
    public command: string,
    public exitCode: number | null,
    public stderr: string,
  ) {
    super(`gh ${command} failed (exit ${exitCode}): ${stderr}`);
    this.name = "GhCliError";
  }
}

const gh = (
  argv: string[],
  opts: { capture?: boolean } = {},
): { stdout: string; stderr: string; status: number | null } => {
  const r = spawnSync("gh", argv, {
    stdio: opts.capture
      ? ["ignore", "pipe", "pipe"]
      : ["ignore", "pipe", "pipe"],
    encoding: "utf8",
  });
  return {
    stdout: r.stdout ?? "",
    stderr: r.stderr ?? "",
    status: r.status,
  };
};

const ghJson = <T>(argv: string[]): T => {
  const r = gh(argv, { capture: true });
  if (r.status !== 0) {
    throw new GhCliError(argv.join(" "), r.status, r.stderr);
  }
  return JSON.parse(r.stdout) as T;
};

type PrViewResponse = {
  headRefOid: string;
  headRepository: { name: string };
  headRepositoryOwner: { login: string };
  url: string;
  number: number;
  title: string;
  headRefName: string;
};

const parseBaseFromUrl = (
  url: string,
): { baseOwner: string; baseRepo: string } => {
  const m = url.match(/github\.com\/([^/]+)\/([^/]+)\/pull\/\d+/);
  if (!m)
    throw new Error(`gh: cannot parse base owner/repo from PR url ${url}`);
  return { baseOwner: m[1]!, baseRepo: m[2]! };
};

export const getPrMeta = (prNumber: number): PrMeta => {
  const data = ghJson<PrViewResponse>([
    "pr",
    "view",
    String(prNumber),
    "--json",
    "headRefOid,headRepository,headRepositoryOwner,url,number,title,headRefName",
  ]);
  const { baseOwner, baseRepo } = parseBaseFromUrl(data.url);
  return {
    sha: data.headRefOid,
    url: data.url,
    number: data.number,
    title: data.title,
    headRefName: data.headRefName,
    owner: data.headRepositoryOwner.login,
    repo: data.headRepository.name,
    baseOwner,
    baseRepo,
  };
};

export const getPrDiff = (prNumber: number, outPath: string): void => {
  const r = gh(["pr", "diff", String(prNumber)], { capture: true });
  if (r.status !== 0) {
    throw new GhCliError(`pr diff ${prNumber}`, r.status, r.stderr);
  }
  writeFileSync(outPath, r.stdout);
};

type GhReview = {
  id: number;
  user: { login: string } | null;
  body: string;
  submitted_at: string;
};

type GhReviewComment = {
  id: number;
  path: string;
  line: number | null;
  body: string;
};

const CLAUDE_REVIEWER_MARKERS = [
  "Claude Code",
  "claude-code",
  "Generated with Claude",
];

const isClaudeReview = (r: GhReview): boolean =>
  CLAUDE_REVIEWER_MARKERS.some((m) => r.body?.includes(m));

export const getPriorIssues = (
  baseOwner: string,
  baseRepo: string,
  prNumber: number,
): PriorIssues => {
  const reviews = ghJson<GhReview[]>([
    "api",
    "--paginate",
    `repos/${baseOwner}/${baseRepo}/pulls/${prNumber}/reviews`,
  ]);
  const claudeReviews = reviews.filter(isClaudeReview);
  if (claudeReviews.length === 0) return { comments: [] };
  const latest = claudeReviews[claudeReviews.length - 1]!;
  const comments = ghJson<GhReviewComment[]>([
    "api",
    "--paginate",
    `repos/${baseOwner}/${baseRepo}/pulls/${prNumber}/reviews/${latest.id}/comments`,
  ]);
  return {
    review_id: latest.id,
    submitted_at: latest.submitted_at,
    comments: comments.map((c) => {
      const issue: PriorIssue = { id: c.id, path: c.path, body: c.body };
      if (c.line !== null) issue.line = c.line;
      return issue;
    }),
  };
};
