// Command jsonfmt rewrites JSON files in place with two-space indentation,
// the layout json.MarshalIndent produces. Only whitespace changes: key order,
// string escapes and number spellings are kept byte-for-byte.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

func format(src []byte) ([]byte, error) {
	var out bytes.Buffer
	if err := json.Indent(&out, bytes.TrimSpace(src), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func formatFile(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := format(src)
	if err != nil || bytes.Equal(src, out) {
		return err
	}
	return os.WriteFile(path, out, 0o644) // perm only applies on create; existing modes are kept
}

func main() {
	failed := false
	for _, path := range os.Args[1:] {
		if err := formatFile(path); err != nil {
			fmt.Fprintf(os.Stderr, "jsonfmt: %s: %v\n", path, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}
