package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestLoadPRMeta(t *testing.T) {
	dir := t.TempDir()

	t.Run("extracts sha, owner, repo", func(t *testing.T) {
		path := writeFile(t, dir, "ok.json", `{
			"headRefOid": "d7f348e9148a6e6320b038e44705527d4546841b",
			"url": "https://github.com/FS-Main/fairsquare/pull/1727",
			"number": 1727,
			"state": "OPEN",
			"author": {"login": "someone"}
		}`)
		meta, owner, repo, err := loadPRMeta(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if owner != "FS-Main" || repo != "fairsquare" {
			t.Errorf("want FS-Main/fairsquare, got %s/%s", owner, repo)
		}
		if meta.HeadRefOid != "d7f348e9148a6e6320b038e44705527d4546841b" || meta.Number != 1727 {
			t.Errorf("unexpected meta: %+v", meta)
		}
	})

	t.Run("a repo named pull does not shift the capture", func(t *testing.T) {
		path := writeFile(t, dir, "pull-repo.json", `{"headRefOid":"abc","url":"https://github.com/acme/pull/pull/12","number":12,"state":"OPEN"}`)
		_, owner, repo, err := loadPRMeta(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if owner != "acme" || repo != "pull" {
			t.Errorf("want acme/pull, got %s/%s", owner, repo)
		}
	})

	t.Run("empty headRefOid is a hard error", func(t *testing.T) {
		path := writeFile(t, dir, "no-sha.json", `{"url":"https://github.com/o/r/pull/1","number":1,"state":"OPEN"}`)
		if _, _, _, err := loadPRMeta(path); err == nil {
			t.Error("want error when headRefOid is missing")
		}
	})

	t.Run("unparseable url is a hard error", func(t *testing.T) {
		path := writeFile(t, dir, "bad-url.json", `{"headRefOid":"abc","url":"https://example.com/nope","number":1,"state":"OPEN"}`)
		if _, _, _, err := loadPRMeta(path); err == nil {
			t.Error("want error when owner/repo can't be extracted")
		}
	})
}

func TestPriorIssuesFrom(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing file yields an empty set, not an error", func(t *testing.T) {
		got, err := priorIssuesFrom(filepath.Join(dir, "absent.json"), "author")
		if err != nil {
			t.Fatalf("a PR with no prior review must not error: %v", err)
		}
		if len(got.Issues) != 0 {
			t.Errorf("want no issues, got %d", len(got.Issues))
		}
		// Must marshal as [] rather than null so the dedup pass sees a real slice.
		b, _ := json.Marshal(got)
		if want := `{"last_review_date":null,"last_review_commit":null,"issues":[]}`; string(b) != want {
			t.Errorf("want %s, got %s", want, b)
		}
	})

	t.Run("only threads carrying the cr-finding marker count", func(t *testing.T) {
		path := writeFile(t, dir, "threads.json", `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
			{"isResolved":false,"isOutdated":false,"comments":{"nodes":[
				{"body":"just a human comment","path":"a.ts","line":10,"author":{"login":"reviewer"}}
			]}},
			{"isResolved":true,"isOutdated":false,"comments":{"nodes":[
				{"body":"finding one\n<!-- cr-finding id=\"abc123def456\" snippet64=\"Y29uc3QgeCA9IDE=\" -->","path":"b.ts","line":42,"author":{"login":"claude"}}
			]}}
		]}}}}}`)
		got, err := priorIssuesFrom(path, "someone")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.Issues) != 1 {
			t.Fatalf("want 1 issue (the human comment is not a prior finding), got %d", len(got.Issues))
		}
		issue := got.Issues[0]
		if issue.Fingerprint != "abc123def456" {
			t.Errorf("want fingerprint abc123def456, got %q", issue.Fingerprint)
		}
		if issue.Snippet != "const x = 1" {
			t.Errorf("want decoded snippet, got %q", issue.Snippet)
		}
		if issue.Path != "b.ts" || issue.Line != 42 || !issue.IsResolved {
			t.Errorf("unexpected projection: %+v", issue)
		}
	})

	t.Run("line falls back to originalLine when GitHub cannot re-anchor", func(t *testing.T) {
		path := writeFile(t, dir, "unanchored.json", `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
			{"comments":{"nodes":[
				{"body":"<!-- cr-finding id=\"aaaaaaaaaaaa\" -->","path":"c.ts","line":null,"originalLine":77,"originalStartLine":75,"author":{"login":"claude"}}
			]}}
		]}}}}}`)
		got, err := priorIssuesFrom(path, "someone")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Issues[0].Line != 77 || got.Issues[0].StartLine != 75 {
			t.Errorf("want line=77 start_line=75, got line=%d start_line=%d", got.Issues[0].Line, got.Issues[0].StartLine)
		}
	})

	t.Run("author_dismissed is set only by a reply from the PR author", func(t *testing.T) {
		mk := func(replyLogin string) string {
			return `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
				{"comments":{"nodes":[
					{"body":"<!-- cr-finding id=\"bbbbbbbbbbbb\" -->","path":"d.ts","line":5,"author":{"login":"claude"}},
					{"body":"not a real issue","author":{"login":"` + replyLogin + `"}}
				]}}
			]}}}}}`
		}
		byAuthor := writeFile(t, dir, "dismissed.json", mk("pr-author"))
		got, err := priorIssuesFrom(byAuthor, "pr-author")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.Issues[0].AuthorDismissed {
			t.Error("a reply from the PR author must set author_dismissed")
		}

		byOther := writeFile(t, dir, "not-dismissed.json", mk("someone-else"))
		got, err = priorIssuesFrom(byOther, "pr-author")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Issues[0].AuthorDismissed {
			t.Error("a reply from a third party must not set author_dismissed")
		}
	})

	t.Run("an undecodable snippet is dropped, not fatal", func(t *testing.T) {
		path := writeFile(t, dir, "bad-snippet.json", `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
			{"comments":{"nodes":[
				{"body":"<!-- cr-finding id=\"cccccccccccc\" snippet64=\"!!!notbase64\" -->","path":"e.ts","line":1,"author":{"login":"claude"}}
			]}}
		]}}}}}`)
		got, err := priorIssuesFrom(path, "author")
		if err != nil {
			t.Fatalf("want the issue kept without its snippet, got error: %v", err)
		}
		if len(got.Issues) != 1 || got.Issues[0].Snippet != "" {
			t.Errorf("want 1 issue with empty snippet, got %+v", got.Issues)
		}
	})
}

func TestParsePrepareArgs_Defaults(t *testing.T) {
	opts, err := parsePrepareArgs([]string{
		"--review-tmpdir", "/tmp/rev", "--repo-root", "/repo", "--rubric", "/r/rubrics.md",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.prMetaPath != "/tmp/rev/pr-meta.json" {
		t.Errorf("pr-meta should default under review-tmpdir, got %q", opts.prMetaPath)
	}
	if opts.threadsPath != "/tmp/rev/review-threads.json" {
		t.Errorf("review-threads should default under review-tmpdir, got %q", opts.threadsPath)
	}
	if opts.rubricOut != "/tmp/rev/rubric.md" {
		t.Errorf("rubric-out should default under review-tmpdir, got %q", opts.rubricOut)
	}
	if opts.gitWorkdir != "/repo" {
		t.Errorf("git-workdir should default to repo-root, got %q", opts.gitWorkdir)
	}
	if !opts.requireOpen {
		t.Error("require-open should default to true")
	}
}

func TestParsePrepareArgs_MissingRequired(t *testing.T) {
	if _, err := parsePrepareArgs([]string{"--review-tmpdir", "/tmp/rev"}); err == nil {
		t.Error("want error when --repo-root and --rubric are absent")
	}
}
