#!/bin/bash
# enforce-lsp-navigation-go.sh
# PreToolUse hook: steer Go symbol searches (the Grep tool, or `grep`/`rg` run
# via Bash) toward the LSP tool. Thin wrapper — all parsing and symbol-gating
# live in lib/lsp-nav-common.sh; this file only declares the Go language
# contract. Registered alongside the TS variant under the same Bash|Grep
# matcher: CC runs both in parallel and merges decisions most-restrictive-wins,
# and a command resolves to at most one language scope, so at most one ever
# denies.
#
# Fail-open / opt-out (all handled in the shared lib):
#   TOOL_DISCIPLINE_BLOCK_LSP=0  -> disable.
#   ENABLE_LSP_TOOL not "1"      -> steered-to tool off; do nothing.
#   gopls absent                 -> no LSP to use; pass through.
#   TOOL_DISCIPLINE_LSP_MODE=ask -> soft prompt instead of a hard deny.
source "${BASH_SOURCE[0]%/*}/lib/lsp-nav-common.sh"

LSP_LANG_LABEL="Go"
LSP_TYPE_NAMES="go"
LSP_EXTENSIONS="go"

# gopls commonly installs to ~/go/bin (or $GOBIN / $GOPATH/bin), which is often
# absent from the hook's PATH, so check those locations explicitly too.
lsp_server_available() {
  command -v gopls > /dev/null 2>&1 \
    || [ -x "${GOBIN:-${GOPATH:-$HOME/go}/bin}/gopls" ] \
    || [ -x "$HOME/go/bin/gopls" ]
}

run_lsp_nav
