#!/bin/bash
# run1.sh <tool: amdl|zhaarey|amd> <item-id> <codec> <tag>: one measured download in a fresh dir and a fresh unit.
# Prints one JSON line: wall, cpu, memory peak (cgroup, page cache included), anon peak, replay misses.
set -uo pipefail
tool=$1 item=$2 codec=$3 tag=$4
export XDG_RUNTIME_DIR=/run/user/1000
D=$HOME/bench2/cmp/$tag/$tool/$item-$codec; [ -e $D ] && mv $D ~/.trash/bench2-cmp-$(date +%s%N)
mkdir -p $D; cd $D
urls=$(python3 -c "
import json,sys
for i in json.load(open('/home/lyjw/bench2/corpus.json'))['items']:
    if i['id']=='$item': print(' '.join(i.get('urls') or [i['url']]))")
case $tool in
  amdl)
    cat > cmd.sh <<C
#!/bin/bash
$HOME/bench2/amdl-one.sh $D $codec $urls
C
    ;;
  zhaarey)
    cp ~/bench2/comp/zhaarey.config.yaml config.yaml
    case $codec in alac) f="";; ec3) f="--atmos";; aac-binaural|aac-downmix) f="--aac --aac-type $codec";; aac) f="--aac --aac-type aac-lc";; esac
    echo "exec $HOME/bench2/bin/zhaarey -c config.yaml $f $urls" > cmd.sh ;;
  amd)
    cp ~/bench2/comp/amd.config.toml config.toml
    echo "exec $HOME/bench2/comp/amd/.venv/bin/python $HOME/bench2/drv/amd-run.py $HOME/bench2/comp/amd $codec $urls" > cmd.sh ;;
esac
chmod +x cmd.sh
curl -s --max-time 5 http://127.0.0.1:18700/__reset >/dev/null
unit=bx-t-$tool-$$
CG=/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/app.slice/$unit.service
( anon=0; cur=0; while [ ! -d $CG ]; do sleep 0.05; done
  while [ -d $CG ]; do a=$(awk '$1=="anon"{print $2}' $CG/memory.stat 2>/dev/null); c=$(cat $CG/memory.current 2>/dev/null)
    [ -n "$a" ] && [ "$a" -gt "$anon" ] && anon=$a; [ -n "$c" ] && [ "$c" -gt "$cur" ] && cur=$c; sleep 0.2; done
  echo "$anon $cur" > $D/.mem ) &
smp=$!
t0=$(date +%s.%N)
timeout 3600 systemd-run --user --wait --collect --unit=$unit -p MemoryAccounting=yes -p WorkingDirectory=$D \
  -E HTTPS_PROXY=http://127.0.0.1:18700 -E SSL_CERT_FILE=$HOME/bench2/bundle.pem -E NO_PROXY=127.0.0.1,localhost \
  -E PATH=/home/lyjw/enc:/usr/bin:/bin -E HOME=$D -E TERM=dumb -E PYTHONUNBUFFERED=1 \
  -p StandardOutput=file:$D/out.log -p StandardError=file:$D/err.log $D/cmd.sh 2> $D/unit.txt
rc=$?
t1=$(date +%s.%N)
wait $smp
stats=$(curl -s --max-time 5 http://127.0.0.1:18700/__stats)
python3 - "$tool" "$item" "$codec" "$tag" "$t0" "$t1" "$rc" "$D" "$stats" <<'PY'
import json,sys,re
tool,item,codec,tag,t0,t1,rc,D,stats=sys.argv[1:]
u=open(D+'/unit.txt').read()
def f(p):
    m=re.search(p,u); return m.group(1) if m else None
anon,cur=(open(D+'/.mem').read().split()+['0','0'])[:2]
def secs(s):
    if not s: return None
    t=0.0
    for v,un in re.findall(r'([\d.]+)(min|ms|us|s|h)',s): t+=float(v)*{'h':3600,'min':60,'s':1,'ms':1e-3,'us':1e-6}[un]
    return round(t,2)
st=json.loads(stats or '{}')
print(json.dumps(dict(tool=tool,item=item,codec=codec,tag=tag,rc=int(rc),wall=round(float(t1)-float(t0),2),
  result=f(r'Finished with result: (\S+)'),cpu=secs(f(r'CPU time consumed: ([^\n]+)')),mem_peak_mib=f(r'Memory peak: ([^\n(]+)'),
  anon_peak_mib=round(int(anon)/2**20,1),cur_peak_mib=round(int(cur)/2**20,1),replay_hits=st.get('hits'),misses=st.get('misses'),dir=D),ensure_ascii=False))
PY
