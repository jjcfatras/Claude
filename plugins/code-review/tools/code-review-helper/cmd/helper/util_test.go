package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteJSON_WritesIndentedWithTrailingNewline(t *testing.T) {
	out := filepath.Join(t.TempDir(), "obj.json")

	if err := writeJSON(out, map[string]string{"k": "v"}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	want := "{\n  \"k\": \"v\"\n}\n"
	if string(got) != want {
		t.Errorf("output mismatch\n got: %q\nwant: %q", got, want)
	}

	var rt map[string]string
	if err := json.Unmarshal(got, &rt); err != nil {
		t.Errorf("output not valid JSON: %v", err)
	}
}

func TestWriteJSON_MarshalErrorWrapsPath(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bad.json")
	err := writeJSON(out, make(chan int))
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if !strings.Contains(err.Error(), out) {
		t.Errorf("error should mention path %q; got %v", out, err)
	}
}

func TestWriteJSON_WriteErrorWrapsPath(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "no-such-dir", "x.json")
	err := writeJSON(bad, map[string]string{"a": "b"})
	if err == nil {
		t.Fatal("expected write error")
	}
	if !strings.Contains(err.Error(), bad) {
		t.Errorf("error should mention path %q; got %v", bad, err)
	}
}
