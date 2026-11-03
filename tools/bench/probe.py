import json,sys,urllib.request
B='http://127.0.0.1:18710/api/v1'
def get(p): return json.load(urllib.request.urlopen(B+p,timeout=60))
def post(p,b):
    r=urllib.request.Request(B+p,data=json.dumps(b).encode(),headers={'content-type':'application/json'})
    return json.load(urllib.request.urlopen(r,timeout=120))
for aid in sys.argv[1:]:
    try:
        a=get('/albums/'+aid); t=a['tracks'][0]
        q=post('/quality',{'url':f'https://music.apple.com/cn/song/{t["id"]}'})
        qs={x['id']:x for x in q['tracks'][0]['qualities']}
        al=qs['alac']; s=f"alac={al.get('sample_rate')}/{al.get('bit_depth')}" if al['available'] else 'alac=-'
        av=[k for k in ('aac-binaural','aac-downmix','ec3','ac3') if qs[k]['available']]
        print(aid, len(a['tracks']), s, av, a['title'][:40], flush=True)
    except Exception as e: print(aid,'ERR',e, flush=True)
