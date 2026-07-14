// Package roster computes the specialist roster and the list of CLAUDE.md
// ancestor files for a /code-review run. The orchestrator used to do this
// with five sequential jq / shell calls; doing it here keeps the wire
// contract identical (two JSON arrays) and collapses the pre-spawn phase
// to one Bash tool call.
package roster

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Always-on specialist roles, in canonical order.
var alwaysOn = []string{"security", "quality", "errors", "perf"}

var (
	// patTypescript matches TS sources and the TS/JS compiler-config files
	// (tsconfig*.json, jsconfig.json) that govern compilation but carry no TS
	// extension — those previously fell through to the always-on roster only.
	patTypescript = regexp.MustCompile(`(?i)(\.(ts|tsx|cts|mts)$|(^|/)tsconfig[^/]*\.json$|(^|/)jsconfig\.json$)`)
	patReact      = regexp.MustCompile(`(?i)\.(tsx|jsx)$`)
	patInfra      = regexp.MustCompile(`(?i)(\.sql$|(^|/)migrations/|(^|/)db/migrations/|\.tf$|\.hcl$|(^|/)terraform/|(^|/)Dockerfile|(^|/)docker-compose|(^|/)compose\.ya?ml$|(^|/)k8s/|(^|/)kubernetes/|(^|/)helm/|(^|/)deploy/|(^|/)infra(structure)?/|(^|/)\.github/workflows/|(^|/)Jenkinsfile|(^|/)\.circleci/|(^|/)\.buildkite/)`)
	// patFrameworkConfig matches build/bundler config files that affect both the
	// TypeScript build and the React/bundler surface but match neither extension
	// pattern (e.g. next.config.js, vite.config.ts, babel.config.json, .babelrc).
	// It feeds both the typescript and react conditionals.
	patFrameworkConfig = regexp.MustCompile(`(?i)((^|/)(next|vite|nuxt|webpack|rollup|esbuild|babel)\.config\.(js|mjs|cjs|ts|mts|cts)$|(^|/)babel\.config\.json$|(^|/)\.babelrc$)`)
)

// ClaudeMdFiles returns the deduplicated set of repo-relative CLAUDE.md
// paths that exist at or above the changed files. The root CLAUDE.md (if
// it exists) is included whenever any file changed.
//
// repoRoot must be an absolute or process-relative directory; existence is
// checked via os.Stat on path.Join(repoRoot, candidate).
func ClaudeMdFiles(changedFiles []string, repoRoot string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, f := range changedFiles {
		// strip leading "./" so candidates stay repo-relative
		f = strings.TrimPrefix(f, "./")
		parts := strings.Split(f, "/")
		// Drop the filename — only walk directory prefixes plus the root.
		parts = parts[:len(parts)-1]
		for i := 0; i <= len(parts); i++ {
			cand := filepath.Join(strings.Join(parts[:i], "/"), "CLAUDE.md")
			if seen[cand] {
				continue
			}
			seen[cand] = true
			full := filepath.Join(repoRoot, cand)
			info, err := os.Stat(full)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("stat %s: %w", full, err)
			}
			if info.IsDir() {
				continue
			}
			out = append(out, cand)
		}
	}
	slices.Sort(out)
	return out, nil
}

// Build returns the specialist roster for the given changed files and the
// number of CLAUDE.md files found by ClaudeMdFiles. Order matches the
// orchestrator's previous jq output: always-on roles first, then
// typescript, react, infra, claude-md in that order.
func Build(changedFiles []string, claudeMdCount int) []string {
	roster := append([]string(nil), alwaysOn...)
	frameworkConfig := anyMatch(changedFiles, patFrameworkConfig)
	if anyMatch(changedFiles, patTypescript) || frameworkConfig {
		roster = append(roster, "typescript")
	}
	if anyMatch(changedFiles, patReact) || frameworkConfig {
		roster = append(roster, "react")
	}
	if anyMatch(changedFiles, patInfra) {
		roster = append(roster, "infra")
	}
	if claudeMdCount > 0 {
		roster = append(roster, "claude-md")
	}
	return roster
}

func anyMatch(files []string, re *regexp.Regexp) bool {
	return slices.ContainsFunc(files, re.MatchString)
}
