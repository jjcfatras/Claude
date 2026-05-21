package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

// promptTemplate is the per-role specialist prompt body. Placeholders are
// substituted by runSpawnManifest before the manifest is written; the
// orchestrator forwards the rendered prompt verbatim to the Agent tool.
//
// Placeholders:
//
//	{{ROLE}}, {{TMP}}, {{PR_NUMBER}}, {{HEAD_SHA}}, {{REPO_ROOT}}, {{OWNER}}, {{REPO}}
//
// The wording mirrors the legacy inline template in commands/code-review.md
// §[3b/5]; keep them in sync if either side is edited.
const promptTemplate = "Read {{TMP}}/spawn-context.md once at startup " +
	"(use offset:0, limit:200 and paginate — the bundle may exceed the 25,000-token Read cap on large PRs) " +
	"and Read {{TMP}}/rubric.md once. " +
	"Scan {{TMP}}/pr-{{PR_NUMBER}}.diff for issues in your domain " +
	"(the raw diff is often >256 KB — use the `## Diff map` section in spawn-context.md to pick a targeted offset+limit, " +
	"or `Bash: grep -n \"^diff --git\"` to enumerate file sections; do not bare-Read the diff). " +
	"Populate `suggested_fix` whenever the fix is a concrete code replacement; " +
	"use `null` only for structural findings where no single-snippet replacement applies, " +
	"and set `startLine` when the replacement spans more than one line. " +
	"Then Write findings JSON to {{TMP}}/findings/{{ROLE}}.json per the rubric schema.\n\n" +
	"HEAD_SHA: {{HEAD_SHA}}\n" +
	"REPO_ROOT: {{REPO_ROOT}}\n" +
	"REVIEW_TMPDIR: {{TMP}}\n" +
	"PR: #{{PR_NUMBER}} in {{OWNER}}/{{REPO}}"

type spawnEntry struct {
	SubagentType string `json:"subagent_type"`
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`
}

func runSpawnManifest(argv []string) error {
	fs := flag.NewFlagSet("spawn-manifest", flag.ContinueOnError)
	rosterPath := fs.String("roster", "", "path to roster JSON array (from `roster --out-roster`)")
	reviewTmpDir := fs.String("review-tmpdir", "", "path to $REVIEW_TMPDIR (substituted into prompts as {{TMP}})")
	headSHA := fs.String("head-sha", "", "full HEAD SHA")
	prNumber := fs.Int("pr-number", 0, "pull request number")
	owner := fs.String("owner", "", "GitHub owner")
	repo := fs.String("repo", "", "GitHub repo")
	repoRoot := fs.String("repo-root", "", "repo working-tree root; emitted as REPO_ROOT so specialists never synthesize paths from cwd")
	out := fs.String("out", "", "output path for spawn-manifest JSON array")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *rosterPath == "" || *reviewTmpDir == "" || *headSHA == "" || *prNumber == 0 || *owner == "" || *repo == "" || *repoRoot == "" || *out == "" {
		return fmt.Errorf("spawn-manifest: --roster, --review-tmpdir, --head-sha, --pr-number, --owner, --repo, --repo-root, --out are all required")
	}

	raw, err := os.ReadFile(*rosterPath)
	if err != nil {
		return fmt.Errorf("read --roster: %w", err)
	}
	var roles []string
	if err := json.Unmarshal(raw, &roles); err != nil {
		return fmt.Errorf("parse --roster: %w", err)
	}
	if len(roles) == 0 {
		return fmt.Errorf("spawn-manifest: roster is empty; nothing to spawn")
	}

	prNumberStr := fmt.Sprintf("%d", *prNumber)
	subs := strings.NewReplacer(
		"{{TMP}}", *reviewTmpDir,
		"{{PR_NUMBER}}", prNumberStr,
		"{{HEAD_SHA}}", *headSHA,
		"{{REPO_ROOT}}", *repoRoot,
		"{{OWNER}}", *owner,
		"{{REPO}}", *repo,
	)

	entries := make([]spawnEntry, 0, len(roles))
	for _, role := range roles {
		if role == "" {
			return fmt.Errorf("spawn-manifest: roster contains an empty role name")
		}
		rolePrompt := strings.ReplaceAll(promptTemplate, "{{ROLE}}", role)
		entries = append(entries, spawnEntry{
			SubagentType: "code-review:" + role,
			Description:  role + " specialist scan",
			Prompt:       subs.Replace(rolePrompt),
		})
	}

	return writeJSON(*out, entries)
}
