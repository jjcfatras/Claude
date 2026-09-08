// `show` is the specialists' shared read-through cache for HEAD blobs.
//
// Why it exists: specialists scan independently and never talk to each other,
// which is deliberate — but that isolation was also making each of them pay its
// own `git show` for the same unchanged callee. On transcript d67f09e0 (PR
// #1817) 21 of 46 specialist Bash calls were re-derivation, and 19 of those 21
// targeted files outside the changed set, so no amount of bundle tuning could
// have reached them: `base/controller.ts` was fetched 5 times by 4 specialists,
// `banking-asset-report.service.ts` 6 times by 4.
//
// A blob cache shares repository bytes, never opinions, so the independence
// invariant in references/code-review-rubrics.md is untouched — specialists
// already share spawn-context.md, rubric.md and the diff by exactly this
// mechanism. The key is `<sha>:<path>`, which is immutable content-addressing:
// a cache entry cannot go stale, only missing.
//
// The output is a path, not the content. That keeps this subcommand cheap for
// the caller (one short line, whatever the file's size) and hands the paging
// decision back to Read's offset/limit, which is where it belongs.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runShow(argv []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	reviewTmpDir := fs.String("review-tmpdir", "", "path to $REVIEW_TMPDIR; the cache lives at <dir>/head/<sha>/")
	repoRoot := fs.String("repo-root", "", "repo working-tree root — the cwd for `git show`")
	sha := fs.String("sha", "", "full HEAD SHA")
	path := fs.String("path", "", "repo-relative path to fetch")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *reviewTmpDir == "" || *repoRoot == "" || *sha == "" || *path == "" {
		return fmt.Errorf("show: --review-tmpdir, --repo-root, --sha, --path are all required")
	}
	if filepath.IsAbs(*path) || strings.Contains(*path, "..") {
		return fmt.Errorf("show: --path must be repo-relative and free of `..` (got %q)", *path)
	}

	local, err := cachedHeadBlob(*reviewTmpDir, *repoRoot, *sha, *path)
	if err != nil {
		return err
	}
	fmt.Println(local)
	return nil
}

// cachedHeadBlob returns the local path holding <sha>:<path>, fetching it only
// on a miss. Concurrency is the normal case here — the whole roster is spawned
// in one message, so several specialists can ask for the same blob within the
// same second — hence the write goes to a unique temp file and is published
// with rename(2), which is atomic on the same filesystem. A reader therefore
// sees either no file or a complete one, never a half-written prefix.
func cachedHeadBlob(reviewTmpDir, repoRoot, sha, path string) (string, error) {
	dst := filepath.Join(reviewTmpDir, "head", sha, filepath.FromSlash(path))
	if st, err := os.Stat(dst); err == nil && st.Mode().IsRegular() {
		return dst, nil
	}

	out, err := exec.Command("git", "-C", repoRoot, "show", sha+":"+path).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("git show %s:%s: %s", sha, path, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git show %s:%s: %w", sha, path, err)
	}

	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".show-*")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	// No-op once the rename below publishes the entry; on every failure path
	// below it is the cleanup.
	defer os.Remove(tmpName)
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return "", fmt.Errorf("publish cache entry: %w", err)
	}
	return dst, nil
}
