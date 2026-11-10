package findings

import "testing"

func TestFingerprint_StableAndBounded(t *testing.T) {
	a := Fingerprint("src/x.ts", 42, "security")
	b := Fingerprint("src/x.ts", 42, "security")
	if a != b {
		t.Fatalf("fingerprint not stable: %s != %s", a, b)
	}
	if len(a) != 12 {
		t.Fatalf("want 12 hex chars, got %d (%q)", len(a), a)
	}
}

func TestFingerprint_SensitiveToEachComponent(t *testing.T) {
	base := Fingerprint("src/x.ts", 42, "security")
	if base == Fingerprint("src/x.ts", 43, "security") {
		t.Error("line change should change fingerprint")
	}
	if base == Fingerprint("src/y.ts", 42, "security") {
		t.Error("file change should change fingerprint")
	}
	if base == Fingerprint("src/x.ts", 42, "perf") {
		t.Error("category change should change fingerprint")
	}
}
