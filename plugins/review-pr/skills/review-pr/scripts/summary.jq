# What the finding is, in one line: the first sentence of `explanation`, minus the
# line-snap note finalize prepends; falls back to `rationale` when that is empty.
def gist:
  ((.explanation // "")
   | gsub("^_Note:[^\n]*_\\s*"; "")
   | gsub("\\s+"; " ") | ltrimstr(" ")
   | (capture("^(?<s>.{1,200}?[.!?])(\\s|$)").s
      // (if length > 160 then .[0:157] + "…" else . end))) as $g
  | if $g == "" then .rationale else $g end;

"=== Review summary ===",
"  Specialists: \(.specialists_used | join(", "))",
(if (.budget_exhausted_roles // []) | length > 0 then "  Budget exhausted (self-reported partial scan): \(.budget_exhausted_roles | join(", "))" else empty end),
(if (.missing_roles // []) | length > 0 then "  Missing: \(.missing_roles | join(", "))" else empty end),
(if (.unreadable_roles // []) | length > 0 then "  Unreadable: \(.unreadable_roles | join(", "))" else empty end),
(if (.invalid_findings // []) | length > 0 then "  Invalid findings (\(.invalid_findings | length)):" else empty end),
((.invalid_findings // [])[] | "    [\(.role)/\(.id)] \(.reason)"),
(if (.unknown_categories // []) | length > 0 then "  Off-vocabulary categories (\(.unknown_categories | length)) — posted, but they match nothing in category-based dedup:" else empty end),
((.unknown_categories // [])[] | "    [\(.role)/\(.id)] \(.category)"),
(if (.potential_conflicts // []) | length > 0 then "  ⚠ Potential conflicts (\(.potential_conflicts | length)) — each pair anchors near the other and discusses its subject; confirm they do not give opposite advice before posting:" else empty end),
((.potential_conflicts // [])[] | "    [\(.id_a)] \(.file):\(.line_a) ↔ [\(.id_b)] \(.file):\(.line_b) — both concern `\(.identifier)`"),
(if (.dropped_prior_review // []) | length > 0 then "  Dropped (prior review): \(.dropped_prior_review | length)" else empty end),
(if (.dropped_by_gate // []) | length > 0 then "  Reconciled (below gate): \(.dropped_by_gate | length)" else empty end),
((.dropped_by_gate // [])[] | "    [\(.id)] \(.severity) conf=\(.confidence) \(.file):\(.line) — \(.rationale)"),
(if (.deduped // []) | length > 0 then "  Deduped (merged into another finding): \(.deduped | length)" else empty end),
((.deduped // [])[] | "    [\(.id)] \(.severity) conf=\(.confidence) \(.file):\(.line) → merged into \(.merged_into) by \(.merged_by) — \(.rationale)"),
"  Inline eligible: \(.inline_eligible | length)",
"  Summary only: \(.summary_only | length)",
(if .accounting.ok then "  Accounted: \(.accounting.accounted) of \(.accounting.loaded) findings"
 else "  ⚠ ACCOUNTING MISMATCH: \(.accounting.accounted) of \(.accounting.loaded) findings accounted for — orphans: \(.accounting.orphans | join(", "))" end),
"",
((.inline_eligible // [])[] | "  [\(.id)] \(.severity) \(.file):\(.line) — \(gist)"),
((.summary_only // [])[]    | "  [\(.id)] \(.severity) (summary) \(.file):\(.line) — \(gist)")
