# Assemble one GitHub payload from a /pr-review-v2 workflow result (the task output file; the return value is
# under .result). The workflow renders every finding; this only picks the layout and the subset.
#   $mode: "payload" (review, event COMMENT) | "pending" (no event) | "body" ({body}) | "fallback" (markdown)
#   $ids:  null for every finding, or the finding IDs the user chose to post
def keep: $ids == null or (.id as $i | $ids | index($i)) != null;
def blocks: map(.block + "\n") | join("");

.result.review as $r
| [($r.inline + $r.summary_only)[].id] as $all
| (($ids // []) - $all) as $unknown
| if ($unknown | length) > 0 then
    error("unknown finding id(s): \($unknown | join(", ")) (available: \($all | join(", ")))")
  else . end
| [$r.inline[] | select(keep)] as $in
| [$r.summary_only[] | select(keep)] as $so
| "### Code review\n\n" as $head
| ($head + (if ($in | length) == 0 and ($so | length) == 0 then
     "No issues found. Reviewed by: \($r.specialists | join(", ")).\n\n" + $r.footer
   elif ($in | length) == 0 then
     "Found \($so | length) issue(s). These could not be placed as inline comments because their line numbers fall outside the diff's visible range.\n\n"
     + ($so | blocks) + "\n" + $r.footer
   else
     "| # | Severity | Confidence | File |\n| - | -------- | ---------- | ---- |\n"
     + ([$in | to_entries[] | "| \(.key + 1) | \(.value.cells) |\n"] | join(""))
     + "\nSee inline comments for full details, code examples, and suggested fixes.\n"
     + (if ($so | length) > 0 then "\n#### Additional issues (could not attach inline)\n\n" + ($so | blocks) else "" end)
     + "\n" + $r.footer
   end)) as $body
| if $mode == "body" then {body: $body}
  elif $mode == "fallback" then
    $head
    + (if ($in + $so | length) == 0 then "No issues found.\n\n"
       else "Inline comment posting failed. All issues listed below.\n\n" + ($in + $so | blocks) end)
    + "_Note: Inline comments failed ({API_ERROR})._\n\n" + $r.footer + "\n"
  else
    {commit_id: $r.commit_id}
    + (if $mode == "payload" then {event: "COMMENT"} else {} end)
    + {body: $body,
       comments: [$in[] | {path, line, side: "RIGHT"}
         + (if .start_line then {start_line, start_side: "RIGHT"} else {} end)
         + {body}]}
  end
