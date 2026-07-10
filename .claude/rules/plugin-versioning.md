# Plugin versioning

When a change touches anything under `plugins/<name>/` (commands, agents, references, helper sources, prebuilt binaries, the plugin manifest itself), bump the `version` field in `plugins/<name>/.claude-plugin/plugin.json` per [Semantic Versioning 2.0](https://semver.org/) — `MAJOR.MINOR.PATCH`:

- **MAJOR** — backwards-incompatible change. Examples: removing or renaming a slash command, removing a command flag/argument, changing a command's required arguments, removing or renaming an agent, breaking a published reference path that other tools consume.
- **MINOR** — backwards-compatible new functionality. Examples: adding a new slash command, adding a new agent or specialist domain, adding a new optional flag/argument to an existing command, adding a new reference doc.
- **PATCH** — backwards-compatible fix or internal-only change. Examples: bug fix in a command/agent, prompt or wording tweaks, refactoring the Go helper without changing its CLI surface, rebuilding `bin/*` prebuilts from unchanged sources, dependency-only updates, formatting/typo fixes.

Bump rules:

- Bump only the affected plugin(s). A change scoped to `plugins/code-review/` does not touch `plugins/transcript/.claude-plugin/plugin.json`.
- A single change picks one tier — the highest tier triggered by any part of the diff. (A breaking command rename plus a bug fix is MAJOR, not MAJOR + PATCH.)
- Bumping a higher tier resets lower tiers to `0` (1.4.7 → MINOR → 1.5.0, not 1.5.7).
- Pure non-plugin changes (root `CLAUDE.md`, `.claude/settings.json`, `.claude-plugin/marketplace.json`, repo-level docs, `code-review-workspace/`) do not require any plugin version bump.
- Include the manifest version bump in the same commit as the plugin change.
