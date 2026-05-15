import { runReview } from "./orchestrator.js";

const usage = (): never => {
  process.stderr.write(
    "usage: code-review <pr-number>\n  pr-number: positive integer\n",
  );
  process.exit(2);
};

const REQUIRED_ENV = [
  "ANTHROPIC_API_KEY",
  "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS",
] as const;

const preflight = (): void => {
  const missing = REQUIRED_ENV.filter((k) => !process.env[k]);
  if (missing.length > 0) {
    process.stderr.write(
      `code-review-AT: missing required environment variable(s): ${missing.join(", ")}\n`,
    );
    process.exit(2);
  }
};

const main = async (): Promise<void> => {
  preflight();
  const [arg] = process.argv.slice(2);
  if (!arg || arg === "-h" || arg === "--help") usage();
  const prNumber = Number.parseInt(arg!, 10);
  if (!Number.isInteger(prNumber) || prNumber <= 0) {
    process.stderr.write(`invalid pr-number: ${arg}\n`);
    process.exit(2);
  }
  try {
    await runReview(prNumber);
  } catch (e) {
    process.stderr.write(`code-review failed: ${(e as Error).message}\n`);
    process.exit(1);
  }
};

void main();
