#!/usr/bin/env bash
# A desktop install whose local wrapper guest is the retired bare-lite one: the
# candidate cannot run it, so it must start without a wrapper, say so once, and
# leave the guest's record and the account disk exactly as they were. The
# desktop app tells the user the guest needs an update from that record (an
# installed.json without "kind": "manager"; apps/desktop/internal/setup#Guest),
# so a candidate that rewrites, moves or boots it hides the prompt or loses the
# account. Nothing may start QEMU, restart the guest in a loop, or stop.
#
# Usage: legacy-guest.sh <candidate-amdl> <work-dir>
# The caller exports HOME and XDG_CONFIG_HOME to directories of its own, so the
# candidate's per-user files land there. tools/upgradecheck/run.sh calls this
# after its other scenarios; upgradecheck_test.go runs it against fake candidates.
set -uo pipefail
[ $# -eq 2 ] && [ -x "$1" ] && [ -d "$2" ] || { echo "usage: legacy-guest.sh <candidate-amdl> <work-dir>" >&2; exit 2; }
[ -n "${HOME:-}" ] && [ -n "${XDG_CONFIG_HOME:-}" ] || { echo "legacy-guest.sh: HOME and XDG_CONFIG_HOME must be set to a throwaway directory" >&2; exit 2; }
new=$1
d=$2
os=$(uname -s | tr '[:upper:]' '[:lower:]')

sha() { { sha256sum "$1" 2>/dev/null || shasum -a 256 "$1"; } | cut -d' ' -f1; }
if [ "$os" = darwin ]; then w="$HOME/Library/Application Support/AMDL/wrapper"; else w="$XDG_CONFIG_HOME/AMDL/wrapper"; fi
mkdir -p "$w/logs" "$w/assets/0123456789ab"
head -c 4194304 /dev/urandom >"$w/data.img"
digest=$(printf 'a%.0s' $(seq 64))
port=${LEGACY_PORT:-$((20000 + RANDOM % 20000))}
printf '{"id":"0123456789ab","commit":"0123456789abcdef0123456789abcdef01234567","artifact_id":1,"sha256":"%s","source":"artifact 1","installed_at":"2026-10-01T00:00:00Z","port":%s,"qemu":"%s"}\n' \
	"$digest" $((port + 1)) "$(command -v true)" >"$w/installed.json"
disk=$(sha "$w/data.img")
record=$(sha "$w/installed.json")
url=http://127.0.0.1:$port
log=$d/legacy-serve.log
(exec "$new" --wrapper http://127.0.0.1:9 serve --local --listen "127.0.0.1:$port") >"$log" 2>&1 &
pid=$!
trap 'kill "$pid" 2>/dev/null || true' EXIT
up=0
for _ in $(seq 60); do
	curl -fsS -m 2 "$url/api/v1/health" >/dev/null 2>&1 && { up=1; break; }
	kill -0 "$pid" 2>/dev/null || break
	sleep 0.5
done
if [ $up = 0 ]; then
	echo "FAIL: bare-lite guest: the candidate did not start:"; tail -5 "$log"; exit 1
fi
sleep "${LEGACY_WAIT:-8}" # a restart loop shows within a few backoff steps
problems=()
kill -0 "$pid" 2>/dev/null || problems+=("the backend exited")
said=$(grep -c 'needs a manager guest update' "$log" || true)
[ "$said" -ge 1 ] || problems+=("the backend never said the guest needs an update")
[ "$said" -le 1 ] || problems+=("the backend repeats that the guest needs an update ($said times)")
# 200 without a managed guest, or 503 wrapper_unavailable while no wrapper answers.
code=$(curl -sS -m 5 -o "$d/legacy-status.json" -w '%{http_code}' "$url/api/v1/wrapper/status") || code=
case "$code" in
200 | 503) ;;
*) problems+=("wrapper status did not answer (HTTP ${code:-none})") ;;
esac
if grep -q '"managed"' "$d/legacy-status.json" 2>/dev/null; then problems+=("wrapper status reports a managed guest it cannot run"); fi
[ "$(sha "$w/data.img")" = "$disk" ] || problems+=("the account disk changed")
[ "$(sha "$w/installed.json")" = "$record" ] || problems+=("installed.json changed, so the app cannot show the update")
if ls "$w" | grep -q '^data-lite-'; then problems+=("the account disk was moved aside"); fi
[ ! -e "$w/state.json" ] || problems+=("a guest was started (state.json)")
[ -z "$(pgrep -P "$pid" || true)" ] || problems+=("the backend started a guest process")
if [ ${#problems[@]} -eq 0 ]; then
	echo "ok: bare-lite guest needs an update, disk and record untouched, no restart loop"
else
	printf 'FAIL: bare-lite guest: %s\n' "${problems[@]}"; tail -8 "$log"; exit 1
fi
