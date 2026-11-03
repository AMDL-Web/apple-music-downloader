#!/bin/bash
# amdl-one.sh <dir> <codec> <url>...: amdl serve and one download in the same cgroup, then stop.
D=$1 codec=$2; shift 2
port=$((19000 + RANDOM % 900))
mkdir -p $D/db $D/downloads $D/tmp
python3 - "$D" "$port" <<'PY'
import sys
D, port = sys.argv[1:]
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
    elif s.startswith('motion_artwork_enabled:'): l = kv('motion_artwork_enabled', 'false')
    elif s.startswith('ffmpeg:') and sec == 'tools': l = kv('ffmpeg', '"/home/lyjw/enc/ffmpeg"')
    out.append(l)
open(f'{D}/config.yaml', 'w').write('\n'.join(out))
PY
$HOME/bench2/bin/amdl serve --config $D/config.yaml --quiet > $D/serve.log 2>&1 &
sp=$!
for i in $(seq 1 100); do curl -fsS --max-time 1 http://127.0.0.1:$port/api/v1/health >/dev/null 2>&1 && break; sleep 0.1; done
fb=true; [ "$codec" != alac ] && fb=false
$HOME/bench2/bin/amdl download "$@" --codec $codec --codec-fallback=$fb --server http://127.0.0.1:$port --wait --timeout 50m > $D/result.json
rc=$?
curl -s --max-time 10 "http://127.0.0.1:$port/api/v1/downloads?limit=50" > $D/jobs.json
kill $sp; wait $sp
exit $rc
