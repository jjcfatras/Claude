// Package diff parses unified-diff output from `gh pr diff` (or `git diff`)
// into the two structures the code-review skill needs:
//
//   - ChangedFiles: every path the PR touched (including binary, pure renames,
//     and deletions; the new b-path is used for renames).
//   - ValidLines: per-file list of [start, end] line ranges in the *new* version
//     of the file. Inline review comments must target a line within one of these
//     ranges. Binary files, deletions, and pure renames are omitted here.
package diff

import (
	"bufio"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/intmath"
)

type Parsed struct {
	ChangedFiles []string         `json:"changed_files"`
	ValidLines   map[string][]Run `json:"valid_lines"`

	// AddedLines[path] is the set of new-version line numbers that are `+` lines
	// in the diff (i.e., the PR introduced them). Lines that appear in ValidLines
	// but NOT in AddedLines are context lines — pre-existing code. Used by
	// prior-review dedup to distinguish "issue on unchanged code" from "issue on
	// code the PR just touched."
	AddedLines map[string]map[int]bool `json:"-"`
}

// Run is a [start, end] inclusive line range.
type Run struct {
	Start int `json:"-"`
	End   int `json:"-"`
}

func (r Run) MarshalJSON() ([]byte, error) {
	return []byte("[" + strconv.Itoa(r.Start) + "," + strconv.Itoa(r.End) + "]"), nil
}

func (r *Run) UnmarshalJSON(data []byte) error {
	text := strings.Trim(string(data), "[] ")
	parts := strings.Split(text, ",")
	if len(parts) != 2 {
		return strconvErr(text)
	}
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return err
	}
	end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return err
	}
	r.Start = start
	r.End = end
	return nil
}

func strconvErr(text string) error {
	return &strconv.NumError{Func: "Atoi", Num: text, Err: strconv.ErrSyntax}
}

var (
	diffGitRE = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)
	hunkRE    = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)
)

// Parse reads a unified diff and produces ChangedFiles + ValidLines + AddedLines.
func Parse(reader io.Reader) (*Parsed, error) {
	parsed := &Parsed{
		ValidLines: make(map[string][]Run),
		AddedLines: make(map[string]map[int]bool),
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var currentPath string
	var nextNewLine int // tracks the new-version line number while scanning hunk body
	var inHunkBody bool // true after a `@@` header until the next non-body line
	seen := make(map[string]bool)
	addFile := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		parsed.ChangedFiles = append(parsed.ChangedFiles, path)
	}

	for scanner.Scan() {
		line := scanner.Text()

		// Hunk-body tracking: a hunk body ends when we hit any line that starts
		// a new file ("diff --git "), a new hunk ("@@"), or a non-diff metadata
		// prefix. The cheapest invariant is: body lines start with ' ', '+', or
		// '-'. Anything else closes the body.
		if inHunkBody {
			if len(line) == 0 {
				// Empty line inside a hunk body is rare but legal — treat as context.
				nextNewLine++
				continue
			}
			switch line[0] {
			case ' ':
				nextNewLine++
				continue
			case '+':
				if !strings.HasPrefix(line, "+++") {
					if parsed.AddedLines[currentPath] == nil {
						parsed.AddedLines[currentPath] = make(map[int]bool)
					}
					parsed.AddedLines[currentPath][nextNewLine] = true
					nextNewLine++
					continue
				}
				inHunkBody = false
			case '-':
				if !strings.HasPrefix(line, "---") {
					// Removed line — does not advance new-version line counter.
					continue
				}
				inHunkBody = false
			case '\\':
				// "\ No newline at end of file" — skip without advancing.
				continue
			default:
				inHunkBody = false
			}
		}

		switch {
		case strings.HasPrefix(line, "diff --git "):
			if match := diffGitRE.FindStringSubmatch(line); match != nil {
				currentPath = match[2]
				addFile(currentPath)
			}
			inHunkBody = false
		case strings.HasPrefix(line, "rename to "):
			newPath := strings.TrimPrefix(line, "rename to ")
			if newPath != currentPath {
				if seen[currentPath] {
					parsed.ChangedFiles = slices.DeleteFunc(parsed.ChangedFiles, func(s string) bool { return s == currentPath })
					delete(seen, currentPath)
				}
				currentPath = newPath
				addFile(currentPath)
			}
		case strings.HasPrefix(line, "+++ b/"):
			newPath := strings.TrimPrefix(line, "+++ b/")
			if newPath != currentPath {
				currentPath = newPath
				addFile(currentPath)
			}
		case strings.HasPrefix(line, "+++ /dev/null"):
		default:
			if match := hunkRE.FindStringSubmatch(line); match != nil {
				if currentPath == "" {
					continue
				}
				newStart, _ := strconv.Atoi(match[1])
				newCount := 1
				if match[2] != "" {
					newCount, _ = strconv.Atoi(match[2])
				}
				if newCount == 0 {
					inHunkBody = false
					continue
				}
				parsed.ValidLines[currentPath] = append(parsed.ValidLines[currentPath],
					Run{Start: newStart, End: newStart + newCount - 1})
				nextNewLine = newStart
				inHunkBody = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	for path, runs := range parsed.ValidLines {
		if len(runs) == 0 {
			delete(parsed.ValidLines, path)
		}
	}

	slices.Sort(parsed.ChangedFiles)
	return parsed, nil
}

// IsAddedLine reports whether `line` was introduced by the PR (a `+` line in
// the diff). Returns false for context lines and for lines outside any hunk.
func (p *Parsed) IsAddedLine(path string, line int) bool {
	return p.AddedLines[path][line]
}

// InRange reports whether `line` falls within any run for `path`.
func (p *Parsed) InRange(path string, line int) bool {
	for _, run := range p.ValidLines[path] {
		if line >= run.Start && line <= run.End {
			return true
		}
	}
	return false
}

// NearestValid returns the valid line in `path` closest to `line`. Returns
// (0, false) if `path` has no runs.
func (p *Parsed) NearestValid(path string, line int) (int, bool) {
	runs := p.ValidLines[path]
	if len(runs) == 0 {
		return 0, false
	}
	best := -1
	bestDist := -1
	for _, run := range runs {
		var candidate int
		switch {
		case line < run.Start:
			candidate = run.Start
		case line > run.End:
			candidate = run.End
		default:
			return line, true
		}
		dist := intmath.Abs(candidate - line)
		if bestDist == -1 || dist < bestDist {
			best = candidate
			bestDist = dist
		}
	}
	return best, true
}
