package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/roster"
)

func runRoster(argv []string) error {
	fs := flag.NewFlagSet("roster", flag.ContinueOnError)
	in := fs.String("changed-files", "", "path to changed-files JSON array (from `diff`)")
	repoRoot := fs.String("repo-root", "", "absolute path to the repo root")
	outClaudeMd := fs.String("out-claude-md-files", "", "path to write claude-md-files JSON array")
	outRoster := fs.String("out-roster", "", "path to write roster JSON array of role strings")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *in == "" || *repoRoot == "" || *outClaudeMd == "" || *outRoster == "" {
		return fmt.Errorf("roster: --changed-files, --repo-root, --out-claude-md-files, --out-roster are required")
	}

	raw, err := os.ReadFile(*in)
	if err != nil {
		return fmt.Errorf("read --changed-files: %w", err)
	}
	var changed []string
	if err := json.Unmarshal(raw, &changed); err != nil {
		return fmt.Errorf("parse --changed-files: %w", err)
	}

	cmFiles, err := roster.ClaudeMdFiles(changed, *repoRoot)
	if err != nil {
		return err
	}
	// nil → [] so the JSON output is [] not null
	if cmFiles == nil {
		cmFiles = []string{}
	}
	if err := writeJSON(*outClaudeMd, cmFiles); err != nil {
		return err
	}

	r := roster.Build(changed, len(cmFiles))
	if err := writeJSON(*outRoster, r); err != nil {
		return err
	}
	return nil
}
