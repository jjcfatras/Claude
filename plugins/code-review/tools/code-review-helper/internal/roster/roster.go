// Package roster computes the specialist roster and the list of CLAUDE.md
// ancestor files for a /code-review run. The orchestrator used to do this
// with five sequential jq / shell calls; doing it here keeps the wire
// contract identical (two JSON arrays) and collapses the pre-spawn phase
// to one Bash tool call.
package roster

import (
	"errors"
	"fmt"
	"io/fs"
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
// it exists) is included whenever any file changed. It also returns every
// `.claude/rules/**/*.md` whose `paths:` frontmatter matches a changed file:
// path-scoped rules load only when a session reads a matching live path, and
// specialists read at HEAD_SHA, so without this no specialist ever holds them
// (transcript f6147551: 7 of 14 rules matched the PR, none were checked).
// Unscoped rules load at startup like the root CLAUDE.md, so they are omitted.
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
	rules, err := matchingRules(changedFiles, repoRoot)
	if err != nil {
		return nil, err
	}
	out = append(out, rules...)
	slices.Sort(out)
	return out, nil
}

// matchingRules walks <repoRoot>/.claude/rules for path-scoped rule files that
// match at least one changed file.
// ponytail: WalkDir doesn't follow a symlinked subdirectory; resolve links if a
// repo ever shares rules that way.
func matchingRules(changedFiles []string, repoRoot string) ([]string, error) {
	dir := filepath.Join(repoRoot, ".claude", "rules")
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, g := range rulePaths(string(raw)) {
			re, err := globRegexp(g)
			if err != nil {
				continue // malformed glob: Claude Code can't load it either
			}
			if slices.ContainsFunc(changedFiles, func(f string) bool { return re.MatchString(strings.TrimPrefix(f, "./")) }) {
				rel, _ := filepath.Rel(repoRoot, p)
				out = append(out, filepath.ToSlash(rel))
				break
			}
		}
		return nil
	})
	return out, err
}

// rulePaths returns the `paths:` globs from a rule file's YAML frontmatter, in
// any of the forms Claude Code accepts: a block list (`- "src/**"`), a flow
// list (`[a, b]`), or a comma string. Commas inside `{a,b}` stay put.
func rulePaths(doc string) []string {
	doc = strings.ReplaceAll(doc, "\r\n", "\n")
	fm, ok := strings.CutPrefix(doc, "---\n")
	if !ok {
		return nil
	}
	fm, _, _ = strings.Cut(fm, "\n---")
	lines := strings.Split(fm, "\n")
	for i, l := range lines {
		v, ok := strings.CutPrefix(l, "paths:")
		if !ok {
			continue
		}
		var items []string
		if v = strings.TrimSpace(v); v == "" {
			for _, l := range lines[i+1:] {
				item, ok := strings.CutPrefix(strings.TrimSpace(l), "- ")
				if !ok {
					break
				}
				items = append(items, item)
			}
		} else {
			items = splitOutsideBraces(strings.Trim(v, "[]"))
		}
		var out []string
		for _, it := range items {
			if it = strings.Trim(strings.TrimSpace(it), `"'`); it != "" {
				out = append(out, it)
			}
		}
		return out
	}
	return nil
}

func splitOutsideBraces(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, c := range s {
		switch {
		case c == '{':
			depth++
		case c == '}':
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// globRegexp compiles a rules `paths:` glob anchored at the repo root: `**`
// spans directories, `*` and `?` stay within one segment, `{a,b}` alternates,
// and everything else is literal.
func globRegexp(g string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	depth := 0
	for i := 0; i < len(g); i++ {
		switch c := g[i]; {
		case strings.HasPrefix(g[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(g[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '{':
			depth++
			b.WriteString("(?:")
		case c == '}' && depth > 0:
			depth--
			b.WriteString(")")
		case c == ',' && depth > 0:
			b.WriteString("|")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
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
