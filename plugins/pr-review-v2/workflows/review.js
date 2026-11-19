export const meta = {
  name: "review",
  description:
    "Multi-specialist PR review: parallel specialists, one skeptic per finding, dedup and consolidation, then the rendered GitHub review",
  whenToUse:
    "Invoked by /pr-review-v2 step [2/5]. Requires args {pr, owner, repo, headSha, repoRoot, tmp, rubricPath, files, claudeMd, rules, priorIssues}: the ARGS line that skills/pr-review-v2/scripts/prepare.sh prints.",
  phases: [
    { title: "Review", detail: "One specialist per roster role" },
    {
      title: "Verify",
      detail: "One skeptic per finding that passes the gate",
      model: "sonnet",
    },
    {
      title: "Consolidate",
      detail:
        "Group one defect reported at several anchors; flag opposite advice",
      model: "opus",
    },
  ],
};

// Port of pr-review's Go code-review-helper (roster, finalize) to a Workflow script. The I/O half (gh, git,
// posting) stays in the skill's scripts/, because a workflow script has no filesystem or shell access.

// ─── Vocabulary and thresholds (mirrored in skills/pr-review-v2/references/rubric.md) ───

const CATEGORIES = [
  "accessibility",
  "api-contract",
  "concurrency",
  "config",
  "convention-adherence",
  "correctness",
  "data-integrity",
  "design",
  "duplication",
  "error-handling",
  "observability",
  "performance",
  "security",
  "testing",
  "type-safety",
];
// Category pairs that count as two lenses on one defect (positional dedup's same-defect signal).
const RELATED = new Set(
  [
    ["security", "config"],
    ["security", "api-contract"],
    ["error-handling", "observability"],
    ["error-handling", "correctness"],
    ["error-handling", "concurrency"],
    ["correctness", "concurrency"],
    ["correctness", "type-safety"],
    ["correctness", "data-integrity"],
    ["type-safety", "api-contract"],
    ["performance", "concurrency"],
    ["performance", "data-integrity"],
    ["duplication", "design"],
    ["duplication", "convention-adherence"],
    ["design", "convention-adherence"],
    ["config", "convention-adherence"],
    ["config", "observability"],
    ["config", "data-integrity"],
  ].flatMap(([x, y]) => [`${x}|${y}`, `${y}|${x}`]),
);
const SEVERITY_RANK = { Critical: 3, Medium: 2, Minor: 1 };
const EMOJI = { Critical: "🔴", Medium: "🟡", Minor: "📝" };
const POSITIONAL_WINDOW = 3;
const SAME_DEFECT_OVERLAP = 60;
const PRIOR_LINE_WINDOW = 5;
const PRIOR_SNIPPET_OVERLAP = 40;
const PRIOR_DESCRIPTION_OVERLAP = 60;
const SNAP_WINDOW = 5;
const SNIPPET_BYTES = 256;
const PRIOR_KEPT_NOTE =
  "\n\n_Note: This issue was flagged in a prior review but the code has since changed._";
const FOOTER = `🤖 Generated with [Claude Code](https://claude.ai/code)

<sub>If this code review was useful, please react with 👍. Otherwise, react with 👎.</sub>

<sub>Diff-scoped automated review — does not run tests or builds, verify test correctness, or examine code outside this PR's diff. Pair with CI and human review.</sub>`;

// ─── Roster ───

const ALWAYS_ON = ["security", "quality", "errors", "perf"];
// Longest-running roles first, so they start first when the concurrency cap is below the roster size.
const SPAWN_RANK = [
  "infra",
  "security",
  "typescript",
  "quality",
  "errors",
  "react",
  "perf",
  "claude-md",
];
const PAT_TS =
  /(\.(ts|tsx|cts|mts)$|(^|\/)tsconfig[^/]*\.json$|(^|\/)jsconfig\.json$)/i;
const PAT_REACT = /\.(tsx|jsx)$/i;
const PAT_INFRA =
  /(\.sql$|(^|\/)migrations\/|(^|\/)db\/migrations\/|\.tf$|\.hcl$|(^|\/)terraform\/|(^|\/)Dockerfile|(^|\/)docker-compose|(^|\/)compose\.ya?ml$|(^|\/)k8s\/|(^|\/)kubernetes\/|(^|\/)helm\/|(^|\/)deploy\/|(^|\/)infra(structure)?\/|(^|\/)\.github\/workflows\/|(^|\/)Jenkinsfile|(^|\/)\.circleci\/|(^|\/)\.buildkite\/)/i;
// Build/bundler configs that affect both the TS build and the React surface.
const PAT_FRAMEWORK_CONFIG =
  /((^|\/)(next|vite|nuxt|webpack|rollup|esbuild|babel)\.config\.(js|mjs|cjs|ts|mts|cts)$|(^|\/)babel\.config\.json$|(^|\/)\.babelrc$)/i;

const buildRoster = (paths, guidanceCount) => {
  const any = (re) => paths.some((p) => re.test(p));
  const framework = any(PAT_FRAMEWORK_CONFIG);
  return [
    ...ALWAYS_ON,
    ...(any(PAT_TS) || framework ? ["typescript"] : []),
    ...(any(PAT_REACT) || framework ? ["react"] : []),
    ...(any(PAT_INFRA) ? ["infra"] : []),
    ...(guidanceCount > 0 ? ["claude-md"] : []),
  ];
};

// `.claude/rules/*.md` `paths:` frontmatter, in every form Claude Code accepts: block list, flow list, or
// comma string. Commas inside `{a,b}` stay put.
const splitOutsideBraces = (s) => {
  const out = [];
  let depth = 0;
  let start = 0;
  for (let i = 0; i < s.length; i++) {
    if (s[i] === "{") depth++;
    else if (s[i] === "}") depth--;
    else if (s[i] === "," && depth === 0) {
      out.push(s.slice(start, i));
      start = i + 1;
    }
  }
  out.push(s.slice(start));
  return out;
};
const rulePaths = (frontmatter) => {
  const lines = String(frontmatter).replace(/\r\n/g, "\n").split("\n");
  for (let i = 0; i < lines.length; i++) {
    if (!lines[i].startsWith("paths:")) continue;
    const value = lines[i].slice("paths:".length).trim();
    let items = [];
    if (value === "") {
      for (const l of lines.slice(i + 1)) {
        const t = l.trim();
        if (!t.startsWith("- ")) break;
        items.push(t.slice(2));
      }
    } else {
      items = splitOutsideBraces(value.replace(/^[[\]]+|[[\]]+$/g, ""));
    }
    return items
      .map((it) => it.trim().replace(/^["']+|["']+$/g, ""))
      .filter(Boolean);
  }
  return [];
};
// A rules glob anchored at the repo root: `**` spans directories, `*` and `?` stay in one segment, `{a,b}`
// alternates, everything else is literal. Throws on a malformed glob, which Claude Code can't load either.
const globRegExp = (g) => {
  let re = "^";
  let depth = 0;
  for (let i = 0; i < g.length; i++) {
    const c = g[i];
    if (g.startsWith("**/", i)) {
      re += "(?:.*/)?";
      i += 2;
    } else if (g.startsWith("**", i)) {
      re += ".*";
      i += 1;
    } else if (c === "*") re += "[^/]*";
    else if (c === "?") re += "[^/]";
    else if (c === "{") {
      depth++;
      re += "(?:";
    } else if (c === "}" && depth > 0) {
      depth--;
      re += ")";
    } else if (c === "," && depth > 0) re += "|";
    else re += c.replace(/[.*+?^${}()|[\]\\/]/g, "\\$&");
  }
  return new RegExp(`${re}$`);
};
const ruleMatches = (frontmatter, paths) =>
  rulePaths(frontmatter).some((g) => {
    try {
      const re = globRegExp(g);
      return paths.some((p) => re.test(p.replace(/^\.\//, "")));
    } catch {
      return false;
    }
  });

// ─── Fingerprint: SHA-256 and base64 in plain JS (the runtime has no crypto API) ───

const utf8 = (s) => {
  const out = [];
  for (const ch of String(s)) {
    const c = ch.codePointAt(0);
    if (c < 0x80) out.push(c);
    else if (c < 0x800) out.push(0xc0 | (c >> 6), 0x80 | (c & 63));
    else if (c < 0x10000)
      out.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
    else
      out.push(
        0xf0 | (c >> 18),
        0x80 | ((c >> 12) & 63),
        0x80 | ((c >> 6) & 63),
        0x80 | (c & 63),
      );
  }
  return out;
};
const primes = (n) => {
  const out = [];
  for (let k = 2; out.length < n; k++)
    if (out.every((p) => k % p !== 0)) out.push(k);
  return out;
};
// Round constants and initial hash: the fractional bits of the primes' cube and square roots.
const SHA_K = primes(64).map((p) => ((Math.cbrt(p) % 1) * 2 ** 32) | 0);
const SHA_H = primes(8).map((p) => ((Math.sqrt(p) % 1) * 2 ** 32) | 0);
const ror = (x, n) => (x >>> n) | (x << (32 - n));
const sha256hex = (bytes) => {
  const bits = bytes.length * 8;
  const m = [...bytes, 0x80];
  while (m.length % 64 !== 56) m.push(0);
  // shortcut: a 32-bit length field, so inputs must stay under 512 MB; fingerprints hash a few hundred bytes.
  m.push(
    0,
    0,
    0,
    0,
    (bits >>> 24) & 255,
    (bits >>> 16) & 255,
    (bits >>> 8) & 255,
    bits & 255,
  );
  const h = [...SHA_H];
  const w = new Array(64);
  for (let o = 0; o < m.length; o += 64) {
    for (let i = 0; i < 16; i++)
      w[i] =
        (m[o + 4 * i] << 24) |
        (m[o + 4 * i + 1] << 16) |
        (m[o + 4 * i + 2] << 8) |
        m[o + 4 * i + 3];
    for (let i = 16; i < 64; i++) {
      const s0 = ror(w[i - 15], 7) ^ ror(w[i - 15], 18) ^ (w[i - 15] >>> 3);
      const s1 = ror(w[i - 2], 17) ^ ror(w[i - 2], 19) ^ (w[i - 2] >>> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) | 0;
    }
    let [a, b, c, d, e, f, g, hh] = h;
    for (let i = 0; i < 64; i++) {
      const t1 =
        (hh +
          (ror(e, 6) ^ ror(e, 11) ^ ror(e, 25)) +
          ((e & f) ^ (~e & g)) +
          SHA_K[i] +
          w[i]) |
        0;
      const t2 =
        ((ror(a, 2) ^ ror(a, 13) ^ ror(a, 22)) +
          ((a & b) ^ (a & c) ^ (b & c))) |
        0;
      hh = g;
      g = f;
      f = e;
      e = (d + t1) | 0;
      d = c;
      c = b;
      b = a;
      a = (t1 + t2) | 0;
    }
    [a, b, c, d, e, f, g, hh].forEach((v, i) => (h[i] = (h[i] + v) | 0));
  }
  return h.map((x) => (x >>> 0).toString(16).padStart(8, "0")).join("");
};
const B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
const base64 = (bytes) => {
  let s = "";
  for (let i = 0; i < bytes.length; i += 3) {
    const n =
      (bytes[i] << 16) | ((bytes[i + 1] ?? 0) << 8) | (bytes[i + 2] ?? 0);
    s +=
      B64[n >> 18] +
      B64[(n >> 12) & 63] +
      (i + 1 < bytes.length ? B64[(n >> 6) & 63] : "=") +
      (i + 2 < bytes.length ? B64[n & 63] : "=");
  }
  return s;
};
// Same identity pr-review v1 embeds, so either plugin recognises the other's prior comments.
const fingerprint = (file, line, category) =>
  sha256hex(utf8(`${file}:${line}:${category}`)).slice(0, 12);
// First 256 bytes of the code, minus a multi-byte character the cut split (Go's strings.ToValidUTF8).
const encodeSnippet = (code) => {
  const bytes = utf8(code).slice(0, SNIPPET_BYTES);
  let lead = bytes.length - 1;
  while (lead >= 0 && (bytes[lead] & 0xc0) === 0x80) lead--;
  if (lead >= 0) {
    const b = bytes[lead];
    const need = b >= 0xf0 ? 4 : b >= 0xe0 ? 3 : b >= 0xc0 ? 2 : 1;
    if (bytes.length - lead < need) bytes.length = lead;
  }
  return base64(bytes);
};

// ─── Finding pipeline: validate → gate → prior review → (verify) → positional dedup → (consolidate) → snap ───

// Longest common substring length, O(n·m); explanations are a few sentences.
const lcs = (x, y) => {
  if (!x || !y) return 0;
  let prev = new Array(y.length + 1).fill(0);
  let best = 0;
  for (let i = 1; i <= x.length; i++) {
    const cur = new Array(y.length + 1).fill(0);
    for (let j = 1; j <= y.length; j++) {
      if (x[i - 1] === y[j - 1]) {
        cur[j] = prev[j - 1] + 1;
        if (cur[j] > best) best = cur[j];
      }
    }
    prev = cur;
  }
  return best;
};

const invalidReason = (f) => {
  if (!f.id) return "missing id";
  if (!f.file) return "empty file";
  if (!(Number.isInteger(f.line) && f.line > 0))
    return `non-positive line ${f.line}`;
  if (
    f.startLine != null &&
    !(Number.isInteger(f.startLine) && f.startLine > 0 && f.startLine <= f.line)
  )
    return `invalid startLine ${f.startLine} (line=${f.line})`;
  if (!(
    Number.isInteger(f.confidence) &&
    f.confidence >= 0 &&
    f.confidence <= 100
  ))
    return `confidence ${f.confidence} out of range`;
  if (!SEVERITY_RANK[f.severity])
    return `unknown severity ${JSON.stringify(f.severity)}`;
  for (const k of ["rationale", "explanation", "code", "language"])
    if (!String(f[k] ?? "").trim()) return `empty ${k}`;
  return null;
};
// Specialists number findings locally (f-1, or <role>-1); prefixing the role makes IDs unique per review.
const namespaceId = (role, id) => {
  let s = String(id);
  if (s.startsWith("f-")) s = s.slice(2);
  if (s.startsWith(`${role}-`)) s = s.slice(role.length + 1);
  return `${role}-${s}`;
};
// confidence < 50 drops; 50-74 keeps only Critical and Medium; ≥ 75 keeps everything.
const gatePass = (confidence, severity) =>
  confidence >= 75 ||
  (confidence >= 50 && (severity === "Critical" || severity === "Medium"));

const inRuns = (runs, n) => (runs ?? []).some(([s, e]) => n >= s && n <= e);

const matchPrior = (f, priorIssues) =>
  priorIssues.find(
    (p) =>
      p.path === f.file &&
      ((p.fingerprint &&
        p.fingerprint === fingerprint(f.file, f.line, f.category)) ||
        (p.line && Math.abs(p.line - f.line) <= PRIOR_LINE_WINDOW) ||
        (p.snippet && lcs(p.snippet, f.code) >= PRIOR_SNIPPET_OVERLAP) ||
        (p.description &&
          (lcs(p.description, f.explanation) >= PRIOR_DESCRIPTION_OVERLAP ||
            lcs(p.description, f.rationale) >= PRIOR_DESCRIPTION_OVERLAP))),
  );

// One role's raw output → valid findings split into invalid / below-gate / prior-review / candidates.
// A prior match on a PR-added line survives with a note; anything else the author already engaged with drops.
const triage = (role, out, priorIssues, addedRuns) => {
  const r = {
    role,
    budgetExhausted: out.scan_status === "budget_exhausted",
    invalid: [],
    gated: [],
    priorDropped: [],
    candidates: [],
  };
  const ids = new Set();
  (out.findings ?? []).forEach((raw, i) => {
    const reason = invalidReason(raw);
    const id = raw.id ? namespaceId(role, raw.id) : `${role}-#${i + 1}`;
    if (reason || ids.has(id)) {
      r.invalid.push({ role, id, reason: reason ?? "duplicate id" });
      return;
    }
    ids.add(id);
    const f = { ...raw, id, specialist: role, crossRefs: [] };
    f.startLine = raw.startLine ?? null;
    f.suggested_fix =
      raw.suggested_fix && raw.suggested_fix.trim() !== raw.code.trim()
        ? raw.suggested_fix
        : null;
    if (!gatePass(f.confidence, f.severity)) return r.gated.push(f);
    const prior = matchPrior(f, priorIssues);
    if (!prior) return r.candidates.push(f);
    if (
      !prior.author_dismissed &&
      !prior.is_resolved &&
      !prior.is_outdated &&
      inRuns(addedRuns.get(f.file), f.line)
    ) {
      f.explanation += PRIOR_KEPT_NOTE;
      return r.candidates.push(f);
    }
    r.priorDropped.push(f);
  });
  return r;
};

const crossRef = (f, mergedBy) => ({
  id: f.id,
  specialist: f.specialist,
  severity: f.severity,
  confidence: f.confidence,
  file: f.file,
  line: f.line,
  rationale: f.rationale,
  merged_by: mergedBy,
});
// Fold `drop` into `keep`, carrying any peers drop had already absorbed, so none goes unaccounted.
const fold = (keep, drop, mergedBy) => {
  keep.crossRefs.push(crossRef(drop, mergedBy), ...drop.crossRefs);
  drop.crossRefs = [];
};
const cmp = (a, b) => (a < b ? -1 : a > b ? 1 : 0);
const bySeverityFirst = (x, y) =>
  SEVERITY_RANK[y.severity] - SEVERITY_RANK[x.severity] ||
  y.confidence - x.confidence ||
  cmp(x.specialist, y.specialist) ||
  cmp(x.id, y.id);
const byFileLine = (x, y) => cmp(x.file, y.file) || x.line - y.line;

// A peer folds into a representative on a same-defect signal (shared or related category, or 60+ shared
// explanation chars), or when it is strictly less severe. The representative is the most severe member.
const canFold = (rep, peer) =>
  rep.category === peer.category ||
  RELATED.has(`${rep.category}|${peer.category}`) ||
  lcs(rep.explanation, peer.explanation) >= SAME_DEFECT_OVERLAP ||
  SEVERITY_RANK[peer.severity] < SEVERITY_RANK[rep.severity];

// Clusters findings within ±3 lines of each other in one file and folds each cluster to one finding per
// distinct defect: line proximity alone is not duplication.
const positional = (findings) => {
  const byFile = new Map();
  for (const f of findings)
    byFile.set(f.file, [...(byFile.get(f.file) ?? []), f]);
  const out = [];
  for (const group of byFile.values()) {
    group.sort((x, y) => x.line - y.line);
    const clusters = [];
    for (const f of group) {
      const last = clusters[clusters.length - 1];
      if (last && f.line - last[last.length - 1].line <= POSITIONAL_WINDOW)
        last.push(f);
      else clusters.push([f]);
    }
    for (const cluster of clusters) {
      const reps = [];
      for (const f of [...cluster].sort(bySeverityFirst)) {
        const rep = reps.find((r) => canFold(r, f));
        if (rep) fold(rep, f, "positional");
        else reps.push(f);
      }
      out.push(...reps);
    }
  }
  return out.sort(byFileLine);
};

// Applies the consolidator's duplicate groups. It only names the members; the posted one is picked here, by a
// fixed rule: a file in the diff first, then severity, then confidence, then ID.
const applyGroups = (findings, groups, inDiff) => {
  const byId = new Map(findings.map((f) => [f.id, f]));
  const taken = new Set();
  const skipped = [];
  const pick = (x, y) =>
    Number(inDiff(y.file)) - Number(inDiff(x.file)) ||
    SEVERITY_RANK[y.severity] - SEVERITY_RANK[x.severity] ||
    y.confidence - x.confidence ||
    cmp(x.id, y.id);
  for (const g of groups ?? []) {
    const ids = [...new Set(g.ids ?? [])].filter(
      (id) => byId.has(id) && !taken.has(id),
    );
    if (ids.length < 2) {
      skipped.push((g.ids ?? []).join("+"));
      continue;
    }
    const [keep, ...drop] = ids.map((id) => byId.get(id)).sort(pick);
    for (const d of drop) {
      fold(keep, d, "consolidator");
      byId.delete(d.id);
    }
    ids.forEach((id) => taken.add(id));
  }
  return { findings: findings.filter((f) => byId.has(f.id)), skipped };
};

const SNAP_NOTE = (line) =>
  `_Note: This comment was placed on the nearest diff line; the issue actually occurs on line ${line}._\n\n`;
// In a hunk → inline. Within 5 lines of one → inline, snapped, with a note. Otherwise → summary-only.
// A multi-line finding collapses to its last line when its start falls outside the hunks.
const classify = (findings, hunksOf) => {
  const inline = [];
  const summaryOnly = [];
  for (const f of findings) {
    const runs = hunksOf(f.file);
    if (
      f.startLine != null &&
      !(inRuns(runs, f.startLine) && inRuns(runs, f.line))
    )
      f.startLine = null;
    if (inRuns(runs, f.line)) {
      inline.push(f);
      continue;
    }
    let nearest = null;
    for (const [s, e] of runs ?? []) {
      const c = f.line < s ? s : e;
      if (nearest === null || Math.abs(c - f.line) < Math.abs(nearest - f.line))
        nearest = c;
    }
    if (nearest === null || Math.abs(nearest - f.line) > SNAP_WINDOW) {
      summaryOnly.push(f);
      continue;
    }
    f.explanation = SNAP_NOTE(f.line) + f.explanation;
    f.line = nearest;
    inline.push(f);
  }
  return { inline, summaryOnly };
};

// ─── Rendering ───

const firstSentence = (t) => {
  const s = String(t ?? "").trim();
  const i = s.search(/[.\n]/);
  return (i >= 0 ? s.slice(0, i) : s).trim();
};
const fence = (lang, code) =>
  `\`\`\`${lang}\n${code.replace(/\n+$/, "")}\n\`\`\`\n`;
const renderIssue = (f, includePath) =>
  (includePath ? `**${f.file}:${f.line}**\n\n` : "") +
  `${EMOJI[f.severity]} **${f.severity}** (Confidence: ${f.confidence}/100) - ${
    f.rationale.trim() || firstSentence(f.explanation) || "(no description)"
  }\n\n` +
  `<!-- cr-finding id="${fingerprint(f.file, f.line, f.category)}" snippet64="${encodeSnippet(f.code)}" -->\n\n` +
  `**Issue & impact:** ${f.explanation}\n\n` +
  `**Code:**\n\n${fence(f.language, f.code)}` +
  (f.suggested_fix
    ? `\n**Suggested fix:**\n\n${fence(f.language, f.suggested_fix)}`
    : "");
const fileLink = (f, owner, repo, sha) => {
  const base = f.file.split("/").pop();
  const url = `https://github.com/${owner}/${repo}/blob/${sha}/${f.file}`;
  return f.startLine != null && f.startLine !== f.line
    ? `[${base}:${f.startLine}-${f.line}](${url}#L${f.startLine}-L${f.line})`
    : `[${base}:${f.line}](${url}#L${f.line})`;
};
// One line per finding for the terminal: the first sentence of the explanation, minus the snap note.
const gist = (f) => {
  const t = String(f.explanation ?? "")
    .replace(/^_Note:[^\n]*_\s*/, "")
    .replace(/\s+/g, " ")
    .trimStart();
  const m = t.match(/^(.{1,200}?[.!?])(\s|$)/);
  return (m ? m[1] : t.length > 160 ? `${t.slice(0, 157)}…` : t) || f.rationale;
};

// ─── Self-check: runs before any agent, so a broken port fails fast and costs nothing ───

const check = (ok, what) => {
  if (!ok) throw new Error(`pr-review-v2 self-check failed: ${what}`);
};
const fx = (o) => ({
  id: "x-1",
  specialist: "x",
  category: "correctness",
  file: "a.ts",
  line: 10,
  startLine: null,
  confidence: 80,
  severity: "Medium",
  rationale: "r",
  explanation: "e",
  code: "c",
  language: "ts",
  suggested_fix: null,
  crossRefs: [],
  ...o,
});
check(
  sha256hex(utf8("abc")) ===
    "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
  "sha256 test vector",
);
check(
  fingerprint("src/a.ts", 10, "security") === "bf3c19ec423e",
  "fingerprint matches pr-review v1",
);
check(base64(utf8("é→x")) === "w6nihpJ4", "base64 of multi-byte text");
check(
  encodeSnippet(`${"a".repeat(255)}é`) === base64(utf8("a".repeat(255))),
  "snippet cut drops a split character",
);
check(
  globRegExp("src/**/*.ts").test("src/a.ts") &&
    globRegExp("src/**/*.ts").test("src/x/y/a.ts") &&
    !globRegExp("src/*.ts").test("src/x/a.ts") &&
    globRegExp("{a,b}/*.md").test("b/x.md") &&
    !globRegExp("a.md").test("aXmd"),
  "rules glob",
);
check(
  JSON.stringify(
    rulePaths('description: x\npaths:\n  - "src/**"\n  - lib/*.ts\nother: y'),
  ) === '["src/**","lib/*.ts"]' &&
    JSON.stringify(rulePaths("paths: [a/{b,c}/*.ts, 'd']")) ===
      '["a/{b,c}/*.ts","d"]' &&
    rulePaths("description: none").length === 0,
  "rule paths frontmatter",
);
check(
  buildRoster(["web/App.tsx"], 0).join() ===
    "security,quality,errors,perf,typescript,react" &&
    buildRoster(["infra/main.tf", "README.md"], 2).join() ===
      "security,quality,errors,perf,infra,claude-md" &&
    buildRoster(["vite.config.js"], 0).join() ===
      "security,quality,errors,perf,typescript,react",
  "roster",
);
check(
  gatePass(75, "Minor") &&
    gatePass(50, "Medium") &&
    !gatePass(74, "Minor") &&
    !gatePass(49, "Critical"),
  "gate",
);
check(
  namespaceId("perf", "f-2") === "perf-2" &&
    namespaceId("claude-md", "claude-md-1") === "claude-md-1" &&
    lcs("xxabcdyy", "zabcdz") === 4,
  "ids and lcs",
);
{
  const added = new Map([["a.ts", [[8, 12]]]]);
  const t = triage(
    "x",
    {
      scan_status: "complete",
      findings: [
        fx({ id: "f-1", line: 10 }), // prior match on an added line: kept with a note
        fx({ id: "f-2", file: "b.ts", line: 3 }), // prior match on context: dropped
        fx({ id: "f-3", confidence: 60, severity: "Minor" }), // below the gate
        fx({ id: "f-4", line: 0 }), // invalid
        fx({ id: "f-5", file: "c.ts", code: "same", suggested_fix: " same " }), // redundant fix nulled
      ],
    },
    [
      { path: "a.ts", line: 11 },
      { path: "b.ts", line: 4 },
    ],
    added,
  );
  check(
    t.candidates.map((f) => f.id).join() === "x-1,x-5" &&
      t.candidates[0].explanation.endsWith(PRIOR_KEPT_NOTE) &&
      t.candidates[1].suggested_fix === null &&
      t.priorDropped[0].id === "x-2" &&
      t.gated[0].id === "x-3" &&
      t.invalid[0].id === "x-4",
    "triage buckets",
  );
}
{
  const folded = positional([
    fx({ id: "a-1", specialist: "a", line: 10, severity: "Minor" }),
    fx({ id: "b-1", specialist: "b", line: 12, severity: "Critical" }),
    fx({ id: "c-1", specialist: "c", line: 13, category: "testing" }),
    fx({ id: "d-1", specialist: "d", line: 30 }),
  ]);
  // b-1 (Critical) represents. Visited most severe first: c-1 folds for being less severe, a-1 on
  // category. d-1 is far away.
  check(
    folded.map((f) => f.id).join() === "b-1,d-1" &&
      folded[0].crossRefs.map((x) => x.id).join() === "c-1,a-1",
    "positional fold",
  );
  const apart = positional([
    fx({ id: "a-1", line: 10, category: "testing", explanation: "one" }),
    fx({ id: "b-1", line: 11, category: "security", explanation: "two" }),
  ]);
  check(
    apart.length === 2,
    "positional keeps equal-severity unrelated neighbours apart",
  );
  const g = applyGroups(
    [
      fx({ id: "a-1", file: "out.ts", severity: "Critical" }),
      fx({ id: "b-1" }),
      fx({ id: "c-1" }),
    ],
    [{ ids: ["a-1", "b-1", "ghost"] }, { ids: ["b-1", "c-1"] }],
    (file) => file !== "out.ts",
  );
  check(
    g.findings.map((f) => f.id).join() === "b-1,c-1" &&
      g.findings[0].crossRefs[0].id === "a-1" &&
      g.skipped.length === 1,
    "consolidator groups: in-diff keeper, one group per finding",
  );
}
{
  const runs = () => [
    [10, 20],
    [40, 42],
  ];
  const c = classify(
    [
      fx({ id: "in", line: 15, startLine: 12 }),
      fx({ id: "snap", line: 45 }),
      fx({ id: "far", line: 60 }),
      fx({ id: "collapse", line: 20, startLine: 5 }),
    ],
    runs,
  );
  check(
    c.inline.map((f) => f.id).join() === "in,snap,collapse" &&
      c.inline[1].line === 42 &&
      c.inline[1].explanation.startsWith("_Note: This comment was placed") &&
      c.inline[2].startLine === null &&
      c.summaryOnly[0].id === "far",
    "line snap",
  );
  check(
    gist(fx({ explanation: "_Note: snapped._\n\nFirst sentence. Second." })) ===
      "First sentence.",
    "summary gist",
  );
}

// ─── Coverage: a "complete" scan that opened little of its domain is flagged, not taken as a clean bill ───

const PAT_TEST =
  /(\.(spec|test)\.[cm]?[jt]sx?$|(^|\/)(__tests__|__mocks__|e2e|tests?)\/)/i;
const FAMILY = { typescript: PAT_TS, react: PAT_REACT, infra: PAT_INFRA };
// shortcut: a flat 25% floor for every domain role; tune per role if real runs flag clean scans.
const THIN_SHARE = 0.25;
// claude-md must read every path-scoped rule it was handed; other roles the non-test files of their family.
const coverage = (role, reviewed, paths, rules) => {
  const expected =
    role === "claude-md"
      ? rules
      : paths.filter(
          (p) => !PAT_TEST.test(p) && (!FAMILY[role] || FAMILY[role].test(p)),
        );
  const seen = new Set(reviewed.map((p) => String(p).replace(/^\.\//, "")));
  const read = expected.filter((p) => seen.has(p)).length;
  const thin =
    role === "claude-md"
      ? read < expected.length
      : expected.length > 0 && read / expected.length < THIN_SHARE;
  return { read, expected: expected.length, thin };
};
check(
  JSON.stringify(
    coverage(
      "perf",
      ["./src/a.ts"],
      ["src/a.ts", "src/b.ts", "src/a.spec.ts", "c.ts", "d.ts", "e.ts"],
      [],
    ),
  ) === '{"read":1,"expected":5,"thin":true}' &&
    !coverage("infra", ["main.tf"], ["main.tf", "src/a.ts"], []).thin &&
    coverage("claude-md", ["r/a.md"], [], ["r/a.md", "r/b.md"]).thin,
  "coverage",
);

// ─── Args ───

// Inverse of prepare.sh's `files` text: "dir/" lines set the directory ("/" is the root), every other
// line is `<name>\t<hunks>\t<added>` with run lists `start-end,start-end`.
const decodeFiles = (text) => {
  const runs = (s) =>
    s ? s.split(",").map((r) => r.split("-").map(Number)) : [];
  const out = [];
  let dir = "";
  for (const line of String(text).split("\n")) {
    if (!line) continue;
    if (line.endsWith("/")) {
      dir = line === "/" ? "" : line;
      continue;
    }
    const [name, hunks, added] = line.split("\t");
    out.push({ path: dir + name, hunks: runs(hunks), added: runs(added) });
  }
  return out;
};
check(
  JSON.stringify(
    decodeFiles("/\nREADME.md\nsrc/a/\nx.ts\t1-4,9-9\ny b.ts\t3-3\t3-3"),
  ) ===
    JSON.stringify([
      { path: "README.md", hunks: [], added: [] },
      {
        path: "src/a/x.ts",
        hunks: [
          [1, 4],
          [9, 9],
        ],
        added: [],
      },
      { path: "src/a/y b.ts", hunks: [[3, 3]], added: [[3, 3]] },
    ]),
  "files decode",
);

const REQUIRED = [
  "pr",
  "owner",
  "repo",
  "headSha",
  "repoRoot",
  "tmp",
  "rubricPath",
  "files",
];
const missingArgs = REQUIRED.filter((k) => args?.[k] == null);
if (missingArgs.length || typeof args.files !== "string") {
  throw new Error(
    `pr-review-v2:review needs args {${REQUIRED.join(", ")}, claudeMd, rules, priorIssues}: the ARGS line /pr-review-v2's prepare.sh prints. Missing or malformed: ${missingArgs.join(", ") || "files"}`,
  );
}
const A = args;
const files = decodeFiles(A.files);
const priorIssues = A.priorIssues ?? [];
const hunkRuns = new Map(files.map((f) => [f.path, f.hunks]));
const inDiff = (file) => (hunkRuns.get(file) ?? []).length > 0;
const addedRuns = new Map(files.map((f) => [f.path, f.added]));
const paths = files.map((f) => f.path);
const matchedRules = (A.rules ?? [])
  .filter((r) => ruleMatches(r.frontmatter, paths))
  .map((r) => r.path)
  .sort();
const guidance = [...(A.claudeMd ?? []), ...matchedRules].sort();
const roster = buildRoster(paths, guidance.length).sort(
  (x, y) => SPAWN_RANK.indexOf(x) - SPAWN_RANK.indexOf(y),
);

// ─── Agents ───

const nullable = (type) => ({ type: [type, "null"] });
const FINDING_PROPS = {
  id: { type: "string" },
  category: { type: "string", enum: CATEGORIES },
  file: { type: "string" },
  line: { type: "integer" },
  startLine: nullable("integer"),
  confidence: { type: "integer" },
  severity: { type: "string", enum: ["Critical", "Medium", "Minor"] },
  rationale: { type: "string" },
  explanation: { type: "string" },
  code: { type: "string" },
  suggested_fix: nullable("string"),
  language: { type: "string" },
};
const FINDINGS = {
  type: "object",
  properties: {
    scan_status: { type: "string", enum: ["complete", "budget_exhausted"] },
    files_reviewed: { type: "array", items: { type: "string" } },
    findings: {
      type: "array",
      items: {
        type: "object",
        properties: FINDING_PROPS,
        required: Object.keys(FINDING_PROPS),
      },
    },
  },
  required: ["scan_status", "files_reviewed", "findings"],
};
const VERDICT = {
  type: "object",
  properties: {
    verdict: { type: "string", enum: ["confirmed", "refuted", "uncertain"] },
    reason: { type: "string" },
  },
  required: ["verdict", "reason"],
};
const CONSOLIDATION = {
  type: "object",
  properties: {
    groups: {
      type: "array",
      items: {
        type: "object",
        properties: {
          ids: { type: "array", items: { type: "string" } },
          reason: { type: "string" },
        },
        required: ["ids", "reason"],
      },
    },
    conflicts: {
      type: "array",
      items: {
        type: "object",
        properties: {
          a: { type: "string" },
          b: { type: "string" },
          subject: { type: "string" },
          note: { type: "string" },
        },
        required: ["a", "b", "subject", "note"],
      },
    },
  },
  required: ["groups", "conflicts"],
};

const where = `HEAD_SHA: ${A.headSha}. REPO_ROOT: ${A.repoRoot}. REVIEW_TMPDIR: ${A.tmp}. Diff: ${A.tmp}/pr.diff (per-file line ranges: ${A.tmp}/diff-index.tsv). Rubric: ${A.rubricPath}.`;
const specialistPrompt = (role) =>
  [
    `PR #${A.pr} in ${A.owner}/${A.repo}. ${where}`,
    `Prior-review issues: ${priorIssues.length ? `${A.tmp}/prior-issues.json (${priorIssues.length})` : "none"}.`,
    `Changed files (${paths.length}): ${paths.join(", ")}`,
    role === "claude-md"
      ? `Guidance files at HEAD_SHA: ${guidance.join(", ")}${matchedRules.length ? `\n\nRead all ${matchedRules.length} listed .claude/rules files in your first turn, as parallel Read calls, and list each in files_reviewed: the summary flags any you skip.` : ""}`
      : !guidance.length
        ? ""
        : role === "quality" // quality.md alone keeps the CLAUDE.md auto-load (no omitClaudeMd)
          ? `Project guidance at HEAD_SHA: ${guidance.join(", ")}. CLAUDE.md and unscoped rules are already in your context, but loaded from the working tree, which may differ from HEAD_SHA: before a finding hinges on a documented project rule, confirm it with git show <HEAD_SHA>:<path>.`
          : `Project guidance at HEAD_SHA, not in your context: ${guidance.join(", ")}. Open one (git show <HEAD_SHA>:<path>) only when a finding hinges on a documented project rule.`,
    "Review the diff for issues in your domain and return them as the structured output.",
  ]
    .filter(Boolean)
    .join("\n\n");
const FINDING_FIELDS = [
  "id",
  "category",
  "file",
  "line",
  "startLine",
  "severity",
  "confidence",
  "rationale",
  "explanation",
  "code",
  "suggested_fix",
];
const pickFields = (f, keys) => Object.fromEntries(keys.map((k) => [k, f[k]]));
const verifyPrompt = (f) =>
  `${where}\n\nThe ${f.specialist} specialist reported this finding. Try to refute it.\n\n${JSON.stringify(pickFields(f, FINDING_FIELDS), null, 2)}`;
const consolidatePrompt = (findings) =>
  `PR #${A.pr} in ${A.owner}/${A.repo}. These ${findings.length} findings survived the gate and verification. \`in_diff\` says whether the finding's file has diff hunks.\n\n${findings
    .map((f) =>
      JSON.stringify({
        ...pickFields(f, [
          "id",
          "specialist",
          "category",
          "severity",
          "confidence",
          "file",
          "line",
        ]),
        in_diff: inDiff(f.file),
        ...pickFields(f, ["rationale", "explanation"]),
      }),
    )
    .join("\n")}`;

// ─── Run ───

log(
  `Roster: ${roster.join(", ")}. ${paths.length} changed files, ${guidance.length} guidance files, ${priorIssues.length} prior issues`,
);

const perRole = await pipeline(
  roster,
  (role) =>
    agent(specialistPrompt(role), {
      label: role,
      phase: "Review",
      agentType: `pr-review-v2:${role}`,
      schema: FINDINGS,
    }),
  async (out, role) => {
    if (!out) return { role, missing: true };
    const r = triage(role, out, priorIssues, addedRuns);
    r.coverage = coverage(role, out.files_reviewed ?? [], paths, matchedRules);
    const verdicts = await parallel(
      r.candidates.map(
        (f) => () =>
          agent(verifyPrompt(f), {
            label: `verify:${f.id}`,
            phase: "Verify",
            agentType: "pr-review-v2:verifier",
            schema: VERDICT,
          }),
      ),
    );
    r.candidates.forEach((f, i) => {
      f.verdict = verdicts[i]?.verdict ?? "unverified";
      f.verdictReason = verdicts[i]?.reason ?? "verifier unavailable";
    });
    log(
      `${role}: ${(out.findings ?? []).length} findings, ${r.candidates.length} verified, ${r.candidates.filter((f) => f.verdict === "refuted").length} refuted`,
    );
    return r;
  },
);

// A null slot is a role whose specialist died or was skipped, or whose triage threw.
const results = perRole.map((r, i) => r ?? { role: roster[i], missing: true });
const ran = results.filter((r) => !r.missing);
const candidates = ran.flatMap((r) => r.candidates);
const refuted = candidates.filter((f) => f.verdict === "refuted");
const verified = candidates.filter((f) => f.verdict !== "refuted");
let survivors = positional(verified);

phase("Consolidate");
let conflicts = [];
const notes = [];
if (survivors.length >= 2) {
  const res = await agent(consolidatePrompt(survivors), {
    label: "consolidate",
    phase: "Consolidate",
    agentType: "pr-review-v2:consolidator",
    schema: CONSOLIDATION,
  });
  if (res) {
    const g = applyGroups(survivors, res.groups, inDiff);
    survivors = g.findings;
    if (g.skipped.length)
      notes.push(
        `consolidator groups ignored (unknown or reused IDs): ${g.skipped.join(", ")}`,
      );
    const live = new Set(survivors.map((f) => f.id));
    conflicts = (res.conflicts ?? []).filter(
      (c) => c.a !== c.b && live.has(c.a) && live.has(c.b),
    );
  } else {
    notes.push(
      "consolidator unavailable: findings that describe one defect at different anchors were not merged",
    );
  }
}
const { inline, summaryOnly } = classify(survivors, (file) =>
  hunkRuns.get(file),
);

// ─── Accounting: every valid finding a specialist returned lands in exactly one bucket ───

const deduped = survivors.flatMap((f) =>
  f.crossRefs.map((x) => ({ ...x, merged_into: f.id })),
);
const gated = ran.flatMap((r) => r.gated);
const priorDropped = ran.flatMap((r) => r.priorDropped);
const buckets = {
  inline_eligible: inline.map((f) => f.id),
  summary_only: summaryOnly.map((f) => f.id),
  dropped_by_gate: gated.map((f) => f.id),
  dropped_prior_review: priorDropped.map((f) => f.id),
  refuted: refuted.map((f) => f.id),
  deduped: deduped.map((x) => x.id),
};
const loadedIds = [
  ...buckets.dropped_by_gate,
  ...buckets.dropped_prior_review,
  ...candidates.map((f) => f.id),
];
const counted = new Map();
for (const ids of Object.values(buckets))
  for (const id of ids) counted.set(id, (counted.get(id) ?? 0) + 1);
const invalid = ran.flatMap((r) => r.invalid);
const orphans = loadedIds.filter((id) => !counted.has(id));
const doubled = [...counted].filter(([, n]) => n > 1).map(([id]) => id);
const accounting = {
  ok: orphans.length === 0 && doubled.length === 0,
  loaded: loadedIds.length + invalid.length,
  accounted: counted.size + invalid.length,
  by_bucket: {
    ...Object.fromEntries(
      Object.entries(buckets).map(([k, v]) => [k, v.length]),
    ),
    invalid: invalid.length,
  },
  orphans,
  doubled,
};

// ─── Terminal summary (shown to the user before the post prompt; never posted) ───

const row = (f, tail) =>
  `    [${f.id}] ${f.severity} conf=${f.confidence} ${f.file}:${f.line} — ${tail}`;
const section = (title, items, fmt) =>
  items.length ? [`  ${title}: ${items.length}`, ...items.map(fmt)] : [];
const roles = (title, rs) =>
  rs.length ? [`  ${title}: ${rs.map((r) => r.role).join(", ")}`] : [];
const specialists = ran.map((r) => r.role).sort();
const summary = [
  "=== Review summary ===",
  `  Specialists: ${specialists.join(", ") || "(none)"}`,
  ...(ran.length
    ? [
        `  Coverage (files read / expected): ${ran
          .map(
            (r) =>
              `${r.role}${r.role === "claude-md" ? " rules" : ""} ${r.coverage.read}/${r.coverage.expected}${r.coverage.thin ? " ⚠" : ""}`,
          )
          .join(", ")}`,
      ]
    : []),
  ...roles(
    "⚠ Thin coverage (reported complete but read little; treat its silence as unreviewed)",
    ran.filter((r) => r.coverage.thin),
  ),
  ...roles(
    "Budget exhausted (self-reported partial scan)",
    ran.filter((r) => r.budgetExhausted),
  ),
  ...roles(
    "Missing (specialist failed or was skipped)",
    results.filter((r) => r.missing),
  ),
  ...section("Invalid findings", invalid, (x) => `    [${x.id}] ${x.reason}`),
  ...section(
    "⚠ Potential conflicts — confirm they don't give opposite advice before posting",
    conflicts,
    (c) => `    [${c.a}] ↔ [${c.b}] — ${c.subject}: ${c.note}`,
  ),
  ...section("Dropped (prior review)", priorDropped, (f) =>
    row(f, f.rationale),
  ),
  ...section("Reconciled (below gate)", gated, (f) => row(f, f.rationale)),
  ...section("Refuted by verifier", refuted, (f) => row(f, f.verdictReason)),
  ...section(
    "Deduped (merged into another finding)",
    deduped,
    (x) =>
      `    [${x.id}] ${x.severity} conf=${x.confidence} ${x.file}:${x.line} → merged into ${x.merged_into} by ${x.merged_by} — ${x.rationale}`,
  ),
  ...(verified.length
    ? [
        `  Verified: ${["confirmed", "uncertain", "unverified"]
          .map((v) => `${verified.filter((f) => f.verdict === v).length} ${v}`)
          .join(", ")}`,
      ]
    : []),
  ...notes.map((n) => `  Note: ${n}`),
  `  Inline eligible: ${inline.length}`,
  `  Summary only: ${summaryOnly.length}`,
  accounting.ok
    ? `  Accounted: ${accounting.accounted} of ${accounting.loaded} findings`
    : `  ⚠ ACCOUNTING MISMATCH: ${accounting.accounted} of ${accounting.loaded} findings accounted for — orphans: ${orphans.join(", ") || "none"}; in two buckets: ${doubled.join(", ") || "none"}`,
  "",
  ...inline.map(
    (f) => `  [${f.id}] ${f.severity} ${f.file}:${f.line} — ${gist(f)}`,
  ),
  ...summaryOnly.map(
    (f) =>
      `  [${f.id}] ${f.severity} (summary) ${f.file}:${f.line} — ${gist(f)}`,
  ),
].join("\n");

log(
  `${inline.length} inline + ${summaryOnly.length} summary-only to post; accounting ${accounting.ok ? "ok" : "MISMATCH"}`,
);

// summary first: when the completion notification truncates the value, the human-readable part survives.
// The skill's payload.sh assembles the GitHub payloads from `review`, optionally for a subset of IDs.
return {
  summary,
  counts: { inline: inline.length, summary_only: summaryOnly.length },
  accounting,
  review: {
    commit_id: A.headSha,
    specialists,
    footer: FOOTER,
    inline: inline.map((f) => ({
      id: f.id,
      path: f.file,
      line: f.line,
      ...(f.startLine != null && f.startLine !== f.line
        ? { start_line: f.startLine }
        : {}),
      body: renderIssue(f, false),
      block: renderIssue(f, true),
      cells: `${EMOJI[f.severity]} ${f.severity} | ${f.confidence} | ${fileLink(f, A.owner, A.repo, A.headSha)}`,
    })),
    summary_only: summaryOnly.map((f) => ({
      id: f.id,
      block: renderIssue(f, true),
    })),
  },
};
