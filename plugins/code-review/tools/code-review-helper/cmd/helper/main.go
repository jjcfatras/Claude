// code-review-helper is the deterministic backend for the /code-review plugin.
// It owns diff parsing, roster construction, the dedup + gate + snap pipeline,
// and final payload assembly. The command invokes it via four subcommands;
// see the package documentation for each subcommand for the exact contract.
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

	switch cmd {
	case "diff":
		if err := runDiff(args); err != nil {
			fail(err)
		}
	case "roster":
		if err := runRoster(args); err != nil {
			fail(err)
		}
	case "finalize":
		if err := runFinalize(args); err != nil {
			fail(err)
		}
	case "bundle-context":
		if err := runBundleContext(args); err != nil {
			fail(err)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "code-review-helper: unknown subcommand %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `code-review-helper — deterministic backend for the /code-review plugin

Usage:
  code-review-helper diff            [flags]
  code-review-helper roster          [flags]
  code-review-helper finalize        [flags]
  code-review-helper bundle-context  [flags]

Run "code-review-helper <subcommand> -h" for subcommand flags.
`)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "code-review-helper: %v\n", err)
	os.Exit(1)
}
