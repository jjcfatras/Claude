package main

import (
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
)

// promptTemplate is the per-role specialist prompt body. Placeholders are
// substituted by runSpawnManifest before the manifest is written; the
// orchestrator forwards the rendered prompt verbatim to the Agent tool.
//
// Placeholders:
//
//	{{ROLE}}, {{TMP}}, {{PR_NUMBER}}, {{HEAD_SHA}}, {{REPO_ROOT}}, {{OWNER}}, {{REPO}}
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
// REPO_ROOT, REVIEW_TMPDIR, and the PR/owner/repo triple are deliberately NOT
// repeated here: the bundle's `## Per-PR` header carries all of them verbatim,
// and the specialist Reads it as its first action. HEAD_SHA stays because it is
// the one value a specialist may need before that Read succeeds.
//
// The first-page limit is 400, not 200. Transcript 2eb9ac1c (Aug 2026) showed
// 200 landing 13 lines short of the `## Source index`'s first data row, so every
// specialist got the index's heading and zero rows — and "page the remainder"
// then walked three of the six through all 1,492 lines sequentially, 78s of it
// on the critical path. 400 clears the orientation payload on a normal PR;
// SOURCE_START makes the tail self-correcting when the index is taller, which is
// why the fallback is phrased as "read up to SOURCE_START" and not "keep paging".
const promptTemplate = "Read {{TMP}}/spawn-context.md " +
	"(offset:0, limit:400 — this covers the orientation payload, ending with the `## Source index`. " +
	"Its `SOURCE_START` header field is the first line of per-file source content: if that number is " +
	"past your first page, Read up to it before anything else, then stop paging and use the index's " +
	"`Bundle line` column to jump straight to the files you need) " +
	"and {{TMP}}/rubric.md, once each. " +
	"Scan {{TMP}}/pr-{{PR_NUMBER}}.diff for issues in your domain — {{DIFF_GUIDANCE}} " +
	"Then Write findings JSON to {{TMP}}/findings/{{ROLE}}.json per the rubric schema.\n\n" +
	"HEAD_SHA: {{HEAD_SHA}}"

// spawnRank orders the manifest so the roles that historically run longest are
// emitted first. The orchestrator streams one Agent block per entry into a
// single message, so a later entry starts later by however long the preceding
// blocks took to generate — on transcript db83dbda the 5th-ranked `infra` began
// 26s after `security` and then defined the critical path for the whole run.
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

	entries, err := buildSpawnEntries(roles, spawnSubs{
		reviewTmpDir: *reviewTmpDir,
		headSHA:      *headSHA,
		prNumber:     *prNumber,
		owner:        *owner,
		repo:         *repo,
		repoRoot:     *repoRoot,
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
	diffBytes    int // raw diff size; 0 means unmeasured, which falls back to the diff-map guidance
	diffLines    int
}

// buildSpawnEntries renders one Agent payload per roster role. Shared by the
// `spawn-manifest` subcommand and by `prepare`, which produces the same manifest
// without a separate process round-trip.
func buildSpawnEntries(roles []string, s spawnSubs) ([]spawnEntry, error) {
	if len(roles) == 0 {
		return nil, fmt.Errorf("spawn-manifest: roster is empty; nothing to spawn")
	}
	subs := strings.NewReplacer(
		"{{TMP}}", s.reviewTmpDir,
		"{{PR_NUMBER}}", fmt.Sprintf("%d", s.prNumber),
		"{{HEAD_SHA}}", s.headSHA,
		"{{REPO_ROOT}}", s.repoRoot,
		"{{OWNER}}", s.owner,
		"{{REPO}}", s.repo,
		"{{DIFF_GUIDANCE}}", diffGuidance(s.diffBytes, s.diffLines),
	)
	ordered := append([]string(nil), roles...)
	// Longest-running roles first; unknown roles keep roster order behind the
	// ranked ones. Stable so the tiebreak is the caller's order, which keeps
	// the manifest deterministic and golden-testable.
	slices.SortStableFunc(ordered, func(a, b string) int {
		return cmp.Compare(rankOf(a), rankOf(b))
	})

	entries := make([]spawnEntry, 0, len(ordered))
	for _, role := range ordered {
		if role == "" {
			return nil, fmt.Errorf("spawn-manifest: roster contains an empty role name")
		}
		rolePrompt := strings.ReplaceAll(promptTemplate, "{{ROLE}}", role)
		entries = append(entries, spawnEntry{
			SubagentType: "code-review:" + role,
			Description:  role + " specialist scan",
			Prompt:       subs.Replace(rolePrompt),
		})
	}
	return entries, nil
}

// Thresholds for reading the diff in one call. Read caps out at 256 KB and
// 25,000 tokens, and its default line budget is 2,000 — so a diff comfortably
// inside all three is cheaper to take whole than to visit file by file.
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
func diffGuidance(diffBytes, diffLines int) string {
	if diffBytes <= 0 {
		return "use the `## Diff map` on the bundle's first page to jump to each file's line offset instead of paging from 0."
	}
	if diffBytes <= oneShotDiffBytes && diffLines <= oneShotDiffLines {
		return fmt.Sprintf("it is %d bytes / %d lines, so Read it in ONE call and work from that.", diffBytes, diffLines)
	}
	return fmt.Sprintf("it is %d bytes / %d lines — too big for one Read, so use the `## Diff map` on the bundle's first page to jump to each file's offset instead of paging from 0.", diffBytes, diffLines)
}

// rankOf returns role's spawn rank, sorting unranked roles last.
func rankOf(role string) int {
	if r, ok := spawnRank[role]; ok {
		return r
	}
	return len(spawnRank)
}
