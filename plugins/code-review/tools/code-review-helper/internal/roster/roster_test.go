package roster

import (
	"os"
	"path/filepath"
	"reflect"
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
		hasInfra := false
		for _, r := range got {
			if r == "infra" {
				hasInfra = true
			}
		}
		if !hasInfra {
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
		hasTS, hasReact := false, false
		for _, r := range got {
			switch r {
			case "typescript":
				hasTS = true
			case "react":
				hasReact = true
			}
		}
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

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
