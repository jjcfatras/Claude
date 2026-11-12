package roster

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestBuild_AlwaysOnOnly(t *testing.T) {
	got := Build([]string{"README.md"}, 0)
	want := []string{"security", "quality", "errors", "perf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestBuild_TypescriptReactInfraClaudeMd(t *testing.T) {
	files := []string{
		"src/app.tsx",
		"src/lib/util.ts",
		"db/migrations/0001_init.sql",
		"infra/Dockerfile",
	}
	got := Build(files, 2)
	want := []string{"security", "quality", "errors", "perf", "typescript", "react", "infra", "claude-md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestBuild_ReactOnlyOnJsx(t *testing.T) {
	got := Build([]string{"src/old.jsx"}, 0)
	want := []string{"security", "quality", "errors", "perf", "react"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestBuild_InfraVariants(t *testing.T) {
	cases := map[string]string{
		"foo.sql":                    "sql ext",
		"migrations/2024_add.sql":    "migrations dir (also matches sql)",
		"app/db/migrations/x.txt":    "db/migrations dir",
		"terraform/main.tf":          "tf ext (also matches terraform dir)",
		"infrastructure/setup.hcl":   "hcl ext",
		"deploy/prod/k8s/dep.yaml":   "deploy dir",
		"k8s/ns.yaml":                "k8s dir",
		"helm/chart/values.yaml":     "helm dir",
		"docker-compose.yml":         "docker-compose",
		"compose.yaml":               "compose.yaml",
		"build/Dockerfile.prod":      "Dockerfile prefix",
		"kubernetes/cluster/svc.yml": "kubernetes dir",
		".github/workflows/ci.yml":   "github actions workflow",
		"Jenkinsfile":                "jenkins",
		".circleci/config.yml":       "circleci",
		".buildkite/pipeline.yml":    "buildkite",
	}
	for p, label := range cases {
		got := Build([]string{p}, 0)
		if !slices.Contains(got, "infra") {
			t.Errorf("%s (%s) did not produce infra role; got %v", p, label, got)
		}
	}
}

func TestBuild_ConfigFileTriggers(t *testing.T) {
	// Config files that govern TS compilation or framework/bundler builds but
	// carry no .ts/.tsx/.jsx extension — previously these fell through to the
	// always-on roster only (the con-5 routing gap).
	cases := []struct {
		file      string
		wantTS    bool
		wantReact bool
	}{
		{"tsconfig.json", true, false},
		{"tsconfig.base.json", true, false},
		{"packages/web/jsconfig.json", true, false},
		{"next.config.js", true, true},
		{"vite.config.ts", true, true}, // also matches the .ts extension
		{"webpack.config.mjs", true, true},
		{"babel.config.json", true, true},
		{".babelrc", true, true},
		{"README.md", false, false},
	}
	for _, c := range cases {
		got := Build([]string{c.file}, 0)
		hasTS := slices.Contains(got, "typescript")
		hasReact := slices.Contains(got, "react")
		if hasTS != c.wantTS || hasReact != c.wantReact {
			t.Errorf("%s: got typescript=%v react=%v; want typescript=%v react=%v (roster %v)",
				c.file, hasTS, hasReact, c.wantTS, c.wantReact, got)
		}
	}
}

func TestClaudeMdFiles_WalkAndDedupe(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CLAUDE.md"), "root")
	mustWrite(t, filepath.Join(dir, "src", "CLAUDE.md"), "src")
	mustWrite(t, filepath.Join(dir, "src", "lib", "CLAUDE.md"), "lib")

	got, err := ClaudeMdFiles([]string{"src/lib/a.ts", "src/lib/b.ts", "src/other.ts"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"CLAUDE.md", "src/CLAUDE.md", "src/lib/CLAUDE.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestClaudeMdFiles_RootOnly(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CLAUDE.md"), "root")
	got, err := ClaudeMdFiles([]string{"deep/path/file.go"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"CLAUDE.md"}) {
		t.Fatalf("got %v want [CLAUDE.md]", got)
	}
}

func TestClaudeMdFiles_None(t *testing.T) {
	dir := t.TempDir()
	got, err := ClaudeMdFiles([]string{"x/y/z.ts"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestClaudeMdFiles_PathScopedRules(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CLAUDE.md"), "root")
	rules := filepath.Join(dir, ".claude", "rules")
	mustWrite(t, filepath.Join(rules, "api.md"), "---\npaths:\n  - \"src/api/**\"\n---\nbody")
	mustWrite(t, filepath.Join(rules, "always.md"), "no frontmatter, loads at startup")
	mustWrite(t, filepath.Join(rules, "docs.md"), "---\npaths: docs/**/*.md\n---\n")
	mustWrite(t, filepath.Join(rules, "nested", "ui.md"), "---\ndescription: x\npaths: [\"src/**/*.{tsx,jsx}\", 'other/**']\n---\n")
	mustWrite(t, filepath.Join(rules, "notes.txt"), "---\npaths: src/**\n---\n")

	got, err := ClaudeMdFiles([]string{"./src/api/handler.ts", "src/web/App.tsx"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".claude/rules/api.md", ".claude/rules/nested/ui.md", "CLAUDE.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestRulePaths(t *testing.T) {
	cases := map[string][]string{
		"---\npaths:\n  - src/**\n  - 'lib/*.ts'\nother: 1\n---\n": {"src/**", "lib/*.ts"},
		"---\r\npaths: [a/**, \"b/*.{ts,tsx}\"]\r\n---\r\n":        {"a/**", "b/*.{ts,tsx}"},
		"---\npaths: src/**/*.{ts,tsx}, docs/**\n---\n":            {"src/**/*.{ts,tsx}", "docs/**"},
		"---\ndescription: none\n---\npaths: body-only\n":          nil,
		"paths: no-frontmatter\n":                                  nil,
	}
	for doc, want := range cases {
		if got := rulePaths(doc); !reflect.DeepEqual(got, want) {
			t.Errorf("rulePaths(%q) = %v, want %v", doc, got, want)
		}
	}
}

func TestGlobRegexp(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"src/**", "src/a/b.ts", true},
		{"src/**/*.ts", "src/a.ts", true},
		{"src/**/*.ts", "src/a/b/c.ts", true},
		{"**/*.ts", "a.ts", true},
		{"src/*.ts", "src/a/b.ts", false},
		{"src/*.{ts,tsx}", "src/a.tsx", true},
		{"src/*.{ts,tsx}", "src/a.js", false},
		{"a?.md", "ab.md", true},
		{"a.md", "aXmd", false},
		{"src/components/**", "src/componentsX/a.ts", false},
	}
	for _, tc := range cases {
		re, err := globRegexp(tc.glob)
		if err != nil {
			t.Fatalf("globRegexp(%q): %v", tc.glob, err)
		}
		if got := re.MatchString(tc.path); got != tc.want {
			t.Errorf("glob %q vs %q = %v, want %v", tc.glob, tc.path, got, tc.want)
		}
	}
	if _, err := globRegexp("src/{a,b"); err == nil {
		t.Error("unbalanced brace should fail to compile")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
