# ModernChat 会话改动归档（2026-11-08）

> 本文档汇总本次会话对 ModernChat 的全部改动，用于归档与日后恢复上下文。
> 服务器：root@<部署机 IP> / 站点 https://newchat.modern-chat.top
> 代码树：/opt/modernchat-nc/app（git，branch main）
> 数据库：库 modernchat_nc，连接 `mysql -umodernchat_nc -p'<DB 密码>' modernchat_nc`
> 迁移最终版本：**28, dirty=0**

---

## 一、改动总览（按任务）

| # | 任务 | 迁移 | 状态 |
|---|---|---|---|
| 1 | 附件签名修复（媒体加载失败） | — | ✅ 已部署 |
| 2 | 对象存储 13 家 + 自定义接入 | — | ✅ 已部署 |
| 3 | 音频消息显示原始文件名 | 0027 | ✅ 已部署 |
| 4 | 七牛图片内容审核回调 | 0028 | ✅ 已部署 |
| 5 | 播放器竖条布局 | — | ✅ 已部署 |
| 6 | 页面内下载 + 3MB/s 限速 | — | ✅ 已部署 |
| 7 | 音乐播放服务端中转（隐藏源站直链） | — | ✅ 已部署 |

---

## 二、任务 1：附件签名修复

**根因**：`messages.content` 里存了带时效签名 URL（本地 `?exp=&sig=` / 七牛 `?e=&token=`），6 小时后必 401；历史出站口漏签；nginx 正则 `location` 抢走 `/uploads` 与 `/_dl` 的 `.jpg/.mp4` 返回 404。

**改动文件（后端）**：
- 新增 `server/internal/service/media_sign.go`：`MediaSigner` 统一签名器，`Sign`（出站现签）/`Normalize`（剥签名）/`Owns`（归属校验，忽略 scheme）/`Verify`（验签），支持 `/qiniu` 反代改写
- `server/internal/handler/attach_sign.go`：重写为 `AttachSigner` 薄封装 + `POST /api/attach/sign` 批量重签（登录 + 30 次/分限流 + 单批 50 条 + `Normalize` 自愈）
- 出站点全接签名器：`conversation.go`（消息列表/搜索/收藏/群头像）、`sticker.go`、`file.go`、`admin_message.go`、WS 帧（`isMediaType` 时 `SignMedia`）
- WS 入库前 `NormalizeMedia`，防止签名地址再进库（`dispatch.go`）
- nginx：`/uploads/`、`/_dl/`、`/qiniu/` 全改 `^~` 前缀（防 `.jpg/.mp4` 被正则静态规则抢走 404）

**修掉的真 bug**（均会导致媒体打不开）：
1. `splitScheme` 保留 scheme → `http://` 历史地址与 `https://` 配置永不匹配 → 私有空间不签名 → 401
2. `ownsObject` 要求 rest 非空 → 基址无路径时永假
3. `DownloadURL` 按域名严格 TrimPrefix → 历史 `http://` 地址被当成整条 key 去签
4. 存储层 `Owns`（Qiniu/Upyun/S3）未忽略 scheme → 已用 `sameHostPrefix` 统一修复（影响任务 6 下载）

**配置**：
- `.env`：`MEDIA_SIGN_PROXY_PREFIX=/qiniu`（初装时）→ 后改为 `off`（见任务 2 直连）
- `MEDIA_SIGN_TTL=6h`（默认）
- `DB storage.publicBase`：`https://modern-chat-new.chenfei.tech`（后配 SSL 后直连）
- 工具：`cmd/cleanurls`（清洗历史脏 URL，已跑）

---

## 三、任务 2：对象存储多服务商接入

**新增**：
- `server/internal/service/storage_provider.go`：注册表 `providers`（13 家 + custom/local），每家含 `Protocol`/`AddressStyle`/`EndpointTemplate`/`DefaultRegion`/`DocURL`/`Note`/`Group`
- `server/internal/service/storage_upyun.go` + `_test.go`：UPYUN 独立实现（REST PUT、UPYUN 签名认证 HMAC-SHA1、`_upt` 令牌 `MD5(secret&etime&URI)[12:20]+etime`，官方文档 vector 验证）
- `storage_probe.go` **重写为真实网络探测**（BucketExists + PutObject + RemoveObject），旧版只校验字段非空等于没测
- `storage.go`：`StorageConfig` 增 `Provider/AddressStyle/Secure/TokenSecret`；`NewStorage` 按服务商分派；`newS3Storage` 支持 `BucketLookupDNS/Path`
- `handler/admin_settings.go`：`storageSetting` 增 `provider`/`tokenSecret`；保存时 `kind` 由 `ResolveConfig` 推导覆盖；新增 `POST /api/admin/settings/storage/test` 测试连接（空值回落库里已存密钥）；GET settings 返回 `storageProviders` 元数据
- `store/setting.go`：新键 `KeyStorageProvider`、`KeyStorageTokenSecret`（加密）
- 前端 `AdminView.vue` 存储表单按服务商动态渲染（分组 builtin/cn/overseas，字段联动），`AttachExpiredMask.vue`、`api/index.ts` 类型
- 文档 `docs/storage-providers.md`

**关键决策**：接入按**协议**（S3/Qiniu/Upyun/Local）而非厂商名分派；寻址风格显式声明（虚拟主机/path/auto），避免 minio-go 自动探测对国内云商选错。

---

## 四、任务 3：音频文件名展示

- 迁移 `0027_message_file_name.{up,down}.sql`：`messages.file_name VARCHAR(255) NULL`
- `model/chat.go`：Message 增 `FileName *string`
- 上传接口返回 `name`（复用 `sanitizeFileName`）；分片上传已带 `meta.Name`
- WS 帧 `fileName` 上行接受 + `cleanFileName` 清洗（去路径、限 120 字）；下行帧与历史接口带 `fileName`
- 前端 `AudioMessage.vue`：`displayName` 去扩展名显示 + title 显全名 + 下载按钮提示；`Message` 类型增 `fileName`
- 端到端验证通过（上传回传 name → WS 帧带名 → 历史持久化 → content 仍为纯 URL）

---

## 五、任务 4：七牛图片内容审核回调

- 迁移 `0028_image_audit.{up,down}.sql`：`messages.audit_status`（pass/review/block）+ `audit_detail`；新表 `audit_callbacks`（`(persist_id,input_key)` 唯一索引幂等、`raw` 原样留档）
- `service/audit_callback.go` + `_test.go`：`ParseQiniuAuditCallback` 同时解析 v1/v2（判断 `scenes` 字段）；**v2 内层 code=200 非 0**（只认 0 会把所有 v2 回调判失败）；`AuditResult.OK()` 同时接受 0/200；`review` 也拦截；处理失败不改状态
- `handler/audit.go`：`POST /api/audit/callback`，HMAC token 验签（防伪造，伪造/无 token 401）；先留档再改状态；按 `inputKey` 后缀反查消息；**block 不被后续 pass 降级**；`GET /api/admin/audit/config` 给出回调整 URL
- 下发拦截：`handler/common.go` `auditBlocked()`/`applyAuditGate()`，消息列表/WS 帧/搜索/收藏/后台**全部清空 content**（不只前端隐藏）；`view.go`/`admin_message.go` 补 `AuditStatus` 字段
- 前端 `MessagePane.vue`：`isAuditBlocked()`；图片/表情违规显示「图片违规无法展示！」且**不渲染 `<img>`**（防止内容到过客户端）
- 修：GORM 把 `ReqID` 映射成 `req_id`，而建表是 `reqid` → 用 `gorm:"column:reqid"` 修复
- 端到端验证 10/10（后台给回调整、伪造 401、v1 pass 放行、v2 block、重推幂等、block 不被覆盖、下发拦截 content 空、留档）

**回调整 URL**（含 token，等密钥，勿外泄，换 JWT_SECRET 会失效）：
```
https://newchat.modern-chat.top/api/audit/callback?token=<站长后台取一次，勿写进文档>
```
七牛队列处理命令：`image-censor/v2/pulp/terror/politician`

---

## 六、任务 5：播放器竖条布局

- `web/src/components/chat/MusicFloatBar.vue`：贴顶/贴底从 96px 横条改为**右侧 264px 竖条面板**（与 left/right 一致），`flex-direction:column` + `order` 摊开；窄屏收窄 216px 并隐藏歌词
- `web/src/views/ChatLayout.vue`：`has-player-top/bottom` 留白改 `padding-right: 264px`（窄屏 216px），不再占上下

---

## 七、任务 6：页面内下载 + 3MB/s 限速

- 新增 `server/internal/handler/download.go`：`DownloadHandler`
  - `GET /api/download/:messageId`：登录 + 会话成员（`convs.MemberOf`）+ 审核拦截 + 媒体类型校验
  - 服务端令牌桶限速 3MB/s（`golang.org/x/time/rate`），单连接流式中转
  - 返回 `Content-Disposition` 附件头（RFC 5987 中文名）、透传 Range
  - `sourceURL()` 用 signer 现签回源（库里是稳定地址）；`sameHostPrefix` 处理 http/https
- 前端 `utils/download.ts`：新增 `startMessageDownload(messageId, name)` 单连接走 `/api/download`；`MessagePane.saveImage` 改走它
- 已验证：未登录 401 / 非成员 403 / 非法 id 400 / 不存在 404 / 审核 403 / 正常 200+附件头
- **带宽真相**：限速器上限 3MB/s 正确，但服务器→七牛回源带宽实测仅 ~0.6MB/s（直连 CDN 同一文件 11s/0.34MB/s），故实际达不到上限，非限速器问题

---

## 八、任务 7：音乐播放器服务端中转（隐藏源站直链）

**目标**：F12 Network 不再暴露第三方音源直链，浏览器只与本站通信。

- 新增 `server/internal/handler/music_stream.go`：`MusicStreamHandler`
  - AES-256-GCM（`crypto_token.go` 的 `sealGCM`/`openGCM`）把 `{URL, UID, Exp}` 封装成**不透明 token**
  - `GET /api/music/stream/:token`：登录校验（token 绑定 user id）→ 解开真 URL → 服务端中转播放（透传 Range/Content-Type、不设下载头、不缓存）
  - token 有效期 1 小时
- `handler/music.go`：`NewMusicHandler(baseURL, key, stream)`；`Resolve` 改走 `doResolve`，把响应里 `url/play_url/src/link/audio` 全部替换为 `/api/music/stream/<token>` 并标 `stream:true`；抽 `callUpstream` 辅助
- `router.go`：注册 `authed.GET /music/stream/:token`；`authed.POST /music/resolve`
- 前端 `src/music/onlineApi.ts`：`resolveOnline` 返回本站中转地址；`src/music/engine.ts`：`playOnline` 对同源地址带 `Authorization`（补 `import { getToken } from '@/api/client'`）
- 已验证：未登录 stream 401；`/api/music/status` `{"online":true}`；产物含 `same-origin`/`stream` 逻辑

**边界说明**：浏览器要播放音视频就必须发请求，F12 无法完全消除“看到请求”，但现在只看到本站 `/api/music/stream/<token>`，源站域名已隐藏，token 绑定账号 + 1 小时过期，他人复制也无法播放。

---

## 九、配置与环境关键值

`.env`（/opt/modernchat-nc/app/server/.env）：
```env
PORT=8100
GIN_MODE=release
DB_DSN=modernchat_nc:<DB 密码>@tcp(127.0.0.1:3306)/modernchat_nc?charset=utf8mb4&parseTime=True&loc=Local
JWT_SECRET=（64 字符，勿泄露）
FRONTEND_URL=https://newchat.modern-chat.top
MIGRATE=true
MEDIA_SIGN_PROXY_PREFIX=off        # 直连对象存储
MEDIA_SIGN_TTL=6h
DATA_DIR=data
LOG_DIR=logs
TRUSTED_PROXIES=127.0.0.1,::1
ACCEL_PREFIX=/_dl                  # systemd drop-in accel.conf
BACKUP_INTERVAL_HOURS=24
CHAT_MUSIC_ENDPOINT=http://127.0.0.1/api/chat_music.php
CHAT_MUSIC_KEY=（勿泄露）
```

数据库 `system_settings` storage 相关：
```
storage.kind=qiniu
storage.provider=（未写，靠 kind 反推 qiniu）
storage.publicBase=https://modern-chat-new.chenfei.tech
storage.bucket=modern-chat-new
storage.accessKey=.../secretKey=...（加密存储）
```

对象存储：
- CDN/访问域名：`modern-chat-new.chenfei.tech`（已配 Let's Encrypt，HTTPS/HTTP2/Range 均可用）
- 源站域名：`modern-chat-next.chenfei.tech`（证书 *.qbox.me 不匹配，仅回源）

---

## 十、常用命令

```bash
# 连接服务器
ssh root@<部署机 IP> -p <SSH 端口>

# 服务
systemctl status/restart modernchat-nc
journalctl -u modernchat-nc -f
curl -s http://127.0.0.1:8100/api/health

# 后端构建+部署
cd /opt/modernchat-nc/app/server
export PATH=/usr/local/go/bin:$PATH
go build ./... && go vet ./...
go build -o /tmp/mc-new ./cmd/server
cp /opt/modernchat-nc/bin/modernchat-server /opt/modernchat-nc/bin/modernchat-server.bak-$(date +%F-%H%M)
mv /tmp/mc-new /opt/modernchat-nc/bin/modernchat-server && systemctl restart modernchat-nc

# 前端构建+部署
cd /opt/modernchat-nc/app/web && export PATH=/opt/node22/bin:$PATH
npm run type-check && npm run build
rsync -a --delete dist/ /www/wwwroot/newchat.modern-chat.top/

# 数据库迁移状态
mysql -umodernchat_nc -p'<DB 密码>' modernchat_nc -e "select version,dirty from schema_migrations;"

# 七牛审核回调整配置
curl -H "Authorization: Bearer <站长token>" https://newchat.modern-chat.top/api/admin/audit/config

# 回调记录
mysql -umodernchat_nc -p'<DB 密码>' modernchat_nc -e "select * from audit_callbacks order by id desc limit 10;"
```

诊断工具（`cmd/` 下，不影响主构建）：
- `cmd/apicheck`（历史消息下发/可加载）、`cmd/storagecheck`（服务商注册表+存储测试）、`cmd/auditcheck`（审核回调端到端）、`cmd/dlcheck`（下载+限速）、`cmd/signcheck`/`cmd/signdiag`、`cmd/cleanurls`、`cmd/verifyfix`、`cmd/filenamecheck`、`cmd/signtest`

---

## 十一、技术债与注意

- `server/internal/service/storage_test.go` 有**既有失败** `TestDetectTypeNeverYieldsExecutableMarkupExtension`（群文件 `.bin` 兜底导致，与本次无关，勿改）
- 用户自己删除了 `data/uploads/2026/11/` 下 4 个本地文件（mp3/mp4/jpg），非事故
- 本机无 Docker/Minic，云对存储/审核测试需真实凭据（用后台「测试连接」自证）
- 测试消息 `id=8`（"周杰伦 - 晴天.mp3"）待用户在界面删除
- Audit 回调 token 与 `JWT_SECRET` 绑定，**更换 JWT_SECRET 会使回调地址失效**，需重新取

---

## 十二、新接口清单

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/api/attach/sign` | 登录 | 批量媒体重签（50 条/次） |
| POST | `/api/admin/settings/storage/test` | 站长 | 存储测试连接（不保存） |
| GET | `/api/admin/audit/config` | 站长 | 审核回调地址 |
| POST | `/api/audit/callback` | token | 七牛审核回调 |
| GET | `/api/download/:messageId` | 登录+成员 | 页面内下载（限速 3MB/s） |
| GET | `/api/music/stream/:token` | 登录 | 音乐中转播放 |
| POST | `/api/music/resolve` | 登录 | 解析（返回中转地址） |