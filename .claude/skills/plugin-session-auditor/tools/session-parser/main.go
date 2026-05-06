// session-parser is the deterministic backend for the plugin-session-auditor
// skill. It reads one or more Claude Code jsonl session transcripts and emits a
// structured JSON document describing the session: per-event categories (tool
// calls, tool failures, permission denials, agent spawns, slash commands, hook
// errors, api errors), aggregate stats, and the set of repo plugins the session
// exercised. The downstream specialist agents read this JSON instead of
// re-parsing the raw jsonl, which keeps their context lean and lets them focus
// on judgement rather than parsing.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// rawEntry holds the union of fields we care about across the various entry
// types in the jsonl. Fields are pointers/interfaces so we can tell "absent"
// from "zero".
type rawEntry struct {
	Type        string          `json:"type"`
	Subtype     string          `json:"subtype"`
	UUID        string          `json:"uuid"`
	ParentUUID  string          `json:"parentUuid"`
	Timestamp   string          `json:"timestamp"`
	SessionID   string          `json:"sessionId"`
	CWD         string          `json:"cwd"`
	GitBranch   string          `json:"gitBranch"`
	Version     string          `json:"version"`
	IsSidechain bool            `json:"isSidechain"`
	Message     json.RawMessage `json:"message"`
	HookErrors  json.RawMessage `json:"hookErrors"`
	ToolUseID   string          `json:"toolUseID"`
	// Turn duration field name has varied across Claude Code versions; try a few.
	DurationMs     *int64 `json:"durationMs,omitempty"`
	TurnDurationMs *int64 `json:"turnDurationMs,omitempty"`
	Duration       *int64 `json:"duration,omitempty"`
	// api_error system entries occasionally carry a free-form payload.
	ErrorMessage string `json:"message_str,omitempty"`
}

type messageEnvelope struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentItem struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
	Thinking  string          `json:"thinking"`
}

type ToolCall struct {
	UUID         string      `json:"uuid"`
	Timestamp    string      `json:"timestamp"`
	ToolUseID    string      `json:"tool_use_id"`
	Name         string      `json:"name"`
	InputPreview string      `json:"input_preview"`
	IsSidechain  bool        `json:"is_sidechain"`
	ParentUUID   string      `json:"parent_uuid"`
	Result       *ToolResult `json:"result,omitempty"`
}

type ToolResult struct {
	Timestamp      string `json:"timestamp"`
	IsError        bool   `json:"is_error"`
	IsSidechain    bool   `json:"is_sidechain"`
	ContentPreview string `json:"content_preview"`
	rawText        string
}

type FailureRec struct {
	Timestamp    string `json:"timestamp"`
	ToolUseID    string `json:"tool_use_id"`
	ToolName     string `json:"tool_name"`
	InputPreview string `json:"input_preview"`
	IsSidechain  bool   `json:"is_sidechain"`
	ErrorPreview string `json:"error_preview"`
}

type AgentSpawn struct {
	Timestamp    string `json:"timestamp"`
	ToolUseID    string `json:"tool_use_id"`
	AgentSubtype string `json:"agent_subtype,omitempty"`
	AgentName    string `json:"agent_name,omitempty"`
	Description  string `json:"description,omitempty"`
	TeamName     string `json:"team_name,omitempty"`
	IsSidechain  bool   `json:"is_sidechain"`
	// Sidechain correlation: pulled from the Agent tool_result, which carries
	// `agentId: <id>` text and (after completion) a <usage> block. The id is
	// either a hex `a<hex>` (general-purpose spawns) or `name@team` (team-mode
	// spawns from TeamCreate). Hex-id spawns map to a single sidechain file;
	// team spawns can map to multiple sidechain files because the same role
	// may be re-instructed multiple times across the team's lifetime — those
	// are returned in Sidechains. Sidechain holds the first matching transcript
	// for ergonomics; consumers that need everything should walk Sidechains.
	AgentID          string       `json:"agent_id,omitempty"`
	UsageTotalTokens int64        `json:"usage_total_tokens,omitempty"`
	UsageToolUses    int          `json:"usage_tool_uses,omitempty"`
	UsageDurationMs  int64        `json:"usage_duration_ms,omitempty"`
	SidechainFile    string       `json:"sidechain_file,omitempty"`
	Sidechain        *Sidechain   `json:"sidechain,omitempty"`
	Sidechains       []*Sidechain `json:"sidechains,omitempty"`
}

// Sidechain holds the parsed contents of a single subagent transcript. Stats
// match the top-level Stats struct so downstream consumers can analyze a
// specialist's work the same way they analyze the main session. Events are
// optional; the parser drops them in --summary-only mode to keep payloads small.
type Sidechain struct {
	AgentType      string  `json:"agent_type,omitempty"`
	Description    string  `json:"description,omitempty"`
	FirstTimestamp string  `json:"first_timestamp,omitempty"`
	LastTimestamp  string  `json:"last_timestamp,omitempty"`
	Stats          Stats   `json:"stats"`
	Events         *Events `json:"events,omitempty"`
}

type SlashCmd struct {
	Timestamp string `json:"timestamp"`
	Name      string `json:"name"`
	UUID      string `json:"uuid"`
}

type HookEvent struct {
	Timestamp             string          `json:"timestamp"`
	Subtype               string          `json:"subtype"`
	ToolUseID             string          `json:"tool_use_id,omitempty"`
	Errors                json.RawMessage `json:"errors"`
	PreventedContinuation *bool           `json:"prevented_continuation,omitempty"`
}

type APIError struct {
	Timestamp string `json:"timestamp"`
	Summary   string `json:"summary"`
}

type DenialRun struct {
	ToolName string `json:"tool_name"`
	Start    string `json:"start"`
	Length   int    `json:"length"`
}

type Stats struct {
	TotalToolCalls          int            `json:"total_tool_calls"`
	ToolCallsByName         map[string]int `json:"tool_calls_by_name"`
	SidechainToolCalls      int            `json:"sidechain_tool_calls"`
	MainThreadToolCalls     int            `json:"main_thread_tool_calls"`
	ToolFailureCount        int            `json:"tool_failure_count"`
	ToolFailureRate         float64        `json:"tool_failure_rate"`
	PermissionDenialCount   int            `json:"permission_denial_count"`
	PermissionDenialsByTool map[string]int `json:"permission_denials_by_tool"`
	PermissionDenialRuns    []DenialRun    `json:"permission_denial_runs"`
	AgentSpawnCount         int            `json:"agent_spawn_count"`
	AgentSpawnsBySubtype    map[string]int `json:"agent_spawns_by_subtype"`
	SlashCommandCount       int            `json:"slash_command_count"`
	SlashCommandsByName     map[string]int `json:"slash_commands_by_name"`
	HookErrorCount          int            `json:"hook_error_count"`
	APIErrorCount           int            `json:"api_error_count"`
	TurnCount               int            `json:"turn_count"`
	TurnDurationMsTotal     int64          `json:"turn_duration_ms_total"`
	TurnDurationMsP50       int64          `json:"turn_duration_ms_p50"`
	TurnDurationMsP95       int64          `json:"turn_duration_ms_p95"`
}

type Events struct {
	ToolCalls         []ToolCall   `json:"tool_calls"`
	ToolFailures      []FailureRec `json:"tool_failures"`
	PermissionDenials []FailureRec `json:"permission_denials"`
	AgentSpawns       []AgentSpawn `json:"agent_spawns"`
	SlashCommands     []SlashCmd   `json:"slash_commands"`
	HookEvents        []HookEvent  `json:"hook_events"`
	APIErrors         []APIError   `json:"api_errors"`
}

type PluginScope struct {
	Commands []string `json:"commands"`
	Agents   []string `json:"agents"`
}

type Session struct {
	SourceFile       string                 `json:"source_file"`
	SessionID        string                 `json:"session_id"`
	CWD              string                 `json:"cwd"`
	GitBranch        string                 `json:"git_branch"`
	Version          string                 `json:"version"`
	FirstTimestamp   string                 `json:"first_timestamp"`
	LastTimestamp    string                 `json:"last_timestamp"`
	PluginsUsed      []string               `json:"plugins_used"`
	PluginScopeKnown map[string]PluginScope `json:"plugin_scope_known,omitempty"`
	Stats            Stats                  `json:"stats"`
	Events           *Events                `json:"events,omitempty"`
}

var permissionRejectMarkers = []string{
	"doesn't want to proceed with this tool use",
	"tool use was rejected",
	"user has denied permission",
	"permission denied",
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + fmt.Sprintf("... [+%d chars]", len(s)-limit)
}

func previewJSON(raw json.RawMessage, limit int) string {
	if len(raw) == 0 {
		return ""
	}
	// Try string first; otherwise compact JSON.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return truncate(s, limit)
	}
	return truncate(string(raw), limit)
}

func contentToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// hookErrorCount returns the number of actual error entries in a system entry's
// hookErrors field. The field can be absent (nil), JSON null, an empty array,
// or a populated array — only the last counts as a real hook error.
func hookErrorCount(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		return len(arr)
	}
	// Some hook payloads may surface as objects rather than arrays; treat any
	// non-empty non-array payload as a single error so we don't drop signal.
	return 1
}

func isPermissionDenial(text string) bool {
	low := strings.ToLower(text)
	for _, m := range permissionRejectMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// extractSlashCommands scans a string for <command-name>X</command-name> markers.
// Slash command invocations show up that way in user messages.
func extractSlashCommands(text string) []string {
	const open = "<command-name>"
	const close = "</command-name>"
	var out []string
	for {
		i := strings.Index(text, open)
		if i < 0 {
			return out
		}
		j := strings.Index(text[i+len(open):], close)
		if j < 0 {
			return out
		}
		out = append(out, text[i+len(open):i+len(open)+j])
		text = text[i+len(open)+j+len(close):]
	}
}

func discoverPluginScope(repoRoot string) map[string]PluginScope {
	out := map[string]PluginScope{}
	pluginsDir := filepath.Join(repoRoot, "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		ps := PluginScope{Commands: []string{}, Agents: []string{}}
		for _, sub := range []struct {
			dir string
			dst *[]string
		}{
			{filepath.Join(pluginsDir, name, "commands"), &ps.Commands},
			{filepath.Join(pluginsDir, name, "agents"), &ps.Agents},
		} {
			matches, _ := filepath.Glob(filepath.Join(sub.dir, "*.md"))
			for _, m := range matches {
				stem := strings.TrimSuffix(filepath.Base(m), ".md")
				*sub.dst = append(*sub.dst, stem)
			}
			sort.Strings(*sub.dst)
		}
		out[name] = ps
	}
	return out
}

func detectPluginsInSession(events *Events, scope map[string]PluginScope) []string {
	slash := map[string]bool{}
	for _, s := range events.SlashCommands {
		slash[s.Name] = true
	}
	agentNames := map[string]bool{}
	for _, a := range events.AgentSpawns {
		if a.AgentSubtype != "" {
			agentNames[a.AgentSubtype] = true
		}
		if a.AgentName != "" {
			agentNames[a.AgentName] = true
		}
	}
	used := map[string]bool{}
	for plugin, ps := range scope {
		for _, c := range ps.Commands {
			if slash[c] {
				used[plugin] = true
			}
		}
		for _, a := range ps.Agents {
			if agentNames[a] {
				used[plugin] = true
			}
		}
	}
	out := make([]string, 0, len(used))
	for p := range used {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func percentile(values []int64, pct float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	k := int(pct / 100.0 * float64(len(sorted)-1))
	if k < 0 {
		k = 0
	}
	if k >= len(sorted) {
		k = len(sorted) - 1
	}
	return sorted[k]
}

// parsedFile is the return shape of parseFile — everything we extract from one
// jsonl, before correlating tool calls/results or attaching sidechains.
type parsedFile struct {
	Events          *Events
	ToolResultsByID map[string]*ToolResult
	TurnDurations   []int64
	SessionID       string
	CWD             string
	GitBranch       string
	Version         string
	FirstTimestamp  string
	LastTimestamp   string
}

func parseFile(path string) (*parsedFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.UseNumber()

	pf := &parsedFile{
		Events:          &Events{},
		ToolResultsByID: map[string]*ToolResult{},
	}
	events := pf.Events
	toolResultsByID := pf.ToolResultsByID

	for {
		var raw map[string]json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			// Skip malformed lines and keep going. Encoding/json's Decoder will
			// advance past the bad token if we ask it to read again, but in
			// practice malformed jsonl ends the stream — bail.
			break
		}

		var entry rawEntry
		// Manually unmarshal because we want to use UseNumber on the decoder.
		buf, _ := json.Marshal(raw)
		_ = json.Unmarshal(buf, &entry)

		if entry.SessionID != "" {
			pf.SessionID = entry.SessionID
		}
		if entry.CWD != "" {
			pf.CWD = entry.CWD
		}
		if entry.GitBranch != "" {
			pf.GitBranch = entry.GitBranch
		}
		if entry.Version != "" {
			pf.Version = entry.Version
		}
		if entry.Timestamp != "" {
			if pf.FirstTimestamp == "" || entry.Timestamp < pf.FirstTimestamp {
				pf.FirstTimestamp = entry.Timestamp
			}
			if entry.Timestamp > pf.LastTimestamp {
				pf.LastTimestamp = entry.Timestamp
			}
		}

		switch entry.Type {
		case "system":
			switch entry.Subtype {
			case "turn_duration":
				if entry.DurationMs != nil {
					pf.TurnDurations = append(pf.TurnDurations, *entry.DurationMs)
				} else if entry.TurnDurationMs != nil {
					pf.TurnDurations = append(pf.TurnDurations, *entry.TurnDurationMs)
				} else if entry.Duration != nil {
					pf.TurnDurations = append(pf.TurnDurations, *entry.Duration)
				}
			case "api_error":
				summary := previewJSON(raw["message"], 400)
				if summary == "" {
					summary = previewJSON(raw["error"], 400)
				}
				events.APIErrors = append(events.APIErrors, APIError{
					Timestamp: entry.Timestamp,
					Summary:   summary,
				})
			}
			// Only record hook events that actually carry an error. Without
			// this guard, every stop_hook_summary entry would be counted as a
			// hook error even when its errors array is empty, which inflates
			// hook_error_count and produces noise findings.
			if hookErrorCount(entry.HookErrors) > 0 {
				var prevented *bool
				if rp, ok := raw["preventedContinuation"]; ok {
					var b bool
					if err := json.Unmarshal(rp, &b); err == nil {
						prevented = &b
					}
				}
				events.HookEvents = append(events.HookEvents, HookEvent{
					Timestamp:             entry.Timestamp,
					Subtype:               entry.Subtype,
					ToolUseID:             entry.ToolUseID,
					Errors:                entry.HookErrors,
					PreventedContinuation: prevented,
				})
			}
			continue
		}

		// Decode the message envelope.
		if len(entry.Message) == 0 {
			continue
		}
		var env messageEnvelope
		if err := json.Unmarshal(entry.Message, &env); err != nil {
			continue
		}

		// Some user messages carry plain string content with embedded slash command markers.
		if len(env.Content) > 0 && env.Content[0] == '"' {
			s := contentToString(env.Content)
			if entry.Type == "user" {
				for _, name := range extractSlashCommands(s) {
					events.SlashCommands = append(events.SlashCommands, SlashCmd{
						Timestamp: entry.Timestamp, Name: name, UUID: entry.UUID,
					})
				}
			}
			continue
		}

		var items []contentItem
		if err := json.Unmarshal(env.Content, &items); err != nil {
			continue
		}

		for _, it := range items {
			switch it.Type {
			case "tool_use":
				tc := ToolCall{
					UUID:         entry.UUID,
					Timestamp:    entry.Timestamp,
					ToolUseID:    it.ID,
					Name:         it.Name,
					InputPreview: previewJSON(it.Input, 400),
					IsSidechain:  entry.IsSidechain,
					ParentUUID:   entry.ParentUUID,
				}
				events.ToolCalls = append(events.ToolCalls, tc)
				if it.Name == "Agent" {
					var inp struct {
						SubagentType string `json:"subagent_type"`
						Name         string `json:"name"`
						Description  string `json:"description"`
						TeamName     string `json:"team_name"`
					}
					_ = json.Unmarshal(it.Input, &inp)
					events.AgentSpawns = append(events.AgentSpawns, AgentSpawn{
						Timestamp:    entry.Timestamp,
						ToolUseID:    it.ID,
						AgentSubtype: inp.SubagentType,
						AgentName:    inp.Name,
						Description:  inp.Description,
						TeamName:     inp.TeamName,
						IsSidechain:  entry.IsSidechain,
					})
				}
			case "tool_result":
				txt := contentToString(it.Content)
				toolResultsByID[it.ToolUseID] = &ToolResult{
					Timestamp:      entry.Timestamp,
					IsError:        it.IsError,
					IsSidechain:    entry.IsSidechain,
					ContentPreview: truncate(txt, 600),
					rawText:        txt,
				}
			case "text":
				if entry.Type == "user" {
					for _, name := range extractSlashCommands(it.Text) {
						events.SlashCommands = append(events.SlashCommands, SlashCmd{
							Timestamp: entry.Timestamp, Name: name, UUID: entry.UUID,
						})
					}
				}
			}
		}
	}

	return pf, nil
}

// correlateAndStat takes a parsedFile and produces the publicly-visible Events
// + Stats by pairing tool calls with their results, classifying failures, and
// computing aggregate counters. Used for both the main session and each
// sidechain transcript.
func correlateAndStat(pf *parsedFile) (*Events, Stats) {
	events := pf.Events

	for i := range events.ToolCalls {
		call := &events.ToolCalls[i]
		res, ok := pf.ToolResultsByID[call.ToolUseID]
		if !ok {
			continue
		}
		call.Result = &ToolResult{
			Timestamp:      res.Timestamp,
			IsError:        res.IsError,
			IsSidechain:    res.IsSidechain,
			ContentPreview: res.ContentPreview,
		}
		if !res.IsError {
			continue
		}
		rec := FailureRec{
			Timestamp:    res.Timestamp,
			ToolUseID:    call.ToolUseID,
			ToolName:     call.Name,
			InputPreview: call.InputPreview,
			IsSidechain:  call.IsSidechain,
			ErrorPreview: res.ContentPreview,
		}
		if isPermissionDenial(res.rawText) {
			events.PermissionDenials = append(events.PermissionDenials, rec)
		} else {
			events.ToolFailures = append(events.ToolFailures, rec)
		}
	}

	denialCounts := map[string]int{}
	for _, d := range events.PermissionDenials {
		denialCounts[d.ToolName]++
	}
	var runs []DenialRun
	if len(events.PermissionDenials) > 0 {
		runTool := events.PermissionDenials[0].ToolName
		runStart := events.PermissionDenials[0].Timestamp
		runLen := 1
		for _, d := range events.PermissionDenials[1:] {
			if d.ToolName == runTool {
				runLen++
				continue
			}
			if runLen >= 2 {
				runs = append(runs, DenialRun{ToolName: runTool, Start: runStart, Length: runLen})
			}
			runTool = d.ToolName
			runStart = d.Timestamp
			runLen = 1
		}
		if runLen >= 2 {
			runs = append(runs, DenialRun{ToolName: runTool, Start: runStart, Length: runLen})
		}
	}

	toolCallCounts := map[string]int{}
	sidechain := 0
	for _, c := range events.ToolCalls {
		if c.Name != "" {
			toolCallCounts[c.Name]++
		}
		if c.IsSidechain {
			sidechain++
		}
	}
	spawnsBySubtype := map[string]int{}
	for _, a := range events.AgentSpawns {
		key := a.AgentSubtype
		if key == "" {
			key = "(default)"
		}
		spawnsBySubtype[key]++
	}
	slashByName := map[string]int{}
	for _, s := range events.SlashCommands {
		slashByName[s.Name]++
	}

	stats := Stats{
		TotalToolCalls:          len(events.ToolCalls),
		ToolCallsByName:         toolCallCounts,
		SidechainToolCalls:      sidechain,
		MainThreadToolCalls:     len(events.ToolCalls) - sidechain,
		ToolFailureCount:        len(events.ToolFailures),
		PermissionDenialCount:   len(events.PermissionDenials),
		PermissionDenialsByTool: denialCounts,
		PermissionDenialRuns:    runs,
		AgentSpawnCount:         len(events.AgentSpawns),
		AgentSpawnsBySubtype:    spawnsBySubtype,
		SlashCommandCount:       len(events.SlashCommands),
		SlashCommandsByName:     slashByName,
		HookErrorCount:          len(events.HookEvents),
		APIErrorCount:           len(events.APIErrors),
		TurnCount:               len(pf.TurnDurations),
	}
	if len(events.ToolCalls) > 0 {
		stats.ToolFailureRate = float64(len(events.ToolFailures)) / float64(len(events.ToolCalls))
	}
	for _, d := range pf.TurnDurations {
		stats.TurnDurationMsTotal += d
	}
	stats.TurnDurationMsP50 = percentile(pf.TurnDurations, 50)
	stats.TurnDurationMsP95 = percentile(pf.TurnDurations, 95)

	return events, stats
}

// agentIDFromToolResult extracts the agent identifier from an Agent tool_result.
// Three formats appear in the wild:
//   - "agentId: a<hex>"          (general-purpose spawns; case-sensitive `agentId`)
//   - "agent_id: <name>@<team>"  (team-mode spawns from TeamCreate; underscore form)
//   - "agent_id: a<hex>"         (rare team-mode variant)
//
// Returns the raw identifier text after the prefix, stopping at whitespace.
func agentIDFromToolResult(text string) string {
	for _, prefix := range []string{"agentId: ", "agent_id: "} {
		idx := strings.Index(text, prefix)
		if idx < 0 {
			continue
		}
		rest := text[idx+len(prefix):]
		end := strings.IndexAny(rest, " \n\t\r")
		if end < 0 {
			end = len(rest)
		}
		return strings.TrimSpace(rest[:end])
	}
	return ""
}

// sidechainEntry pairs a transcript path with its first-timestamp so the
// matcher can pick the temporally-correct file for a given spawn. Multiple
// transcripts may share an agentType (e.g., `security-reviewer` re-spawned
// across two `/code-review` runs in the same session); ordering by start time
// lets us partition them per-spawn instead of attaching all to the first.
type sidechainEntry struct {
	Path           string
	FirstTimestamp string
}

// indexSidechainsByAgentType walks subagents/*.meta.json and returns a map of
// agentType -> list of (path, first_timestamp) sorted by start time.
func indexSidechainsByAgentType(sidechainDir string) map[string][]sidechainEntry {
	idx := map[string][]sidechainEntry{}
	matches, _ := filepath.Glob(filepath.Join(sidechainDir, "*.meta.json"))
	for _, mp := range matches {
		b, err := os.ReadFile(mp)
		if err != nil {
			continue
		}
		var m struct {
			AgentType string `json:"agentType"`
		}
		if err := json.Unmarshal(b, &m); err != nil || m.AgentType == "" {
			continue
		}
		jsonlPath := strings.TrimSuffix(mp, ".meta.json") + ".jsonl"
		idx[m.AgentType] = append(idx[m.AgentType], sidechainEntry{
			Path:           jsonlPath,
			FirstTimestamp: firstTimestamp(jsonlPath),
		})
	}
	for k := range idx {
		sort.Slice(idx[k], func(i, j int) bool {
			return idx[k][i].FirstTimestamp < idx[k][j].FirstTimestamp
		})
	}
	return idx
}

// firstTimestamp returns the timestamp of the first valid line in a jsonl, or
// empty string on error. Used only for sidechain ordering, so we don't fail
// on parse errors — we just lose ordering precision.
func firstTimestamp(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var m map[string]json.RawMessage
		if err := dec.Decode(&m); err != nil {
			return ""
		}
		var ts string
		if raw, ok := m["timestamp"]; ok {
			_ = json.Unmarshal(raw, &ts)
			if ts != "" {
				return ts
			}
		}
	}
}

// resolveSidechainFiles maps an agent_id from a tool_result to the on-disk
// transcript path(s). For hex ids it is a direct file lookup. For `name@team`
// ids the matcher uses temporal partitioning: it returns every sidechain whose
// agentType matches `name` AND whose first timestamp falls on or after the
// spawn timestamp but before the next same-typed spawn at or after the spawn.
// nextSameTypeTs is the spawn timestamp of the next same-typed spawn in the
// session ("" if this is the last). Without temporal partitioning, sessions
// that invoke the same plugin twice would attach every matching transcript to
// the first spawn, hiding the second invocation's specialist work.
func resolveSidechainFiles(agentID, spawnTs, nextSameTypeTs, sidechainDir string, byAgentType map[string][]sidechainEntry) []string {
	if agentID == "" {
		return nil
	}
	if i := strings.Index(agentID, "@"); i > 0 {
		name := agentID[:i]
		var out []string
		for _, e := range byAgentType[name] {
			if spawnTs != "" && e.FirstTimestamp != "" && e.FirstTimestamp < spawnTs {
				continue
			}
			if nextSameTypeTs != "" && e.FirstTimestamp != "" && e.FirstTimestamp >= nextSameTypeTs {
				continue
			}
			out = append(out, e.Path)
		}
		return out
	}
	p := filepath.Join(sidechainDir, "agent-"+agentID+".jsonl")
	if _, err := os.Stat(p); err == nil {
		return []string{p}
	}
	return nil
}

// nextSameTypeSpawnTimestamp finds the timestamp of the next AgentSpawn whose
// agent_id starts with the same `name@` as the spawn at index i, or "" if
// none. Used to bound temporal partitioning of sidechains.
func nextSameTypeSpawnTimestamp(spawns []AgentSpawn, i int, currentName string) string {
	for j := i + 1; j < len(spawns); j++ {
		next := spawns[j]
		if next.AgentID == "" {
			continue
		}
		at := strings.Index(next.AgentID, "@")
		if at < 0 {
			continue
		}
		if next.AgentID[:at] == currentName {
			return next.Timestamp
		}
	}
	return ""
}

// usageFromToolResult parses the <usage>...</usage> block that the Agent tool_result
// emits when a subagent completes. Returns zeroed values if absent or unparseable.
func usageFromToolResult(text string) (totalTokens int64, toolUses int, durationMs int64) {
	start := strings.Index(text, "<usage>")
	end := strings.Index(text, "</usage>")
	if start < 0 || end < 0 || end < start {
		return
	}
	body := text[start+len("<usage>") : end]
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "total_tokens:"):
			fmt.Sscanf(strings.TrimSpace(line[len("total_tokens:"):]), "%d", &totalTokens)
		case strings.HasPrefix(line, "tool_uses:"):
			fmt.Sscanf(strings.TrimSpace(line[len("tool_uses:"):]), "%d", &toolUses)
		case strings.HasPrefix(line, "duration_ms:"):
			fmt.Sscanf(strings.TrimSpace(line[len("duration_ms:"):]), "%d", &durationMs)
		}
	}
	return
}

// loadSidechainMeta reads the sibling .meta.json next to a sidechain jsonl, if present.
func loadSidechainMeta(jsonlPath string) (agentType, description string) {
	metaPath := strings.TrimSuffix(jsonlPath, ".jsonl") + ".meta.json"
	b, err := os.ReadFile(metaPath)
	if err != nil {
		return
	}
	var m struct {
		AgentType   string `json:"agentType"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(b, &m); err == nil {
		return m.AgentType, m.Description
	}
	return
}

// parseSidechain recursively parses one subagent transcript and the descendants
// it spawned (rare but possible). visited prevents cycles when the same path
// appears under two different spawns.
func parseSidechain(jsonlPath string, sidechainDir string, byAgentType map[string][]sidechainEntry, visited map[string]bool, includeEvents bool) (*Sidechain, error) {
	if visited[jsonlPath] {
		return nil, nil
	}
	visited[jsonlPath] = true

	pf, err := parseFile(jsonlPath)
	if err != nil {
		return nil, err
	}
	events, stats := correlateAndStat(pf)
	agentType, description := loadSidechainMeta(jsonlPath)

	// Two passes — same as parseSession — so temporal partitioning has the
	// next-same-type timestamp available.
	for i := range events.AgentSpawns {
		spawn := &events.AgentSpawns[i]
		res, ok := pf.ToolResultsByID[spawn.ToolUseID]
		if !ok {
			continue
		}
		spawn.AgentID = agentIDFromToolResult(res.rawText)
		spawn.UsageTotalTokens, spawn.UsageToolUses, spawn.UsageDurationMs = usageFromToolResult(res.rawText)
	}
	for i := range events.AgentSpawns {
		spawn := &events.AgentSpawns[i]
		if spawn.AgentID == "" {
			continue
		}
		var name, nextTs string
		if at := strings.Index(spawn.AgentID, "@"); at > 0 {
			name = spawn.AgentID[:at]
			nextTs = nextSameTypeSpawnTimestamp(events.AgentSpawns, i, name)
		}
		paths := resolveSidechainFiles(spawn.AgentID, spawn.Timestamp, nextTs, sidechainDir, byAgentType)
		if len(paths) == 0 {
			continue
		}
		spawn.SidechainFile = paths[0]
		for _, p := range paths {
			child, err := parseSidechain(p, sidechainDir, byAgentType, visited, includeEvents)
			if err != nil || child == nil {
				continue
			}
			if spawn.Sidechain == nil {
				spawn.Sidechain = child
			}
			spawn.Sidechains = append(spawn.Sidechains, child)
		}
	}

	sc := &Sidechain{
		AgentType:      agentType,
		Description:    description,
		FirstTimestamp: pf.FirstTimestamp,
		LastTimestamp:  pf.LastTimestamp,
		Stats:          stats,
	}
	if includeEvents {
		sc.Events = events
	}
	return sc, nil
}

func parseSession(path string) (*Session, error) {
	pf, err := parseFile(path)
	if err != nil {
		return nil, err
	}
	events, stats := correlateAndStat(pf)

	sess := &Session{
		SourceFile:     path,
		SessionID:      pf.SessionID,
		CWD:            pf.CWD,
		GitBranch:      pf.GitBranch,
		Version:        pf.Version,
		FirstTimestamp: pf.FirstTimestamp,
		LastTimestamp:  pf.LastTimestamp,
		Stats:          stats,
		Events:         events,
	}

	// Locate the sibling subagents/ dir. Layout:
	//   <project-dir>/<session-uuid>.jsonl       (this file)
	//   <project-dir>/<session-uuid>/subagents/agent-<id>.jsonl
	sidechainDir := strings.TrimSuffix(path, ".jsonl")
	sidechainDir = filepath.Join(sidechainDir, "subagents")
	if info, err := os.Stat(sidechainDir); err == nil && info.IsDir() {
		byAgentType := indexSidechainsByAgentType(sidechainDir)
		visited := map[string]bool{path: true}

		// Pass 1: populate agent_id + usage on every spawn so pass 2 can use
		// the next-same-type spawn timestamp for temporal partitioning.
		for i := range events.AgentSpawns {
			spawn := &events.AgentSpawns[i]
			res, ok := pf.ToolResultsByID[spawn.ToolUseID]
			if !ok {
				continue
			}
			spawn.AgentID = agentIDFromToolResult(res.rawText)
			spawn.UsageTotalTokens, spawn.UsageToolUses, spawn.UsageDurationMs = usageFromToolResult(res.rawText)
		}
		// Pass 2: resolve and attach sidechains, bounded by the next spawn of
		// the same name@team so two `/code-review` runs in one session don't
		// collapse onto the first spawn.
		for i := range events.AgentSpawns {
			spawn := &events.AgentSpawns[i]
			if spawn.AgentID == "" {
				continue
			}
			var name, nextTs string
			if at := strings.Index(spawn.AgentID, "@"); at > 0 {
				name = spawn.AgentID[:at]
				nextTs = nextSameTypeSpawnTimestamp(events.AgentSpawns, i, name)
			}
			paths := resolveSidechainFiles(spawn.AgentID, spawn.Timestamp, nextTs, sidechainDir, byAgentType)
			if len(paths) == 0 {
				continue
			}
			spawn.SidechainFile = paths[0]
			for _, p := range paths {
				sc, err := parseSidechain(p, sidechainDir, byAgentType, visited, true)
				if err != nil || sc == nil {
					continue
				}
				if spawn.Sidechain == nil {
					spawn.Sidechain = sc
				}
				spawn.Sidechains = append(spawn.Sidechains, sc)
			}
		}
	}

	scope := discoverPluginScope(sess.CWD)
	sess.PluginsUsed = detectPluginsInSession(events, scope)
	sess.PluginScopeKnown = scope
	return sess, nil
}

func expandInputs(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		info, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			matches, err := filepath.Glob(filepath.Join(a, "*.jsonl"))
			if err != nil {
				return nil, err
			}
			sort.Strings(matches)
			out = append(out, matches...)
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

func main() {
	outPath := flag.String("out", "", "write JSON here instead of stdout")
	summaryOnly := flag.Bool("summary-only", false, "drop raw events; keep stats + plugins_used")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: session-parser [--out FILE] [--summary-only] PATH [PATH ...]")
		fmt.Fprintln(os.Stderr, "  PATH may be a jsonl file or a directory containing *.jsonl")
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	files, err := expandInputs(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "input error:", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "no .jsonl files found")
		os.Exit(1)
	}

	sessions := make([]*Session, 0, len(files))
	for _, f := range files {
		s, err := parseSession(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "parse error:", f, err)
			os.Exit(1)
		}
		if *summaryOnly {
			s.Events = nil
			s.PluginScopeKnown = nil
		}
		sessions = append(sessions, s)
	}

	var payload any
	if len(sessions) == 1 {
		payload = sessions[0]
	} else {
		payload = map[string]any{"sessions": sessions}
	}

	enc := json.NewEncoder(os.Stdout)
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "write error:", err)
			os.Exit(1)
		}
		defer f.Close()
		enc = json.NewEncoder(f)
	}
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		fmt.Fprintln(os.Stderr, "encode error:", err)
		os.Exit(1)
	}
}
