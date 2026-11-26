# Compares one govulncheck JSON stream (slurped) with the reviewed policy,
# which check.sh has already validated.
# Inputs: $target (scan name), $policy (slurped policy.json).
# Emits tab-separated lines: database, accepted, unaccepted or stale.

def contains_value($value): any(.[]?; . == $value);
def frame: "\(.package).\(if .receiver then "(\(.receiver))." else "" end)\(.function)";

(map(select(.config)) | first | .config.db_last_modified // "unknown") as $db
| (map(select(.osv)) | map({key: .osv.id, value: .osv.summary}) | from_entries) as $summaries
# Only symbol-level findings (a vulnerable function in the trace) fail the gate,
# matching govulncheck's own exit semantics; module/package reports stay informational.
| (map(select(.finding.trace[0].function)) | map(.finding) | group_by(.osv)
   | map({id: .[0].osv, trace: .[0].trace})) as $found
| ($policy[0].accepted | map(select(.targets | contains_value($target))) | map(.id)) as $accepted
| "database\t\($target)\t\($db)",
  ($found[]
   | .id as $id
   | if ($accepted | contains_value($id)) then "accepted\t\($target)\t\($id)"
     else "unaccepted\t\($target)\t\($id)\t\($summaries[$id] // "")\t\(.trace | map(select(.function)) | if length > 1 then [first, last] else . end | map(frame) | join(" <- "))"
     end),
  ($accepted[] as $id
   | select($found | map(.id) | contains_value($id) | not)
   | "stale\t\($target)\t\($id)\tno longer reported; remove it from the policy")
