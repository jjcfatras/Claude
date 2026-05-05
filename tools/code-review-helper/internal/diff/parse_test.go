package diff

import (
	"strings"
	"testing"
)

func parseString(t *testing.T, s string) *Parsed {
	t.Helper()
	p, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return p
}

func TestParse_SingleHunk(t *testing.T) {
	in := `diff --git a/src/foo.ts b/src/foo.ts
index abc..def 100644
--- a/src/foo.ts
+++ b/src/foo.ts
@@ -10,5 +10,8 @@ ctx
 ctx
-removed
+added
+added
+added
 ctx
`
	p := parseString(t, in)
	if got, want := p.ChangedFiles, []string{"src/foo.ts"}; !equal(got, want) {
		t.Errorf("changed files: got %v want %v", got, want)
	}
	runs := p.ValidLines["src/foo.ts"]
	if len(runs) != 1 || runs[0].Start != 10 || runs[0].End != 17 {
		t.Errorf("runs: got %+v want [{10 17}]", runs)
	}
	// Hunk body in the new file:
	//   line 10: " ctx"   (context)
	//   line 11: "+added" (added)
	//   line 12: "+added" (added)
	//   line 13: "+added" (added)
	//   line 14: " ctx"   (context)
	// (The `-removed` doesn't consume a new-version line.)
	wantAdded := map[int]bool{11: true, 12: true, 13: true}
	got := p.AddedLines["src/foo.ts"]
	if len(got) != len(wantAdded) {
		t.Fatalf("AddedLines: got %v want %v", got, wantAdded)
	}
	for k := range wantAdded {
		if !got[k] {
			t.Errorf("AddedLines missing %d", k)
		}
	}
	if p.IsAddedLine("src/foo.ts", 10) {
		t.Errorf("line 10 is context, not added")
	}
	if !p.IsAddedLine("src/foo.ts", 11) {
		t.Errorf("line 11 is added")
	}
}

func TestParse_MultiHunk(t *testing.T) {
	in := `diff --git a/x.go b/x.go
--- a/x.go
+++ b/x.go
@@ -1,3 +1,4 @@
 a
+b
 c
 d
@@ -50 +52 @@
-old
+new
@@ -100,2 +103 @@
-x
-y
+z
`
	p := parseString(t, in)
	runs := p.ValidLines["x.go"]
	want := []Run{{1, 4}, {52, 52}, {103, 103}}
	if len(runs) != len(want) {
		t.Fatalf("runs len: got %d want %d (%+v)", len(runs), len(want), runs)
	}
	for i := range want {
		if runs[i] != want[i] {
			t.Errorf("run %d: got %+v want %+v", i, runs[i], want[i])
		}
	}
}

func TestParse_BinaryFile(t *testing.T) {
	in := `diff --git a/img.png b/img.png
index abc..def 100644
Binary files a/img.png and b/img.png differ
`
	p := parseString(t, in)
	if got, want := p.ChangedFiles, []string{"img.png"}; !equal(got, want) {
		t.Errorf("changed files: got %v want %v", got, want)
	}
	if _, ok := p.ValidLines["img.png"]; ok {
		t.Errorf("binary file should not be in valid-lines map")
	}
}

func TestParse_PureRename(t *testing.T) {
	in := `diff --git a/old.txt b/new.txt
similarity index 100%
rename from old.txt
rename to new.txt
`
	p := parseString(t, in)
	if got, want := p.ChangedFiles, []string{"new.txt"}; !equal(got, want) {
		t.Errorf("changed files: got %v want %v", got, want)
	}
	if _, ok := p.ValidLines["new.txt"]; ok {
		t.Errorf("pure rename should not be in valid-lines map")
	}
}

func TestParse_Deletion(t *testing.T) {
	in := `diff --git a/gone.txt b/gone.txt
deleted file mode 100644
--- a/gone.txt
+++ /dev/null
@@ -1,3 +0,0 @@
-a
-b
-c
`
	p := parseString(t, in)
	if got, want := p.ChangedFiles, []string{"gone.txt"}; !equal(got, want) {
		t.Errorf("changed files: got %v want %v", got, want)
	}
	if _, ok := p.ValidLines["gone.txt"]; ok {
		t.Errorf("deleted file should not be in valid-lines map")
	}
}

func TestParse_NewFile(t *testing.T) {
	in := `diff --git a/new.go b/new.go
new file mode 100644
index 0000000..abcdef
--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+package new
+
+func main() {}
`
	p := parseString(t, in)
	runs := p.ValidLines["new.go"]
	if len(runs) != 1 || runs[0].Start != 1 || runs[0].End != 3 {
		t.Errorf("runs: got %+v want [{1 3}]", runs)
	}
}

func TestParse_RenameWithEdits(t *testing.T) {
	in := `diff --git a/old.go b/new.go
similarity index 80%
rename from old.go
rename to new.go
--- a/old.go
+++ b/new.go
@@ -5,2 +5,3 @@
 keep
+added
 keep
`
	p := parseString(t, in)
	if got, want := p.ChangedFiles, []string{"new.go"}; !equal(got, want) {
		t.Errorf("changed files: got %v want %v", got, want)
	}
	if _, ok := p.ValidLines["old.go"]; ok {
		t.Errorf("old path leaked into valid-lines")
	}
	runs := p.ValidLines["new.go"]
	if len(runs) != 1 || runs[0].Start != 5 || runs[0].End != 7 {
		t.Errorf("runs: got %+v want [{5 7}]", runs)
	}
}

func TestParse_ManyFiles(t *testing.T) {
	in := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1 +1,2 @@
 a
+b
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -10 +20 @@
-x
+y
diff --git a/img.bin b/img.bin
Binary files a/img.bin and b/img.bin differ
`
	p := parseString(t, in)
	if got, want := p.ChangedFiles, []string{"a.go", "b.go", "img.bin"}; !equal(got, want) {
		t.Errorf("changed files: got %v want %v", got, want)
	}
}

func TestInRangeAndNearest(t *testing.T) {
	p := &Parsed{ValidLines: map[string][]Run{
		"x.go": {{10, 15}, {30, 33}},
	}}
	if !p.InRange("x.go", 12) {
		t.Errorf("12 should be in range")
	}
	if p.InRange("x.go", 20) {
		t.Errorf("20 should not be in range")
	}
	if got, ok := p.NearestValid("x.go", 12); !ok || got != 12 {
		t.Errorf("nearest(12) = (%d,%v) want (12,true)", got, ok)
	}
	if got, ok := p.NearestValid("x.go", 20); !ok || got != 15 {
		t.Errorf("nearest(20) = (%d,%v) want (15,true)", got, ok)
	}
	if got, ok := p.NearestValid("x.go", 28); !ok || got != 30 {
		t.Errorf("nearest(28) = (%d,%v) want (30,true)", got, ok)
	}
	if _, ok := p.NearestValid("y.go", 1); ok {
		t.Errorf("missing path should report no run")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
