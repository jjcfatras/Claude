package main

import (
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// promptTemplate is the per-role specialist prompt body. Placeholders are
// substituted by runSpawnManifest before the manifest is written; the
// orchestrator forwards the rendered prompt verbatim to the Agent tool.
//
// Placeholders:
//
//	{{TMP}}, {{PR_NUMBER}}, {{HEAD_SHA}}, {{REPO_ROOT}}, {{OWNER}}, {{REPO}}
//
// This prompt carries per-run paths and nothing else. Everything static —
// review lifecycle, findings schema, calibration, and the harness guardrails —
// lives in the role's own agents/*.md system prompt, which costs the
// orchestrator zero output tokens. The prompt costs it one copy per roster
// entry.
//
// That difference is the reason this template keeps shrinking. In transcript
// 8428d029 (Aug 2026) it duplicated the rubric's schema rules, creating a
// four-way sync burden and a direct Bash-vs-Read contradiction that the prompt
// won. Moving those out left ~740 chars of static "environment notes", which
// transcript db83dbda (Aug 2026) showed cost 6 × ~6.4s of pure token streaming
// before the last specialist could start — while still omitting the one
// guardrail that actually fired that run. Those notes now live in every
// agents/*.md under `## Environment`.
//
// REPO_ROOT and the PR/owner/repo triple are deliberately NOT repeated here:
// the bundle's `## Per-PR` header carries them verbatim, and the specialist
// Reads it as its first action. The tmpdir is stated exactly once, as the
// `$REVIEW_TMPDIR` the agent files already use, because the startup Reads run in
// parallel before the bundle is open; HEAD_SHA stays for the same reason. The
// findings path is not here at all: every agents/*.md `## Output` names its own.
// Transcript 70510fd5 (Nov 2026) measured the previous form — the tmpdir spelled
// out four times plus that findings sentence — at ~505 output tokens per Agent
// block, ~3.3s each, and no specialist starts until the whole message streams.
//
// The first-page limit is 400, not 200. Transcript 2eb9ac1c (Aug 2026) showed
// 200 landing 13 lines short of the `## Source index`'s first data row, so every
// specialist got the index's heading and zero rows — and "page the remainder"
// then walked three of the six through all 1,492 lines sequentially, 78s of it
// on the critical path. 400 clears the orientation payload on a normal PR;
// SOURCE_START makes the tail self-correcting when the index is taller, which is
// why the fallback is phrased as "read up to SOURCE_START" and not "keep paging".
//
// When prepare has the bundle in hand it replaces that fallback with an exact,
// byte-sized read plan (see orientationReads): transcript f6147551 showed "Read
// up to it" becoming one ~59k-token Read on a 263-file PR.
const promptTemplate = "Your $REVIEW_TMPDIR is {{TMP}}. " +
	"Read $REVIEW_TMPDIR/spawn-context.md ({{ORIENTATION_READS}}) " +
	"and $REVIEW_TMPDIR/rubric.md, once each. " +
	"Scan $REVIEW_TMPDIR/pr-{{PR_NUMBER}}.diff for issues in your domain — {{DIFF_GUIDANCE}}\n\n" +
	"HEAD_SHA: {{HEAD_SHA}}"

// spawnRank orders the manifest so the roles that historically run longest are
// emitted first. On transcript db83dbda the harness started each agent as its
// block streamed, so a later entry started later: the 5th-ranked `infra` began
// 26s after `security` and then defined the critical path for the whole run.
// Since transcript 70510fd5 (Nov 2026) every agent starts together once the
// whole spawn message has streamed, so order no longer moves start times — what
// costs every specialist is the message's length (see promptTemplate). The rank
// stays because it is free and still helps on a harness that starts per block.
//
// ponytail: a static rank, not a per-PR cost model. It is wrong whenever the
// diff's shape disagrees (a huge TS PR makes `typescript` the long pole), but it
// is deterministic, costs nothing, and cannot do worse than the arbitrary roster
// order it replaces. Upgrade path if it ever matters: rank by the triggering
// files' total hunk count, which `prepare` already computes for the diff map.
var spawnRank = map[string]int{
	"infra":      0,
	"security":   1,
	"typescript": 2,
	"quality":    3,
	"errors":     4,
	"react":      5,
	"perf":       6,
	"claude-md":  7,
}

type spawnEntry struct {
	SubagentType string `json:"subagent_type"`
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`
}

func runSpawnManifest(argv []string) error {
	fs := flag.NewFlagSet("spawn-manifest", flag.ContinueOnError)
	rosterPath := fs.String("roster", "", "path to roster JSON array (from `roster --out-roster`)")
	reviewTmpDir := fs.String("review-tmpdir", "", "path to $REVIEW_TMPDIR (substituted into prompts as {{TMP}})")
	headSHA := fs.String("head-sha", "", "full HEAD SHA")
	prNumber := fs.Int("pr-number", 0, "pull request number")
	owner := fs.String("owner", "", "GitHub owner")
	repo := fs.String("repo", "", "GitHub repo")
	repoRoot := fs.String("repo-root", "", "repo working-tree root; emitted as REPO_ROOT so specialists never synthesize paths from cwd")
	out := fs.String("out", "", "output path for spawn-manifest JSON array")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *rosterPath == "" || *reviewTmpDir == "" || *headSHA == "" || *prNumber == 0 || *owner == "" || *repo == "" || *repoRoot == "" || *out == "" {
		return fmt.Errorf("spawn-manifest: --roster, --review-tmpdir, --head-sha, --pr-number, --owner, --repo, --repo-root, --out are all required")
	}

	raw, err := os.ReadFile(*rosterPath)
	if err != nil {
		return fmt.Errorf("read --roster: %w", err)
	}
	var roles []string
	if err := json.Unmarshal(raw, &roles); err != nil {
		return fmt.Errorf("parse --roster: %w", err)
	}

	bundle, _ := os.ReadFile(*reviewTmpDir + "/spawn-context.md") // absent → legacy first-page directive
	entries, err := buildSpawnEntries(roles, spawnSubs{
		reviewTmpDir: *reviewTmpDir,
		headSHA:      *headSHA,
		prNumber:     *prNumber,
		owner:        *owner,
		repo:         *repo,
		repoRoot:     *repoRoot,
		bundle:       string(bundle),
	})
	if err != nil {
		return err
	}
	return writeJSON(*out, entries)
}

// spawnSubs carries the per-run values substituted into promptTemplate.
type spawnSubs struct {
	reviewTmpDir string
	headSHA      string
	prNumber     int
	owner        string
	repo         string
	repoRoot     string
	diff         string // raw diff text; empty means unmeasured, which falls back to the diff-map guidance
	bundle       string // rendered spawn-context.md; empty falls back to the open-ended first-page directive
}

// legacyOrientation is the first-page directive used when the bundle isn't
// available to plan exact reads from.
const legacyOrientation = "offset:0, limit:400 — this covers the orientation payload, ending with the `## Source index`. " +
	"Its `SOURCE_START` header field is the first line of per-file source content: if that number is " +
	"past your first page, Read up to it before anything else, then stop paging and use the index's " +
	"`Bundle line` column to jump straight to the files you need"

// Read caps out at 25,000 tokens and 2,000 lines by default. Source-index rows
// are dense with paths and digits: transcript f6147551 measured one 450-line
// page at 117,624 bytes = 59,259 tokens, ~2 bytes/token including the line
// gutter. 32 KB is ~17k tokens at that density, leaving room for denser rows.
const (
	maxReadBytes = 32_000
	maxReadLines = 2_000
)

var sourceStartRe = regexp.MustCompile(`(?m)^- SOURCE_START: (\d+) `)

// orientationReads plans the Reads that cover spawn-context.md up to
// SOURCE_START (the whole file when it has no source section), each at most
// maxReadBytes. A fixed line limit can't do this: on transcript f6147551 one
// page of the same bundle ran ~180 tokens/line and another ~45. The plan says
// nothing about what lies past SOURCE_START: the bundle's own SOURCE_START
// header line does, and it sits inside the first planned Read.
func orientationReads(bundle string) string {
	lines := strings.SplitAfter(bundle, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	end := len(lines)
	if m := sourceStartRe.FindStringSubmatch(bundle); m != nil {
		if n, _ := strconv.Atoi(m[1]); n > 0 && n < end {
			end = n
		}
	}
	var reads []string
	for _, c := range planReads(lines[:end]) {
		reads = append(reads, fmt.Sprintf("offset:%d limit:%d", c[0], c[1]))
	}
	plan := strings.Join(reads, "; then ")
	if len(reads) > 1 {
		plan = fmt.Sprintf("%d Reads, each sized under the 25k-token cap — issue them exactly as given, never merged: %s", len(reads), plan)
	}
	return plan
}

// planReads splits lines into consecutive {0-based start, count} runs of at
// most maxReadBytes and maxReadLines each (a single over-size line still gets
// its own run).
func planReads(lines []string) [][2]int {
	var runs [][2]int
	for start := 0; start < len(lines); {
		n, size := 0, 0
		for start+n < len(lines) && n < maxReadLines && (n == 0 || size+len(lines[start+n]) <= maxReadBytes) {
			size += len(lines[start+n])
			n++
		}
		runs = append(runs, [2]int{start, n})
		start += n
	}
	return runs
}

// buildSpawnEntries renders one Agent payload per roster role. Shared by the
// `spawn-manifest` subcommand and by `prepare`, which produces the same manifest
// without a separate process round-trip.
func buildSpawnEntries(roles []string, s spawnSubs) ([]spawnEntry, error) {
	if len(roles) == 0 {
		return nil, fmt.Errorf("spawn-manifest: roster is empty; nothing to spawn")
	}
	orientation := legacyOrientation
	if s.bundle != "" {
		orientation = orientationReads(s.bundle)
	}
	subs := strings.NewReplacer(
		"{{ORIENTATION_READS}}", orientation,
		"{{TMP}}", s.reviewTmpDir,
		"{{PR_NUMBER}}", fmt.Sprintf("%d", s.prNumber),
		"{{HEAD_SHA}}", s.headSHA,
		"{{REPO_ROOT}}", s.repoRoot,
		"{{OWNER}}", s.owner,
		"{{REPO}}", s.repo,
		"{{DIFF_GUIDANCE}}", diffGuidance(s.diff),
	)
	ordered := append([]string(nil), roles...)
	// Longest-running roles first; unknown roles keep roster order behind the
	// ranked ones. Stable so the tiebreak is the caller's order, which keeps
	// the manifest deterministic and golden-testable.
	slices.SortStableFunc(ordered, func(a, b string) int {
		return cmp.Compare(rankOf(a), rankOf(b))
	})

	prompt := subs.Replace(promptTemplate)
	entries := make([]spawnEntry, 0, len(ordered))
	for _, role := range ordered {
		if role == "" {
			return nil, fmt.Errorf("spawn-manifest: roster contains an empty role name")
		}
		entries = append(entries, spawnEntry{
			SubagentType: "pr-review:" + role,
			Description:  role + " specialist scan",
			Prompt:       prompt,
		})
	}
	return entries, nil
}

// Thresholds for reading the diff whole rather than file by file through the
// diff map. Up to these, the diff is read in maxReadBytes-sized parallel calls:
// one call when it fits, otherwise a byte-planned set. A byte limit alone
// doesn't keep one Read under the 25,000-token cap — transcript 70331439's
// 72,001-byte diff was 29,358 tokens, so all six specialists' "ONE call" came
// back as a 742-of-1,026-line partial view and each spent a sequential turn
// fetching the rest.
const (
	oneShotDiffBytes = 150_000
	oneShotDiffLines = 1_500
)

// diffGuidance renders the diff-reading directive from the diff's measured
// size, because the same sentence is right on one PR and wrong on the next.
//
// The rule used to be absolute — "never bare-Read the diff" — while its
// rationale ("the raw diff often exceeds the Read byte cap on large PRs") was
// conditional. On a small diff the map costs one Read per file to replace a
// single Read of the whole thing, so the instruction was simply worse than
// ignoring it, and specialists noticed: all five live roles on transcript
// d67f09e0 bare-Read a 13,456-byte / 224-line diff in one call, in defiance of
// this prompt. They were right. An always-false directive does not just waste
// its own line, it teaches the reader to discount the ones around it — and
// prepare already knows the number that settles the question.
func diffGuidance(diff string) string {
	if diff == "" {
		return "use the `## Diff map` on the bundle's first page to jump to each file's line offset instead of paging from 0."
	}
	diffBytes, diffLines := len(diff), strings.Count(diff, "\n")
	if diffBytes > oneShotDiffBytes || diffLines > oneShotDiffLines {
		return fmt.Sprintf("it is %d bytes / %d lines — too big for one Read, so use the `## Diff map` on the bundle's first page to jump to each file's offset instead of paging from 0.", diffBytes, diffLines)
	}
	lines := strings.SplitAfter(diff, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	runs := planReads(lines)
	if len(runs) == 1 {
		return fmt.Sprintf("it is %d bytes / %d lines, so Read it in ONE call and work from that.", diffBytes, diffLines)
	}
	reads := make([]string, len(runs))
	for i, r := range runs {
		reads[i] = fmt.Sprintf("offset:%d limit:%d", r[0]+1, r[1]) // Read's offset is a 1-based line number
	}
	return fmt.Sprintf("it is %d bytes / %d lines, over one Read's 25k-token cap, so Read it in these %d calls, all in one parallel batch, and work from them together: %s.", diffBytes, diffLines, len(runs), strings.Join(reads, "; "))
}

// rankOf returns role's spawn rank, sorting unranked roles last.
func rankOf(role string) int {
	if r, ok := spawnRank[role]; ok {
		return r
	}
	return len(spawnRank)
}
