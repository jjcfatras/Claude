export const ROLES = [
  "security",
  "typescript",
  "react",
  "infra",
  "errors",
  "perf",
  "quality",
  "claude-md",
] as const;

export type Role = (typeof ROLES)[number];

export const ALWAYS_ON_ROLES: readonly Role[] = [
  "security",
  "quality",
  "errors",
  "perf",
] as const;

export const SEVERITIES = ["Critical", "Medium", "Minor"] as const;
export type Severity = (typeof SEVERITIES)[number];

export const SCAN_STATUSES = ["complete", "timed_out", "errored"] as const;
export type ScanStatus = (typeof SCAN_STATUSES)[number];

export type PrMeta = {
  sha: string;
  url: string;
  number: number;
  title: string;
  headRefName: string;
  owner: string;
  repo: string;
  baseOwner: string;
  baseRepo: string;
};

export type PriorIssue = {
  id?: number;
  path?: string;
  line?: number;
  body?: string;
};

export type PriorIssues = {
  review_id?: number;
  submitted_at?: string;
  comments: PriorIssue[];
};
