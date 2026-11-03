#!/bin/bash
# rec-amdl.sh <port> <out.jsonl> [ids...]: download every corpus item x codec through the amdl on <port>, one at a time.
port=$1 out=$2; shift 2
python3 - "$@" <<'PY' > /tmp/bx-runs.txt
import json,sys
c=json.load(open('/home/lyjw/bench2/corpus.json'))
want=set(sys.argv[1:])
for i in c['items']:
    if want and i['id'] not in want: continue
    urls=i.get('urls') or [i['url']]
    for codec in i['codecs']: print(i['id'], codec, ' '.join(urls))
PY
while read id codec urls; do
  t0=$(date +%s.%N)
  r=$(~/bench2/bin/amdl download $urls --codec $codec --codec-fallback=false --server http://127.0.0.1:$port --wait --timeout 60m 2>&1 | python3 -c "
import json,sys
raw=sys.stdin.read()
try: d=json.loads(raw)
except Exception: print(json.dumps({'raw':raw[-800:]})); sys.exit()
jobs=d if isinstance(d,list) else d.get('jobs') or d.get('items') or [d]
print(json.dumps([{k:j.get(k) for k in ('id','status','total','completed','failed','skipped','error','title')} for j in jobs],ensure_ascii=False))")
  t1=$(date +%s.%N)
  echo "{\"id\":\"$id\",\"codec\":\"$codec\",\"secs\":$(python3 -c "print(round($t1-$t0,1))"),\"jobs\":$r}" >> $out
done < /tmp/bx-runs.txt
echo DONE >> $out
