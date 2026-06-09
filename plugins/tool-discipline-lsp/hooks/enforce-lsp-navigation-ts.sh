#!/bin/bash
# enforce-lsp-navigation-ts.sh
# PreToolUse hook: steer TypeScript-family symbol searches (the Grep tool, or
# `grep`/`rg` run via Bash) toward the LSP tool. Thin wrapper — see
# lib/lsp-nav-common.sh for the shared engine and enforce-lsp-navigation-go.sh
# for the sibling Go hook.
#
# Fail-open / opt-out (all handled in the shared lib):
#   TOOL_DISCIPLINE_BLOCK_LSP=0       -> disable.
#   ENABLE_LSP_TOOL not "1"           -> steered-to tool off; do nothing.
#   typescript-language-server absent -> no LSP to use; pass through.
#   TOOL_DISCIPLINE_LSP_MODE=ask      -> soft prompt instead of a hard deny.
source "${BASH_SOURCE[0]%/*}/lib/lsp-nav-common.sh"

LSP_LANG_LABEL="TS"
LSP_TYPE_NAMES="ts typescript tsx js javascript jsx"
LSP_EXTENSIONS="ts tsx js jsx mts cts mjs cjs"

lsp_server_available() { command -v typescript-language-server > /dev/null 2>&1; }

run_lsp_nav
