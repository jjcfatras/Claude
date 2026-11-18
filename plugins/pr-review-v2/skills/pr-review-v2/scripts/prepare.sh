#!/usr/bin/env bash
# /pr-review-v2 step [1/5]: fetch PR <n> into a fresh scratch dir and print the review workflow's args.
# Usage: prepare.sh <pr-number> <rubric-path>
# Prints shell-safe KEY=value lines: TMP= first, before anything can fail, so Cleanup always has it, and
# ARGS=<json> last. Exits non-zero with the reason on stderr when the review can't run.
set -u

PR=${1:-}
RUBRIC=${2:-}
case "$PR" in
  '' | *[!0-9]*)
    echo "usage: prepare.sh <pr-number> <rubric-path>" >&2
    exit 1
    ;;
esac
HERE=$(cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(git rev-parse --show-toplevel) || exit 1
cd "$REPO_ROOT" || exit 1

TMP_ROOT=${TMPDIR:-/tmp}
TMP="$TMP_ROOT/pr-review-v2-$PR-$(date +%s)"
# Own namespace: pr-review v1 sweeps refs/code-review/* refs that have no live pr-review-<n>-* dir.
REF="refs/pr-review-v2/pr-$PR"
mkdir -p "$TMP/scratch" || exit 1
echo "TMP=$TMP"

# Reap what an earlier run's skipped Cleanup left behind: dirs at least 48 h old, and refs whose review dir is
# gone. Never this PR's ref, which the fetch below overwrites anyway.
find "$TMP_ROOT" -maxdepth 1 -type d -name 'pr-review-v2-*' -mtime +1 -exec rm -rf {} + 2> /dev/null
git for-each-ref --format='%(refname)' refs/pr-review-v2/ 2> /dev/null | while IFS= read -r ref; do
  n=${ref#refs/pr-review-v2/pr-}
  [ "$n" = "$PR" ] && continue
  set -- "$TMP_ROOT"/pr-review-v2-"$n"-*
  [ -e "$1" ] || git update-ref -d "$ref" 2> /dev/null
done

gh pr view "$PR" --json headRefOid,url,number,state,author,baseRefName > "$TMP/meta.json" 2> "$TMP/meta.err" &
META=$!
gh pr diff "$PR" > "$TMP/pr.diff" 2> "$TMP/diff.err" &
DIFF=$!
git fetch origin "+pull/$PR/head:$REF" --no-tags > /dev/null 2> "$TMP/fetch.err" &
FETCH=$!

stop() {
  kill "$DIFF" "$FETCH" 2> /dev/null
  wait
  echo "$1" >&2
  exit 1
}
wait "$META" || stop "gh pr view failed: $(cat "$TMP/meta.err")"
eval "$(jq -r '@sh "HEAD_SHA=\(.headRefOid) STATE=\(.state) AUTHOR=\(.author.login // "") BASE=\(.baseRefName)", (.url | capture("^https?://[^/]+/(?<o>[^/]+)/(?<r>[^/]+)/pull/") | @sh "OWNER=\(.o) REPO=\(.r)")' "$TMP/meta.json")"
# Reviewing a merged or closed PR wastes every specialist and risks posting to a finished thread.
[ "$STATE" = OPEN ] || stop "PR #$PR is $STATE — aborting"

# Prior review threads need owner/repo, so this starts once meta lands, while the diff and fetch still run.
# 50 threads x 50 comments is one page; a PR past that needs a cursor loop here.
Q='query($owner:String!,$repo:String!,$pr:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$pr){reviewThreads(first:50){nodes{isResolved isOutdated comments(first:50){nodes{author{login} body path line originalLine}}}}}}}'
if ! gh api graphql -F owner="$OWNER" -F repo="$REPO" -F pr="$PR" -f query="$Q" > "$TMP/threads.json" 2> "$TMP/threads.err"; then
  echo "warning: prior review threads unavailable, prior-review dedup skipped: $(cat "$TMP/threads.err")" >&2
  echo '{}' > "$TMP/threads.json"
fi

FETCHED=1
if ! wait "$FETCH"; then
  FETCHED=0
  echo "warning: git fetch of $REF failed, so specialists can read HEAD only if this clone already has it: $(cat "$TMP/fetch.err")" >&2
fi
if ! wait "$DIFF"; then
  # GitHub refuses diffs over 20,000 lines; rebuild it from the fetched ref with GitHub's merge-base semantics.
  case "$(cat "$TMP/diff.err")" in
    *too_large* | *"HTTP 406"*) [ "$FETCHED" = 1 ] || stop "gh pr diff failed and the local fallback needs $REF: $(cat "$TMP/diff.err")" ;;
    *) stop "gh pr diff failed: $(cat "$TMP/diff.err")" ;;
  esac
  git fetch origin "$BASE" --no-tags > /dev/null 2>&1 || echo "warning: git fetch origin $BASE failed; diffing against the existing origin/$BASE" >&2
  git diff --no-color --no-ext-diff --src-prefix=a/ --dst-prefix=b/ "origin/$BASE...$REF" > "$TMP/pr.diff" 2> "$TMP/diff.err" \
    || stop "gh pr diff refused the PR as too large, and the local diff failed: $(cat "$TMP/diff.err")"
  echo "warning: gh pr diff refused the PR as too large; built pr.diff locally from origin/$BASE...$REF" >&2
fi

# Guidance files are read at the PR head: the PR, not the reviewer's checkout, decides which rules apply.
if git cat-file -e "$HEAD_SHA^{commit}" 2> /dev/null; then
  list_files() { git ls-tree -r --name-only "$HEAD_SHA" -- "$@"; }
  show_file() { git show "$HEAD_SHA:$1"; }
else
  echo "warning: $HEAD_SHA isn't in this clone; CLAUDE.md files and rules are read from the working tree" >&2
  list_files() { git ls-files -- "$@"; }
  show_file() { cat "$1"; }
fi
list_files | awk '/(^|\/)CLAUDE\.md$/' > "$TMP/claude-md-all.txt"
# Path-scoped rules only: unscoped ones load at startup like CLAUDE.md. The workflow matches their globs, so
# only the `paths:` key (and its block-list items) travels in ARGS: rulePaths in review.js reads nothing else.
list_files .claude/rules/ | awk '/\.md$/' | while IFS= read -r f; do
  fm=$(show_file "$f" | awk '
    NR == 1 { if ($0 !~ /^---\r?$/) exit; next }
    /^---/ { exit }
    { sub(/\r$/, "") }
    /^paths:/ { p = 1; print; next }
    p && /^[ \t]*- / { print; next }
    { p = 0 }')
  case "$fm" in *paths:*) jq -n --arg path "$f" --arg fm "$fm" '{path: $path, frontmatter: $fm}' ;; esac
done | jq -s . > "$TMP/rules.json"

awk -f "$HERE/diff-ranges.awk" "$TMP/pr.diff" | jq -R -s '
  reduce (split("\n")[] | select(length > 0) | split("\t")) as $r ({};
    if $r[0] == "F" then .[$r[1]] = {path: $r[1], hunks: [], added: []}
    elif $r[0] == "H" then .[$r[1]].hunks += [[($r[2] | tonumber), ($r[3] | tonumber)]]
    else .[$r[1]].added += [[($r[2] | tonumber), ($r[3] | tonumber)]] end)
  | [.[]] | sort_by(.path)' > "$TMP/files.json"

# One row per changed file, so specialists page pr.diff by range instead of each grepping its own index.
# added/removed count body lines only; the a/ and b/ header lines come before the first @@.
awk -v OFS='\t' '
  function emit(e) { if (p != "") print p, s, e, a, d, k }
  BEGIN { print "path", "diff_start", "diff_end", "added", "removed", "status" }
  /^diff --git / {
    emit(NR - 1)
    off = 0
    while ((i = index(substr($0, off + 1), " b/")) > 0) off += i
    p = off ? substr($0, off + 3) : "?"
    s = NR; a = d = h = 0; k = "modified"
    next
  }
  !h && /^new file mode/ { k = "new" }
  !h && /^deleted file mode/ { k = "deleted" }
  !h && /^rename to / { k = "renamed" }
  /^@@ / { h = 1; next }
  h && /^[+]/ { a++ }
  h && /^-/ { d++ }
  END { emit(NR) }' "$TMP/pr.diff" > "$TMP/diff-index.tsv"

# Only threads this plugin (or pr-review v1) opened count: their first comment carries the cr-finding marker.
# A reply from the PR author is how they dismiss a finding.
jq --arg author "$AUTHOR" '
  [.data.repository.pullRequest.reviewThreads.nodes[]?
   | . as $t | .comments.nodes[0]? // empty | . as $c
   | (.body | capture("<!-- cr-finding id=\"(?<fp>[a-f0-9]+)\"")) as $m
   | {
       path: $c.path,
       line: ($c.line // $c.originalLine // 0),
       fingerprint: $m.fp,
       snippet: ([$c.body | capture("snippet64=\"(?<s>[A-Za-z0-9+/=]+)\"").s | try @base64d catch ""] | first // ""),
       description: ($c.body | sub("<!-- cr-finding [^>]*-->\\s*"; "") | split("\n\n**Code:**")[0] | .[0:600]),
       is_resolved: $t.isResolved,
       is_outdated: $t.isOutdated,
       author_dismissed: ($author != "" and any($t.comments.nodes[1:][]; .author.login? == $author))
     }]' "$TMP/threads.json" > "$TMP/prior-issues.json" || stop "could not read the review threads"

# Workflow args stay compact: the model re-types this line into the Workflow call, ~1 s per 300 bytes.
# `files` is text, not JSON objects: a line ending in "/" sets the directory for the lines after it ("/" is
# the repo root), and every other line is `<name>\t<hunks>\t<added>`, each run list `start-end,start-end`.
# review.js decodeFiles reverses it. Added-line runs are only needed to judge prior issues, so they're kept
# for files that have one. shortcut: a path with a tab or newline breaks the format, as it already breaks
# diff-ranges.awk.
jq -n -c \
  --argjson pr "$PR" --arg owner "$OWNER" --arg repo "$REPO" --arg headSha "$HEAD_SHA" \
  --arg repoRoot "$REPO_ROOT" --arg tmp "$TMP" --arg rubricPath "$RUBRIC" \
  --slurpfile files "$TMP/files.json" --slurpfile prior "$TMP/prior-issues.json" \
  --slurpfile rules "$TMP/rules.json" --rawfile claude "$TMP/claude-md-all.txt" '
  $files[0] as $f | $prior[0] as $p
  | ($claude | split("\n") | map(select(length > 0))) as $have
  | ($p | map(.path) | unique) as $priorPaths
  | {
      pr: $pr, owner: $owner, repo: $repo, headSha: $headSha, repoRoot: $repoRoot, tmp: $tmp,
      rubricPath: $rubricPath,
      files: ($f | group_by(.path | sub("[^/]*$"; "")) | map(
          (.[0].path | sub("[^/]*$"; "") | if . == "" then "/" else . end),
          (.[] | (.path as $x | $priorPaths | index($x)) as $keep
            | [(.path | sub("^.*/"; "")),
               (.hunks | map("\(.[0])-\(.[1])") | join(",")),
               (if $keep then .added | map("\(.[0])-\(.[1])") | join(",") else "" end)]
            | join("\t") | sub("\t+$"; "")))
        | join("\n")),
      claudeMd: ([$f[].path | split("/") | .[:-1] as $d
        | range(0; ($d | length) + 1) as $i | $d[:$i] + ["CLAUDE.md"] | join("/")]
        | unique | map(select(. as $c | $have | index($c)))),
      rules: $rules[0],
      priorIssues: $p
    }' > "$TMP/args.json" || stop "could not assemble the workflow args"

echo "HEAD_SHA=$HEAD_SHA"
echo "OWNER=$OWNER"
echo "REPO=$REPO"
echo "CHANGED_FILES=$(jq length "$TMP/files.json")"
echo "PRIOR_ISSUES=$(jq length "$TMP/prior-issues.json")"
echo "ARGS=$(cat "$TMP/args.json")"
