import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);

export class GoHelperError extends Error {
  constructor(
    public subcommand: string,
    public exitCode: number | null,
    public stderr: string,
    public binPath: string,
    public spawnError?: NodeJS.ErrnoException,
  ) {
    const detail = spawnError
      ? `spawn error: ${spawnError.code ?? spawnError.message} (bin=${binPath})`
      : stderr || `no stderr (bin=${binPath})`;
    super(
      `code-review-helper ${subcommand} failed (exit ${exitCode}): ${detail}`,
    );
    this.name = "GoHelperError";
  }
}

const helperBin = (): string => {
  if (process.env.CLAUDE_PLUGIN_ROOT) {
    return path.join(
      process.env.CLAUDE_PLUGIN_ROOT,
      "bin",
      "code-review-helper",
    );
  }
  // Bundled by tsup into dist/cli.js, so __filename resolves there at runtime.
  return path.resolve(__filename, "../../bin/code-review-helper");
};

const run = (
  subcommand: string,
  args: string[],
  input?: string,
): { stdout: string; stderr: string } => {
  const stdio: ("inherit" | "pipe")[] =
    input !== undefined
      ? ["pipe", "pipe", "pipe"]
      : ["inherit", "pipe", "pipe"];
  const bin = helperBin();
  const r = spawnSync(bin, [subcommand, ...args], {
    stdio,
    input,
    encoding: "utf8",
  });
  if (r.error || r.status !== 0) {
    throw new GoHelperError(
      subcommand,
      r.status,
      r.stderr ?? "",
      bin,
      r.error as NodeJS.ErrnoException | undefined,
    );
  }
  return { stdout: r.stdout ?? "", stderr: r.stderr ?? "" };
};

export type DiffArgs = {
  in: string;
  outChangedFiles: string;
  outValidLines: string;
};

export const runDiff = (args: DiffArgs): void => {
  run("diff", [
    "--in",
    args.in,
    "--out-changed-files",
    args.outChangedFiles,
    "--out-valid-lines",
    args.outValidLines,
  ]);
};

export type BundleContextArgs = {
  reviewTmpdir: string;
  headSha: string;
  prNumber: number;
  owner: string;
  repo: string;
  repoRoot: string;
  rubric: string;
  rubricOut?: string;
  summaryParagraph: string;
  out?: string;
  maxSourceBytes?: number;
  maxTotalSourceBytes?: number;
  gitWorkdir?: string;
};

export const runBundleContext = (args: BundleContextArgs): void => {
  const argv = [
    "--review-tmpdir",
    args.reviewTmpdir,
    "--head-sha",
    args.headSha,
    "--pr-number",
    String(args.prNumber),
    "--owner",
    args.owner,
    "--repo",
    args.repo,
    "--repo-root",
    args.repoRoot,
    "--rubric",
    args.rubric,
    "--summary-paragraph",
    "-",
  ];
  if (args.rubricOut) argv.push("--rubric-out", args.rubricOut);
  if (args.out) argv.push("--out", args.out);
  if (args.maxSourceBytes !== undefined)
    argv.push("--max-source-bytes", String(args.maxSourceBytes));
  if (args.maxTotalSourceBytes !== undefined)
    argv.push("--max-total-source-bytes", String(args.maxTotalSourceBytes));
  if (args.gitWorkdir) argv.push("--git-workdir", args.gitWorkdir);
  run("bundle-context", argv, args.summaryParagraph);
};

export type FinalizeArgs = {
  diff: string;
  findingsDir: string;
  priorIssues: string;
  headSha: string;
  owner: string;
  repo: string;
  prNumber: number;
  expectedRoles: string;
  outConsolidated: string;
  outPayload: string;
  outPendingPayload: string;
  outBody: string;
  outFallback: string;
  includeFindingIds?: string;
  excludeFindingIds?: string;
};

export const runFinalize = (args: FinalizeArgs): void => {
  const argv = [
    "--diff",
    args.diff,
    "--findings-dir",
    args.findingsDir,
    "--prior-issues",
    args.priorIssues,
    "--head-sha",
    args.headSha,
    "--owner",
    args.owner,
    "--repo",
    args.repo,
    "--pr-number",
    String(args.prNumber),
    "--expected-roles",
    args.expectedRoles,
    "--out-consolidated",
    args.outConsolidated,
    "--out-payload",
    args.outPayload,
    "--out-pending-payload",
    args.outPendingPayload,
    "--out-body",
    args.outBody,
    "--out-fallback",
    args.outFallback,
  ];
  if (args.includeFindingIds)
    argv.push("--include-finding-ids", args.includeFindingIds);
  if (args.excludeFindingIds)
    argv.push("--exclude-finding-ids", args.excludeFindingIds);
  run("finalize", argv);
};
