#!/usr/bin/env bash
# Format the given files (default: every tracked file), routed by type to the repo's Go formatters.
# Single routing source for `make format`, the lefthook pre-commit job, and .claude/hooks/format.sh.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# go run, not go tool: go-prettier does not support `go tool` (golang/go#71192).
PRETTIER=github.com/wasilibs/go-prettier/v3/cmd/prettier@v3.10.1

if [ $# -eq 0 ]; then
  while IFS= read -r f; do set -- "$@" "$f"; done < <(git ls-files)
fi

gofiles=() mods=() sh=() yaml=() json=() web=()
for f in "$@"; do
  f=${f#"$PWD"/} # the PostToolUse hook passes absolute paths
  [ -f "$f" ] || continue
  case "$f" in
    # byte-compared goldens and the raw fixtures that drive them
    plugins/pr-review/tools/code-review-helper/testdata/*) ;;
    *.go) gofiles+=("$f") ;;
    go.mod | */go.mod) mods+=("$f") ;;
    *.sh) sh+=("$f") ;;
    *.yml | *.yaml) yaml+=("$f") ;;
    *.json) json+=("$f") ;;
    *.md | *.js) web+=("$f") ;;
    *) case $(head -c 64 "$f" | head -n 1) in '#!'*sh*) sh+=("$f") ;; esac ;;
  esac
done

[ ${#gofiles[@]} -eq 0 ] || gofmt -w "${gofiles[@]}"
[ ${#mods[@]} -eq 0 ] || for m in "${mods[@]}"; do go mod edit -fmt "$m"; done
[ ${#sh[@]} -eq 0 ] || go tool shfmt -w -i 2 -bn -ci -sr "${sh[@]}"
[ ${#yaml[@]} -eq 0 ] || go tool yamlfmt -formatter retain_line_breaks_single=true "${yaml[@]}"
[ ${#json[@]} -eq 0 ] || go run ./tools/jsonfmt "${json[@]}"
[ ${#web[@]} -eq 0 ] || go run "$PRETTIER" --write --log-level warn "${web[@]}"
