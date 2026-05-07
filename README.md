# jjcfatras-tools — Claude Code marketplace

A Claude Code [plugin marketplace](https://docs.claude.com/en/docs/claude-code/plugin-marketplaces) shipping four slash commands the author uses for everyday Git, testing, and code-review workflows.

## Install

```text
/plugin marketplace add jjcfatras/Claude
/plugin install <plugin-name>@jjcfatras-tools
```

## Plugins

| Plugin              | Slash command                                             | What it does                                                                                                                                               |
| ------------------- | --------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `cherry-pick`       | `/cherry-pick <source-branch> [commit-sha or sha1..sha2]` | Cherry-picks one or more commits from a source branch into the current branch and resolves conflicts intelligently.                                        |
| `test-driven-fix`   | `/test-driven-fix <spec-or-bug>`                          | Autonomous patch → test → revert-on-regression loop, hard-capped at 10 iterations.                                                                         |
| `respond-to-review` | `/respond-to-review <pr-number> [comment-id]`             | Triages every flagged issue on a PR — inline comments and review-body findings — dismissing false positives and fixing valid ones.                         |
| `code-review`       | `/code-review [pr-number]`                                | Multi-specialist PR review (security, typescript, react, infra, errors, perf, quality, claude-md) coordinated via a sub-agent team. Posts inline comments. |

Install only the plugins you want — each is independent.

## `code-review` — extras

- Bundles a Go helper (`code-review-helper`) used to deterministically parse diffs and assemble review payloads. The plugin ships prebuilt binaries for `darwin-amd64`, `darwin-arm64`, `linux-amd64`, and `linux-arm64`; a `bin/code-review-helper` shell wrapper dispatches to the right one.
- Installs eight `code-review-*` review specialists into the team that runs the review.

### Building the helper from source

```sh
cd "${CLAUDE_PLUGIN_ROOT}/tools/code-review-helper"
make release # cross-compile all 4 platforms into ../../bin/
make test
```

`make release` is what the author runs before tagging a new plugin version. End users do not need a Go toolchain.

## Repository layout

```
.claude-plugin/marketplace.json   # marketplace manifest
plugins/<name>/                    # one directory per plugin
  .claude-plugin/plugin.json
  commands/<name>.md
  agents/, references/, bin/, tools/  # only where the plugin needs them
```
