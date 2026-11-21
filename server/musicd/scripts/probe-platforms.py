#!/usr/bin/env python3
"""musicd 各平台解析 + 直连可播性实测。只读，不改任何东西。"""
import json
import time
import urllib.request

KEY = open('/etc/modernchat-nc/musicd.env').read().split('=', 1)[1].strip()
BASE = 'http://127.0.0.1:8310'
ORIGIN = 'https://newchat.modern-chat.top'


def api(path, obj=None, timeout=180):
    data = json.dumps(obj).encode() if obj else None
    req = urllib.request.Request(
        BASE + path,
        data=data,
        headers={'X-Music-Key': KEY, 'Content-Type': 'application/json'},
    )
    return json.loads(urllib.request.urlopen(req, timeout=timeout).read())


def main():
    info = api('/sources')['data']
    print('音源总数:', info['count'], ' 失败:', len(info.get('errors') or []))
    print('各平台音源数:', {k: len(v) for k, v in info['platforms'].items()})
    print()
    print(f"{'平台':<4} {'解析':>6}  {'采用音源':<26} {'CORS':<5} {'类型':<20} 直连")
    for src in ('tx', 'kw', 'kg', 'wy', 'mg', 'qs'):
        try:
            found = api(f'/search?source={src}&name=%E5%91%A8%E6%9D%B0%E4%BC%A6')
        except Exception as exc:                       # noqa: BLE001
            print(f'{src:<4} 搜索失败 {exc!r}'[:110])
            continue
        songs = (found.get('data') or {}).get('list') or []
        if not songs:
            print(f'{src:<4} 搜索无结果')
            continue

        t0 = time.time()
        try:
            res = api('/resolve', {'source': src, 'musicInfo': songs[0], 'quality': '128k'})
        except Exception as exc:                       # noqa: BLE001
            print(f'{src:<4} 解析异常 {exc!r}'[:110])
            continue
        cost = time.time() - t0
        if not res.get('ok'):
            print(f'{src:<4} {cost:5.2f}s 解析失败 {str(res.get("error"))[:70]}')
            continue

        data = res['data']
        url = data['url']
        req = urllib.request.Request(
            url,
            headers={'Range': 'bytes=0-2047', 'Origin': ORIGIN, 'User-Agent': 'Mozilla/5.0'},
        )
        t1 = time.time()
        try:
            resp = urllib.request.urlopen(req, timeout=40)
            head = {k.lower(): v for k, v in resp.headers.items()}
            got = len(resp.read(2048))
            print(
                f"{src:<4} {cost:5.2f}s  {str(data.get('by'))[:26]:<26} "
                f"{str(data.get('cors')):<5} {head.get('content-type', '?')[:19]:<20} "
                f"{time.time() - t1:4.2f}s {got}B acao={head.get('access-control-allow-origin', '(无)')}"
            )
        except Exception as exc:                       # noqa: BLE001
            print(f'{src:<4} {cost:5.2f}s  {str(data.get("by"))[:26]:<26} 直连失败 {exc!r}'[:130])


if __name__ == '__main__':
    main()
