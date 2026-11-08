#!/bin/sh
# Prints a Markdown coverage summary for a Go coverage profile: the total
# statement coverage and one line per area. Display only; it never judges.
# Usage: sh .github/scripts/cover-summary.sh coverage.out
set -eu

profile="${1:?usage: cover-summary.sh <coverage profile>}"

total="$(go tool cover -func="$profile" | awk '$1 == "total:" { print $NF }')"
test -n "$total" || { echo "cover-summary: no total in $profile" >&2; exit 1; }

echo "**Go test coverage: $total** of statements (display only)"
echo
echo "| Area | Coverage | Statements |"
echo "| --- | --- | --- |"

# A profile line is "module/path/file.go:start,end numStatements count".
awk '
NR == 1 { next }
{
	file = $1
	sub(/:.*/, "", file)
	sub(/^amdl\//, "", file)
	area = "other"
	if (file ~ /^internal\//) area = "internal/"
	else if (file ~ /^apps\/amdl\//) area = "apps/amdl/"
	else if (file ~ /^apps\/desktop\//) area = "apps/desktop/"
	else if (file ~ /^apps\/console\//) area = "apps/console/"
	else if (file ~ /^tools\//) area = "tools/"
	stmts[area] += $2
	if ($3 > 0) covered[area] += $2
}
END {
	n = split("internal/ apps/amdl/ apps/desktop/ apps/console/ tools/ other", order, " ")
	for (i = 1; i <= n; i++) {
		a = order[i]
		if (stmts[a] > 0) printf "| `%s` | %.1f%% | %d |\n", a, 100 * covered[a] / stmts[a], stmts[a]
	}
}' "$profile"
