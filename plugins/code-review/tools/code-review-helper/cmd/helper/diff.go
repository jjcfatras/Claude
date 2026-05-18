package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	diffpkg "github.com/jjcfatras/claude-tools/code-review-helper/internal/diff"
)

// runDiff: parse a unified diff and emit the changed-files list and valid-line map.
func runDiff(argv []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	in := fs.String("in", "-", "path to unified diff (or '-' for stdin)")
	outChanged := fs.String("out-changed-files", "", "path to write changed-files JSON array")
	outValid := fs.String("out-valid-lines", "", "path to write valid-lines map")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *outChanged == "" || *outValid == "" {
		return fmt.Errorf("diff: --out-changed-files and --out-valid-lines are required")
	}

	var r io.Reader
	if *in == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(*in)
		if err != nil {
			return fmt.Errorf("open --in: %w", err)
		}
		defer f.Close()
		r = f
	}

	parsed, err := diffpkg.Parse(r)
	if err != nil {
		return fmt.Errorf("parse diff: %w", err)
	}

	if err := writeJSON(*outChanged, parsed.ChangedFiles); err != nil {
		return err
	}
	if err := writeJSON(*outValid, parsed.ValidLines); err != nil {
		return err
	}
	return nil
}
