# musicd —— 自研自定义音源运行时

聊天室的在线音乐**不再由 Go 后端代理音频字节**，而是：

1. `musicd`（Node 进程）加载「自定义音源脚本」，按请求做 **搜索 / 解析直链 / 歌词 / 封面**；
2. 浏览器拿到**第三方平台的直链**后自己下载、自己缓存、自己播放；
3. Go 后端只做鉴权与转发（不碰音频字节）。

这样做的三个直接好处：

- **音频不经过自己的服务器**：不占带宽、不吃 CPU、也没有 429 上游限流把整站拖垮的问题；
- **首字节就是音源的首字节**：没有「先整首下载再解码」的中转环节，实测 QQ 音乐 0.26s；
- **音源可扩展**：管理员/用户都能自己加音源脚本，不用改代码。

---

## 一、两种脚本格式

| 格式 | 来源 | 覆盖平台 | 脚本形态 |
|---|---|---|---|
| **lx** | lx-music-desktop / lxserver 音源 | QQ(`tx`)、酷我(`kw`)、酷狗(`kg`)、网易(`wy`)、咪咕(`mg`) | `globalThis.lx` + `on(EVENT_NAMES.request, ...)` |
| **musicfree** | MusicFree 插件 | 汽水(`qs`)，也可扩展其它 | CommonJS `module.exports = { search, getMediaSource, getLyric, ... }` |

运行时自动识别：脚本里出现 `module.exports` 判为 `musicfree`，否则按 `lx` 处理。

### lx 脚本协议（精简）

```js
const { EVENT_NAMES, request, on, send, utils, env, version } = globalThis.lx

on(EVENT_NAMES.request, async ({ action, source, info }) => {
  if (action === 'musicUrl') return 'https://.../song.mp3'   // 直链
  if (action === 'lyric')    return { lyric: '...', tlyric: '' }
  if (action === 'pic')      return 'https://.../cover.jpg'
  throw new Error('不支持的操作')
})

send(EVENT_NAMES.inited, { status: true, sources: { tx: { name: 'QQ音乐', actions: ['musicUrl'], qualitys: ['128k'] } } })
```

- `request(url, options, callback)` 是 **callback 风格**，回调签名兼容 `(err, resp)` 与 `(err, resp, body)`；
- `resp.body` 已自动 `JSON.parse`（解析失败保留字符串）；
- 初始化必须调用 `send('inited', …)`，否则 3 秒后判失败。

### MusicFree 插件协议（精简）

```js
module.exports = {
  platform: '汽水音乐',
  search: async (query, page, type) => ({ isEnd: true, data: [ { id, title, artist, album, duration, artwork } ] }),
  getMediaSource: async (musicItem, quality) => ({ url: 'https://...' }),
  getLyric: async (musicItem) => ({ rawLrc: '...' }),
}
```

---

## 二、沙箱

音源脚本是**第三方代码**，必须隔离：

- Node 内置 `node:vm` 的 `vm.createContext` + `vm.runInContext`，**不注入** `require` / `process` / `fs` / `net` / `child_process`；
- 只注入白名单：`console`（转到宿主日志）、`setTimeout/clearTimeout/setInterval/clearInterval`、`Buffer`、`URL`、`URLSearchParams`、`TextEncoder/TextDecoder`、`atob/btoa`、`crypto`（走受控封装）；
- `vm` 的执行超时 10 秒，脚本初始化等待上限 3 秒；
- **出网只能通过 `lx.request`**：宿主用 `undici`/`fetch` 代理，带域名白名单、5MB 响应上限、15 秒超时；
- 返回值一律 `decontextify`（JSON 深拷贝），防止把沙箱对象原型链泄漏进宿主。

> 说明：`node:vm` 不是安全边界（官方文档明确写它不是）。这里的定位是**防止误用与意外**，
> 真正的信任边界是「只有管理员能上传音源」+「用户音源只能自己用」。

---

## 三、HTTP 接口（musicd）

默认监听 `127.0.0.1:8310`，仅本机；由 Go 后端转发。

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/health` | 健康检查，返回已加载音源数 |
| `GET` | `/sources` | 列出当前可用的音源（id/名称/平台/音质） |
| `POST` | `/sources/reload` | 重新扫描音源目录 |
| `GET` | `/search?source=tx&name=起风了&page=1` | 搜索 |
| `POST` | `/resolve` | `{ source, musicInfo, quality }` → `{ url }` **直链** |
| `POST` | `/lyric` | `{ source, musicInfo }` → `{ lyric, tlyric }` |

所有请求带 `X-Music-Key` 头做本机鉴权（与 Go 侧共享）。

---

## 四、音源目录

```
server/musicd/sources/          # 内置（随仓库分发）
data/musicd/sources/<user>/     # 用户/管理员上传的
```

- 文件名即音源 id 的来源，元数据从脚本头部注释的 `@name/@version/@author` 解析；
- 支持 `.protected.js`：`protected` 标记的音源禁止导出原文（后台只显示元信息）。

---

## 五、和旧实现的关系

旧的 `handler/music.go` + `handler/music_stream.go` 会：

- 用共享密钥调播放器站点 `api/chat_music.php` 拿「中转地址」；
- 再把音频字节**从服务器转发**给浏览器。

新实现把这两步都去掉：解析直链给前端 → 前端直连音源。旧代码保留为降级路径，
可用配置项 `MUSIC_BACKEND=legacy|musicd` 切换。
