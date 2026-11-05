#!/bin/bash
# round.sh: check a look or motion round of the desktop app, and install it.
#
#   tools/lookround/round.sh [options] [<commit>]
#
# From the Mac: sends <commit> (default HEAD; committed, not the working
# tree) to the build host, films the scene set there in the maintainer's
# window, light and dark (apps/desktop/internal/ui#TestScenes, checked by
# apps/desktop/internal/filmcheck), and runs the tests of the packages the
# round touched. Copies the contact sheet, the summary and the frames worth
# a look into dist/review/look-<sha>/, and prints the summary.
#
#   --build            on PASS, also build the macOS binary (build-mac.sh)
#   --install          on PASS, build, record it with accept.sh (the summary
#                      as criteria) and install it with install-mac.sh
#   --session <name>   who accepts, for --install: the calling session
#   --waive <reason>   go on to --build or --install despite a FAIL; the
#                      reason goes into the acceptance record
#   --base <commit>    the round starts after this commit (default: the newest
#                      accepted build on the host that <commit> descends from,
#                      else where <commit>'s branch leaves origin/dev, else
#                      <commit>~1)
#   --host <host>      rog or z16 (default: rog when it answers, else z16)
#   --no-full          do not start the whole desktop suite in the background
#   --recheck          check again a commit the host already passed (by
#                      default a rerun, as to install after looking, reuses it)
#
# Exit status: 0 PASS (and built or installed when asked), 1 FAIL or a step
# failed, 2 bad arguments.
set -euo pipefail

usage() { echo "usage: round.sh [options] [<commit>]" >&2; awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0" >&2; exit 2; }
build=0 install=0 session="" waive="" base="" host="" full=1 recheck=0 commit=""
while [ $# -gt 0 ]; do
  case "$1" in
    --build) build=1 ;;
    --install) install=1 build=1 ;;
    --session) [ $# -ge 2 ] && [ -n "$2" ] || usage; session=$2; shift ;;
    --waive) [ $# -ge 2 ] && [ -n "$2" ] || usage; waive=$2; shift ;;
    --base) [ $# -ge 2 ] && [ -n "$2" ] || usage; base=$2; shift ;;
    --host) [ $# -ge 2 ] || usage; host=$2; shift
      case "$host" in rog|z16) ;; *) echo "round.sh: --host is rog or z16" >&2; exit 2;; esac ;;
    --no-full) full=0 ;;
    --recheck) recheck=1 ;;
    -h|--help) usage ;;
    -*) echo "round.sh: unknown option $1" >&2; usage ;;
    *) [ -z "$commit" ] || usage; commit=$1 ;;
  esac
  shift
done
if [ $install = 1 ] && [ -z "$session" ]; then echo "round.sh: --install needs --session <name>" >&2; exit 2; fi
case "$session" in *[!A-Za-z0-9._-]*) echo "round.sh: --session takes letters, digits, . _ -" >&2; exit 2;; esac

top=$(git rev-parse --show-toplevel)
sha=$(git -C "$top" rev-parse --verify --quiet "${commit:-HEAD}^{commit}") || { echo "round.sh: no commit ${commit:-HEAD}" >&2; exit 2; }
short=${sha:0:8}
if [ -z "$commit" ] && [ -n "$(git -C "$top" status --porcelain --untracked-files=no)" ]; then
  echo "round.sh: note: uncommitted changes are not in the round; it checks $short" >&2
fi

hostsh() { timeout "${2:-120}" ssh -o BatchMode=yes -o ConnectTimeout=5 "$1" bash -s; }
if [ -z "$host" ]; then
  if echo true | hostsh rog 15 > /dev/null 2>&1; then host=rog; else host=z16; echo "round.sh: rog does not answer; using z16" >&2; fi
fi
home=$(echo 'echo $HOME' | hostsh "$host" 15) || { echo "round.sh: cannot reach $host" >&2; exit 1; }
id=round-$short
ws=$home/amdl-dev/ws/$id
out=$home/amdl-dev/look/$short
since() { echo $(( $(date +%s) - $1 )); }
t0=$(date +%s)

# The round: from the last accepted build it descends from.
if [ -z "$base" ]; then
  for c in $(echo 'cd ~/amdl-dev/accepted 2>/dev/null && ls -t | grep -E "^[0-9a-f]{32}$" | head -40 | xargs -r sed -n "s/^commit=//p"' | hostsh "$host" 30); do
    if git -C "$top" merge-base --is-ancestor "$c" "$sha" 2>/dev/null && [ "$c" != "$sha" ]; then base=$c; break; fi
  done
  # A rebase leaves the accepted build off the branch: the round is then all
  # the branch adds to origin/dev.
  if [ -z "$base" ] && mb=$(git -C "$top" merge-base origin/dev "$sha" 2>/dev/null) && [ "$mb" != "$sha" ]; then
    base=$mb
    echo "round.sh: note: no accepted build on $host is an ancestor of $short; the round starts where it leaves origin/dev" >&2
  fi
  if [ -z "$base" ]; then
    base=$sha~1
    echo "round.sh: note: no accepted ancestor and nothing past origin/dev; the round is $short alone (--base widens it)" >&2
  fi
fi
git -C "$top" rev-parse --verify --quiet "$base^{commit}" > /dev/null || { echo "round.sh: no commit $base" >&2; exit 2; }
pkgs=()
while read -r d; do
  [ -n "$d" ] && pkgs+=("./$d")
done < <(git -C "$top" diff --name-only "$base" "$sha" -- '*.go' | grep -v '^explore/' | xargs -r -n1 dirname | sort -u |
  while read -r d; do git -C "$top" ls-tree --name-only "$sha" "$d/" | grep -q '\.go$' && echo "$d"; done)
if git -C "$top" diff --name-only "$base" "$sha" | grep -qx -e go.mod -e go.sum; then pkgs=(./apps/desktop/...); fi
echo "round $(git -C "$top" rev-parse --short "$base")..$short on $host; tests of: ${pkgs[*]:-(no Go package touched)}"

t_sync=0 t_check=0
passed=$(printf 'head -1 %q/summary.txt 2>/dev/null | grep "^PASS" || true\n' "$out" | hostsh "$host" 15)
if [ -n "$passed" ] && [ $recheck = 0 ]; then
  echo "reusing this commit's passed check on $host (--recheck runs it again)"
  # Its run started the whole suite already.
  full=0
else
  # Sync the commit itself.
  s=$(date +%s)
  git -C "$top" archive --format=tar.gz "$sha" | timeout 300 ssh -o BatchMode=yes -o ConnectTimeout=5 "$host" bash "$home/amdl-dev/bin/recv.sh" "$id" > /dev/null
  t_sync=$(since "$s")

  s=$(date +%s)
  rc=0
  printf 'mkdir -p ~/.trash && { [ ! -d %q ] || mv %q ~/.trash/look-%s-$(date +%%Y%%m%%d-%%H%%M%%S); } && cd %q && mkdir -p %q && bash tools/lookround/host.sh %q %s\n' \
    "$out" "$out" "$short" "$ws" "$out" "$out" "${pkgs[*]:-}" | hostsh "$host" 1800 || rc=$?
  t_check=$(since "$s")
  [ $rc -le 1 ] || { echo "round.sh: host.sh failed ($rc)" >&2; exit 1; }
fi

# What the author looks at.
s=$(date +%s)
review=$top/dist/review/look-$short
mkdir -p "$review"
echo "cd $out && tar cf - summary.txt sheet.jpg \$(ls findings.html 2>/dev/null) \$(cd scenes && find . -name 'bad-*.png' -o -name 'settled-*.jpg' | sed 's|^./|scenes/|')" |
  hostsh "$host" 120 | tar xf - -C "$review"
t_pull=$(since "$s")

# The whole desktop suite, after the gate and the build so it slows
# neither. Windows's sshd ends a session's WSL processes with it, so this
# Mac holds the session open in the background until the suite ends.
start_full() {
  [ $full = 1 ] || return 0
  printf 'cd %q && . ~/amdl-dev/env.sh && export GOWORK=off CGO_ENABLED=0 && s=$(date +%%s) && timeout 2400 go test -count=1 -timeout 2300s ./apps/desktop/... > %q 2>&1; echo "exit=$? after $(( $(date +%%s) - s )) s" >> %q\n' \
    "$ws" "$out/full.log" "$out/full.log" > "$review/full.sh"
  nohup timeout 2500 ssh -o BatchMode=yes -o ConnectTimeout=5 "$host" bash -s < "$review/full.sh" > /dev/null 2>&1 &
  echo "full desktop suite in the background on $host: $out/full.log"
}

verdict=$(head -1 "$review/summary.txt" | cut -d' ' -f1)
echo "time: sync ${t_sync} s, check ${t_check} s, copy back ${t_pull} s"
echo "review: $review/sheet.jpg and $review/summary.txt"
[ ! -f "$review/findings.html" ] || echo "findings page (frames to flip; publish it as an Artifact for the maintainer): $review/findings.html"
if [ "$verdict" != PASS ] && [ -z "$waive" ]; then start_full; exit 1; fi
[ $build = 1 ] || { start_full; exit 0; }

s=$(date +%s)
version=$(git -C "$top" describe --tags --always "$sha")
# rog's build-mac.sh prints the md5 alone, z16's as md5sum does.
md5=$(echo "\$HOME/amdl-dev/bin/build-mac.sh $id $version $short" | hostsh "$host" 600 | awk 'END { print $1 }')
case "$md5" in *[!0-9a-f]*|"") echo "round.sh: build-mac.sh gave no md5" >&2; exit 1;; esac
[ ${#md5} -eq 32 ] || { echo "round.sh: build-mac.sh gave no md5" >&2; exit 1; }
echo "built $md5 in $(since "$s") s"
start_full
[ $install = 1 ] || exit 0

{
  echo "Look round $(git -C "$top" rev-parse --short "$base")..$short, checked by its author with tools/lookround (scene set and the tests of: ${pkgs[*]:-none})."
  [ -z "$waive" ] || echo "Findings waived by $session: $waive"
  cat "$review/summary.txt"
} > "$review/criteria.txt"
{
  printf 'set -e\nf=$(mktemp)\necho %q | base64 -d > "$f"\n' "$(base64 < "$review/criteria.txt" | tr -d '\n')"
  printf '[ "$(md5sum ~/amdl-dev/out/AMDL-darwin-arm64 | cut -c1-32)" = %q ] || { echo "out/ changed since the build: another build ran" >&2; exit 1; }\n' "$md5"
  printf '~/amdl-dev/bin/accept.sh %q %q "$f"\n' "$sha" "$session"
} | hostsh "$host" 60

inst=$(mktemp -t install-mac)
echo 'cat ~/amdl-dev/bin/install-mac.sh' | hostsh "$host" 30 > "$inst"
AMDL_BUILD_HOST=$host bash "$inst" "$md5"
mv "$inst" ~/.Trash/ 2>/dev/null || true
