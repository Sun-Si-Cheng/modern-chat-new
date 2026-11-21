# Online Music & Custom Sources

> This document describes the current online-music pipeline in the chat app, the structure of the `musicd` source runtime and how to operate it.
> Last updated: 2026-11-20

> **Deployment status (evening of 2026-11-20)**: built-in lyrics (`lib/lyric.js`), multi-mode player and search pagination (host + plugin 1.0.2) are all live;
> the "short cache for empty results" fix in `lib/builtin.js` **has been uploaded but is not effective until `modernchat-musicd` is restarted** — see §7 for the restart command.

## 1. One-line summary

**The server only turns a song name into a third-party direct link; audio bytes are downloaded and cached by the browser, straight from the source.**
This server no longer relays any audio data.

## 2. Why the change

The old pipeline was "browser → our `/api/music/stream/<token>` → the server fetches audio from the third party → streams it back to the browser".
Three problems were confirmed in practice:

| Problem | Symptom | Root cause |
|---|---|---|
| Upstream rate limiting takes the whole site down | playback on one platform suddenly fails wholesale, logs show `429` | every user's playback is fetched through **the same server egress IP**, and upstream rate-limits per IP |
| Slow first byte | playback stuck on "downloading audio…" for a long time | the sources themselves are slow (Kuwo's first byte measured at 5.4 s); relaying just adds another queue |
| Eats our own bandwidth | a song is a few MB; a few simultaneous listeners mean tens of MB/s | every audio byte passes through this machine |

With direct links all three disappear: each user's download goes through **their own IP**, and the server only handles a few KB of JSON.

## 3. The current pipeline

```
Browser
  │  ① GET /api/music/search?name=…&source=tx      (same origin, authenticated)
  ▼
Go backend handler/music.go  ──②──▶  musicd (127.0.0.1:8310)
  │                                   │  loads source scripts, runs search / resolve
  │  ③ { ok:true, data:{ list:[…] } } │
  ▼                                   ▼
Browser  ──④ POST /api/music/resolve──▶ musicd ──▶ third-party source API
  │                                   │
  │  ⑤ { ok:true, data:{ url:"https://…" } }  ← the real third-party direct link
  ▼
Browser  ──⑥ downloads audio directly from https://third-party/… (not through this server)──▶ local cache + playback
```

Key points:

- Step ⑤ returns the **signed, real address from the third-party platform**, usually valid for 1–2 hours;
- Step ⑥ happens entirely between the browser and the source; you can see the address in `F12 → Network` — this is by design.
  "Hiding the direct link" is no longer pursued (it is technically impossible: the browser must send the request itself);
- the frontend downloads the **whole** track and writes it to IndexedDB (`music/onlineCache.ts`, default cap 300 MB, LRU eviction);
  on a cache hit the next play skips resolution entirely.

## 4. Two CORS paths (important)

Audio direct links differ per platform on cross-origin access; the frontend must pick the right path, otherwise you get "resolve succeeded but playback failed":

| Platform | `Access-Control-Allow-Origin` | Frontend behavior |
|---|---|---|
| QQ Music `tx` | returns `*` | `fetch` the whole track → IndexedDB cache → Web Audio decode (cacheable, precise seek) |
| KuGou `kg` | returns `*` | same as above |
| Kuwo `kw` | **absent** | falls back to an `<audio>` element playing the URL directly (media elements are not subject to CORS, but bytes cannot be read → not cacheable) |
| NetEase `wy` | returns `*` | same as QQ |
| Migu `mg` | **absent** | falls back to `<audio>` |

Implemented in `playOnline()` in `web/src/music/engine.ts`: try `fetch` first; on error (typically no ACAO) switch to
`playByElement()`. Both paths share volume and track-switching logic.

> Note: the `<audio>` fallback path **cannot be cached** because its bytes are unreadable. That is a hard limit of the browser security model, not an implementation shortcut.

## 5. The musicd runtime

Location: `server/musicd/`, systemd unit `modernchat-musicd.service`, listening on `127.0.0.1:8310`.

### Directory layout

```
server/musicd/
├── index.js               HTTP service (search / resolve / lyrics / source list)
├── lib/
│   ├── lx.js              lx source loader (vm sandbox + lx.request injection)
│   ├── musicfree.js       MusicFree plugin loader (CommonJS shim)
│   ├── registry.js        source registry + resolve racing + playability probing
│   ├── builtin.js         built-in platform search (5 platforms, with cache/queueing/retry)
│   ├── lyric.js           built-in platform lyrics (public lyric APIs, no auth)
│   ├── request.js         outbound proxy for source scripts
│   └── decontextify.js    sanitizes sandbox return values
├── sdk/musicSdk/…         search implementation de-dependencied from lx-music-desktop
├── sources/*.js           source scripts (25 at the time of writing)
└── scripts/probe-platforms.py  per-platform probe script
```

### The two source formats

**lx format** (QQ/Kuwo/KuGou/NetEase/Migu) — provides only "swap direct link / fetch lyrics / fetch cover", no search:

```js
const { EVENT_NAMES, request, on, send } = globalThis.lx
on(EVENT_NAMES.request, async ({ action, source, info }) => {
  if (action === 'musicUrl') return 'https://…'      // direct link
  if (action === 'lyric')    return { lyric: '…', tlyric: '' }
})
send(EVENT_NAMES.inited, { status: true, sources: { tx: { name: 'QQ音乐' } } })
```

**MusicFree format** (Qishui) — ships its own search and link resolution:

```js
module.exports = {
  platform: '汽水音乐',
  search: async (query, page, type) => ({ isEnd: true, data: [ … ] }),
  getMediaSource: async (item, quality) => ({ url: 'https://…' }),
}
```

The runtime detects the format from the script body: `module.exports` present → MusicFree, otherwise lx.

### Why search uses the built-in SDK

In practice most third-party sources implement **search** against their own private backends that require card keys or IP allowlists
(e.g. one source answers unauthorized IPs with `403 该音源仅对认证用户开放`), while the platforms' **public search APIs** are stable and auth-free.
So search goes through `lib/builtin.js` (derived from lx-music-desktop, MIT, de-dependencied), and source scripts only
"swap the songmid for a direct link". Both paths are under our control: if one side dies the other still works.

### Why lyrics use built-in APIs (added 2026-11-20)

Lyrics used to depend entirely on the source scripts' `lyric` action; in practice most sources either do not implement it or route it
through private backends that need card keys / IP allowlists — the result being "it plays, but lyrics are always blank". Same idea as search:
`lib/lyric.js` adds **built-in public lyric APIs per platform** (no auth), and `doLyric` now tries **built-in first, source script as fallback**,
tagging the origin in a `via` field:

| Platform | API | Notes |
|---|---|---|
| QQ `tx` | `c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg` | `lyric/trans` may be base64; decoded automatically |
| NetEase `wy` | `music.163.com/api/song/lyric` | `lrc` + `tlyric` (no word-by-word yrc) |
| KuGou `kg` | `krcs.kugou.com/search` → `lyrics.kugou.com/download` | two steps; with `fmt=lrc` the server already converts KRC; BOM stripped |
| Kuwo `kw` | `www.kuwo.cn/openapi/v1/www/lyric/getlyric` | returns `lrclist` line by line; frontend reassembles standard LRC |
| Migu `mg` | `lrcUrl` carried on search results | fetched directly |

Results are cached per `platform:songmid` for 1 hour (successes only; reuses the same LRU structure as `builtin.js`).
All five platforms returned real lyrics in testing. **Qishui (`qs`) has no public lyric API** (the official LUNA API requires signing) and stays blank — an upstream limitation.

### A pitfall in search-result caching (2026-11-20)

`lib/builtin.js` used to cache "even empty results, unconditionally, for 5 minutes"; upstream (kg/tx confirmed) occasionally returns an empty list,
so one sporadic empty response froze search into "always empty for 5 minutes" — the user saw an empty first page while page 2 had songs.
Fixed: **empty results are cached only 30 seconds (debounce); non-empty results keep the 5-minute cache**.
**This change requires restarting `modernchat-musicd` to take effect.**

### Frontend player (host + plugin) 1.0.2 (2026-11-20)

- **Fixed "tap play does nothing" on NetEase/Migu/KuGou**: the plugin matched `s.songmid === parts[1]` strictly, but those platforms return
  `songmid` as a **number** in search results while the action string is text, so the match always failed → `picked` undefined → nothing happened.
  Now uses `String(s.songmid) === String(parts[1])`.
- **Full-queue playback**: `engine.setQueue` had no callers, so prev/next/auto-advance never worked. Playing an online song now builds the engine queue
  from the **latest search results** (the local library uses the current playlist), so tapping one track plays through the whole list.
- **Playback modes**: the persistent play bar gains a 🔁 button (repeat-all `all` default / play-through `off` / repeat-one `one`);
  the engine `repeat` field is wired to the UI (`api.music.setRepeat` / `api.music.repeat` bridge).
- **Fixed progress/lyrics jumping after auto-advance**: ① engine `play()` dispatches online/local automatically; ② the host subscribes to track changes and
  **loads lyrics for the new song automatically**, with `loadLyricFor` guarded against races (only results still matching the current song are written back,
  otherwise a stale lyric response overwrites the new one); ③ fixed the `<audio>` fallback path (NetEase/Migu/KuGou) progress clock — it used the
  AudioContext clock without ever updating `startedAt`; in `usingElement` mode progress now follows `el.currentTime`, and `<audio>`
  pause/resume/seek plus auto-advance on `ended` are wired up.
- **Search pagination**: `searchOnline` gains a `page` parameter; the plugin shows "◀ prev / page N / next ▶".

## 6. Sandbox & security

Source scripts are third-party code; inside `node:vm` **only** the following are injected:

`console`, `setTimeout/clearTimeout/setInterval/clearInterval`, `Buffer`, `URL`, `URLSearchParams`,
`TextEncoder/TextDecoder`, `atob/btoa`, `crypto` (a controlled wrapper), `lx` (only `request/on/send/utils/EVENT_NAMES`).

**Not injected**: `require`, `process.env`, `fs`, `net`, `child_process`. All outbound traffic must go through `lx.request`,
which enforces a 15-second timeout, a 5 MB response cap and a method whitelist.

> `node:vm` is not a security boundary (Node's own docs say so). Its role here is **misuse prevention**;
> the real trust boundary is "only admins can upload sources" + "a user's own sources are usable only by that user".

## 7. Operations

### Service management

```bash
systemctl status modernchat-musicd      # status
systemctl restart modernchat-musicd     # restart (required after changing source scripts)
journalctl -u modernchat-musicd -n 50   # logs
```

### Adding sources

```bash
# Built-in (distributed with the repo, maintained by admins)
cp new-source.js /opt/modernchat-nc/app/server/musicd/sources/

# User-added (not in the repo; survives overwrite upgrades)
cp new-source.js /opt/modernchat-nc/app/server/data/musicd/sources/

# Hot reload (owner only, authenticated) — or just restart the service
curl -X POST -H "Authorization: Bearer <token>" https://site/api/music/sources/reload
```

### Health check

```bash
curl -s -H "X-Music-Key: $(grep MUSICD_KEY /etc/modernchat-nc/musicd.env | cut -d= -f2)" \
     http://127.0.0.1:8310/health
# {"ok":true,"sources":25,"errors":0}
```

### Per-platform probing

```bash
python3 /opt/modernchat-nc/app/server/musicd/scripts/probe-platforms.py
```

## 8. Resolve strategy: concurrent racing + playability probing

One platform may carry a dozen sources; trying them one by one is too slow. The current approach:

1. fire up to 8 candidates simultaneously; the first one that yields a **playable** address wins, the rest are dropped;
2. every candidate must pass `probeAudio()`: a request with `Range: bytes=0-1023` and an `Origin` header,
   checking the status code and whether `Content-Type` is audio, while also recording **whether ACAO comes back** (which decides the frontend path);
3. candidates that fail probing are dropped outright — e.g. NetEase's legacy `music.163.com/song/media/outer/url?id=` always answers 200
   but actually 302s to a `/404` page; judging by URL shape alone would call it a success, and the user only finds the HTML on playback.

## 9. Known limitations

| Limitation | Notes |
|---|---|
| Qishui `qs` search unavailable | the MusicFree plugin loads and the platform registers, but its upstream search endpoint keeps returning empty lists (server-side problem); the official LUNA API requires signing. The other 5 platforms are unaffected |
| Duplicate sources | among the 25 sources, "Xinghai / Yuningxi / K×H / aggregate API" each appear twice. Concurrent racing + playability probing picks the best automatically; correctness is unaffected, only a few extra requests |
| Direct links expire | usually 1–2 hours. A stale link needs re-resolution; the audio itself lives in IndexedDB, so re-resolution only happens on a cache miss |
| No-CORS platforms cannot be cached | see §4 — a hard limit of the browser security model |

## 10. Configuration

`server/.env`:

| Variable | Default | Notes |
|---|---|---|
| `MUSICD_ENDPOINT` | `http://127.0.0.1:8310` | musicd internal address |
| `MUSICD_KEY` | empty | shared secret with musicd; keep in sync with `/etc/modernchat-nc/musicd.env` |
| `CHAT_MUSIC_ENDPOINT` | `http://127.0.0.1/api/chat_music.php` | legacy player endpoint, used only as a resolve fallback when musicd is down (**resolve only, no audio relay**) |
| `CHAT_MUSIC_KEY` | empty | shared secret of the fallback endpoint |

`/etc/modernchat-nc/musicd.env`:

| Variable | Notes |
|---|---|
| `MUSICD_KEY` | must equal the Go-side `MUSICD_KEY` |
