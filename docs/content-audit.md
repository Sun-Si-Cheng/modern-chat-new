# 图片内容审核接入（七牛 image-censor）

> 面向站长：怎么把七牛的内容审核接上，违规图会怎样表现，出问题怎么查。
> 代码位置：`server/internal/service/audit_callback.go`（报文解析）、
> `server/internal/handler/audit.go`（回调接口与状态落库）。

---

## 1. 它是怎么工作的

七牛的内容审核是**异步**的，整个链路是：

```
用户上传图片
   ↓
我方存库、消息正常下发（audit_status = pass，先放行）
   ↓
七牛持久化处理队列跑 image-censor（涉黄/暴恐/敏感人物）
   ↓
七牛 POST 回调 → /api/audit/callback?token=<签名>
   ↓
我方解析 → block/review 时把消息的 audit_status 改掉
   ↓
之后所有下发路径（历史列表/WS 帧/搜索/收藏/后台）**不再返回 content**
   ↓
前端显示「图片违规无法展示！」
```

**关键设计：回调到达后是「撤回」而不是「拦截首次展示」。**
审核需要几秒到几十秒，不可能让用户干等；所以先放行，判定违规后再收回。
这是行业常规做法，但意味着**违规图在被判定的那段时间里是可见的**。
如果业务要求「先审后发」，需要把新图默认设为 `pending`（见 §5）。

---

## 2. 站长要做的三件事

### 2.1 拿到回调地址

后台 → 系统设置 → 内容审核，或直接调接口：

```bash
curl -H "Authorization: Bearer <站长token>" https://你的站点/api/admin/audit/config
```

返回：

```json
{
  "callbackUrl": "https://你的站点/api/audit/callback?token=<站长后台生成的 token>",
  "token": "<站长后台生成的 token>"
}
```

> **这个地址等同于密钥**：拿到它就能伪造审核结果（把违规图判成通过）。
> 不要发到群里、不要提交进仓库。地址里的 token 由 `JWT_SECRET` 派生，
> **更换 JWT_SECRET 会让旧地址失效**，届时需要重新取一次并更新七牛配置。

### 2.2 在七牛配置持久化处理队列

七牛控制台 → 对象存储 → 数据处理 → 持久化处理：

1. 新建队列（或使用 `0.default`）
2. 处理命令填：

```
image-censor/v2/pulp/terror/politician
```

- `pulp` 涉黄、`terror` 暴恐、`politician` 敏感人物，按需增减
- 想自动禁用违规文件，在七牛控制台开启「自动禁用违规文件」（回调里 `disable=true`）

3. **notifyURL 填 2.1 拿到的 callbackUrl**
4. 绑定到「上传后自动触发」（七牛里叫「上传预处理」，或由业务侧提交 pfop）

### 2.3 验证回调通不通

配好后传一张图，然后看回调记录：

```bash
mysql -umodernchat_nc -p'<密码>' modernchat_nc -e "
select id, persist_id, input_key, format, suggestion, disabled, matched_msg, created_at
from audit_callbacks order by id desc limit 10;"
```

- `format` 是 `v2` 或 `v1` —— 系统两代格式都支持（见 §3）
- `matched_msg` 是匹配到的消息 id，`0` 表示没找到对应消息（可能 key 不匹配，见 §4）
- `suggestion` 是 `pass` / `review` / `block`

---

## 3. 两代回调格式（都已支持）

七牛同时保留新旧两套格式，**必须都支持**，否则升级了格式的站点会完全不拦违规图。

| | 旧格式 v1 | 新格式 v2 |
|---|---|---|
| 判断依据 | 有 `result.result.result`（嵌套） | 有 `result.result.scenes` |
| 内层 code | `0` 表示成功 | **`200`** 表示成功 |
| 结论字段 | `result.result.result.label`（0/1/2） | `scenes.<类型>.suggestion`（pass/review/block） |
| 明细 | `details[]` | `scenes` 下各审核类型 |

**最容易踩的坑**：v2 的 code 是 `200` 而不是 `0`。
只认 `0` 会把所有 v2 回调判成「处理失败」→ 完全不改消息状态 → **违规图一张都不会被拦**。
本实现在 `AuditResult.OK()` 里同时接受 `0` 和 `200`。

### 状态映射

| 七牛给出 | 我方 audit_status | 是否拦截 |
|---|---|---|
| `suggestion=block`（v2）/ `label=2`（v1） | `block` | ✅ 拦截 |
| `suggestion=review` / `label=1` / `review=true` | `review` | ✅ **也拦截** |
| `suggestion=pass` / `label=0` 且 review=false | `pass` | 正常展示 |
| `disable=true`（七牛已自动禁用） | 至少 `block` | ✅ 拦截 |
| `code` 非成功（1/2/3/4） | **不改状态** | —— |

> **为什么 `review` 也拦截**：审核队列里待定性的图先展示出去，等于已经泄露；
> 宁可不显示，等人工复核确认为 `pass` 再放行更稳妥。
>
> **为什么处理失败不改状态**：审核没跑完就判「通过」等于没审。
> 保持原状态（默认 pass），并在回调记录里留下 `code` 供排查。

---

## 4. 匹配逻辑与常见问题

### 怎么把回调对应到消息

七牛只回 `inputKey`（对象 key，如 `2026/11/xxx.jpg`），我方用
**「content 以该 key 结尾」** 来匹配，覆盖全部地址形态：

```
https://cdn.example.com/2026/11/xxx.jpg   ← 对象存储绝对地址
http://cdn.example.com/2026/11/xxx.jpg    ← 历史 http 前缀
/qiniu/2026/11/xxx.jpg                    ← 同源反代形态
/uploads/2026/11/xxx.jpg                  ← 切存储前的本地地址
```

匹配到 **多条** 消息时全部一起改：同一张图可能被转发/秒传复用，
只改第一条的话其它消息里同一张违规图仍然可见。

### 排障对照表

| 现象 | 原因 | 处理 |
|---|---|---|
| 七牛控制台显示回调失败 | token 不对 / 地址填错 | 重新从 `/api/admin/audit/config` 取地址 |
| 回调 401 | token 不匹配，或换过 `JWT_SECRET` | 重新取地址并更新七牛配置 |
| 回调 200 但 `matched_msg=0` | key 与消息 content 对不上 | 查 `audit_callbacks.input_key` 与 `messages.content` 是否真的对应 |
| 回调记录里 `format` 为空且 `desc` 含「解析失败」 | 七牛改了格式，或响应不是审核回调 | 看 `raw` 字段的原始报文 |
| 违规图仍能显示 | 消息 `audit_status` 还是 `pass` | 查是否 `code` 非 0（处理未成功） |

### 已实现的防护

- **验签**：`?token=` 用 HMAC-SHA256 派生、常量时间比较；伪造返回 401
- **不降级**：已是 `block` 的消息不会被后续的 `pass` 回调覆盖
  （防止七牛重放低置信度结果解除拦截）；人工要放行得直接改库
- **幂等**：七牛会重推，`(persist_id, input_key)` 唯一索引保证只留一条
- **先留档再改状态**：即便改状态失败，原始报文也已入库可复盘
- **报文限长**：接口免登录，`body` 上限 256KB、入库截断 16KB

---

## 5. 想改成「先审后发」

当前默认**先放行、事后撤回**。若业务要求先审后发：

把 `server/internal/ws/dispatch.go` 里新消息的 `AuditStatus: "pass"`
改成 `"pending"`，并在 `handler/common.go` 的 `auditBlocked()` 里把 `pending` 也纳入拦截。

代价：**没配审核队列的站点，所有图片都会显示「违规无法展示」**。
所以这个改动只适合确定会配审核的场景，不适合作为默认值。

---

## 6. 数据库

| 表 / 列 | 用途 |
|---|---|
| `messages.audit_status` | `pass` / `review` / `block`，下发时据此拦截 |
| `messages.audit_detail` | 结论摘要，如 `v2 pulp:block,terror:pass label=pulp score=0.98 disabled` |
| `audit_callbacks` | 原始回调留档（含 `raw` 完整报文） |

手工放行某条被误判的图（审核误报时用）：

```sql
UPDATE messages SET audit_status = 'pass', audit_detail = 'manual pass'
WHERE id = <消息id>;
```

---

## 7. 实现位置

| 想做什么 | 改哪里 |
|---|---|
| 解析逻辑 / 支持更多云商 | `server/internal/service/audit_callback.go` |
| 回调接口 / 匹配规则 | `server/internal/handler/audit.go` |
| 下发拦截 | `handler/common.go` 的 `applyAuditGate`，各出站口调用 |
| 前端占位文案 | `web/src/i18n/*.ts` 的 `message.imageAuditBlocked` |
| 前端拦截渲染 | `web/src/components/chat/MessagePane.vue` 的 `isAuditBlocked` |

---

## 8. 已验证项（本次实现自测）

用七牛官方文档的**原始报文**直接打线上接口，10 项全通过：

```
 1. 后台给出回调地址        ✅
 2. 伪造 token 被拒 (401)   ✅
 3. 无 token 被拒 (401)     ✅
 4. v2 block 处理成功       ✅
 5. v2 状态写成 block       ✅
 6. v2 回调留档             ✅
 7. 重推幂等（只留 1 条）    ✅
 8. v1 pass 放行            ✅
 9. block 不被 pass 覆盖     ✅
10. 下发拦截违规图（content 为空） ✅
```

> 未做真实七牛审核队列联调（缺少审核队列凭据）。
> 站长配好队列后按 §2.3 查 `audit_callbacks` 即可确认链路打通。
