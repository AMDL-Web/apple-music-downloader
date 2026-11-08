#!/usr/bin/env bash
# Starts a release candidate on the files the previous release wrote, as an
# upgrade does: a server install (serve, simulate mode), the embedded backend
# the desktop app and --server embedded run, and a desktop install whose local
# wrapper is still the retired bare-lite guest (installed.json without a kind,
# plus an account disk; legacy-guest.sh). The release process runs it before
# the release PR; ci.yml runs it on every push and pull request.
#
# Usage: tools/upgradecheck/run.sh <previous-version> <candidate-amdl>
# Needs gh, curl and a Linux or macOS host; downloads the previous release.
set -euo pipefail
[ $# -eq 2 ] && [ -x "$2" ] || { echo "usage: run.sh <previous-version> <candidate-amdl>" >&2; exit 2; }
prev=$1
new=$(cd "$(dirname "$2")" && pwd)/$(basename "$2")
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m); case $arch in x86_64) arch=amd64;; aarch64) arch=arm64;; esac
d=$(mktemp -d)
echo "work dir $d"
# bounded <seconds> <command...>: perl's alarm works the same on Linux and macOS.
bounded() { local s=$1; shift; perl -e 'alarm shift; exec @ARGV' "$s" "$@"; }
bounded 180 gh release download "$prev" -R AMDL-Web/apple-music-downloader -p "amdl_${prev}_${os}_${arch}.tar.gz" -D "$d"
mkdir -p "$d/old" && tar -xzf "$d"/amdl_*.tar.gz -C "$d/old"
old=$(find "$d/old" -type f -name amdl | head -1)

port=$((20000 + RANDOM % 20000))
url=http://127.0.0.1:$port
fail=0
pids=()
trap 'for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done' EXIT

# serve <binary> <log>: starts a server in $d/srv and waits up to 30 s for it.
serve() {
	(cd "$d/srv" && exec "$1" serve --no-console --init "server.listen=127.0.0.1:$port" --init simulate.enabled=true) >"$2" 2>&1 &
	pids+=($!)
	for _ in $(seq 60); do
		curl -fsS -m 2 "$url/api/v1/health" >/dev/null 2>&1 && return 0
		kill -0 "$!" 2>/dev/null || break
		sleep 0.5
	done
	return 1
}
stop() { kill "${pids[-1]}" && wait "${pids[-1]}" 2>/dev/null || true; }

mkdir -p "$d/srv"
serve "$old" "$d/old-serve.log" || { echo "FAIL: $prev did not start; see $d/old-serve.log"; exit 1; }
# Without the previous release's own download there is no old state to upgrade
# from, and a check of nothing must not pass.
"$old" --server "$url" download https://music.apple.com/us/album/1524443761 --wait --timeout 5m >/dev/null ||
	{ echo "FAIL: $prev's simulated download did not finish, so there is no download state to upgrade; see $d/old-serve.log"; exit 1; }
stop
if serve "$new" "$d/new-serve.log"; then
	"$new" --server "$url" download https://music.apple.com/us/album/1524443761 --wait --timeout 5m >/dev/null &&
		echo "ok: server install" || { echo "FAIL: server install starts, a download fails"; fail=1; }
	stop
else
	echo "FAIL: server install did not start:"; tail -5 "$d/new-serve.log"; fail=1
fi

# The embedded backend keeps its files in the per-user directories.
export HOME=$d/home XDG_CONFIG_HOME=$d/home/.config XDG_CACHE_HOME=$d/home/.cache
"$old" --server embedded --wrapper http://127.0.0.1:9 system get >/dev/null ||
	{ echo "FAIL: $prev's embedded backend did not answer, so it left no per-user files to upgrade"; exit 1; }
if out=$("$new" --server embedded system get 2>&1); then
	echo "ok: embedded backend"
else
	echo "FAIL: embedded backend:"; echo "$out" | tail -5; fail=1
fi

# A desktop install with the retired bare-lite wrapper guest (legacy-guest.sh).
bash "$(dirname "$0")/legacy-guest.sh" "$new" "$d" || fail=1
exit $fail
