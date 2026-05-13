import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
  existsSync,
} from "node:fs";
import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { tmpdir as osTmpdir } from "node:os";
import { query } from "@anthropic-ai/claude-agent-sdk";
import { agentsByRole, prSummary } from "./agents/index.js";
import { buildPeerMcpServer } from "./tools/verify-with-peer.js";
import { buildRoster } from "./roster.js";
import { makeCtx, type ReviewContext } from "./context.js";
import {
  FindingsOutputSchema,
  findingsJsonSchema,
  type FindingsOutput,
} from "./schemas/finding.js";
import { ROLES, type Role } from "./types.js";
import * as gh from "./helpers/gh-cli.js";
import * as goHelper from "./helpers/go-helper.js";

const __filename = fileURLToPath(import.meta.url);

const SCAN_BUDGET_FLOOR_S = 240;
const SCAN_BUDGET_PER_FILE_OVER_50_S = 2;
const SCAN_BUDGET_CAP_S = 540;
const SAFETY_SLACK_MS = 30_000;

const computeScanBudgetSeconds = (changedFileCount: number): number => {
  const above50 = Math.max(0, changedFileCount - 50);
  return Math.min(
    SCAN_BUDGET_FLOOR_S + SCAN_BUDGET_PER_FILE_OVER_50_S * above50,
    SCAN_BUDGET_CAP_S,
  );
};

const emit = (line: string): void => {
  process.stdout.write(line.endsWith("\n") ? line : `${line}\n`);
};

const readJson = <T>(p: string): T => JSON.parse(readFileSync(p, "utf8")) as T;
const writeJson = (p: string, v: unknown): void => {
  writeFileSync(p, JSON.stringify(v, null, 2));
};

const createTmpdir = (): string => {
  const candidates = [
    path.join(osTmpdir(), "pr-review-"),
    path.join(process.env.HOME ?? "", ".claude/tmp/pr-review-"),
  ];
  for (const prefix of candidates) {
    const dir = path.dirname(prefix);
    try {
      mkdirSync(dir, { recursive: true });
      const tmp = mkdtempSync(prefix);
      writeFileSync(path.join(tmp, ".writable"), "ok");
      return tmp;
    } catch {
      continue;
    }
  }
  throw new Error(
    "Cannot create a writable temp dir under /tmp or ~/.claude/tmp",
  );
};

const repoRoot = (): string => {
  const r = spawnSync("git", ["rev-parse", "--show-toplevel"], {
    encoding: "utf8",
  });
  if (r.status !== 0)
    throw new Error(`git rev-parse failed: ${r.stderr.trim()}`);
  return r.stdout.trim();
};

const walkClaudeMd = (
  changedFiles: string[],
  repoRootPath: string,
): string[] => {
  const result = new Set<string>();
  for (const f of changedFiles) {
    let dir = path.dirname(f);
    while (dir && dir !== "." && dir !== "/") {
      const claudeMd = path.join(repoRootPath, dir, "CLAUDE.md");
      if (existsSync(claudeMd))
        result.add(path.relative(repoRootPath, claudeMd));
      const parent = path.dirname(dir);
      if (parent === dir) break;
      dir = parent;
    }
  }
  const rootClaudeMd = path.join(repoRootPath, "CLAUDE.md");
  if (existsSync(rootClaudeMd)) result.add("CLAUDE.md");
  return [...result];
};

const rubricPath = (): string => {
  if (process.env.CLAUDE_PLUGIN_ROOT) {
    return path.join(
      process.env.CLAUDE_PLUGIN_ROOT,
      "references",
      "code-review-rubrics.md",
    );
  }
  // Bundled by tsup into dist/cli.js, so __filename resolves there at runtime.
  return path.resolve(__filename, "../references/code-review-rubrics.md");
};

const summarizeSdkFailure = (
  role: string,
  diagnostics: string[],
  lastResult: unknown,
): string => {
  const parts: string[] = [];
  if (diagnostics.length) parts.push(diagnostics.join(" | "));
  if (lastResult && typeof lastResult === "object") {
    const r = lastResult as { subtype?: string; result?: string };
    if (r.subtype && r.subtype !== "success")
      parts.push(`result.subtype=${r.subtype}`);
    if (r.result) parts.push(`result=${String(r.result).slice(0, 500)}`);
  }
  return parts.length
    ? parts.join(" | ")
    : `no diagnostic output from SDK (${role})`;
};

const runPrSummary = async (ctx: ReviewContext): Promise<string> => {
  const diffPath = path.join(ctx.tmpdir, `pr-${ctx.prNumber}.diff`);
  const userPrompt = `Read the PR diff at ${diffPath} and return a single-paragraph technical summary of PR #${ctx.prNumber} in ${ctx.baseOwner}/${ctx.baseRepo}.`;

  let summary = "";
  let lastResult: unknown = undefined;
  const diagnostics: string[] = [];
  const ac = new AbortController();
  const timer = setTimeout(() => ac.abort(), 90_000);
  try {
    for await (const msg of query({
      prompt: userPrompt,
      options: {
        agent: "pr-summary",
        agents: { "pr-summary": prSummary },
        abortController: ac,
        permissionMode: "bypassPermissions",
        allowDangerouslySkipPermissions: true,
        settingSources: [],
      },
    })) {
      if (msg.type === "result") {
        lastResult = msg;
        if (msg.subtype === "success" && msg.result) summary = msg.result;
      } else if ((msg as { type: string }).type === "error") {
        const m = msg as { message?: string; error?: { message?: string } };
        diagnostics.push(
          `sdk-error: ${m.message ?? m.error?.message ?? JSON.stringify(msg).slice(0, 200)}`,
        );
      }
    }
  } catch (e) {
    const detail = summarizeSdkFailure("pr-summary", diagnostics, lastResult);
    throw new Error(
      `pr-summary failed: ${(e as Error).message}${detail ? ` — ${detail}` : ""}`,
    );
  } finally {
    clearTimeout(timer);
  }
  if (!summary) {
    const detail = summarizeSdkFailure("pr-summary", diagnostics, lastResult);
    throw new Error(`pr-summary returned no summary — ${detail}`);
  }
  return summary;
};

const buildSpawnUserPrompt = (
  role: Role,
  ctx: ReviewContext,
): string => `Review PR #${ctx.prNumber} (${ctx.baseOwner}/${ctx.baseRepo}) as the ${role} specialist.

Spawn-context bundle: ${ctx.tmpdir}/spawn-context.md
Rubric: ${ctx.tmpdir}/rubric.md
Diff: ${ctx.tmpdir}/pr-${ctx.prNumber}.diff

HEAD_SHA: ${ctx.sha}
REPO_ROOT: ${ctx.repoRoot}

Read the bundle and rubric once at startup, then scan the diff for your domain. Emit findings as structured JSON matching the schema; the orchestrator collects them. Do not write any files.`;

const runSpecialist = async (
  role: Role,
  ctx: ReviewContext,
  roster: readonly Role[],
  scanBudgetMs: number,
): Promise<FindingsOutput> => {
  const ac = new AbortController();
  const timer = setTimeout(() => ac.abort(), scanBudgetMs + SAFETY_SLACK_MS);

  const peerServer = buildPeerMcpServer(role, roster);
  const agentDef = agentsByRole[role];
  const userPrompt = buildSpawnUserPrompt(role, ctx);

  let findings: FindingsOutput = {
    specialist: role,
    scan_status: "errored",
    findings: [],
  };

  const diagnostics: string[] = [];
  let lastResult: unknown = undefined;
  try {
    for await (const msg of query({
      prompt: userPrompt,
      options: {
        agent: role,
        agents: { [role]: agentDef },
        abortController: ac,
        mcpServers: { "peer-verification": peerServer },
        outputFormat: { type: "json_schema", schema: findingsJsonSchema },
        permissionMode: "bypassPermissions",
        allowDangerouslySkipPermissions: true,
        settingSources: ["project"],
      },
    })) {
      if (msg.type === "result") {
        lastResult = msg;
        if (msg.subtype === "success") {
          if (msg.structured_output) {
            const parsed = FindingsOutputSchema.safeParse(
              msg.structured_output,
            );
            if (parsed.success) {
              findings = parsed.data;
            } else {
              findings = {
                specialist: role,
                scan_status: "errored",
                findings: [],
              };
              process.stderr.write(
                `  ! ${role} structured output failed schema: ${parsed.error.message}\n`,
              );
            }
          }
        }
      } else if ((msg as { type: string }).type === "error") {
        const m = msg as { message?: string; error?: { message?: string } };
        diagnostics.push(
          `sdk-error: ${m.message ?? m.error?.message ?? JSON.stringify(msg).slice(0, 200)}`,
        );
      }
    }
  } catch (e) {
    if (ac.signal.aborted) {
      findings = { specialist: role, scan_status: "timed_out", findings: [] };
    } else {
      findings = { specialist: role, scan_status: "errored", findings: [] };
      const detail = summarizeSdkFailure(role, diagnostics, lastResult);
      process.stderr.write(
        `  ! ${role} threw: ${(e as Error).message}${detail ? ` — ${detail}` : ""}\n`,
      );
    }
  } finally {
    clearTimeout(timer);
  }
  if (
    findings.scan_status === "errored" &&
    diagnostics.length &&
    !ac.signal.aborted
  ) {
    process.stderr.write(
      `  ! ${role} sdk diagnostics: ${diagnostics.join(" | ")}\n`,
    );
  }

  const findingsDir = path.join(ctx.tmpdir, "findings");
  mkdirSync(findingsDir, { recursive: true });
  writeJson(path.join(findingsDir, `${role}.json`), findings);
  emit(
    `  ✓ ${role} (${findings.findings.length} findings, ${findings.scan_status})`,
  );
  return findings;
};

const printFindings = (consolidatedPath: string): void => {
  const consolidated = readJson<{
    inline_eligible: {
      id: string;
      file: string;
      line: number;
      severity: string;
      rationale: string;
    }[];
    summary_only: {
      id: string;
      file: string;
      line: number;
      severity: string;
      rationale: string;
    }[];
    dropped_prior_review: { id: string }[];
    specialists_used: string[];
    timed_out_roles: string[];
    missing_roles: string[];
    unreadable_roles: string[];
    invalid_findings: { role: string; id: string; reason: string }[];
  }>(consolidatedPath);

  emit("");
  emit("=== Review summary ===");
  emit(`  Specialists: ${consolidated.specialists_used.join(", ")}`);
  if (consolidated.timed_out_roles.length)
    emit(`  Timed out: ${consolidated.timed_out_roles.join(", ")}`);
  if (consolidated.missing_roles.length)
    emit(`  Missing: ${consolidated.missing_roles.join(", ")}`);
  if (consolidated.unreadable_roles.length)
    emit(`  Unreadable: ${consolidated.unreadable_roles.join(", ")}`);
  if (consolidated.invalid_findings.length)
    emit(`  Invalid findings: ${consolidated.invalid_findings.length}`);
  if (consolidated.dropped_prior_review.length)
    emit(
      `  Dropped (prior review): ${consolidated.dropped_prior_review.length}`,
    );
  emit(`  Inline eligible: ${consolidated.inline_eligible.length}`);
  emit(`  Summary only: ${consolidated.summary_only.length}`);
  emit("");
  for (const f of consolidated.inline_eligible) {
    emit(`  [${f.id}] ${f.severity} ${f.file}:${f.line} — ${f.rationale}`);
  }
  for (const f of consolidated.summary_only) {
    emit(
      `  [${f.id}] ${f.severity} (summary) ${f.file}:${f.line} — ${f.rationale}`,
    );
  }
  emit("");
};

const promptUser = async (
  consolidatedPath: string,
): Promise<{ post: boolean; subset?: { include: string } }> => {
  printFindings(consolidatedPath);
  if (!process.stdin.isTTY) {
    emit("(stdin is not a TTY; defaulting to post-all)");
    return { post: true };
  }
  const readline = await import("node:readline/promises");
  const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout,
  });
  const answer = (
    await rl.question("Post review? [Y]es/[n]o/[i]ds <csv>: ")
  ).trim();
  rl.close();
  if (answer === "" || /^y(es)?$/i.test(answer)) return { post: true };
  if (/^n(o)?$/i.test(answer)) return { post: false };
  if (/^i(ds)?\s+/i.test(answer)) {
    const ids = answer.replace(/^i(ds)?\s+/i, "");
    return { post: true, subset: { include: ids } };
  }
  emit(`(unrecognized answer "${answer}"; treating as no)`);
  return { post: false };
};

export const runReview = async (prNumber: number): Promise<void> => {
  if (!Number.isInteger(prNumber) || prNumber <= 0) {
    throw new Error(`Invalid PR number: ${prNumber}`);
  }
  const tmp = createTmpdir();
  const root = repoRoot();

  try {
    emit(`[1/6] Fetching PR #${prNumber}…`);
    const prMeta = gh.getPrMeta(prNumber);
    gh.getPrDiff(prNumber, path.join(tmp, `pr-${prNumber}.diff`));
    const ctx: ReviewContext = makeCtx(tmp, prNumber, prMeta, root);

    const priorIssues = gh.getPriorIssues(
      ctx.baseOwner,
      ctx.baseRepo,
      prNumber,
    );
    writeJson(path.join(tmp, "prior-issues.json"), priorIssues);

    emit(`[1/6] Parsing diff via code-review-helper…`);
    goHelper.runDiff({
      in: path.join(tmp, `pr-${prNumber}.diff`),
      outChangedFiles: path.join(tmp, "changed-files.json"),
      outValidLines: path.join(tmp, "valid-lines.json"),
    });
    const changedFiles = readJson<string[]>(
      path.join(tmp, "changed-files.json"),
    );

    const claudeMdFiles = walkClaudeMd(changedFiles, root);
    writeJson(path.join(tmp, "claude-md-files.json"), claudeMdFiles);

    emit(`[1/6] Generating PR summary…`);
    const summary = await runPrSummary(ctx);

    const roster = buildRoster(changedFiles, claudeMdFiles);
    writeJson(path.join(tmp, "roster.json"), roster);

    emit(`[2/6] Building spawn-context bundle…`);
    const rubric = rubricPath();
    goHelper.runBundleContext({
      reviewTmpdir: tmp,
      headSha: ctx.sha,
      prNumber,
      owner: ctx.baseOwner,
      repo: ctx.baseRepo,
      repoRoot: root,
      rubric,
      rubricOut: path.join(tmp, "rubric.md"),
      summaryParagraph: summary,
    });

    const scanBudgetMs = computeScanBudgetSeconds(changedFiles.length) * 1000;
    emit(
      `[2/6] Running ${roster.length} specialists in parallel (budget ${scanBudgetMs / 1000}s)…`,
    );
    await Promise.all(
      roster.map((role) => runSpecialist(role, ctx, roster, scanBudgetMs)),
    );

    emit(`[3/6] Consolidating findings…`);
    const finalizeArgs = {
      diff: path.join(tmp, `pr-${prNumber}.diff`),
      findingsDir: path.join(tmp, "findings"),
      priorIssues: path.join(tmp, "prior-issues.json"),
      headSha: ctx.sha,
      owner: ctx.baseOwner,
      repo: ctx.baseRepo,
      prNumber,
      expectedRoles: roster.join(","),
      outConsolidated: path.join(tmp, "consolidated.json"),
      outPayload: path.join(tmp, "payload.json"),
      outPendingPayload: path.join(tmp, "payload-pending.json"),
      outBody: path.join(tmp, "payload-body.json"),
      outFallback: path.join(tmp, "fallback.md"),
    };
    goHelper.runFinalize(finalizeArgs);

    emit(`[4/6] Awaiting user confirmation…`);
    const choice = await promptUser(path.join(tmp, "consolidated.json"));

    if (choice.subset) {
      goHelper.runFinalize({
        ...finalizeArgs,
        includeFindingIds: choice.subset.include,
      });
    }

    if (choice.post) {
      emit(`[5/6] Posting review…`);
      const { postReview } = await import("./helpers/post-review.js");
      const result = postReview(ctx);
      emit(`  posted via tier ${result.tier}`);
    } else {
      emit(`[5/6] Skipped posting per user.`);
    }
  } finally {
    emit(`[6/6] Cleaning up ${tmp}`);
    if (tmp.includes("pr-review-")) {
      rmSync(tmp, { recursive: true, force: true });
    }
  }
};

export { ROLES };
