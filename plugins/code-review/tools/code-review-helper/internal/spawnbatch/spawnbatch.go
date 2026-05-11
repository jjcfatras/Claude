// Package spawnbatch renders roster-driven tool-call batches as verbatim
// markdown. The /code-review skill's lead Reads the rendered file and echoes
// it as the body of the next assistant message; each line in the echo block
// becomes a real tool_use, batched in one message. See
// references/code-review-design-notes.md for why this replaced the prior
// prose-only `<<single-message>>` contract.
package spawnbatch

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

const (
	// EchoMarker opens every batch file. The skill instruction tells the lead
	// to echo from this marker to EOF — explicit boundary so Read line numbers
	// from `cat -n` framing don't bleed into the echoed content. The opening
	// line is intentionally prominent so the lead can't gloss over the contract;
	// the trailing "echo block below verbatim" line keeps prose anchored to that
	// marker working (every batch site uses one of these two anchors).
	EchoMarker = "<!-- BATCH-EMIT CONTRACT — read before echoing -->\n" +
		"<!-- Emit every tool_use line below in ONE assistant message, in order, with nothing between them. -->\n" +
		"<!-- No prose, no thinking text, no narration between calls. No whitespace edits. Strip `cat -n` line-number prefix per line. -->\n" +
		"<!-- If you find yourself about to emit fewer tool_uses than lines below, STOP and re-batch in a single message. -->\n" +
		"<!-- echo block below verbatim -->"

	// Workload-scaled safety-monitor budget. Small PRs stay inside the 300 s
	// prompt-cache TTL; large PRs trade one cache miss on the wake-turn for not
	// amputating slow specialists. Rationale in references/code-review-design-notes.md.
	ScanBudgetSecondsDefault = 240 // floor; used when changed-files <= shoulder or unavailable
	ScanBudgetSecondsCap     = 540 // ceiling; ~2× of TTL — one cache-miss on wake-turn
	ScanBudgetPerFileSec     = 2   // seconds added per changed file above the shoulder
	ScanBudgetShoulderFiles  = 50  // number of files below which the floor applies
)

// computeScanBudget returns the safety-monitor sleep budget in seconds, scaled
// to the changed-file count: min(default + perFile * max(0, files - shoulder), cap).
func computeScanBudget(filesChanged int) int {
	extra := filesChanged - ScanBudgetShoulderFiles
	if extra < 0 {
		extra = 0
	}
	budget := ScanBudgetSecondsDefault + ScanBudgetPerFileSec*extra
	if budget > ScanBudgetSecondsCap {
		budget = ScanBudgetSecondsCap
	}
	return budget
}

// monitorLine renders the trailing safety-timer call appended to the Agent
// batch with a scaled sleep budget.
func monitorLine(scanBudgetSeconds int) string {
	return fmt.Sprintf(
		`Monitor({command: "sleep %d; echo scan_complete_timer_fired", timeout_ms: %d, persistent: false, description: "code-review scan-complete safety timer"})`,
		scanBudgetSeconds, scanBudgetSeconds*1000+5000,
	)
}

// readChangedFilesCount loads <reviewTmpDir>/changed-files.json and returns
// the number of paths. Any error (missing file, malformed JSON) is treated as
// "unknown" — caller falls back to the default budget. Never panics.
func readChangedFilesCount(reviewTmpDir string) int {
	if reviewTmpDir == "" {
		return 0
	}
	data, err := os.ReadFile(filepath.Join(reviewTmpDir, "changed-files.json"))
	if err != nil {
		return 0
	}
	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return 0
	}
	return len(paths)
}

type Kind int

const (
	KindTasks Kind = iota
	KindAgents
	KindFinalize
	KindShutdown
)

func ParseKind(s string) (Kind, error) {
	switch s {
	case "tasks":
		return KindTasks, nil
	case "agents":
		return KindAgents, nil
	case "finalize":
		return KindFinalize, nil
	case "shutdown":
		return KindShutdown, nil
	default:
		return 0, fmt.Errorf("unknown kind %q (want tasks|agents|finalize|shutdown)", s)
	}
}

type Input struct {
	Kind         Kind
	Roster       Roster
	Assignments  map[string]string // role -> task ID; required for KindAgents
	ReviewTmpDir string            // required for KindAgents
	Owner        string            // required for KindAgents
	Repo         string            // required for KindAgents
	PRNumber     int               // required for KindAgents
}

type Member struct {
	Role         string `json:"role"`
	Name         string `json:"name"`
	SubagentType string `json:"subagent_type"`
}

type Roster struct {
	TeamName string   `json:"team_name"`
	Members  []Member `json:"members"`
}

func LoadRoster(path string) (Roster, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Roster{}, fmt.Errorf("read roster: %w", err)
	}
	var r Roster
	if err := json.Unmarshal(raw, &r); err != nil {
		return Roster{}, fmt.Errorf("parse roster JSON: %w", err)
	}
	return r, nil
}

func LoadAssignments(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read assignments: %w", err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse assignments JSON: %w", err)
	}
	return m, nil
}

//go:embed templates/*.tmpl
var templatesFS embed.FS

var tmpl = template.Must(template.New("spawnbatch").Funcs(template.FuncMap{
	"jsonString": jsonString,
}).ParseFS(templatesFS, "templates/*.tmpl"))

// jsonString JSON-encodes a string (with surrounding quotes) so multi-line
// content embeds cleanly into a single-line tool-call argument.
func jsonString(s string) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func Build(in Input) (string, error) {
	if len(in.Roster.Members) == 0 {
		return "", fmt.Errorf("roster has no members")
	}

	var body string
	var err error
	switch in.Kind {
	case KindTasks:
		body, err = renderPerMember(in.Roster, "tasks.tmpl", func(m Member) map[string]any {
			return map[string]any{"Role": m.Role}
		})
	case KindAgents:
		body, err = renderAgents(in)
	case KindFinalize:
		body, err = renderPerMember(in.Roster, "finalize.tmpl", func(m Member) map[string]any {
			return map[string]any{"Name": m.Name}
		})
	case KindShutdown:
		body, err = renderPerMember(in.Roster, "shutdown.tmpl", func(m Member) map[string]any {
			return map[string]any{"Name": m.Name}
		})
	default:
		return "", fmt.Errorf("unknown kind %d", in.Kind)
	}
	if err != nil {
		return "", err
	}

	var out strings.Builder
	out.WriteString(EchoMarker)
	out.WriteString("\n")
	out.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		out.WriteString("\n")
	}
	return out.String(), nil
}

func renderPerMember(r Roster, tmplName string, fields func(Member) map[string]any) (string, error) {
	var buf bytes.Buffer
	for _, m := range r.Members {
		if err := tmpl.ExecuteTemplate(&buf, tmplName, fields(m)); err != nil {
			return "", fmt.Errorf("render %s for %s: %w", tmplName, m.Role, err)
		}
	}
	return buf.String(), nil
}

func renderAgents(in Input) (string, error) {
	if in.Assignments == nil {
		return "", fmt.Errorf("agents kind requires assignments")
	}
	if in.ReviewTmpDir == "" || in.Owner == "" || in.Repo == "" || in.PRNumber == 0 {
		return "", fmt.Errorf("agents kind requires --review-tmpdir, --owner, --repo, --pr-number")
	}

	scanBudget := computeScanBudget(readChangedFilesCount(in.ReviewTmpDir))

	var buf bytes.Buffer
	for _, m := range in.Roster.Members {
		taskID, ok := in.Assignments[m.Role]
		if !ok {
			return "", fmt.Errorf("assignments missing entry for role %q", m.Role)
		}
		prompt, err := renderSpawnPrompt(m.Role, taskID, scanBudget, in)
		if err != nil {
			return "", fmt.Errorf("render spawn prompt for %s: %w", m.Role, err)
		}
		if err := tmpl.ExecuteTemplate(&buf, "agent.tmpl", map[string]any{
			"TeamName":     in.Roster.TeamName,
			"Name":         m.Name,
			"SubagentType": m.SubagentType,
			"Role":         m.Role,
			"Prompt":       prompt,
		}); err != nil {
			return "", fmt.Errorf("render agent for %s: %w", m.Role, err)
		}
	}
	buf.WriteString(monitorLine(scanBudget))
	buf.WriteString("\n")
	return buf.String(), nil
}

// renderSpawnPrompt produces the per-role prompt body. Kept in Go (rather
// than a template variable for the whole prompt body) because the prompt is
// long and benefits from being editable as a coherent string in source.
func renderSpawnPrompt(role, taskID string, scanBudget int, in Input) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "spawn-prompt.tmpl", map[string]any{
		"Role":              role,
		"AssignmentTaskID":  taskID,
		"Owner":             in.Owner,
		"Repo":              in.Repo,
		"PRNumber":          in.PRNumber,
		"ReviewTmpDir":      in.ReviewTmpDir,
		"ScanBudgetSeconds": scanBudget,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
