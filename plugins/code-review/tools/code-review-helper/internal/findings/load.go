package findings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type LoadResult struct {
	Findings        []Finding
	Specialists     []string
	TimedOutRoles   []string
	MissingRoles    []string         // roles in `expectedRoles` with no file present
	UnreadableRoles []string         // file existed but couldn't parse — we still continue
	InvalidFindings []InvalidFinding // findings that failed schema validation; skipped, not fatal
}

// InvalidFinding describes a finding rejected by validateFinding. The role's
// other valid findings are still loaded; the lead surfaces these to the user
// at the post-confirmation gate so the failure mode is visible without
// blocking the run.
type InvalidFinding struct {
	Role   string `json:"role"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// LoadDir reads every `<role>.json` in dir matching the rubric schema. Missing
// files are tolerated; if expectedRoles is non-nil, missing entries are recorded
// in MissingRoles so the caller can include them in its log line.
//
// A file with scan_status: "timed_out" is loaded normally (its findings count)
// but its role is added to TimedOutRoles for the caller to mention.
func LoadDir(dir string, expectedRoles []string) (*LoadResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read findings dir %q: %w", dir, err)
	}

	res := &LoadResult{}
	seen := make(map[string]bool)

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		role := strings.TrimSuffix(e.Name(), ".json")
		path := filepath.Join(dir, e.Name())

		rf, err := loadFile(path)
		if err != nil {
			res.UnreadableRoles = append(res.UnreadableRoles, role)
			continue
		}

		seen[role] = true
		res.Specialists = append(res.Specialists, role)
		if rf.ScanStatus == ScanTimedOut {
			res.TimedOutRoles = append(res.TimedOutRoles, role)
		}

		for i := range rf.Findings {
			rf.Findings[i].Specialist = role
			if err := validateFinding(role, &rf.Findings[i]); err != nil {
				res.InvalidFindings = append(res.InvalidFindings, InvalidFinding{
					Role:   role,
					ID:     rf.Findings[i].ID,
					Reason: err.Error(),
				})
				continue
			}
			res.Findings = append(res.Findings, rf.Findings[i])
		}
	}

	for _, role := range expectedRoles {
		if !seen[role] {
			res.MissingRoles = append(res.MissingRoles, role)
		}
	}

	return res, nil
}

func loadFile(path string) (*RoleFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var rf RoleFile
	if err := json.Unmarshal(b, &rf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &rf, nil
}

func validateFinding(role string, f *Finding) error {
	if f.ID == "" {
		return fmt.Errorf("specialist %s: finding missing id", role)
	}
	if f.File == "" {
		return fmt.Errorf("specialist %s finding %s: empty file", role, f.ID)
	}
	if f.Line <= 0 {
		return fmt.Errorf("specialist %s finding %s: non-positive line %d", role, f.ID, f.Line)
	}
	if f.StartLine != nil && (*f.StartLine <= 0 || *f.StartLine > f.Line) {
		return fmt.Errorf("specialist %s finding %s: invalid startLine %d (line=%d)", role, f.ID, *f.StartLine, f.Line)
	}
	if f.Confidence < 0 || f.Confidence > 100 {
		return fmt.Errorf("specialist %s finding %s: confidence %d out of range", role, f.ID, f.Confidence)
	}
	switch f.Severity {
	case SeverityCritical, SeverityMedium, SeverityMinor:
	default:
		return fmt.Errorf("specialist %s finding %s: unknown severity %q", role, f.ID, f.Severity)
	}
	// Content-field non-emptiness mirrors the rubric's required-fields list
	// (references/code-review-rubrics.md). Empty values here render as visible
	// blank placeholders in the review body, so reject them at load time and
	// surface the role+id via LoadResult.InvalidFindings (skill step 4).
	if strings.TrimSpace(f.Rationale) == "" {
		return fmt.Errorf("specialist %s finding %s: empty rationale", role, f.ID)
	}
	if strings.TrimSpace(f.Explanation) == "" {
		return fmt.Errorf("specialist %s finding %s: empty explanation", role, f.ID)
	}
	if strings.TrimSpace(f.Code) == "" {
		return fmt.Errorf("specialist %s finding %s: empty code", role, f.ID)
	}
	if strings.TrimSpace(f.Language) == "" {
		return fmt.Errorf("specialist %s finding %s: empty language", role, f.ID)
	}
	return nil
}

// ErrNoFindings is returned by callers (not LoadDir itself) when downstream
// stages need to short-circuit. Exposed for type-asserted handling.
var ErrNoFindings = errors.New("no findings to process")
