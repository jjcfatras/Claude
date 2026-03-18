# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Environment

- **Node.js**: v22.21.1 (see `.nvmrc`)
- **Package manager**: pnpm (v10.32.1)
- **Formatting**: Prettier 3.8.1 (default config, no `.prettierrc`)

## Commands

- `pnpm install` — install dependencies
- `pnpm prettier --write .` — format all files
- `pnpm prettier --check .` — check formatting without writing

## Project Structure

This is a Claude Code skills/customization project. The `src/skills/` directory contains custom skill definitions (markdown files with frontmatter) that extend Claude Code's capabilities — e.g., `code-review.md` defines a structured PR review workflow.
