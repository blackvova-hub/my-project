"""Server-side real-data checks. Does not print credentials or session cookies."""
import http.cookiejar
import json
import math
import subprocess
import time
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

access=json.loads(Path('/opt/shortlong/admin-access.json').read_text())
base=access['url']
opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
report={}
def request(path,data=None):
    req=urllib.request.Request(base+path, data=None if data is None else json.dumps(data).encode(), headers={'Content-Type':'application/json'})
    with opener.open(req,timeout=60) as r:
        return json.load(r)
def sql(query):
    command=['docker','exec','shortlong-clickhouse-1','sh','-c','exec clickhouse-client --user "$CLICKHOUSE_USER" --password "$CLICKHOUSE_PASSWORD" --query "$1"','sh',query+' FORMAT JSON']
    return json.loads(subprocess.check_output(command))['data']
def public(url):
    with urllib.request.urlopen(url,timeout=30) as r: return json.load(r)
def poll(path,seconds=240):
    deadline=time.monotonic()+seconds
    while time.monotonic()<deadline:
        value=request(path)
        if value.get('status') not in ['queued','running','pending']:
            assert value.get('status')=='completed',value
            return value
        time.sleep(3)
    raise AssertionError('Job timed out: '+path)

me=request('/api/auth/login',{'email':access['email'],'password':access['password']})
assert me['user']['isAdmin']
report['login']='passed'
assert request('/api/auth/me')
opts=request('/api/similarity/options')
assert opts.get('assets')
report['similarity_catalog_assets']=len(opts['assets'])

# Wait for the normal background loader to reach BTC in all four catalogs.
for attempt in range(100):
    ranges=sql("SELECT exchange,market,min(time) AS first,max(time) AS last FROM shortlong_backtest.candles_1m FINAL WHERE symbol='BTCUSDT' GROUP BY exchange,market")
    if len(ranges)==4 and min(int(x['last']) for x in ranges)-max(int(x['first']) for x in ranges)>=600000: break
    time.sleep(3)
else: raise AssertionError('BTC not yet archived in all four markets')
ts=min(int(x['last']) for x in ranges)-120000
samples=sql("SELECT exchange,market,time,open,high,low,close,volume FROM shortlong_backtest.candles_1m FINAL WHERE symbol='BTCUSDT' AND time="+str(ts))
assert len(samples)==4
for row in samples:
    exchange,market=row['exchange'],row['market']
    if exchange=='bybit':
        url='https://api.bybit.com/v5/market/kline?'+urllib.parse.urlencode({'category':market,'symbol':'BTCUSDT','interval':'1','start':ts,'end':ts+59999,'limit':1})
        payload=public(url);assert payload['retCode']==0
        source=payload['result']['list'][0]
    else:
        prefix='https://api.binance.com/api/v3/klines' if market=='spot' else 'https://fapi.binance.com/fapi/v1/klines'
        source=public(prefix+'?'+urllib.parse.urlencode({'symbol':'BTCUSDT','interval':'1m','startTime':ts,'endTime':ts+59999,'limit':1}))[0]
    assert int(source[0])==ts
    for key,value in zip(['open','high','low','close','volume'],source[1:6]):
        assert math.isclose(float(row[key]),float(value),rel_tol=1e-10,abs_tol=1e-9),(exchange,market,key)
report['minute_samples']=[{'exchange':r['exchange'],'market':r['market'],'timestamp':ts,'matches_exchange':True} for r in samples]

end=(int(time.time()*1000)-300000)//300000*300000
iso=lambda v:time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime(v/1000))
for market in ['spot','linear']:
    body={'exchange':'bybit','market':market,'symbols':['BTCUSDT'],'timeframe':'5m','from':iso(end-86400000),'to':iso(end),'requestKey':str(uuid.uuid4()),
          'strategy':{'version':1,'direction':'long','initialCapital':10000,'positionSizePct':20,'stopLossPct':2,'entry':{'kind':'and','children':[{'kind':'compare','operator':'lt','left':{'kind':'rsi','period':14},'right':{'kind':'constant','value':30}}]}}}
    created=request('/api/backtests',body)
    result=poll('/api/backtests/'+created['jobId'])
    report['backtest_'+market]={'id':created['jobId'],'status':result['status'],'engine':result['result']['engineVersion']}
    print('PASS real backtest '+market,flush=True)

created=request('/api/similarity/search',{'market':'linear','symbol':'BTCUSDT','windowBars':12,'scope':'same_asset','limit':6})
result=poll('/api/similarity/jobs/'+created['jobId'],360)
report['similarity']={'id':created['jobId'],'status':result['status'],'matches':len(result.get('result',{}).get('matches',[]))}
assert report['similarity']['matches']>0, 'Expected matches in recent BTC history'
charts=request('/api/similarity/jobs/'+created['jobId']+'/charts')
report['charts_response']=bool(charts)
report['storage_tables']=sql('SHOW TABLES FROM shortlong_backtest')
assert all(r['name']!='candles' for r in report['storage_tables']), 'Unexpected legacy raw table'
Path('/opt/shortlong/reports/live-api-checks.json').write_text(json.dumps(report,indent=2))
print(json.dumps(report,indent=2),flush=True)
