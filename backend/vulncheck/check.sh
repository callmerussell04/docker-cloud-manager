#!/bin/sh
# Scans production roots and built service binaries with govulncheck, then fails
# on any symbol-level finding not accepted by the reviewed policy and on any
# accepted entry the scans no longer report.
#
# Usage: vulncheck/check.sh <govulncheck> <bin-dir> <service>...
set -eu

govulncheck=$1
bin_dir=$2
shift 2
here=$(dirname "$0")
policy=$here/policy.json

command -v jq >/dev/null || { printf '%s\n' 'vulncheck: jq is required to apply vulncheck/policy.json.' >&2; exit 1; }

# Every entry is complete and names only scans this target runs.
targets=$(printf '%s\n' ./cmd/... "$@" | jq -R . | jq -s -c .)
if ! problems=$(jq -r --argjson targets "$targets" '
	.accepted[]
	| select((.id | type) != "string" or (.targets | type) != "array" or (.targets | length) == 0
		or (.reviewed | type) != "string" or (.rationale | type) != "string"
		or (.evidence | type) != "array" or (.evidence | length) == 0
		or any(.targets[]?; . as $t | $targets | index([$t]) | not))
	| "\(.id // "entry without id"): needs id, reviewed, rationale, evidence and targets from \($targets | join(", "))"
' "$policy"); then
	printf 'vulncheck: cannot read %s\n' "$policy" >&2
	exit 1
fi
if [ -n "$problems" ]; then
	printf 'FAIL invalid policy entry %s\n' "$problems" >&2
	exit 1
fi

reports=$(mktemp -d)
trap 'rm -rf "$reports"' EXIT
touch "$reports/results.tsv"

status=0
scan() {
	name=$1
	report=$2
	shift 2
	if ! "$govulncheck" -format json "$@" >"$reports/$report.json"; then
		printf 'vulncheck: govulncheck failed for %s\n' "$name" >&2
		status=1
		return
	fi
	if ! jq -r -s --arg target "$name" --slurpfile policy "$policy" -f "$here/evaluate.jq" \
		"$reports/$report.json" >>"$reports/results.tsv"; then
		printf 'vulncheck: could not evaluate the %s report\n' "$name" >&2
		status=1
	fi
}

export CGO_ENABLED=0 # match production builds
scan ./cmd/... roots ./cmd/...
for service in "$@"; do
	scan "$service" "$service" -mode=binary "$bin_dir/$service"
done

awk -F '\t' '
	$1 == "database" { printf "%-10s vulnerability database %s\n", $2, $3 }
	$1 == "accepted" { printf "%-10s %s accepted by vulncheck/policy.json\n", $2, $3 }
' "$reports/results.tsv"
if ! awk -F '\t' '
	$1 == "unaccepted" { printf "FAIL %s: %s is reachable and not accepted: %s\n     trace: %s\n", $2, $3, $4, $5; failed = 1 }
	$1 == "stale" { printf "FAIL %s: %s is accepted but %s\n", $2, $3, $4; failed = 1 }
	END { exit failed }
' "$reports/results.tsv" >&2; then
	printf '%s\n' 'vulncheck: fix or upgrade the affected module, or record a reviewed, evidence-backed acceptance in vulncheck/policy.json.' >&2
	status=1
fi
exit "$status"
