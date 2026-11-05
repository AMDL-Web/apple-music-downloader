#!/bin/bash
# host.sh <out> [<package>...]: the build host's half of round.sh, run from
# the root of a workspace that recv.sh filled with the round's commit.
# Films the scene set (apps/desktop/internal/ui#TestScenes), a scene and a
# theme to a process, beside the tests of the given packages, the desktop UI
# package's split over processes too. Writes <out>/summary.txt and
# <out>/sheet.jpg; exits 0 when all pass, 1 when something failed, 2 on bad
# arguments.
#
# LOOKROUND_JOBS sets how many processes run at once (default: a quarter of
# the CPUs, at least 2): a scene keeps about three busy.
set -uo pipefail

usage() { echo "usage: host.sh <out> [<package>...]" >&2; exit 2; }
[ $# -ge 1 ] || usage
case "$1" in -*|"") usage;; esac
out=$1; shift
for p in "$@"; do
  case "$p" in ./*) ;; *) echo "host.sh: package $p must start with ./" >&2; exit 2;; esac
  case "$p" in *[!A-Za-z0-9._/-]*) echo "host.sh: package $p has odd characters" >&2; exit 2;; esac
done
jobs=${LOOKROUND_JOBS:-$(( $(nproc) / 4 ))}
case "$jobs" in ''|*[!0-9]*) echo "host.sh: LOOKROUND_JOBS must be a number" >&2; exit 2;; esac
[ "$jobs" -ge 2 ] || jobs=2
{ [ -f go.mod ] && [ -d apps/desktop/internal/ui ]; } || { echo "host.sh: run from a workspace's root" >&2; exit 2; }

. "$HOME/amdl-dev/env.sh"
export GOWORK=off CGO_ENABLED=0
mkdir -p "$out/scenes" "$out/logs"
ws=$(pwd)
uipkg=apps/desktop/internal/ui
t0=$(date +%s)

if ! { go test -c -o "$out/ui.test" "./$uipkg" && go build -o "$out/framecheck" ./apps/desktop/tools/framecheck; } > "$out/logs/build.log" 2>&1; then
  echo "FAIL: the scene set does not build"; tail -30 "$out/logs/build.log"; exit 1
fi
if ! (cd "$uipkg" && SCENE_LIST="$out/scenes.txt" "$out/ui.test" -test.run '^TestScenes$' > /dev/null 2>&1) || [ ! -s "$out/scenes.txt" ]; then
  echo "FAIL: the scene set does not list its scenes"; exit 1
fi
t1=$(date +%s)

# Jobs, one a line: kind, name, argument. The UI package's tests go first,
# being the longest.
list=()
others=()
for p in "$@"; do
  if [ "$p" = "./$uipkg" ]; then
    names=$(cd "$uipkg" && "$out/ui.test" -test.list '.*' | grep '^Test' | grep -vx TestScenes)
    declare -A shard=()
    i=0
    for n in $names; do k=$((i % (2 * jobs))); shard[$k]+="${shard[$k]:+|}$n"; i=$((i + 1)); done
    for k in "${!shard[@]}"; do list+=("ui ui-$k ${shard[$k]}"); done
  else
    others+=("$p")
  fi
done
[ ${#others[@]} -gt 0 ] && list+=("pkgs other ${others[*]}")
while read -r s; do list+=("scene $s -"); done < "$out/scenes.txt"

run1() {
  local kind=$1 name=$2 arg=$3 log="$out/logs/${2//\//.}.log" s rc=0
  s=$(date +%s)
  case "$kind" in
    scene) (cd "$uipkg" && SCENE_OUT="$out/scenes" timeout 900 "$out/ui.test" -test.timeout 880s -test.run "^TestScenes\$/^${name%/*}\$/^${name#*/}\$" -test.v > "$log" 2>&1) || rc=$? ;;
    ui) (cd "$uipkg" && timeout 900 "$out/ui.test" -test.timeout 880s -test.run "^($arg)\$" -test.count=1 > "$log" 2>&1) || rc=$? ;;
    pkgs) (timeout 900 go test -count=1 -timeout 880s ${arg//,/ } > "$log" 2>&1) || rc=$? ;;
  esac
  printf '%s\t%s\t%s\t%s\n' "$kind" "$name" "$rc" "$(( $(date +%s) - s ))" >> "$out/done.txt"
}
: > "$out/done.txt"
running=0
for job in "${list[@]}"; do
  read -r kind name arg <<< "$job"
  [ "$kind" = pkgs ] && arg=${job#pkgs other }
  if [ $running -ge "$jobs" ]; then wait -n; running=$((running - 1)); fi
  run1 "$kind" "$name" "${arg// /,}" &
  running=$((running + 1))
done
wait
t2=$(date +%s)

"$out/framecheck" sheet -known "$uipkg/testdata/scenes-known.txt" "$out/scenes" > "$out/logs/sheet.log" 2>&1
sheet_rc=$?
{
  if [ -s "$out/scenes/summary.txt" ]; then head -1 "$out/scenes/summary.txt" | sed 's/^/scenes /'; else echo "FAIL scenes: no summary (logs/sheet.log)"; fi
  [ "$(grep -c . "$out/done.txt")" -eq ${#list[@]} ] || echo "FAIL only $(grep -c . "$out/done.txt") of ${#list[@]} jobs finished"
  got=$(find "$out/scenes" -name result.json | wc -l)
  [ "$got" -eq "$(grep -c . "$out/scenes.txt")" ] || echo "FAIL $got of $(grep -c . "$out/scenes.txt") scenes wrote a result"
  sort "$out/done.txt" | while IFS=$'\t' read -r kind name rc secs; do
    v=PASS; [ "$rc" = 0 ] || v=FAIL
    case "$kind" in
      scene) [ "$rc" = 0 ] || echo "FAIL scene $name exited $rc (logs/${name//\//.}.log)";;
      ui) echo "$v tests of ./$uipkg, shard $name ($secs s)";;
      pkgs) echo "$v go test ${others[*]} ($secs s)";;
    esac
  done
  for f in "$out"/logs/ui-*.log "$out/logs/other.log"; do
    [ -f "$f" ] && grep -h -E '^(--- FAIL|FAIL|panic:)' "$f" | head -20 | sed 's/^/    /'
  done
} > "$out/head.txt"
fail=0
grep -q '^FAIL' "$out/head.txt" && fail=1
grep -q '^scenes FAIL' "$out/head.txt" && fail=1
[ "$sheet_rc" = 0 ] || fail=1
verdict=PASS; [ $fail = 0 ] || verdict=FAIL
{
  echo "$verdict on $(hostname): build $((t1 - t0)) s, scenes and tests $((t2 - t1)) s, $jobs processes at once"
  cat "$out/head.txt"
  echo
  cat "$out/scenes/summary.txt" 2>/dev/null
} > "$out/summary.txt"
cp "$out/scenes/sheet.jpg" "$out/sheet.jpg" 2>/dev/null
cp "$out/scenes/findings.html" "$out/findings.html" 2>/dev/null
cat "$out/summary.txt"
exit $fail
