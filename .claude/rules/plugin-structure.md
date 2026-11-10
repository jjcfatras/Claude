---
paths:
  - "plugins/**"
  - ".claude-plugin/**"
---

# Plugin structure

Every plugin follows Anthropic's [standard layout](https://code.claude.com/docs/en/plugins/manifest-reference#standard-layout) (per component: [Add components to a plugin](https://code.claude.com/docs/en/plugins/components)). What each plugin here ships is listed in the root `CLAUDE.md` › Per-plugin layout. What goes inside a skill folder is in [skill-structure.md](skill-structure.md).

## Layout

| Location                     | Holds                                                                                                                               | Here                          |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- | ----------------------------- |
| `.claude-plugin/plugin.json` | The manifest. The only file in `.claude-plugin/`: components saved there don't load                                                 | all                           |
| `skills/<name>/SKILL.md`     | Skills, one folder each                                                                                                             | most                          |
| `commands/*.md`              | Legacy single-file commands. Write new ones as skills                                                                               | `debate`, `respond-to-review` |
| `agents/*.md`                | Subagents. A subfolder becomes part of the name: `agents/review/x.md` loads as `<plugin>:review:x`                                  | `review-pr`, `debate`         |
| `hooks/hooks.json`           | Hooks, wrapped in a top-level `"hooks"` key                                                                                         | `caffeine`                    |
| `scripts/`                   | Files that hooks or skills run by path. A convention, not a folder Claude Code scans                                                | `caffeine`, `review-pr`       |
| `bin/`                       | Executables. Put on the Bash tool's `PATH` while the plugin is enabled, and makes claude.ai and Cowork refuse to install the plugin | —                             |
| `references/`, `tools/`      | Repo conventions, not scanned: docs shared by a plugin's skills and agents; source for a helper built into `scripts/`               | `review-pr`, `jira`, …        |

No plugin here ships a top-level `bin/`. `review-pr` keeps its prebuilt helper in `scripts/` and calls it by its `${CLAUDE_PLUGIN_ROOT}` path, so it never needs `bin/`'s `PATH` entry and stays installable on claude.ai and Cowork. `.mcp.json`, `.lsp.json`, `settings.json`, `output-styles/`, `monitors/` and the rest of the standard layout are unused here.

Add only the folders a plugin uses. Don't put a `CLAUDE.md` at a plugin root: Claude Code doesn't load it, and `claude plugin validate` warns about it. Put instructions in a skill.

## Manifest

- `name` is the only required field. Use kebab-case, never with a `claude-` or `anthropic-` prefix. Never change it after release: users' `enabledPlugins` and `pluginConfigs` are keyed on it. To change the label users see, set `displayName`. If a rename is unavoidable, it's a MAJOR bump, and you add `"<old>": "<new>"` to a top-level `renames` map in `.claude-plugin/marketplace.json` so existing installs migrate. Treat that map as append-only.
- Keep `version`, `description` and `author`; `claude plugin validate` warns when one is missing.
- `version` follows [plugin-versioning.md](plugin-versioning.md). This repo deliberately departs from the docs here. The docs say to set `version` in one place only, but this repo sets it in both `plugin.json` and the plugin's marketplace entry. The version-sync hook keeps the two equal; if they ever differed, `plugin.json` would win without a warning.
- Every plugin here uses the default locations, with no component path keys. If you need one, its paths start with `./` and stay inside the plugin. `commands` and `agents` replace the default folder scan, `skills` adds to it, and `hooks`, `mcpServers` and `lspServers` merge with the default file.

## Paths at runtime

- An install is copied into a per-version cache, so reach plugin files only through `${CLAUDE_PLUGIN_ROOT}`. Never use `..` or a path into this repo. When a plugin is copied in, symlinks to elsewhere in the marketplace are replaced by their targets, and symlinks pointing outside it are skipped.
- `${CLAUDE_PLUGIN_ROOT}` changes on every update, so don't write state there. Use `${CLAUDE_PLUGIN_DATA}`, which survives updates, as `caffeine` does.
- In a shell-form hook command, quote the variable so a path with spaces stays one word: `"\"${CLAUDE_PLUGIN_ROOT}\"/scripts/caffeine.sh on"`.
- Claude Code substitutes these variables in skill, command and agent bodies and exports them to hook, MCP and LSP processes. They are not set in the Bash tool's environment, so write `${CLAUDE_PLUGIN_ROOT}` in the Markdown body, not as `$CLAUDE_PLUGIN_ROOT` in a command Claude composes.

## Validate

Run `pnpm validate:plugins` after a structural change. It runs Anthropic's `claude plugin validate --strict` on the marketplace and every plugin. The PostToolUse hooks only check frontmatter `description` and the manifest `name`/`version`. `validate` also checks every skill, agent and command frontmatter, and catches paths that are missing or escape the plugin.
