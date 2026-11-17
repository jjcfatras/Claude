#!/usr/bin/env bash
# Print a finished /deep:research Workflow's return value: the `result` key of the task output file named in
# the completion notification's <output-file>. Most of that file is harness progress data (`workflowProgress`),
# so reading it whole overflows a Read; journal.jsonl holds only per-agent results, not the return value.
set -eu
if [ $# -ne 1 ] || [ ! -f "$1" ]; then
  echo "usage: result.sh <workflow task output file>" >&2
  exit 1
fi
if command -v jq > /dev/null 2>&1; then
  exec jq '.result' "$1"
fi
exec python3 -I -c 'import json, sys; print(json.dumps(json.load(open(sys.argv[1]))["result"], indent=2, ensure_ascii=False))' "$1"
