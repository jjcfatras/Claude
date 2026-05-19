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
		"build/Dockerfile.prod":      "Dockerfile prefix",
		"kubernetes/cluster/svc.yml": "kubernetes dir",
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
