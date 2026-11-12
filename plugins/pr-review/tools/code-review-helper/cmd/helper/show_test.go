package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// seedGitRepo builds a throwaway repo with one commit and returns (root, sha).
func seedGitRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-qm", "seed")
	sha, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return root, strings.TrimSpace(string(sha))
}

func TestCachedHeadBlob_FetchesThenServesFromCache(t *testing.T) {
	root, sha := seedGitRepo(t, map[string]string{"src/lib/base.ts": "export const x = 1;\n"})
	tmp := t.TempDir()

	local, err := cachedHeadBlob(tmp, root, sha, "src/lib/base.ts")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	want := filepath.Join(tmp, "head", sha, "src", "lib", "base.ts")
	if local != want {
		t.Errorf("cache path = %q, want %q", local, want)
	}
	body, err := os.ReadFile(local)
	if err != nil || string(body) != "export const x = 1;\n" {
		t.Fatalf("cached content wrong: %q, err=%v", body, err)
	}

	// The point of the cache is that a second asker never reaches git. Prove it
	// by making git unreachable: with the repo root gone, a cache hit still
	// answers and a miss could not.
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	again, err := cachedHeadBlob(tmp, "/nonexistent-repo", sha, "src/lib/base.ts")
	if err != nil {
		t.Fatalf("second call should hit cache, got: %v", err)
	}
	if again != want {
		t.Errorf("second call path = %q, want %q", again, want)
	}
}

func TestCachedHeadBlob_MissingPathErrors(t *testing.T) {
	root, sha := seedGitRepo(t, map[string]string{"a.ts": "x\n"})
	if _, err := cachedHeadBlob(t.TempDir(), root, sha, "nope.ts"); err == nil {
		t.Fatal("expected an error for a path not in the commit")
	}
}

// A traversing path would let a specialist write outside $REVIEW_TMPDIR, which
// is the one directory Cleanup can remove.
func TestRunShow_RejectsUnsafePaths(t *testing.T) {
	for _, p := range []string{"../etc/passwd", "/etc/passwd", "a/../../b.ts"} {
		err := runShow([]string{
			"--review-tmpdir", t.TempDir(), "--repo-root", t.TempDir(),
			"--sha", "deadbeef", "--path", p,
		})
		if err == nil {
			t.Errorf("path %q should have been rejected", p)
		}
	}
}

func TestRunShow_RequiresAllFlags(t *testing.T) {
	if err := runShow([]string{"--sha", "deadbeef"}); err == nil {
		t.Fatal("expected an error when required flags are missing")
	}
}
