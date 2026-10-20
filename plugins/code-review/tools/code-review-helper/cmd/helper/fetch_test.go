package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubRunCmd replaces gh/git with canned outputs, failing any command whose
// "name sub sub" key is in fail. It returns the recorded calls.
func stubRunCmd(t *testing.T, fail ...string) func() []string {
	t.Helper()
	orig := runCmd
	t.Cleanup(func() { runCmd = orig })
	bodies := map[string]string{
		"gh pr view":     `{"headRefOid":"abc","url":"https://github.com/o/r/pull/7","number":7,"state":"OPEN"}`,
		"gh pr diff":     "diff --git a/x b/x\n",
		"gh api graphql": "{}",
	}
	var mu sync.Mutex
	var calls []string
	runCmd = func(dir, outPath, name string, args ...string) error {
		key := name + " " + strings.Join(args[:2], " ")
		mu.Lock()
		calls = append(calls, name+" "+strings.Join(args, " "))
		mu.Unlock()
		if slices.Contains(fail, key) {
			return fmt.Errorf("%s failed", key)
		}
		if outPath != "" {
			return os.WriteFile(outPath, []byte(bodies[key]), 0o644)
		}
		return nil
	}
	return func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(calls) }
}

func fetchOpts(t *testing.T) prepareOpts {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "pr-review-7-1")
	o, err := parsePrepareArgs([]string{"--fetch", "--pr", "7", "--review-tmpdir", tmp, "--repo-root", t.TempDir(), "--rubric", "/r.md"})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestFetchPR(t *testing.T) {
	cases := []struct {
		name        string
		fail        []string
		wantErr     bool
		wantThreads bool
	}{
		{name: "all succeed", wantThreads: true},
		{name: "git fetch failure only warns", fail: []string{"git fetch origin"}, wantThreads: true},
		{name: "threads failure only warns", fail: []string{"gh api graphql"}},
		{name: "pr view failure is fatal", fail: []string{"gh pr view"}, wantErr: true},
		{name: "pr diff failure is fatal", fail: []string{"gh pr diff"}, wantErr: true, wantThreads: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := stubRunCmd(t, tc.fail...)
			o := fetchOpts(t)
			err := fetchPR(o)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if _, err := os.Stat(filepath.Join(o.reviewTmpDir, "findings")); err != nil {
				t.Errorf("findings dir not created: %v", err)
			}
			if _, err := os.Stat(o.threadsPath); (err == nil) != tc.wantThreads {
				t.Errorf("threads file present = %v, want %v", err == nil, tc.wantThreads)
			}
			if tc.wantThreads {
				i := slices.IndexFunc(calls(), func(c string) bool { return strings.HasPrefix(c, "gh api graphql") })
				if i < 0 || !strings.Contains(calls()[i], "owner=o -F repo=r -F pr=7") {
					t.Errorf("threads query not issued with owner/repo from pr-meta: %v", calls())
				}
			}
		})
	}
}

func TestRunCmd_RemovesPartialOutputOnFailure(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	err := runCmd("", out, "sh", "-c", "echo partial; echo boom >&2; exit 1")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want error carrying stderr, got %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("partial output should be removed, stat err = %v", err)
	}
	if err := runCmd("", out, "sh", "-c", "echo ok"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != "ok\n" {
		t.Errorf("stdout = %q", b)
	}
}

func TestSweep(t *testing.T) {
	root, _ := seedGitRepo(t, map[string]string{"f": "x"})
	tmp := t.TempDir()
	age := func(name string, d time.Duration) {
		p := filepath.Join(tmp, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		ts := time.Now().Add(-d)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	age("pr-review-1-100", 49*time.Hour) // reaped
	age("pr-review-2-200", 30*time.Hour) // under 48h: kept
	age("unrelated", 100*time.Hour)      // not ours: kept
	for _, n := range []string{"1", "2", "5", "9"} {
		if out, err := exec.Command("git", "-C", root, "update-ref", "refs/code-review/pr-"+n, "HEAD").CombinedOutput(); err != nil {
			t.Fatalf("update-ref: %v\n%s", err, out)
		}
	}

	sweep(tmp, root, 9)

	for name, want := range map[string]bool{"pr-review-1-100": false, "pr-review-2-200": true, "unrelated": true} {
		if _, err := os.Stat(filepath.Join(tmp, name)); (err == nil) != want {
			t.Errorf("%s present = %v, want %v", name, err == nil, want)
		}
	}
	out, err := exec.Command("git", "-C", root, "for-each-ref", "--format=%(refname)", "refs/code-review/").Output()
	if err != nil {
		t.Fatal(err)
	}
	// pr-1: dir reaped → deleted. pr-2: dir alive → kept. pr-5: no dir → deleted. pr-9: PR under review → kept.
	if got, want := strings.Fields(string(out)), []string{"refs/code-review/pr-2", "refs/code-review/pr-9"}; !slices.Equal(got, want) {
		t.Errorf("refs = %v, want %v", got, want)
	}
}

func TestParsePrepareArgs_Fetch(t *testing.T) {
	base := []string{"--review-tmpdir", "/tmp/rev", "--repo-root", "/repo", "--rubric", "/r.md", "--fetch"}
	if _, err := parsePrepareArgs(base); err == nil {
		t.Error("want error when --fetch has no --pr")
	}
	o, err := parsePrepareArgs(append(base, "--pr", "7"))
	if err != nil {
		t.Fatal(err)
	}
	if o.diffPath != "/tmp/rev/pr-7.diff" {
		t.Errorf("diff should default from --pr under --fetch, got %q", o.diffPath)
	}
}
