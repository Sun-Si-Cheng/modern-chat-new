# 在线音乐与自定义音源

> 本文说明聊天室在线音乐现在的链路、`musicd` 音源运行时的结构与运维方式。
> 最后更新：2026-11-20

> **部署状态（2026-11-20 晚）**：歌词内置（`lib/lyric.js`）、播放器多模式与翻页（宿主 + 插件 1.0.2）均已上线；
> `lib/builtin.js` 的「空结果短缓存」修复**已上传但尚未重启 `modernchat-musicd` 生效**，重启命令见 §七。

## 一、一句话概括

**服务器只负责「把歌名换成第三方直链」，音频字节由浏览器直连音源下载并缓存。**
本服务器不再转发任何音频数据。

## 二、为什么改

旧链路是「浏览器 → 本站 `/api/music/stream/<token>` → 服务端去第三方拉音频 → 边读边吐给浏览器」，
实测有三个绕不过去的问题：

| 问题 | 现象 | 根因 |
|---|---|---|
| 上游限流拖垮全站 | 某个平台突然全部播放失败，日志里是 `429` | 所有用户的播放都从**同一个服务器出口 IP** 去拉，上游按 IP 限流 |
| 首字节很慢 | 点播放后长时间停在「正在下载音频…」 | 音源本身慢（酷我实测首字节 5.4 秒），中转只是又加了一段排队 |
| 吃自己的带宽 | 一首歌几 MB，多人同时播就是几十 MB/s | 音频字节全部经过本机 |

改成直链后这三个问题一起消失：每个用户的下载走**自己的 IP**，服务器只处理几 KB 的 JSON。

## 三、现在的链路

```
浏览器
  │  ① GET /api/music/search?name=…&source=tx      （同域，带登录态）
  ▼
Go 后端 handler/music.go  ──②──▶  musicd (127.0.0.1:8310)
  │                                   │  加载音源脚本，执行搜索 / 解析
  │  ③ { ok:true, data:{ list:[…] } } │
  ▼                                   ▼
浏览器  ──④ POST /api/music/resolve──▶ musicd ──▶ 第三方音源接口
  │                                   │
  │  ⑤ { ok:true, data:{ url:"https://…" } }  ← 第三方真实直链
  ▼
浏览器  ──⑥ 直连 https://第三方/… 下载音频（不经本服务器）──▶ 本地缓存 + 播放
```

关键点：

- 第 ⑤ 步返回的是**第三方平台带签名的真实地址**，通常 1~2 小时有效；
- 第 ⑥ 步完全在浏览器与音源之间发生，`F12 → Network` 里能看到这个地址 —— 这是设计如此，
  不再追求「隐藏直链」（技术上做不到，浏览器必须拿它去发请求）；
- 前端把音频**整首**下载后写入 IndexedDB（`music/onlineCache.ts`，默认上限 300MB，LRU 淘汰），
  下次播放命中缓存时连解析都省了。

## 四、CORS 分两条路（重要）

各平台音频直链对跨域的态度不一样，前端必须按情况选路，否则会「解析成功但播放失败」：

| 平台 | `Access-Control-Allow-Origin` | 前端行为 |
|---|---|---|
| QQ 音乐 `tx` | 回 `*` | `fetch` 下载整首 → IndexedDB 缓存 → Web Audio 解码播放（可缓存、可精确 seek） |
| 酷狗 `kg` | 回 `*` | 同上 |
| 酷我 `kw` | **不回** | 降级用 `<audio>` 元素直接播（媒体元素不受 CORS 限制，但不能读字节 → 不可缓存） |
| 网易 `wy` | 回 `*` | 同 QQ |
| 咪咕 `mg` | **不回** | 降级 `<audio>` |

实现在 `web/src/music/engine.ts` 的 `playOnline()`：先试 `fetch`，抛错（典型就是没 ACAO）就转
`playByElement()`。两条路共用同一个音量与切歌逻辑。

> 注意：`<audio>` 兜底路径**无法缓存**，因为读不到字节。这是浏览器安全模型的硬限制，不是实现偷懒。

## 五、musicd 运行时

位置：`server/musicd/`，systemd 单元 `modernchat-musicd.service`，监听 `127.0.0.1:8310`。

### 目录结构

```
server/musicd/
├── index.js               HTTP 服务（搜索 / 解析 / 歌词 / 音源列表）
├── lib/
│   ├── lx.js              lx 音源加载器（vm 沙箱 + lx.request 注入）
│   ├── musicfree.js       MusicFree 插件加载器（CommonJS shim）
│   ├── registry.js        音源注册表 + 解析竞速 + 可播性探测
│   ├── builtin.js         内置平台搜索（5 个平台，带缓存/排队/重试）
│   ├── lyric.js           内置平台歌词（5 个平台的公开歌词接口，免鉴权）
│   ├── request.js         音源脚本的出网代理
│   └── decontextify.js    沙箱返回值净化
├── sdk/musicSdk/…         从 lx-music-desktop 去依赖化而来的搜索实现
├── sources/*.js           音源脚本（当前 25 个）
└── scripts/probe-platforms.py  各平台实测脚本
```

### 两种音源格式

**lx 格式**（QQ/酷我/酷狗/网易/咪咕）——只提供「换直链 / 取歌词 / 取封面」，不提供搜索：

```js
const { EVENT_NAMES, request, on, send } = globalThis.lx
on(EVENT_NAMES.request, async ({ action, source, info }) => {
  if (action === 'musicUrl') return 'https://…'      // 直链
  if (action === 'lyric')    return { lyric: '…', tlyric: '' }
})
send(EVENT_NAMES.inited, { status: true, sources: { tx: { name: 'QQ音乐' } } })
```

**MusicFree 格式**（汽水）——自带搜索与取链：

```js
module.exports = {
  platform: '汽水音乐',
  search: async (query, page, type) => ({ isEnd: true, data: [ … ] }),
  getMediaSource: async (item, quality) => ({ url: 'https://…' }),
}
```

运行时按脚本内容自动识别：出现 `module.exports` 判为 MusicFree，否则按 lx 处理。

### 搜索为什么走内置 SDK

实测多数第三方音源的**搜索**能力走的是各自的私有后端，需要卡密或 IP 白名单（例如某源对未授权 IP
直接返回 `403 该音源仅对认证用户开放`）；而各平台的**公开搜索接口**稳定且无需鉴权。
所以搜索走 `lib/builtin.js`（源自 lx-music-desktop，MIT，已做去依赖化改造），
音源脚本只负责「给 songmid 换直链」。两条路都在自己手里，任何一边挂了另一边还能用。

### 歌词为什么走内置接口（2026-11-20 新增）

歌词此前完全依赖音源脚本的 `lyric` 动作，实测绝大多数音源要么不实现、要么走需要卡密/IP 白名单的私有后端，
表现就是「能播、歌词永远空白」。与搜索同一思路，新增 `lib/lyric.js`——**内置各平台的公开歌词接口**（免鉴权），
`doLyric` 的顺序改为**内置优先、音源脚本兜底**，返回值带 `via` 字段标明来源：

| 平台 | 接口 | 说明 |
|---|---|---|
| QQ `tx` | `c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg` | `lyric/trans` 字段可能是 base64，自动解码 |
| 网易 `wy` | `music.163.com/api/song/lyric` | `lrc` + `tlyric`（不返回逐字 yrc） |
| 酷狗 `kg` | `krcs.kugou.com/search` → `lyrics.kugou.com/download` | 两段式；`fmt=lrc` 服务端已把 KRC 转好，剥 BOM |
| 酷我 `kw` | `www.kuwo.cn/openapi/v1/www/lyric/getlyric` | 返回 `lrclist` 逐行，前端拼回标准 LRC |
| 咪咕 `mg` | 搜索结果自带的 `lrcUrl` | 直接取文本 |

结果按 `平台:songmid` 缓存 1 小时（只缓存成功的；复用 `builtin.js` 同款 LRU 结构）。
实测五平台全部拿到真实歌词。**汽水（qs）无公开歌词接口**（官方 LUNA 需签名），仍为空白，是上游限制。

### 搜索结果缓存的一个坑（2026-11-20）

`lib/builtin.js` 此前的缓存策略是「空结果也无条件缓存 5 分钟」，而上游（kg/tx 实测）会偶发返回空列表，
于是某次偶发空被冻成 5 分钟恒空，表现就是「搜索第一页是空的、翻页才有歌」（用户以为搜索坏了 / 点不了播放）。
已改为**空结果只短缓存 30 秒防抖，非空结果才缓存 5 分钟**。
**此改动需要重启 `modernchat-musicd` 才生效。**

### 前端播放器（宿主 + 插件）1.0.2（2026-11-20）

- **修复网易/咪咕/酷狗点播放没反应**：插件 `on-` 动作用 `s.songmid === parts[1]` 严格比较，而这三个平台的
  `songmid` 在搜索结果是**数字**，action 里拼出来是字符串，恒不匹配 → `picked` 为 undefined → 无动作。
  改为 `String(s.songmid) === String(parts[1])` 宽松比较。
- **支持整段连播（队列）**：`engine.setQueue` 此前没有任何调用方，上一首/下一首/自动切歌全都没生效。
  现在播放在线歌时把**最近一次搜索结果**建成引擎队列（本地曲库用当前歌单列表），点一首即可顺着整个列表连播。
- **播放模式切换**：常驻播放条新增 🔁 按钮（列表循环 `all` 默认 / 列表播放 `off` / 单曲循环 `one`），
  引擎 `repeat` 字段打通到 UI（`api.music.setRepeat` / `api.music.repeat` 桥接）。
- **修复切歌后进度与歌词乱跳**：① 引擎 `play()` 自动分派在线/本地；② 宿主订阅歌曲变化**自动加载新歌歌词**，
  `loadLyricFor` 加「只写回仍是当前歌曲的结果」竞态守卫（否则旧歌词异步回调覆盖新歌词）；
  ③ 修 `<audio>` 兜底路径（网易/咪咕/酷狗都走它）的进度时钟——原来用 AudioContext 时钟但从不更新
  `startedAt`，现 `usingElement` 模式下进度以 `el.currentTime` 为准，并补 `<audio>` 的 pause/resume/seek 与自动切歌。
- **搜索翻页**：`searchOnline` 增加 `page` 参数，插件在线搜索结果显示「◀ 上一页 / 第 N 页 / 下一页 ▶」。

## 六、沙箱与安全

音源脚本是第三方代码，`node:vm` 里**只**注入这些：

`console`、`setTimeout/clearTimeout/setInterval/clearInterval`、`Buffer`、`URL`、`URLSearchParams`、
`TextEncoder/TextDecoder`、`atob/btoa`、`crypto`（受控封装）、`lx`（仅 `request/on/send/utils/EVENT_NAMES`）。

**不注入** `require`、`process.env`、`fs`、`net`、`child_process`。所有出网只能走 `lx.request`，
那里有 15 秒超时、5MB 响应上限、方法白名单。

> `node:vm` 不是安全边界（Node 官方文档明确说它不是）。这里的定位是**防误用**，
> 真正的信任边界是「只有管理员能上传音源」+「用户音源只能自己用」。

## 七、运维

### 服务管理

```bash
systemctl status modernchat-musicd      # 状态
systemctl restart modernchat-musicd     # 重启（改音源脚本后需要）
journalctl -u modernchat-musicd -n 50   # 日志
```

### 加音源

```bash
# 内置（随仓库分发，管理员维护）
cp 新音源.js /opt/modernchat-nc/app/server/musicd/sources/

# 用户自加（不进仓库，覆盖升级不丢）
cp 新音源.js /opt/modernchat-nc/app/server/data/musicd/sources/

# 热重载（仅站长，需登录态；或直接 restart 服务）
curl -X POST -H "Authorization: Bearer <token>" https://站点/api/music/sources/reload
```

### 健康检查

```bash
curl -s -H "X-Music-Key: $(grep MUSICD_KEY /etc/modernchat-nc/musicd.env | cut -d= -f2)" \
     http://127.0.0.1:8310/health
# {"ok":true,"sources":25,"errors":0}
```

### 实测各平台

```bash
python3 /opt/modernchat-nc/app/server/musicd/scripts/probe-platforms.py
```

## 八、解析策略：并发竞速 + 可播性探测

一个平台可能挂着十几个音源，逐个试会慢到不可用。现在的做法：

1. 同时发起最多 8 个候选，谁先给出**可播放**的地址就用谁，其余丢弃；
2. 每个候选都要过一次 `probeAudio()`：带 `Range: bytes=0-1023` 和 `Origin` 头去请求，
   检查状态码、`Content-Type` 是否为音频类，并**顺带记下它是否回 ACAO**（决定前端走哪条路）；
3. 探测不过的直接丢弃 —— 例如网易的老接口 `music.163.com/song/media/outer/url?id=` 永远返回 200，
   实际却 302 到 `/404` 页面，只看 URL 格式会把它当成功，用户点播放才发现是 HTML。

## 九、已知限制

| 限制 | 说明 |
|---|---|
| 汽水 `qs` 搜索不可用 | MusicFree 插件已加载、平台已注册，但其上游搜索接口持续返回空列表（服务端问题），官方 LUNA 接口需要签名。其余 5 个平台不受影响 |
| 音源有重复份 | 25 个音源里「星海 / 玉宁熙 / K×H / 聚合 API」各有两份。靠并发竞速 + 可播探测自动择优，不影响正确性，只是多耗几次请求 |
| 直链有时效 | 通常 1~2 小时。过期后需要重新解析；音频本体在 IndexedDB 里，重新解析只在缓存未命中时发生 |
| 无 CORS 的平台不能缓存 | 见第四节，是浏览器安全模型的硬限制 |

## 十、配置项

`server/.env`：

| 变量 | 默认 | 说明 |
|---|---|---|
| `MUSICD_ENDPOINT` | `http://127.0.0.1:8310` | musicd 内部地址 |
| `MUSICD_KEY` | 空 | 与 musicd 共享的密钥；与 `/etc/modernchat-nc/musicd.env` 保持一致 |
| `CHAT_MUSIC_ENDPOINT` | `http://127.0.0.1/api/chat_music.php` | 旧播放器端点，仅作 musicd 全挂时的兜底解析（**仍只解析，不中转音频**） |
| `CHAT_MUSIC_KEY` | 空 | 兜底端点的共享密钥 |

`/etc/modernchat-nc/musicd.env`：

| 变量 | 说明 |
|---|---|
| `MUSICD_KEY` | 与 Go 侧 `MUSICD_KEY` 一致 |
