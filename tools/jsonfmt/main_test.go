package main

import "testing"

func TestFormat(t *testing.T) {
	in := `{"a": {"b": [1, 2.50]}, "e": "\u00e9—", "z": []}`
	want := "{\n  \"a\": {\n    \"b\": [\n      1,\n      2.50\n    ]\n  },\n  \"e\": \"\\u00e9—\",\n  \"z\": []\n}\n"
	got, err := format([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	again, err := format(got)
	if err != nil || string(again) != want {
		t.Fatalf("not idempotent: %q, %v", again, err)
	}
}

func TestFormatInvalid(t *testing.T) {
	for _, in := range []string{`{"a": }`, `{} {}`, ``} {
		if _, err := format([]byte(in)); err == nil {
			t.Errorf("format(%q): want error", in)
		}
	}
}
