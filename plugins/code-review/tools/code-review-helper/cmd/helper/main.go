// code-review-helper is the deterministic backend for the /code-review plugin.
// It owns diff parsing, roster construction, the dedup + gate + snap pipeline,
// and final payload assembly. The skill drives it with two subcommands —
// `prepare` (everything derivable from the fetched PR) and `finalize` (dedup →
// gate → snap → render); the four single-stage subcommands `prepare` composes
// remain available for debugging one stage in isolation. See the package
// documentation for each subcommand for the exact contract.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	if cmd == "-h" || cmd == "--help" || cmd == "help" {
		usage()
		return
	}

	subcommands := map[string]func([]string) error{
		"prepare":        runPrepare,
		"diff":           runDiff,
		"roster":         runRoster,
		"finalize":       runFinalize,
		"bundle-context": runBundleContext,
		"spawn-manifest": runSpawnManifest,
	}
	run, ok := subcommands[cmd]
	if !ok {
		fmt.Fprintf(os.Stderr, "code-review-helper: unknown subcommand %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err := run(args); err != nil {
		fail(err)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `code-review-helper — deterministic backend for the /code-review plugin

Usage:
  code-review-helper prepare         [flags]   # diff + roster + prior-issues + bundle + manifest
  code-review-helper finalize        [flags]   # dedup + gate + snap + render

Single-stage subcommands (composed by prepare; useful for debugging one stage):
  code-review-helper diff            [flags]
  code-review-helper roster          [flags]
  code-review-helper bundle-context  [flags]
  code-review-helper spawn-manifest  [flags]

Run "code-review-helper <subcommand> -h" for subcommand flags.
`)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "code-review-helper: %v\n", err)
	os.Exit(1)
}
