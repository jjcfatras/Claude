#!/usr/bin/env bash
# /pr-review-v2 steps [3/5] and [4/5]: write the GitHub payloads from the review workflow's result.
# Usage: payload.sh <workflow task output file> <TMP> [comma-separated finding IDs]
# Writes payload.json, payload-pending.json, payload-body.json and fallback.md under <TMP>. Without IDs it then
# prints the review summary; with IDs it rewrites the four files for just that subset.
set -eu
OUT=${1:-}
TMP=${2:-}
if [ ! -f "$OUT" ] || [ ! -d "$TMP" ]; then
  echo "usage: payload.sh <workflow task output file> <TMP> [finding-ids]" >&2
  exit 1
fi
HERE=$(cd "$(dirname "$0")" && pwd)
IDS=null
[ -n "${3:-}" ] && IDS=$(jq -cn --arg s "$3" '$s | split(",") | map(gsub("^\\s+|\\s+$"; "") | select(length > 0))')

render() { jq "$1" --argjson ids "$IDS" --arg mode "$2" -f "$HERE/payload.jq" "$OUT"; }
render -c payload > "$TMP/payload.json"
render -c pending > "$TMP/payload-pending.json"
render -c body > "$TMP/payload-body.json"
render -j fallback > "$TMP/fallback.md"
[ "$IDS" != null ] || jq -r '.result.summary' "$OUT"
