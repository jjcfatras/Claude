---
allowed-tools: Bash(node:*)
description: Code review a pull request via a multi-specialist agent team. Orchestrated in TypeScript via the Anthropic Claude Agent SDK; runs N specialists in parallel, consolidates findings, and posts inline review comments.
argument-hint: [pr-number]
disable-model-invocation: false
model: sonnet
effort: low
---

Run `CLAUDE_PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT}" node "${CLAUDE_PLUGIN_ROOT}/dist/cli.js" $ARGUMENTS` and report its output to the user. Pass through any prompts the CLI asks (it uses readline for the post-confirmation Y/n/ids question; surface its stdout/stderr verbatim). If the CLI exits non-zero, surface the error verbatim — do not introspect the plugin source.
