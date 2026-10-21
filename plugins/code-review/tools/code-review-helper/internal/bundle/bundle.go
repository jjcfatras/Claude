// Package bundle assembles $REVIEW_TMPDIR/spawn-context.md deterministically.
//
// Earlier revisions had the lead model concatenate per-PR scalars, on-disk
// JSON artifacts, and the verbatim rubric into the bundle file as a Write
// call — observed cost was ~4 minutes of pure model-output streaming on every
// run (transcript b5a8dd9d, May 2026). The pipeline is mechanical: read
// changed-files.json / roster.json / prior-issues.json / claude-md-files.json
// (and optional migration-history.json), read the rubric, optionally read each
// small-enough changed file at HEAD via `git show HEAD_SHA:path`, and emit
// markdown. This package owns that assembly so the lead's serial output stream
// stays small.
package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// bundleLinesPlaceholder is substituted with the bundle's own total line count
// once assembly finishes. A placeholder-then-replace pass is used rather than
// rendering the bundle twice because the second pass would re-run one `git show`
// per changed file. Digit width does not change the line count, so the number
// the header reports stays correct after substitution.
const bundleLinesPlaceholder = "{{BUNDLE_LINES}}"

// sourceStartPlaceholder is substituted with the first line of the `## Source at
// HEAD` bodies, i.e. the line just past the Source index's last row. Everything
// above it is orientation, so it is exactly the first page a specialist must
// read to hold the jump table.
//
// It needs the same placeholder-then-replace treatment as BUNDLE_LINES: the
// header is written long before the index's height is known, and that height is
// what fixes the value.
//
// Emitting it closes a real bug. The spawn prompt hard-coded `limit: 200` while
// the index's first data row landed at 213 on transcript 2eb9ac1c (Aug 2026), so
// every specialist received the index's heading and zero rows — and the prompt's
// "page the remainder" wording then walked them through all 1,492 lines
// sequentially. The value was already computed here and discarded.
const sourceStartPlaceholder = "{{SOURCE_START}}"

// Input is what the `bundle-context` subcommand collects from flags +
// $REVIEW_TMPDIR.
type Input struct {
	ReviewTmpDir        string
	HeadSHA             string
	PRNumber            int
	Owner               string
	Repo                string
	RepoRoot            string              // repo working-tree root for specialist `git -C <REPO_ROOT> ...` calls; emitted in Per-PR header so specialists never synthesize paths from cwd (which may be a worktree not checked out to HEAD)
	SummaryParagraph    string              // optional pre-composed summary. When empty, Build synthesizes a deterministic stub from the changed-files list. Kept for backwards compatibility with callers that still pass a prose summary via `--summary-paragraph`.
	RubricPath          string              // path to references/code-review-rubrics.md
	RubricExternal      string              // when non-empty, helper writes rubric to this path and emits `RUBRIC_PATH: <path>` in the bundle header instead of inlining the rubric body — keeps spawn-context.md under the 256 KB Read byte cap
	MaxSourceBytes      int                 // per-file embedding cap; 0 disables
	MaxTotalSourceBytes int                 // aggregate cap across all embedded files; 0 disables (only per-file cap applies). A file whose size would push the running total over the cap is marked _omitted_, but later smaller files still get their chance at the remaining headroom — only once the headroom is fully spent are the rest omitted unmeasured.
	GitWorkdir          string              // cwd for `git show` calls (repo root)
	DiffPath            string              // when non-empty and the file exists, helper emits a `## Diff map` section listing each file's byte offset, starting line, and hunk count in the raw diff — lets specialists jump to relevant ranges instead of scanning from offset 0
	HeadDir             string              // when non-empty, a changed file too big for either cap is written verbatim to <HeadDir>/<path> and the Source index points at that local copy instead of telling every specialist to run its own `git show`. Empty restores the pre-2.4.0 omit-and-refer behavior.
	HelperPath          string              // absolute path to this binary; emitted as HELPER_PATH so specialists can run `helper show` for files outside the changed set
	ValidLines          map[string][][2]int // changed-line ranges per path (as written to valid-lines.json); used to render hunk-neighbourhood Read windows for materialized files

	// GitShowFn allows tests to inject a deterministic substitute for
	// `git show HEAD_SHA:path`. Returns (content, size, error). When nil, the
	// production gitShowAtHead is used.
	GitShowFn func(workdir, sha, path string) (string, int, error)
}

// Build reads the on-disk artifacts in ReviewTmpDir and emits the spawn-context
// bundle as a string. This function is the only definition of the bundle's
// shape; SKILL.md no longer carries a template to keep in sync, it just invokes
// `prepare`. Section order is load-bearing — see the diff-map note below.
func Build(in Input) (string, error) {
	if in.ReviewTmpDir == "" {
		return "", fmt.Errorf("ReviewTmpDir is required")
	}
	if in.RubricPath == "" {
		return "", fmt.Errorf("RubricPath is required")
	}

	changedFilesRaw, err := os.ReadFile(filepath.Join(in.ReviewTmpDir, "changed-files.json"))
	if err != nil {
		return "", fmt.Errorf("read changed-files.json: %w", err)
	}
	rosterRaw, err := os.ReadFile(filepath.Join(in.ReviewTmpDir, "roster.json"))
	if err != nil {
		return "", fmt.Errorf("read roster.json: %w", err)
	}
	priorIssuesRaw, err := os.ReadFile(filepath.Join(in.ReviewTmpDir, "prior-issues.json"))
	if err != nil {
		return "", fmt.Errorf("read prior-issues.json: %w", err)
	}
	claudeMDRaw, err := os.ReadFile(filepath.Join(in.ReviewTmpDir, "claude-md-files.json"))
	if err != nil {
		return "", fmt.Errorf("read claude-md-files.json: %w", err)
	}

	migrationRaw, err := os.ReadFile(filepath.Join(in.ReviewTmpDir, "migration-history.json"))
	hasMigration := err == nil
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read migration-history.json: %w", err)
	}

	rubricRaw, err := os.ReadFile(in.RubricPath)
	if err != nil {
		return "", fmt.Errorf("read rubric %s: %w", in.RubricPath, err)
	}

	// When the caller requested the rubric be externalized, copy it verbatim
	// to that path before assembling the bundle. Specialists Read the bundle
	// once, then Read the rubric path once — splitting them keeps each file
	// under the Read tool's caps (256 KB bytes AND 25,000 tokens; the token
	// cap is the binding limit on dense source).
	if in.RubricExternal != "" {
		if err := os.WriteFile(in.RubricExternal, rubricRaw, 0o644); err != nil {
			return "", fmt.Errorf("write rubric to %s: %w", in.RubricExternal, err)
		}
	}

	var changedFiles []string
	if err := json.Unmarshal(changedFilesRaw, &changedFiles); err != nil {
		return "", fmt.Errorf("parse changed-files.json: %w", err)
	}

	var b bytes.Buffer

	fmt.Fprintf(&b, "# Code review spawn context (PR #%d, %s/%s)\n\n", in.PRNumber, in.Owner, in.Repo)

	fmt.Fprint(&b, "## Per-PR\n")
	fmt.Fprintf(&b, "- HEAD_SHA: %s\n", in.HeadSHA)
	fmt.Fprintf(&b, "- PR_NUMBER: %d\n", in.PRNumber)
	fmt.Fprintf(&b, "- REVIEW_TMPDIR: %s\n", in.ReviewTmpDir)
	fmt.Fprintf(&b, "- DIFF: %s/pr-%d.diff\n", in.ReviewTmpDir, in.PRNumber)
	if in.RepoRoot != "" {
		fmt.Fprintf(&b, "- REPO_ROOT: %s\n", in.RepoRoot)
	}
	if in.RubricExternal != "" {
		fmt.Fprintf(&b, "- RUBRIC_PATH: %s (Read this once after the bundle — moved out of the bundle body to keep spawn-context.md under the Read tool's 256 KB byte cap)\n", in.RubricExternal)
	}
	if in.HelperPath != "" {
		fmt.Fprintf(&b, "- HELPER_PATH: %s — for a file OUTSIDE the changed list, run `Bash: \"%s\" show --review-tmpdir %s --repo-root %s --sha %s --path <repo-relative-path>`. It prints one local path (cached, so the first specialist to ask pays and the rest do not); Read that path with offset/limit. Never `git show` such a file yourself — on transcript d67f09e0 four specialists spent 19 redundant subprocesses re-fetching three shared files.\n", in.HelperPath, in.HelperPath, in.ReviewTmpDir, in.RepoRoot, in.HeadSHA)
	}
	fmt.Fprintf(&b, "- BUNDLE_LINES: %s — total lines in this file. Page with Read `offset`/`limit` up to this number; do not guess whether more content follows.\n", bundleLinesPlaceholder)
	hasSourceSection := in.MaxSourceBytes > 0 && len(changedFiles) > 0
	if hasSourceSection {
		// Deliberately does NOT spell the "## Source ..." headings verbatim:
		// this line sits near the top of the bundle, so repeating them here
		// would make an unanchored search for either section match the header
		// prose instead of the real section.
		fmt.Fprintf(&b, "- SOURCE_START: %s — first line of the per-file source bodies. Everything above it is orientation you need in full; everything below is per-file content you should reach via the Source index's `Bundle line` column rather than by paging. If your first Read stopped short of this line, read up to it before doing anything else, using the offset/limit pieces your prompt lists (one Read across it can exceed the 25k-token cap).\n", sourceStartPlaceholder)
	}
	fmt.Fprintf(&b, "- The findings/ subdirectory is pre-created by the orchestrator — do not mkdir or pre-test it before your Write.\n\n")

	fmt.Fprint(&b, "## Summary\n")
	summary := strings.TrimSpace(in.SummaryParagraph)
	if summary == "" {
		summary = stubSummary(changedFiles)
	}
	fmt.Fprintf(&b, "%s\n\n", summary)

	writeSection(&b, "## Changed files\n", changedFilesRaw)
	writeSection(&b, "## Roster (active specialists)\n", rosterRaw)
	writeSection(&b, "## Prior issues (most recent prior Claude Code review on this PR; may be empty)\n", priorIssuesRaw)
	writeSection(&b, "## CLAUDE.md files (JSON array of repo-relative paths; may be empty `[]`)\n", claudeMDRaw)
	if hasMigration {
		writeSection(&b, "## Migration history\n", migrationRaw)
	}

	// The diff map is emitted BEFORE the source sections, which can run to
	// MaxTotalSourceBytes of fenced code. Specialists are told to Read this
	// bundle with `offset:0, limit:200`; with the source body first, the diff
	// map landed past line 1,100 on a 41-file PR and every specialist re-derived
	// it with `grep -n "^diff --git"` instead (transcript 8428d029, Aug 2026).
	// It is also the input to the source section's relevance ordering below.
	var diffHunks map[string]int
	if in.DiffPath != "" {
		if entries, err := buildDiffMap(in.DiffPath); err == nil && len(entries) > 0 {
			b.WriteString(renderDiffMap(entries))
			diffHunks = make(map[string]int, len(entries))
			for _, e := range entries {
				diffHunks[e.Path] = e.HunkCount
			}
		}
	}

	sourceStart := 0
	if hasSourceSection {
		decisions := planSourceSection(in, changedFiles, diffHunks)
		b.WriteString(renderReplicationGroups(decisions))

		// The Source index reports each file's starting line inside this
		// bundle, which cannot be known until the index's own height is known.
		// Rendering it once with zeroed offsets yields that height — the row
		// count is fixed by len(decisions) and digit width never adds a line —
		// so the second render can resolve absolute positions.
		content, relStart := renderSourceContent(decisions, in)
		indexHeight := countLines(renderSourceIndex(decisions, nil))
		base := countLines(b.String()) + indexHeight
		sourceStart = base
		absStart := make(map[string]int, len(relStart))
		for path, rel := range relStart {
			absStart[path] = base + rel
		}
		b.WriteString(renderSourceIndex(decisions, absStart))
		b.WriteString(content)
	}

	if in.RubricExternal == "" {
		fmt.Fprint(&b, "## Rubric\n")
		b.Write(rubricRaw)
		if !bytes.HasSuffix(rubricRaw, []byte("\n")) {
			b.WriteString("\n")
		}
	}

	out := b.String()
	total := strings.Count(out, "\n")
	if !strings.HasSuffix(out, "\n") {
		total++
	}
	out = strings.Replace(out, bundleLinesPlaceholder, strconv.Itoa(total), 1)
	return strings.Replace(out, sourceStartPlaceholder, strconv.Itoa(sourceStart), 1), nil
}

// countLines reports how many lines s occupies when appended to the bundle. A
// trailing newline terminates the final line rather than starting a new one.
func countLines(s string) int {
	n := strings.Count(s, "\n")
	if s != "" && !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// sourceStatus enumerates the possible source-section dispositions.
type sourceStatus string

const (
	sourceEmbedded  sourceStatus = "embedded"
	sourceOmitted   sourceStatus = "omitted"
	sourceDuplicate sourceStatus = "duplicate"
	// sourceMaterialized: too big to embed, but written to <HeadDir>/<path> so
	// specialists Read one local copy instead of each running `git show`.
	sourceMaterialized sourceStatus = "materialized"
)

// sourceDecision is one row of the per-file embed-or-omit plan. `planSourceSection`
// produces these up front so the Source index and Source content sections agree.
type sourceDecision struct {
	Path        string
	Size        int          // size returned by git show (0 when not measured or err)
	Content     string       // populated only when Status == sourceEmbedded
	Status      sourceStatus // sourceEmbedded | sourceOmitted | sourceDuplicate
	Reason      string       // omission rationale (per-file cap, aggregate cap, git show error)
	DuplicateOf string       // representative path whose body is byte-identical; set only when Status == sourceDuplicate
	LocalPath   string       // on-disk copy under HeadDir; set only when Status == sourceMaterialized
	Windows     string       // pre-rendered hunk-neighbourhood Read windows for LocalPath; may be empty when the diff had no ranges for this path
}

// writeSection emits a heading followed by raw bytes (trimmed of trailing
// newlines so consecutive sections aren't separated by a growing blank-line
// run). The "\n\n" suffix keeps the per-section separator deterministic.
func writeSection(b *bytes.Buffer, heading string, raw []byte) {
	b.WriteString(heading)
	b.Write(bytes.TrimRight(raw, "\n"))
	b.WriteString("\n\n")
}

// planSourceSection walks the changed paths most-churned-first — descending
// diff hunk count, ties broken by path — and decides per-file whether to embed
// the content, mark it omitted under the per-file cap, or mark it omitted under
// the aggregate-bytes cap. Per-file cap is checked BEFORE the aggregate cap so
// a single large file is reported with the more useful per-file reason
// ("47 KB > 32 KB cap") rather than being blamed on cumulative usage.
//
// Ordering is by churn rather than lexicographic because the budget is the
// scarce resource and the alphabet has nothing to do with relevance: on PR
// #1778 (transcript 8428d029, Aug 2026) lexicographic order spent 29,176 of
// 50,000 bytes on `pnpm-workspace.yaml` and terraform before reaching the
// `src/modules/banking/` files the review actually flagged, so every file that
// produced a finding was omitted. Hunk-count-desc with a path tiebreak is still
// fully deterministic and golden-testable. Files absent from the diff map sort
// last at hunk count 0.
//
// A file that overflows the aggregate cap no longer latches the remaining ones
// off: a later, smaller file may still fit the headroom, and skipping it
// unmeasured is how the budget went unspent above. The `git show` subprocess is
// skipped only once the headroom is genuinely gone (running ≥ cap), which keeps
// the pathological case — a 176-file PR whose budget is exhausted early — from
// paying for ~100 subprocesses whose results cannot be used.
//
// Files whose HEAD content is byte-identical to an already-planned file are
// marked `duplicate` and charged nothing against the aggregate cap: only the
// first (the representative, by the churn-desc order above) carries a body. On
// PR #1784 (transcript db83dbda, Aug 2026) the 36 changed files were 12 copies
// of 3 terraform templates — ~90% of the 46 KB embedded was replication, 38% of
// the bundle went unread, and four specialists each spent their own tool calls
// proving "this repeats in all 12 apps". Equality is exact content hashing, not
// a similarity heuristic: a group claim of "identical" must never be approximate,
// because a per-app difference is exactly where a finding hides.
//
// Known ceiling: a file larger than MaxSourceBytes is still unreachable through
// the bundle however much churn it carries, so every specialist that needs it
// runs its own `git show` (6 roles, 10 invocations for one 41 KB service on PR
// #1778). Raising the per-file cap is not the fix — one 41 KB file is ~19k
// tokens, which blows the bundle's own Read budget. The upgrade path is for
// `prepare` to materialize the top files by hunk count to $TMP/head/<path> and
// point the Source index at those local copies, uncapped.
func planSourceSection(in Input, paths []string, diffHunks map[string]int) []sourceDecision {
	sorted := append([]string(nil), paths...)
	slices.SortFunc(sorted, func(a, b string) int {
		if d := diffHunks[b] - diffHunks[a]; d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})

	showFn := in.GitShowFn
	if showFn == nil {
		showFn = gitShowAtHead
	}

	out := make([]sourceDecision, 0, len(sorted))
	running := 0
	repByHash := make(map[string]string, len(sorted))
	for _, p := range sorted {
		if in.MaxTotalSourceBytes > 0 && running >= in.MaxTotalSourceBytes {
			out = append(out, sourceDecision{
				Path:   p,
				Status: sourceOmitted,
				Reason: fmt.Sprintf("aggregate bundle cap already exhausted (running=%d ≥ %d) — use `git show %s:%s`", running, in.MaxTotalSourceBytes, in.HeadSHA, p),
			})
			continue
		}
		content, size, err := showFn(in.GitWorkdir, in.HeadSHA, p)
		d := sourceDecision{Path: p, Size: size}
		// The duplicate check precedes both caps: identical content implies
		// identical size, so a representative that cleared the per-file cap
		// guarantees its siblings would too, and charging them again is the
		// waste this exists to remove.
		hash := ""
		if err == nil {
			sum := sha256.Sum256([]byte(content))
			hash = hex.EncodeToString(sum[:])
		}
		switch {
		case err != nil:
			d.Status = sourceOmitted
			d.Reason = fmt.Sprintf("git show: %s", err)
		case repByHash[hash] != "":
			d.Status = sourceDuplicate
			d.DuplicateOf = repByHash[hash]
		case size > in.MaxSourceBytes:
			d = spillToDisk(in, d, p, content, fmt.Sprintf("%d bytes > %d per-file cap", size, in.MaxSourceBytes))
		case in.MaxTotalSourceBytes > 0 && running+size > in.MaxTotalSourceBytes:
			d = spillToDisk(in, d, p, content, fmt.Sprintf("would exceed %d-byte aggregate bundle cap (running=%d, file=%d)", in.MaxTotalSourceBytes, running, size))
		default:
			d.Status = sourceEmbedded
			d.Content = content
			running += size
			repByHash[hash] = p
		}
		out = append(out, d)
	}
	return out
}

// spillToDisk resolves a file that cleared neither cap. With HeadDir set it
// writes the full content to <HeadDir>/<path> and marks the row `materialized`,
// so the Source index can hand every specialist the same local path; without it
// (or on a write error) it degrades to the historical `omitted` row.
//
// This is the upgrade path named in planSourceSection's "Known ceiling" note.
// Raising the caps was never the answer — one 41 KB file is ~19k tokens, which
// blows the bundle's own Read budget — but the file still has to be reachable
// without six specialists each shelling out for it. Spilling to disk separates
// the two: the bundle stays small because it carries a path, and the content
// stays whole because nothing truncates it. On PR #1817 three of four changed
// files were omitted this way while 93.6% of the aggregate budget went unspent,
// and the one changed non-spec source file was read by no specialist at all.
func spillToDisk(in Input, d sourceDecision, p, content, why string) sourceDecision {
	if in.HeadDir != "" {
		if local, err := writeHeadCopy(in.HeadDir, p, content); err == nil {
			d.Status = sourceMaterialized
			d.LocalPath = local
			d.Windows = readWindows(in.ValidLines[p])
			d.Reason = why
			return d
		}
	}
	d.Status = sourceOmitted
	d.Reason = fmt.Sprintf("%s — use `git show %s:%s`", why, in.HeadSHA, p)
	return d
}

// writeHeadCopy materializes one HEAD blob under headDir, preserving the
// repo-relative path so the copy is self-describing in a Read call.
func writeHeadCopy(headDir, p, content string) (string, error) {
	dst := filepath.Join(headDir, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

// windowPad is how many lines of context each changed range carries into its
// suggested Read window. 40 is not a tuned constant — it is what specialists
// picked unprompted when they paged these files by hand (transcript d67f09e0:
// offset:690 limit:180 against hunks at 726-864; offset:1415 limit:90 against a
// hunk at 1460-1473). Wide enough for the enclosing function and its imports,
// narrow enough that four 50-90 KB files reduce to ~21 KB of suggested reading.
const windowPad = 40

// readWindows renders the hunk neighbourhoods of one file as ready-to-use Read
// arguments, padded by windowPad and with overlaps merged so no line is
// suggested twice. Empty when the diff carried no ranges for the path — the
// caller then just points at the whole local file.
func readWindows(ranges [][2]int) string {
	if len(ranges) == 0 {
		return ""
	}
	type span struct{ lo, hi int }
	spans := make([]span, 0, len(ranges))
	for _, r := range ranges {
		spans = append(spans, span{max(1, r[0]-windowPad), r[1] + windowPad})
	}
	slices.SortFunc(spans, func(a, b span) int { return a.lo - b.lo })
	merged := []span{spans[0]}
	for _, sp := range spans[1:] {
		last := &merged[len(merged)-1]
		if sp.lo <= last.hi+1 {
			if sp.hi > last.hi {
				last.hi = sp.hi
			}
			continue
		}
		merged = append(merged, sp)
	}
	parts := make([]string, 0, len(merged))
	for _, m := range merged {
		parts = append(parts, fmt.Sprintf("offset:%d limit:%d", m.lo, m.hi-m.lo+1))
	}
	return strings.Join(parts, ", ")
}

// replicationGroup is one set of changed files whose HEAD content is
// byte-identical. Only groups with at least one sibling are reported.
type replicationGroup struct {
	Representative string
	Size           int
	Siblings       []string
}

// collectReplicationGroups gathers the duplicate decisions by representative,
// preserving the plan's ordering so the section is deterministic.
func collectReplicationGroups(decisions []sourceDecision) []replicationGroup {
	idx := make(map[string]int)
	var groups []replicationGroup
	sizeOf := make(map[string]int, len(decisions))
	for _, d := range decisions {
		if d.Status == sourceEmbedded {
			sizeOf[d.Path] = d.Size
		}
	}
	for _, d := range decisions {
		if d.Status != sourceDuplicate {
			continue
		}
		i, ok := idx[d.DuplicateOf]
		if !ok {
			groups = append(groups, replicationGroup{Representative: d.DuplicateOf, Size: sizeOf[d.DuplicateOf]})
			i = len(groups) - 1
			idx[d.DuplicateOf] = i
		}
		groups[i].Siblings = append(groups[i].Siblings, d.Path)
	}
	return groups
}

// renderReplicationGroups emits the `## Replication groups` section, or nothing
// when the PR has no byte-identical changed files. It exists so specialists stop
// each independently proving that a monorepo change is replicated across N apps:
// the bundle already knows, so it says so once, authoritatively.
func renderReplicationGroups(decisions []sourceDecision) string {
	groups := collectReplicationGroups(decisions)
	if len(groups) == 0 {
		return ""
	}
	var b bytes.Buffer
	fmt.Fprint(&b, "## Replication groups\n\n")
	fmt.Fprintf(&b, "%d group(s) of changed files are byte-identical at HEAD. Each group's body is embedded ONCE under its representative; the siblings are listed here and marked `duplicate` in `## Source index`. A finding about shared content applies to every member — state it once and cite this table for the member list rather than re-deriving the replication with `git show` or a shell loop. Members are identical by exact content hash, so any per-file difference means the files are in different groups.\n\n", len(groups))
	fmt.Fprint(&b, "| Representative (body embedded here) | Bytes | Identical siblings |\n")
	fmt.Fprint(&b, "|-------------------------------------|-------|--------------------|\n")
	for _, g := range groups {
		fmt.Fprintf(&b, "| %s | %d | %d: %s |\n", g.Representative, g.Size, len(g.Siblings), strings.Join(g.Siblings, ", "))
	}
	b.WriteString("\n")
	return b.String()
}

// renderSourceIndex emits a markdown table listing every changed path with its
// status. Specialists check this index FIRST to decide whether a file is in the
// bundle (read inline / paginate via Read) or needs a `git show` fetch.
//
// bundleLine maps a path to its `### <path>` heading's line number inside the
// assembled bundle, so a specialist can Read straight to a file's body instead
// of grepping for the heading — the same jump-table treatment `## Diff map`
// already gives the raw diff. Callers pass nil on the sizing pass; the row count
// (and therefore this section's height) does not depend on the map.
func renderSourceIndex(decisions []sourceDecision, bundleLine map[string]int) string {
	var b bytes.Buffer
	fmt.Fprint(&b, "## Source index\n\n")
	fmt.Fprint(&b, "Specialists: search this index FIRST. `embedded` → Read `$REVIEW_TMPDIR/spawn-context.md` at the `Bundle line` offset. `duplicate` → byte-identical to another file; its `Bundle line` points at the shared body (see `## Replication groups`). `materialized` → too big to embed, already written to disk for you: **Read the local path in the Note column**, at the suggested offsets, and do NOT `git show` it. `omitted` → fetch via `git show HEAD_SHA:path`. Do not `git show` a file marked `embedded`, `duplicate`, or `materialized` — the content is already available, with HEAD line numbers intact.\n\n")
	fmt.Fprint(&b, "| File | Status | Bundle line | Bytes | Note |\n")
	fmt.Fprint(&b, "|------|--------|-------------|-------|------|\n")
	for _, d := range decisions {
		note := ""
		switch d.Status {
		case sourceOmitted:
			note = d.Reason
		case sourceDuplicate:
			note = fmt.Sprintf("identical to `%s`", d.DuplicateOf)
		case sourceMaterialized:
			note = fmt.Sprintf("%s — Read `%s`", d.Reason, d.LocalPath)
			if d.Windows != "" {
				note += fmt.Sprintf(" (changed regions: %s)", d.Windows)
			}
		}
		line := bundleLine[d.Path]
		if d.Status == sourceDuplicate {
			line = bundleLine[d.DuplicateOf]
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %s |\n", d.Path, d.Status, line, d.Size, note)
	}
	b.WriteString("\n")
	return b.String()
}

// renderSourceContent emits the per-file content section and reports each
// rendered path's `### <path>` heading line, counted from the start of this
// section (1-based). Build shifts those into absolute bundle lines.
//
// Embedded bodies carry a `<n>|` gutter holding the file's own HEAD line
// numbers. Every finding must anchor to a positive HEAD line, and without the
// gutter specialists either ran `git show … | cat -n` on files the index told
// them not to fetch, or derived the line by arithmetic on bundle offsets — which
// is silently wrong the moment a fence or blank line shifts (transcript
// db83dbda, Aug 2026: 5 such calls, two of them on the run's critical path).
//
// Duplicates render as a pointer rather than a second copy; omitted files keep
// their one-line placeholder so the section stays a complete manifest.
func renderSourceContent(decisions []sourceDecision, in Input) (string, map[string]int) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "## Source at HEAD (per-file cap: %d bytes; aggregate cap: %d bytes; omitted files listed in `## Source index` — use `git show %s:<path>` to fetch)\n\n", in.MaxSourceBytes, in.MaxTotalSourceBytes, in.HeadSHA)
	fmt.Fprint(&b, "The `N|` gutter is this bundle's own annotation, not file content: `N` is the line number **at HEAD**, which is the number a finding's `line` field must carry. Do not include the gutter when quoting a finding's `code`.\n\n")
	starts := make(map[string]int, len(decisions))
	// Track the running height incrementally rather than re-counting the buffer
	// each iteration — a 176-file PR would otherwise rescan a ~50 KB buffer
	// once per file.
	height := countLines(b.String())
	for _, d := range decisions {
		starts[d.Path] = height + 1
		before := b.Len()
		switch d.Status {
		case sourceOmitted:
			fmt.Fprintf(&b, "### %s\n_omitted: %s_\n\n", d.Path, d.Reason)
		case sourceDuplicate:
			fmt.Fprintf(&b, "### %s\n_byte-identical to `%s` — body embedded there; see `## Replication groups`._\n\n", d.Path, d.DuplicateOf)
		case sourceMaterialized:
			fmt.Fprintf(&b, "### %s\n_%s — full content at `%s`; Read it there._\n\n", d.Path, d.Reason, d.LocalPath)
		default:
			fmt.Fprintf(&b, "### %s\n```%s\n%s```\n\n", d.Path, languageHint(d.Path), numberLines(d.Content))
		}
		height += countLines(b.String()[before:])
	}
	return b.String(), starts
}

// numberLines prefixes each line of content with its 1-based line number and a
// `|` separator, right-aligned to the widest number so the source column stays
// flush. The result always ends in a newline so the closing fence sits alone.
func numberLines(content string) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	width := len(strconv.Itoa(len(lines)))
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%*d| %s\n", width, i+1, line)
	}
	return b.String()
}

// diffMapEntry is one row of the `## Diff map` section — a jump table that
// lets specialists Read the raw diff with targeted offsets instead of scanning
// from offset 0 (which often blows the Read byte cap on large PRs and causes
// duplicated work across parallel specialists).
type diffMapEntry struct {
	Path       string
	DiffLine   int // 1-based line number of the `diff --git` header inside the raw diff
	DiffOffset int // byte offset of the `diff --git` header inside the raw diff
	HunkCount  int // number of `@@ ... @@` hunks for this file
}

// buildDiffMap scans the raw unified diff at diffPath and returns one entry per
// changed file. The parser is permissive: it splits on `\n`, recognises the
// canonical `diff --git a/<src> b/<dst>` header, and counts `@@ ` hunks until
// the next header. Files that the parser cannot extract a path for are emitted
// with an empty Path so the operator can still see something is off.
func buildDiffMap(diffPath string) ([]diffMapEntry, error) {
	data, err := os.ReadFile(diffPath)
	if err != nil {
		return nil, err
	}
	var entries []diffMapEntry
	cur := -1
	offset := 0
	lineNo := 1
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		switch {
		case bytes.HasPrefix(line, []byte("diff --git ")):
			path := parseDiffGitPath(line)
			entries = append(entries, diffMapEntry{
				Path:       path,
				DiffLine:   lineNo,
				DiffOffset: offset,
			})
			cur = len(entries) - 1
		case cur >= 0 && bytes.HasPrefix(line, []byte("@@ ")):
			entries[cur].HunkCount++
		}
		offset += len(line)
		if len(line) > 0 && line[len(line)-1] == '\n' {
			lineNo++
		}
	}
	return entries, nil
}

// parseDiffGitPath extracts the `b/<path>` side of a `diff --git a/<src> b/<dst>`
// header. Renames (`diff --git a/old b/new`) report the new path, matching how
// specialists think about a changed file. Returns "" if the header shape is
// unrecognised.
func parseDiffGitPath(line []byte) string {
	tail := bytes.TrimSpace(line[len("diff --git "):])
	idx := bytes.Index(tail, []byte(" b/"))
	if idx < 0 {
		return ""
	}
	return string(tail[idx+len(" b/"):])
}

// renderDiffMap emits the diff jump table. Specialists are told to Read the
// raw diff with `offset` set to (or just below) the per-file offset rather
// than scanning from 0.
func renderDiffMap(entries []diffMapEntry) string {
	var b bytes.Buffer
	fmt.Fprint(&b, "## Diff map\n\n")
	fmt.Fprint(&b, "Specialists: use this table to Read `$REVIEW_TMPDIR/pr-N.diff` with a targeted `offset` (in lines) and a `limit` covering the file's hunks. The raw diff often exceeds the 256 KB Read byte cap on large PRs, so avoid bare Reads. Bytes are reported for reference; pass the `Diff line` value as `offset` and pick a `limit` large enough to span `Hunks` × ~30 lines.\n\n")
	fmt.Fprint(&b, "| File | Diff line | Diff byte offset | Hunks |\n")
	fmt.Fprint(&b, "|------|-----------|------------------|-------|\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", e.Path, e.DiffLine, e.DiffOffset, e.HunkCount)
	}
	b.WriteString("\n")
	return b.String()
}

// stubSummary builds a deterministic one-line summary from the changed-files
// list. Used when the caller did not provide a prose summary paragraph —
// avoids a serial lead-model turn purely to compose orientation text, and
// keeps the `## Summary` section self-describing.
func stubSummary(changedFiles []string) string {
	switch len(changedFiles) {
	case 0:
		return "No changed files."
	case 1:
		return fmt.Sprintf("1 changed file: %s.", changedFiles[0])
	}

	counts := make(map[string]int, len(changedFiles))
	for _, p := range changedFiles {
		counts[topDir(p)]++
	}
	type kv struct {
		dir   string
		count int
	}
	pairs := make([]kv, 0, len(counts))
	for d, c := range counts {
		pairs = append(pairs, kv{d, c})
	}
	slices.SortFunc(pairs, func(a, b kv) int {
		if a.count != b.count {
			return b.count - a.count
		}
		return strings.Compare(a.dir, b.dir)
	})

	top := pairs
	if len(top) > 5 {
		top = top[:5]
	}
	parts := make([]string, 0, len(top))
	for _, p := range top {
		parts = append(parts, fmt.Sprintf("%s (%d)", p.dir, p.count))
	}

	dirNoun := "directories"
	if len(counts) == 1 {
		dirNoun = "directory"
	}
	return fmt.Sprintf(
		"PR touches %d changed files across %d top-level %s. Top: %s. Specialists: see `## Changed files` for the full path list and `## Source index` for embedded vs. omitted status.",
		len(changedFiles), len(counts), dirNoun, strings.Join(parts, ", "),
	)
}

// topDir returns the first path segment of p, or "(root)" if p has no
// directory component.
func topDir(p string) string {
	if i := strings.Index(p, "/"); i >= 0 {
		return p[:i]
	}
	return "(root)"
}

// gitShowAtHead returns the contents of `path` at HEAD_SHA. If the file is
// missing at HEAD (e.g., a deletion), git show exits 128 and the error is
// returned for the caller to render as an omission notice.
func gitShowAtHead(workdir, sha, path string) (string, int, error) {
	cmd := exec.Command("git", "show", sha+":"+path)
	if workdir != "" {
		cmd.Dir = workdir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", 0, fmt.Errorf("git show: %s", msg)
	}
	out := stdout.String()
	return out, len(out), nil
}

// languageHint maps a file extension to a fenced-block language tag. Returns
// empty string for unknown extensions (renders as a plain fence, which is
// fine).
func languageHint(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".ts":
		return "ts"
	case ".tsx":
		return "tsx"
	case ".js":
		return "js"
	case ".jsx":
		return "jsx"
	case ".json":
		return "json"
	case ".go":
		return "go"
	case ".py":
		return "py"
	case ".rb":
		return "rb"
	case ".rs":
		return "rs"
	case ".java":
		return "java"
	case ".kt":
		return "kt"
	case ".sh", ".bash":
		return "bash"
	case ".sql":
		return "sql"
	case ".tf", ".hcl":
		return "hcl"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".md":
		return "md"
	case ".css":
		return "css"
	case ".scss":
		return "scss"
	case ".html":
		return "html"
	case ".xml":
		return "xml"
	case ".dockerfile":
		return "dockerfile"
	}
	if strings.EqualFold(filepath.Base(path), "Dockerfile") {
		return "dockerfile"
	}
	return ""
}
