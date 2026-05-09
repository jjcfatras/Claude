# Shared header for path-filtering Edit|Write hooks.
# Source via:  . "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"
# Sets HOOK_FILE to the tool's target file_path (parsed once from stdin).
# shellcheck shell=bash
set -euo pipefail
HOOK_FILE=$(jq -r '.tool_input.file_path')
