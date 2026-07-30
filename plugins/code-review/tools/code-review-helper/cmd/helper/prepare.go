package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/bundle"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/dedup"
	diffpkg "github.com/jjcfatras/claude-tools/code-review-helper/internal/diff"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/roster"
)

// prepare collapses everything between "the shell has fetched the PR" and "the
// orchestrator can spawn specialists" into one call. It replaces six separate
// Bash round-trips — two jq projections plus `diff`, `roster`, `bundle-context`
// and `spawn-manifest` — each of which cost ~7s of fixed harness overhead
// regardless of how little work it did.
//
// The shell keeps only what needs credentials (`gh pr view`, `gh pr diff`,
// `git fetch`, the GraphQL threads query); everything derivable from those
// artifacts happens here. The two jq one-liners this absorbs were also the
// pipeline's most fragile parts: one broke silently when a rendered header's
// wording changed, and the other required the orchestrator to guess a helper
// output's JSON shape.
//
// The existing `diff`/`roster`/`bundle-context`/`spawn-manifest` subcommands
// remain — this is additive, and they are still the right tool for debugging one
// stage in isolation.

// prMeta is the subset of `gh pr view --json headRefOid,url,number,title,headRefName,author,state`
// that the pipeline consumes.
type prMeta struct {
	HeadRefOid string `json:"headRefOid"`
	URL        string `json:"url"`
	Number     int    `json:"number"`
	State      string `json:"state"`
	Author     struct {
		Login string `json:"login"`
	} `json:"author"`
}

// prURLRe extracts owner and repo from a PR html_url. Anchored on `/pull/` so a
// repo named "pull" can't shift the capture groups.
var prURLRe = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/pull/`)

type prepareOpts struct {
	reviewTmpDir  string
	repoRoot      string
	rubric        string
	prMetaPath    string
	threadsPath   string
	diffPath      string
	rubricOut     string
	gitWorkdir    string
	requireOpen   bool
	maxSourceB    int
	maxTotalSrcB  int
	summaryInline string
}

func parsePrepareArgs(argv []string) (prepareOpts, error) {
	var o prepareOpts
	fs := flag.NewFlagSet("prepare", flag.ContinueOnError)
	fs.StringVar(&o.reviewTmpDir, "review-tmpdir", "", "path to $REVIEW_TMPDIR; every output is written here")
	fs.StringVar(&o.repoRoot, "repo-root", "", "absolute path to the repo root")
	fs.StringVar(&o.rubric, "rubric", "", "path to references/code-review-rubrics.md")
	fs.StringVar(&o.prMetaPath, "pr-meta", "", "path to pr-meta.json (from `gh pr view --json headRefOid,url,number,title,headRefName,author,state`); defaults to <review-tmpdir>/pr-meta.json")
	fs.StringVar(&o.threadsPath, "review-threads", "", "path to review-threads.json (the GraphQL reviewThreads response); defaults to <review-tmpdir>/review-threads.json. Missing file is not an error — it yields an empty prior-issues set")
	fs.StringVar(&o.diffPath, "diff", "", "path to the unified diff; defaults to <review-tmpdir>/pr-<number>.diff")
	fs.StringVar(&o.rubricOut, "rubric-out", "", "copy the rubric here and reference it from the bundle header; defaults to <review-tmpdir>/rubric.md")
	fs.StringVar(&o.gitWorkdir, "git-workdir", "", "cwd for `git show` calls when embedding source; defaults to --repo-root")
	fs.BoolVar(&o.requireOpen, "require-open", true, "exit non-zero when the PR state is not OPEN, before doing any work")
	fs.IntVar(&o.maxSourceB, "max-source-bytes", 32768, "embed each changed file <= this many bytes from HEAD; 0 disables source embedding")
	fs.IntVar(&o.maxTotalSrcB, "max-total-source-bytes", 50000, "aggregate cap across all embedded files")
	fs.StringVar(&o.summaryInline, "summary-paragraph", "", "path to a file containing a prep summary paragraph; empty renders the bundle's own deterministic summary")
	if err := fs.Parse(argv); err != nil {
		return prepareOpts{}, err
	}
	if o.reviewTmpDir == "" || o.repoRoot == "" || o.rubric == "" {
		return prepareOpts{}, fmt.Errorf("prepare: --review-tmpdir, --repo-root and --rubric are required")
	}
	if o.prMetaPath == "" {
		o.prMetaPath = o.reviewTmpDir + "/pr-meta.json"
	}
	if o.threadsPath == "" {
		o.threadsPath = o.reviewTmpDir + "/review-threads.json"
	}
	if o.rubricOut == "" {
		o.rubricOut = o.reviewTmpDir + "/rubric.md"
	}
	if o.gitWorkdir == "" {
		o.gitWorkdir = o.repoRoot
	}
	return o, nil
}

func loadPRMeta(path string) (prMeta, string, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return prMeta{}, "", "", fmt.Errorf("read --pr-meta: %w", err)
	}
	var meta prMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return prMeta{}, "", "", fmt.Errorf("parse %s: %w", path, err)
	}
	if meta.HeadRefOid == "" {
		return prMeta{}, "", "", fmt.Errorf("parse %s: headRefOid is empty (was `gh pr view` run with --json headRefOid?)", path)
	}
	m := prURLRe.FindStringSubmatch(meta.URL)
	if m == nil {
		return prMeta{}, "", "", fmt.Errorf("parse %s: could not extract owner/repo from url %q", path, meta.URL)
	}
	return meta, m[1], m[2], nil
}

// runPrepare: derive every pre-spawn artifact from the fetched PR data.
func runPrepare(argv []string) error {
	opts, err := parsePrepareArgs(argv)
	if err != nil {
		return err
	}

	meta, owner, repo, err := loadPRMeta(opts.prMetaPath)
	if err != nil {
		return err
	}
	// Eligibility guard first: reviewing a merged/closed PR wastes the entire
	// specialist budget and risks posting to a finished thread.
	if opts.requireOpen && meta.State != "OPEN" {
		return fmt.Errorf("PR #%d is %s — aborting", meta.Number, meta.State)
	}
	if opts.diffPath == "" {
		opts.diffPath = fmt.Sprintf("%s/pr-%d.diff", opts.reviewTmpDir, meta.Number)
	}

	// 1. Parse the diff → changed-files.json + valid-lines.json.
	df, err := os.Open(opts.diffPath)
	if err != nil {
		return fmt.Errorf("open diff: %w", err)
	}
	parsed, err := diffpkg.Parse(df)
	df.Close()
	if err != nil {
		return fmt.Errorf("parse diff: %w", err)
	}
	if err := writeJSON(opts.reviewTmpDir+"/changed-files.json", coalesce(parsed.ChangedFiles)); err != nil {
		return err
	}
	if err := writeJSON(opts.reviewTmpDir+"/valid-lines.json", parsed.ValidLines); err != nil {
		return err
	}

	// 2. CLAUDE.md ancestor walk + specialist roster.
	cmFiles, err := roster.ClaudeMdFiles(parsed.ChangedFiles, opts.repoRoot)
	if err != nil {
		return err
	}
	if err := writeJSON(opts.reviewTmpDir+"/claude-md-files.json", coalesce(cmFiles)); err != nil {
		return err
	}
	roles := roster.Build(parsed.ChangedFiles, len(cmFiles))
	if err := writeJSON(opts.reviewTmpDir+"/roster.json", roles); err != nil {
		return err
	}

	// 3. Project the GraphQL review threads into the prior-issues shape. This
	//    must land before bundle.Build, which reads prior-issues.json.
	priorIssues, err := priorIssuesFrom(opts.threadsPath, meta.Author.Login)
	if err != nil {
		return err
	}
	if err := writeJSON(opts.reviewTmpDir+"/prior-issues.json", priorIssues); err != nil {
		return err
	}

	// 4. spawn-context.md (+ the rubric copy it references).
	var summary string
	if opts.summaryInline != "" {
		b, err := os.ReadFile(opts.summaryInline)
		if err != nil {
			return fmt.Errorf("read summary paragraph: %w", err)
		}
		summary = string(b)
	}
	bundleStr, err := bundle.Build(bundle.Input{
		ReviewTmpDir:        opts.reviewTmpDir,
		HeadSHA:             meta.HeadRefOid,
		PRNumber:            meta.Number,
		Owner:               owner,
		Repo:                repo,
		RepoRoot:            opts.repoRoot,
		SummaryParagraph:    summary,
		RubricPath:          opts.rubric,
		RubricExternal:      opts.rubricOut,
		MaxSourceBytes:      opts.maxSourceB,
		MaxTotalSourceBytes: opts.maxTotalSrcB,
		GitWorkdir:          opts.gitWorkdir,
		DiffPath:            opts.diffPath,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(opts.reviewTmpDir+"/spawn-context.md", []byte(bundleStr), 0o644); err != nil {
		return fmt.Errorf("write spawn-context.md: %w", err)
	}

	// 5. One fully-rendered Agent payload per roster role.
	entries, err := buildSpawnEntries(roles, spawnSubs{
		reviewTmpDir: opts.reviewTmpDir,
		headSHA:      meta.HeadRefOid,
		prNumber:     meta.Number,
		owner:        owner,
		repo:         repo,
		repoRoot:     opts.repoRoot,
	})
	if err != nil {
		return err
	}
	if err := writeJSON(opts.reviewTmpDir+"/spawn-manifest.json", entries); err != nil {
		return err
	}

	// stdout is the orchestrator's progress line and its source for HEAD_SHA /
	// OWNER / REPO — shell-eval-safe `KEY=value` pairs, one per line.
	fmt.Printf("HEAD_SHA=%s\n", meta.HeadRefOid)
	fmt.Printf("OWNER=%s\n", owner)
	fmt.Printf("REPO=%s\n", repo)
	fmt.Printf("PR_NUMBER=%d\n", meta.Number)
	fmt.Printf("PR_STATE=%s\n", meta.State)
	fmt.Printf("PR_AUTHOR=%s\n", meta.Author.Login)
	fmt.Printf("ROSTER=%s\n", strings.Join(roles, ","))
	fmt.Printf("CHANGED_FILES=%d\n", len(parsed.ChangedFiles))
	fmt.Printf("PRIOR_ISSUES=%d\n", len(priorIssues.Issues))
	return nil
}

// findingMarkerRe and snippetRe mirror the hidden markers internal/render/issue.go
// embeds in every posted finding body. Keying prior-review dedup off these
// machine identities (rather than the human-readable header) is what keeps it
// working when the rendered wording changes.
var (
	findingMarkerRe = regexp.MustCompile(`<!-- cr-finding id="([a-f0-9]+)"`)
	snippetRe       = regexp.MustCompile(`snippet64="([A-Za-z0-9+/=]*)"`)
)

// reviewThreadsResponse is the shape of the GraphQL reviewThreads query the
// skill's step [1/5] issues.
type reviewThreadsResponse struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes []struct {
						ID         string `json:"id"`
						IsResolved bool   `json:"isResolved"`
						IsOutdated bool   `json:"isOutdated"`
						Comments   struct {
							Nodes []struct {
								DatabaseID        int    `json:"databaseId"`
								Body              string `json:"body"`
								Path              string `json:"path"`
								Line              *int   `json:"line"`
								OriginalLine      *int   `json:"originalLine"`
								OriginalStartLine *int   `json:"originalStartLine"`
								Author            struct {
									Login string `json:"login"`
								} `json:"author"`
							} `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

// priorIssuesFrom projects the GraphQL reviewThreads response into the
// PriorIssuesFile shape the dedup pass consumes. Only threads whose first
// comment carries this plugin's `<!-- cr-finding id=… -->` marker count — a
// human's review comment is not a prior finding.
//
// A missing threads file is not an error: a PR with no prior review has nothing
// to project, and the caller should not have to branch on that.
func priorIssuesFrom(path, prAuthor string) (dedup.PriorIssuesFile, error) {
	out := dedup.PriorIssuesFile{Issues: []dedup.PriorIssue{}}

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("read --review-threads: %w", err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return out, nil
	}

	var resp reviewThreadsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return out, fmt.Errorf("parse %s: %w", path, err)
	}

	for _, thread := range resp.Data.Repository.PullRequest.ReviewThreads.Nodes {
		if len(thread.Comments.Nodes) == 0 {
			continue
		}
		first := thread.Comments.Nodes[0]
		fp := findingMarkerRe.FindStringSubmatch(first.Body)
		if fp == nil {
			continue
		}

		issue := dedup.PriorIssue{
			Path:        first.Path,
			Line:        firstNonNil(first.Line, first.OriginalLine),
			StartLine:   firstNonNil(first.OriginalStartLine),
			Description: first.Body,
			IsResolved:  thread.IsResolved,
			IsOutdated:  thread.IsOutdated,
			Fingerprint: fp[1],
		}
		if m := snippetRe.FindStringSubmatch(first.Body); m != nil && m[1] != "" {
			// A snippet that won't decode is dropped rather than fatal: the
			// fingerprint and description arms still match without it.
			if decoded, err := base64.StdEncoding.DecodeString(m[1]); err == nil {
				issue.Snippet = string(decoded)
			}
		}
		// The PR author replying to a finding is how they dismiss it as a false
		// positive, so only replies (comments after the first) count.
		for _, reply := range thread.Comments.Nodes[1:] {
			if reply.Author.Login == prAuthor && prAuthor != "" {
				issue.AuthorDismissed = true
				break
			}
		}
		out.Issues = append(out.Issues, issue)
	}
	return out, nil
}

// firstNonNil returns the first non-nil pointer's value, or 0. GitHub nils
// `line` when it can't re-anchor a comment to the current diff, leaving
// `originalLine` as the only anchor.
func firstNonNil(candidates ...*int) int {
	for _, c := range candidates {
		if c != nil {
			return *c
		}
	}
	return 0
}
