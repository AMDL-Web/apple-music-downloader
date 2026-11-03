#!/bin/bash
# amdl-up.sh <name> <port> [motion=true|false]: fresh amdl (unit bx-a-<name>) behind mediareplay, wrapper wp-bench.
set -euo pipefail
name=$1 port=$2 motion=${3:-false}; D=$HOME/bench2/runs/a-$name
export XDG_RUNTIME_DIR=/run/user/1000
systemctl --user stop bx-a-$name 2>/dev/null || true
[ -e $D ] && mv $D ~/.trash/bench2-a-$name-$(date +%s)
mkdir -p $D/db $D/downloads $D/tmp
python3 - "$D" "$port" "$motion" <<'PY'
import sys
D, port, motion = sys.argv[1:]
src = open('/home/lyjw/amdl-dev/ws/bench2/configs/config.example.yaml').read().split('\n')
out = []; sec = None
for l in src:
    if l and not l[0].isspace() and not l.startswith('#'): sec = l.split(':')[0]
    s = l.strip(); ind = l[:len(l)-len(l.lstrip())]
    kv = lambda k, v: f'{ind}{k}: {v}'
    if s.startswith('listen:') and sec == 'server': l = kv('listen', f'"127.0.0.1:{port}"')
    elif s.startswith('path:') and sec == 'database': l = kv('path', f'"{D}/db/amdl.db"')
    elif s.startswith('address:') and sec == 'wrapper': l = kv('address', '"http://127.0.0.1:12360"')
    elif s.startswith('data_dir:') and sec == 'wrapper': l = kv('data_dir', '"/home/lyjw/wl-par/w-bench"')
    elif s.startswith('downloads_dir:'): l = kv('downloads_dir', f'"{D}/downloads"')
    elif s.startswith('temp_dir:'): l = kv('temp_dir', f'"{D}/tmp"')
    elif s.startswith('motion_artwork_enabled:'): l = kv('motion_artwork_enabled', motion)
    elif s.startswith('ffmpeg:') and sec == 'tools': l = kv('ffmpeg', '"/home/lyjw/enc/ffmpeg"')
    out.append(l)
open(f'{D}/config.yaml', 'w').write('\n'.join(out))
PY
systemctl --user reset-failed bx-a-$name 2>/dev/null || true
systemd-run --user --unit=bx-a-$name --collect -p WorkingDirectory=$D -p MemoryAccounting=yes \
  -E HTTPS_PROXY=http://127.0.0.1:18700 -E SSL_CERT_FILE=$HOME/bench2/bundle.pem -E PATH=/home/lyjw/enc:/usr/bin:/bin \
  -p StandardOutput=append:$D/amdl.log -p StandardError=append:$D/amdl.log \
  $HOME/bench2/bin/amdl serve --config $D/config.yaml --quiet >/dev/null
for i in $(seq 1 30); do curl -fsS --max-time 2 http://127.0.0.1:$port/api/v1/health >/dev/null 2>&1 && { echo "bx-a-$name up"; exit 0; }; sleep 1; done
echo "bx-a-$name not up"; tail -5 $D/amdl.log; exit 1
