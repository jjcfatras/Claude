import type { PrMeta } from "./types.js";

export type ReviewContext = {
  tmpdir: string;
  prNumber: number;
  sha: string;
  url: string;
  owner: string;
  repo: string;
  baseOwner: string;
  baseRepo: string;
  headRefName: string;
  title: string;
  repoRoot: string;
};

export const makeCtx = (
  tmpdir: string,
  prNumber: number,
  prMeta: PrMeta,
  repoRoot: string,
): ReviewContext => ({
  tmpdir,
  prNumber,
  sha: prMeta.sha,
  url: prMeta.url,
  owner: prMeta.owner,
  repo: prMeta.repo,
  baseOwner: prMeta.baseOwner,
  baseRepo: prMeta.baseRepo,
  headRefName: prMeta.headRefName,
  title: prMeta.title,
  repoRoot,
});
