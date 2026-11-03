#!/usr/bin/env bash
# Starts a release candidate on the files the previous release wrote, as an
# upgrade does: a server install (serve, simulate mode), the embedded backend
# the desktop app and --server embedded run, and a desktop install whose local
# wrapper is still the retired bare-lite guest (installed.json without a kind,
# plus an account disk). The amdl-release skill runs it before the release PR;
# ci.yml runs it on every push and pull request.
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
gh release download "$prev" -R AMDL-Web/apple-music-downloader -p "amdl_${prev}_${os}_${arch}.tar.gz" -D "$d"
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
"$old" --server "$url" download https://music.apple.com/us/album/1524443761 --wait --timeout 5m >/dev/null ||
	echo "note: $prev's simulated download did not finish"
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
	echo "note: $prev's embedded backend did not answer"
if out=$("$new" --server embedded system get 2>&1); then
	echo "ok: embedded backend"
else
	echo "FAIL: embedded backend:"; echo "$out" | tail -5; fail=1
fi

# A desktop install whose local wrapper guest is the retired bare-lite one: the
# candidate cannot run it, so it must start without a wrapper, say so once, and
# leave the guest's record and the account disk exactly as they were. The
# desktop app tells the user the guest needs an update from that record (an
# installed.json without "kind": "manager"; apps/desktop/internal/setup#Guest),
# so a candidate that rewrites, moves or boots it hides the prompt or loses the
# account. Nothing may start QEMU or restart the guest in a loop.
sha() { { sha256sum "$1" 2>/dev/null || shasum -a 256 "$1"; } | cut -d' ' -f1; }
if [ "$os" = darwin ]; then w="$HOME/Library/Application Support/AMDL/wrapper"; else w="$XDG_CONFIG_HOME/AMDL/wrapper"; fi
mkdir -p "$w/logs" "$w/assets/0123456789ab"
head -c 4194304 /dev/urandom >"$w/data.img"
digest=$(printf 'a%.0s' $(seq 64))
printf '{"id":"0123456789ab","commit":"0123456789abcdef0123456789abcdef01234567","artifact_id":1,"sha256":"%s","source":"artifact 1","installed_at":"2026-10-01T00:00:00Z","port":%s,"qemu":"%s"}\n' \
	"$digest" $((port + 1)) "$(command -v true)" >"$w/installed.json"
disk=$(sha "$w/data.img")
record=$(sha "$w/installed.json")
lport=$((port + 2))
lurl=http://127.0.0.1:$lport
(exec "$new" --wrapper http://127.0.0.1:9 serve --local --listen "127.0.0.1:$lport") >"$d/legacy-serve.log" 2>&1 &
lpid=$!
pids+=("$lpid")
up=0
for _ in $(seq 60); do
	curl -fsS -m 2 "$lurl/api/v1/health" >/dev/null 2>&1 && { up=1; break; }
	kill -0 "$lpid" 2>/dev/null || break
	sleep 0.5
done
if [ $up = 0 ]; then
	echo "FAIL: bare-lite guest: the candidate did not start:"; tail -5 "$d/legacy-serve.log"; fail=1
else
	sleep 8 # a restart loop shows within a few backoff steps
	problems=()
	said=$(grep -c 'needs a manager guest update' "$d/legacy-serve.log" || true)
	[ "$said" -ge 1 ] || problems+=("the backend never said the guest needs an update")
	[ "$said" -le 1 ] || problems+=("the backend repeats that the guest needs an update ($said times)")
	curl -sS -m 5 "$lurl/api/v1/wrapper/status" | grep -q '"managed"' && problems+=("wrapper status reports a managed guest it cannot run")
	[ "$(sha "$w/data.img")" = "$disk" ] || problems+=("the account disk changed")
	[ "$(sha "$w/installed.json")" = "$record" ] || problems+=("installed.json changed, so the app cannot show the update")
	if ls "$w" | grep -q '^data-lite-'; then problems+=("the account disk was moved aside"); fi
	[ ! -e "$w/state.json" ] || problems+=("a guest was started (state.json)")
	[ -z "$(pgrep -P "$lpid" || true)" ] || problems+=("the backend started a guest process")
	if [ ${#problems[@]} -eq 0 ]; then
		echo "ok: bare-lite guest needs an update, disk and record untouched, no restart loop"
	else
		printf 'FAIL: bare-lite guest: %s\n' "${problems[@]}"; tail -8 "$d/legacy-serve.log"; fail=1
	fi
fi
kill "$lpid" 2>/dev/null || true
exit $fail
