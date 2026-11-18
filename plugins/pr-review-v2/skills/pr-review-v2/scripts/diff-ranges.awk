# Unified diff (gh pr diff / git diff) → tab-separated rows for prepare.sh:
#   F <path>                a changed file (new path for renames; deletions and binaries too)
#   H <path> <start> <end>  a hunk's new-side line range; inline comments must land inside one
#   A <path> <start> <end>  a run of added (+) lines; the rest of a hunk is unchanged context
# Port of pr-review's internal/diff/parse.go, except that a hunk body ends when the header's old and new
# line counts run out. The Go parser ended it at any `+++`/`---` line, so an added line starting with `++`
# (a diff fixture, `++i` at column 0) cut the added runs short and could register a bogus changed file.
# shortcut: like it, C-quoted paths (git's quoting of unusual characters) aren't decoded, so such a
# file gets no hunks and its findings land summary-only; decode them if that ever matters.

function flush() {
  if (astart) print "A\t" path "\t" astart "\t" aend
  astart = 0
}
function setpath(p) {
  flush()
  path = p
  if (p != "" && !(p in seen)) {
    seen[p] = 1
    print "F\t" p
  }
}

{ sub(/\r$/, "") }

# Hunk body: context and added lines advance the new-side counter (nl). A line that isn't body syntax
# ends a malformed body early and falls through to the header rules below.
inhunk {
  c = substr($0, 1, 1)
  if (c == "\\") next
  if ($0 == "" || c == " ") { nl++; oldleft--; newleft-- }
  else if (c == "+") {
    if (astart && nl == aend + 1) aend = nl
    else { flush(); astart = aend = nl }
    nl++
    newleft--
  }
  else if (c == "-") oldleft--
  else inhunk = 0
  if (inhunk) {
    if (oldleft <= 0 && newleft <= 0) inhunk = 0
    next
  }
}

# `diff --git a/<old> b/<new>`: the new path follows the last " b/".
/^diff --git a\// {
  off = 0
  while ((i = index(substr($0, off + 1), " b/")) > 0) off += i
  if (off) setpath(substr($0, off + 3))
  inhunk = 0
  next
}
/^diff --git / { inhunk = 0; next }
/^rename to / { p = substr($0, 11); if (p != path) setpath(p); next }
/^\+\+\+ b\// { p = substr($0, 7); if (p != path) setpath(p); next }

/^@@ -[0-9]+(,[0-9]+)? \+[0-9]+(,[0-9]+)? @@/ {
  if (path == "") next
  split($0, h, " ")
  split(substr(h[2], 2), o, ",")
  split(substr(h[3], 2), r, ",")
  oldleft = (o[2] == "") ? 1 : o[2] + 0
  newleft = (r[2] == "") ? 1 : r[2] + 0
  start = r[1] + 0
  nl = start
  # A pure-deletion hunk has no new-side lines to comment on, but its body is still consumed.
  if (newleft > 0) print "H\t" path "\t" start "\t" start + newleft - 1
  inhunk = (oldleft > 0 || newleft > 0)
}

END { flush() }
