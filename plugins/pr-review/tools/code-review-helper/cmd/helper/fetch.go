package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// fetchPR is `prepare --fetch`: everything step [1/5] used to do as three
// model-typed Bash turns — create $REVIEW_TMPDIR, sweep the previous run's
// leftovers, fetch the PR — in one call. The three fetches run concurrently.
// Issued as sibling Bash blocks they ran serially anyway (the harness only
// parallelizes read-only Bash, and all three write files), and the shell they
// replace cost ~1.3k Opus output tokens per review to re-type.

// threadsQuery's first page is 50 threads × 50 comments, far above any real
// review; a PR that exceeds it needs a cursor-paginated loop here.
const threadsQuery = `query($owner:String!,$repo:String!,$pr:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$pr){reviewThreads(first:50){nodes{id isResolved isOutdated comments(first:50){nodes{databaseId author{login} body path line originalLine originalStartLine}}}}}}}`

// runCmd runs name+args in dir, streaming stdout to outPath when it is
// non-empty. On failure it removes outPath, so a half-written file can't pass
// for a fetched one, and returns stderr verbatim in the error. A var so tests
// can stub out gh and git.
var runCmd = func(dir, outPath, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if outPath != "" {
		f, err := os.Create(outPath)
		if err != nil {
			return err
		}
		defer f.Close()
		cmd.Stdout = f
	}
	if err := cmd.Run(); err != nil {
		if outPath != "" {
			os.Remove(outPath)
		}
		label := name + " " + strings.Join(args[:min(2, len(args))], " ")
		return fmt.Errorf("%s: %w: %s", label, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func fetchPR(o prepareOpts) error {
	if err := os.MkdirAll(filepath.Join(o.reviewTmpDir, "findings"), 0o755); err != nil {
		return fmt.Errorf("create review tmpdir: %w", err)
	}
	// Printed before anything can fail: Cleanup needs $TMP on every exit path.
	fmt.Printf("TMP=%s\n", o.reviewTmpDir)
	sweep(filepath.Dir(o.reviewTmpDir), o.repoRoot, o.pr)

	n := strconv.Itoa(o.pr)
	var wg sync.WaitGroup
	var metaErr, diffErr, refErr error
	wg.Add(3)
	go func() {
		defer wg.Done()
		metaErr = runCmd(o.repoRoot, o.prMetaPath, "gh", "pr", "view", n, "--json", "headRefOid,url,number,title,headRefName,baseRefName,author,state")
		if metaErr != nil {
			return
		}
		// The threads query needs owner/repo from pr-meta.json, so it chains
		// here instead of running alongside. Failure is not fatal: a missing
		// threads file means "no prior review" and prior-review dedup does nothing.
		_, owner, repo, err := loadPRMeta(o.prMetaPath)
		if err == nil {
			err = runCmd(o.repoRoot, o.threadsPath, "gh", "api", "graphql", "-F", "owner="+owner, "-F", "repo="+repo, "-F", "pr="+n, "-f", "query="+threadsQuery)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: prior review threads unavailable, prior-review dedup skipped: %v\n", err)
		}
	}()
	go func() {
		defer wg.Done()
		diffErr = runCmd(o.repoRoot, o.diffPath, "gh", "pr", "diff", n)
	}()
	go func() {
		defer wg.Done()
		// The hidden ref makes HEAD_SHA's objects resolvable on a stale clone or
		// a fork PR. Failure (offline, restricted remote) only degrades
		// specialists to the bundle's embedded source, so warn and carry on.
		if refErr = runCmd(o.repoRoot, "", "git", "fetch", "origin", "+pull/"+n+"/head:refs/code-review/pr-"+n, "--no-tags"); refErr != nil {
			fmt.Fprintf(os.Stderr, "warning: %v — specialists fall back to the bundle's embedded source\n", refErr)
		}
	}()
	wg.Wait()
	if diffErr != nil && metaErr == nil && refErr == nil && diffTooLarge(diffErr) {
		diffErr = localDiff(o, n, diffErr)
	}
	return errors.Join(metaErr, diffErr)
}

// diffTooLarge matches GitHub's refusal to serve a PR diff over 20,000 lines
// (`HTTP 406 … PullRequest.diff too_large`). Transcript f6147551: a 48.5k-line
// PR hard-failed here, and the orchestrator rebuilt --fetch's outputs by hand.
func diffTooLarge(err error) bool {
	s := err.Error()
	return strings.Contains(s, "too_large") || strings.Contains(s, "HTTP 406")
}

// localDiff builds the PR diff from the hidden ref the concurrent fetch just
// wrote, against the merge base with the PR's base branch — GitHub's own
// three-dot semantics, so valid-lines and inline posting still line up. The
// base fetch is best-effort: a stale origin/<base> only widens the diff. The
// explicit prefixes pin the a/ b/ form the diff parser expects, whatever the
// user's diff.noprefix / diff.mnemonicPrefix config says. On failure it returns
// GitHub's original error with the local one attached.
func localDiff(o prepareOpts, n string, ghErr error) error {
	meta, _, _, err := loadPRMeta(o.prMetaPath)
	if err == nil && meta.BaseRefName == "" {
		err = errors.New("pr-meta.json has no baseRefName")
	}
	if err != nil {
		return errors.Join(ghErr, fmt.Errorf("local diff fallback: %w", err))
	}
	base := meta.BaseRefName
	if err := runCmd(o.repoRoot, "", "git", "fetch", "origin", base, "--no-tags"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v — diffing against the existing origin/%s\n", err, base)
	}
	if err := runCmd(o.repoRoot, o.diffPath, "git", "diff", "--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/", "origin/"+base+"...refs/code-review/pr-"+n); err != nil {
		return errors.Join(ghErr, fmt.Errorf("local diff fallback: %w", err))
	}
	fmt.Fprintf(os.Stderr, "warning: gh pr diff refused the PR as too large; built %s locally from origin/%s...refs/code-review/pr-%s\n", filepath.Base(o.diffPath), base, n)
	return nil
}

// sweep reaps what a previous run's skipped Cleanup left behind (see SKILL.md
// § Cleanup). It removes pr-review-* dirs directly under tmpRoot that are at
// least 48h old (`find -mtime +1` semantics, so no review started today or
// yesterday is touched). It also deletes refs/code-review/pr-<n> refs with no
// surviving pr-review-<n>-* dir, except the PR under review, whose ref is about
// to be force-refetched. Best-effort: errors are ignored, as the shell's
// `2>/dev/null` did.
func sweep(tmpRoot, repoRoot string, pr int) {
	entries, _ := os.ReadDir(tmpRoot)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "pr-review-") {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) >= 48*time.Hour {
			os.RemoveAll(filepath.Join(tmpRoot, e.Name()))
		}
	}
	out, err := exec.Command("git", "-C", repoRoot, "for-each-ref", "--format=%(refname)", "refs/code-review/").Output()
	if err != nil {
		return
	}
	for ref := range strings.FieldsSeq(string(out)) {
		n := strings.TrimPrefix(ref, "refs/code-review/pr-")
		if n == strconv.Itoa(pr) {
			continue
		}
		if live, _ := filepath.Glob(filepath.Join(tmpRoot, "pr-review-"+n+"-*")); len(live) == 0 {
			exec.Command("git", "-C", repoRoot, "update-ref", "-d", ref).Run()
		}
	}
}
