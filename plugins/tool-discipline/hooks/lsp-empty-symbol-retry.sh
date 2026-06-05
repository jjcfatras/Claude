#!/bin/bash
# lsp-empty-symbol-retry.sh
# PostToolUse advisory for the LSP tool. When `workspaceSymbol` returns empty,
# inject guidance so the agent retries / pivots to a document-scoped op instead
# of abandoning LSP — empty-on-cold-start is expected (the server may still be
# indexing), not proof the symbol is absent.
#
# Advisory only: never blocks, never denies. Safe under any permission mode.
#
# Fail-open / opt-out (mirrors enforce-lsp-navigation.sh):
#   TOOL_DISCIPLINE_BLOCK_LSP=0  -> disable this hook entirely.
#   ENABLE_LSP_TOOL not "1"      -> the LSP tool is off; nothing to advise on.
INPUT=$(cat)

# Opt-out switch (shared with the LSP-steering PreToolUse hook).
[ "${TOOL_DISCIPLINE_BLOCK_LSP:-1}" = "0" ] && exit 0
# Only meaningful when the LSP tool is enabled.
[ "${ENABLE_LSP_TOOL:-}" = "1" ] || exit 0

TOOL=$(echo "$INPUT" | jq -r '.tool_name // empty')
[ "$TOOL" = "LSP" ] || exit 0

OP=$(echo "$INPUT" | jq -r '.tool_input.operation // empty')
[ "$OP" = "workspaceSymbol" ] || exit 0

# Empty signal: resultCount == 0, OR the result text carries the cold-start /
# no-symbols message. tool_response is normally the object
# {operation,result,resultCount,fileCount}; tolerate it arriving as a bare
# string too (index with `?` so a string doesn't error the filter).
EMPTY=$(echo "$INPUT" | jq -r '
  (.tool_response // {}) as $r
  | (if ($r | type) == "object" then ($r.resultCount // -1) else -1 end) as $rc
  | (if ($r | type) == "object" then ($r.result // "") else ($r | tostring) end) as $txt
  | if ($rc == 0) or ($txt | test("not finished indexing|No symbols found in workspace"))
    then "1" else "0" end
')
[ "$EMPTY" = "1" ] || exit 0

read -r -d '' CTX << 'EOF'
`workspaceSymbol` returned no results. Two likely causes: (a) the LSP server hasn't finished indexing yet — common right after session or server start — or (b) the symbol isn't named exactly that. Don't abandon LSP on this single miss. Prefer: if you already know a file that references the symbol, run `goToDefinition`/`hover` there (loads the project, no global index needed); otherwise retry `workspaceSymbol` once. Only after a retry still fails should you treat it as genuinely absent and fall back to `rg` (set `TOOL_DISCIPLINE_BLOCK_LSP=0` for that one search if the LSP-steering hook blocks it).
EOF

jq -n --arg ctx "$CTX" \
  '{hookSpecificOutput:{hookEventName:"PostToolUse",additionalContext:$ctx}}'
exit 0
