# 对象存储服务商接入手册

> 面向**站长**：本文讲清每一家云商该怎么填、填错会报什么错、以及为什么有些字段是必填的。
> 代码实现见 `server/internal/service/storage_provider.go`（服务商注册表）与
> `storage.go` / `storage_qiniu.go` / `storage_upyun.go`（三种存储实现）。

---

## 1. 先搞清一件事：不是所有云商都走同一套协议

后台的「服务商」下拉里有 13 家，但它们**底层只有 3 种协议**。
这决定了你填错字段时会看到什么样的报错，也决定了哪些字段是必填的。

| 协议 | 有哪些服务商 | 特点 |
|---|---|---|
| **S3 兼容** | AWS S3、MinIO、阿里云 OSS、腾讯云 COS、华为云 OBS、百度智能云 BOS、火山引擎 TOS、金山云 KS3、天翼云 OOS、联通云 OSS、移动云 EOS、UCloud US3、白山云，以及「自定义」 | 同一套客户端（minio-go），差别只在**端点**与**寻址风格** |
| **七牛云 Kodo** | 七牛云 | 自有协议，私有空间下载必须 `?e=&token=` 签名 |
| **又拍云 USS** | 又拍云 | 自有协议，API 认证是 HMAC 签名，下载防盗链是 `_upt` 令牌 |
| **本地磁盘** | — | 存 `DATA_DIR/uploads`，不占外网带宽 |

> **结论**：选服务商不只是「换个名字」，它会切换底层实现。
> 所以不要把「阿里云」的密钥填到「又拍云」上跑 —— 后台会用**服务商声明的协议**去连，
> 与服务商不符的密钥一定失败。

---

## 2. 三种协议分别需要填什么

### 2.1 S3 兼容（大多数云商）

这些字段**必填**：`AccessKey`、`SecretKey`、`Bucket`。

`Endpoint` 与 `Region` **通常不用填**：选了服务商后，后端会按预设模板自动拼出来。
例如选「阿里云 OSS」+ 区域 `cn-hangzhou`，端点自动是 `oss-cn-hangzhou.aliyuncs.com`。

但你**可以手工覆盖**，用于：
- 内网端点（省流量费）：`oss-cn-hangzhou-internal.aliyuncs.com`
- 专有云 / 私有部署的端点
- 预设模板过时（云商改了域名）

**寻址风格**（path-style vs 虚拟主机）也是预设好的，一般不用管。
这是最容易踩的坑：选错风格的表现是 `403 SignatureDoesNotMatch` 或 DNS 解析失败，
而错误信息完全看不出是「寻址风格」的问题。各家默认如下：

| 寻址风格 | 服务商 | 说明 |
|---|---|---|
| 虚拟主机（`bucket.endpoint/key`） | AWS、阿里云、腾讯云、华为云、百度、火山、金山、天翼云、UCloud | 这些云商要求 bucket 出现在域名里 |
| path（`endpoint/bucket/key`） | MinIO（自建） | MinIO 默认就是 path-style |
| 自动探测 | 自定义、联通云、移动云、白山云 | 端点不统一，交给 SDK 判断；连不上就改用「自定义」手填 |

### 2.2 七牛云 Kodo

必填：`AccessKey`、`SecretKey`、`Bucket`、**访问域名**。

**访问域名是必填项**，不是可选项：七牛的对象只能通过你绑定的域名下载，
没有域名就拼不出可访问的 URL。

- 私有空间（`X-Private: 1`）会自动加 `?e=&token=` 签名，**有效期 6 小时**；
- 数据库里存的是**不带签名**的稳定地址，每次下发前现签 —— 所以历史消息不会因为签名过期而打不开；
- **访问域名必须在七牛控制台配好 SSL 证书**，否则 HTTPS 站点加载它会被浏览器按混合内容拦掉。
- 当前实例已切换到 `modern-chat-new-cdn.chenfei.tech`（七牛 CDN 加速域名，A 记录指向七牛边缘节点），
  实测带签名直连 200；未带签名的裸请求会被七牛拒绝（403/401），属私有空间正常行为。
  因此**默认直连、不经本机 nginx 反代**。
- 旧的 `modern-chat-new.chenfei.tech` 已停用（无 A 记录，只有 CNAME 到 `eo.dnse2.com`）——
  注意 nginx 在**加载配置时**就会解析 `proxy_pass` 里的字面域名，写死它会让 `nginx -t` 直接 emerg，
  所以 `/qiniu/` 段必须用 `resolver` + 变量形式延迟解析。
- 直连开关是环境变量 `MEDIA_SIGN_PROXY_PREFIX`：置空或 `off` = 直连，设为 `/qiniu` = 同源反代。
- 七牛的「源站域名」（如 `modern-chat-next.chenfei.tech`）证书是 `*.qbox.me`，**不能**给浏览器用，
  只能用于回源；后台填的访问域名必须是带正式证书的 CDN 域名。

### 2.3 又拍云 USS

又拍云的字段名称和其他家不同，**对应关系如下**（这是最容易填错的地方）：

| 后台字段 | 又拍云控制台里叫 | 说明 |
|---|---|---|
| Bucket | **服务名** | 不是存储桶名 |
| AccessKey | **操作员** | |
| SecretKey | **操作员密码** | 不是登录密码 |
| 访问域名 | **绑定域名** | 在「域名管理」里绑定的 CDN 域名，必填 |
| Token 防盗链密钥 | **Token 密匙** | 在「服务 → 防盗链 → Token 防盗链」里单独设置，**与操作员密码不是同一个东西** |

> **最常见的错误**：把「Token 密匙」填进「操作员密码」。
> 这样上传会成功（因为又拍云 CDN 不校验上传），但下载全部 403。
> 反过来说，如果**不开** Token 防盗链，「Token 防盗链密钥」留空即可。

---

## 3. 填完先点「测试连接」，不要直接保存重启

后台「系统设置 → 对象存储」下面有一个 **测试连接** 按钮。

它会用**当前表单里的值**（不保存也能测）真实发起一次 API 请求：
写一个几字节的探针对象 → 删掉。这样能一次性验证 5 件事：

1. 端点可达（网络/证书/DNS）
2. 凭证有效
3. Bucket 存在
4. 有**写**权限（只读密钥会在这里失败）
5. 有删除权限

失败时它会把**实际使用的端点、区域、寻址风格**一并回显，方便你确认真的是不是连到你想要的地方。

> 为什么强调这一步：13 家里绝大多数你不会有现成的测试环境。
> 如果只能「保存 → 重启 → 传个文件试试」，配错了既不知道错在哪一步，
> 也分不清是配置问题还是服务器网络问题。

---

## 4. 私有空间的 HTTPS 问题（七牛 / 又拍云特别注意）

本站是 HTTPS。如果对象存储的绑定域名**没有可用证书**，浏览器会按
**混合内容（mixed content）**或**证书错误**拦掉它 —— 这跟签名是否正确完全无关，
签名再对也加载不出来。

两种情况要分清：

| 域名状态 | 处理 |
|---|---|
| 已配正式证书（如本实例的 `modern-chat-new-cdn.chenfei.tech`） | **直连**，`MEDIA_SIGN_PROXY_PREFIX` 置空/`off` |
| 只有 HTTP，或证书不匹配（如七牛源站域名的 `*.qbox.me`） | 必须同源反代，`MEDIA_SIGN_PROXY_PREFIX=/qiniu` |

诊断办法（在服务器上跑，看证书 CN 是否等于你的域名）：

```bash
openssl s_client -connect 你的域名:443 -servername 你的域名 </dev/null 2>/dev/null \
  | openssl x509 -noout -subject -dates
```

若输出 `subject=CN = *.qbox.me` 之类**不是你的域名**，说明证书不匹配，直连会被浏览器拒绝。

同源反代的配置（仅在需要时启用）：

```nginx
location ^~ /qiniu/ {
    proxy_pass http://你的七牛域名/;   # 结尾斜杠必须，用于剥掉 /qiniu 前缀
    proxy_set_header Host 你的七牛域名;
    proxy_http_version 1.1;
    proxy_set_header Connection "";

    # 安全：绝不能把用户 Cookie / Authorization 透给第三方存储
    proxy_set_header Cookie "";
    proxy_set_header Authorization "";

    # 私有对象不能进共享缓存（云商会回 public, max-age=31536000）
    proxy_hide_header Cache-Control;
    add_header Cache-Control "private, max-age=3600" always;
    proxy_buffering on;
    proxy_read_timeout 300s;
    proxy_send_timeout 300s;
}
```

两个必须注意的点：

- **`^~` 不能省**。配置里通常有 `location ~* \.(jpg|png|mp4|webm)$` 这类正则规则，
  正则优先级高于普通前缀匹配，不加 `^~` 的话 `/qiniu/xxx.jpg` 会被静态规则接走直接 404。
- 后端通过 `MEDIA_SIGN_PROXY_PREFIX`（默认 `/qiniu`）把签名地址改写成这个前缀，
  一般不需要改；改用别的前缀时两边要一起改。

---

## 5. 常见报错对照表

| 报错关键字 | 含义 | 怎么改 |
|---|---|---|
| `端点不可达` / `no such host` / `i/o timeout` | 域名解析不了或连不上 | 检查 Endpoint 拼写；国内云商的 Endpoint **不要**带 `https://` |
| `鉴权失败` / `SignatureDoesNotMatch` / `403` | 密钥错，或区域与 Bucket 不一致 | 核对 AK/SK；确认 Region 与 Bucket 实际所在地一致 |
| `Bucket 不存在` / `NoSuchBucket` | 桶名错或没权限 | 腾讯云 COS 的桶名**要带 APPID 后缀**（`mybucket-1250000000`） |
| `区域不匹配` / `PermanentRedirect` / `301` | 桶在别的区域 | 去控制台确认区域后重填 |
| `请求时间偏差过大` | 服务器时钟不准 | 开 NTP 同步 |
| `Bucket 名称不合法` | 桶名格式错 | 只能小写字母、数字、短横线 |
| 又拍云 `HTTP 401` | 操作员/密码错 | 填「操作员密码」而非登录密码 |
| 又拍云 `HTTP 404` | 服务名不存在 | 核对「服务名」 |
| 七牛 `401 download token not specified` | 私有空间没签名 | 确认访问域名填了、服务商选的是七牛 |

---

## 6. 切换存储的注意事项

- **保存后需要重启服务**才切换到新存储（存储实现是启动时构造的）。
- **历史文件不会自动迁移**。切换前上传的文件仍在原存储（或本地磁盘），
  消息里存的仍是旧地址；只要旧存储还能访问，历史消息就正常。
  要整体搬家得自己用 `rclone` 之类工具搬文件并批量改库。
- **配置错时不会静默写本地**。探测失败会退回本地磁盘，并在后台显示**红字告警**
  —— 这是刻意的：如果配置了云端却偷偷写到本机，重装机器就全丢了。
- 用云端存储时**不需要**备份 `DATA_DIR/uploads`，但数据库备份仍然必须。

---

## 7. 旧配置兼容性

升级前就已经配好对象存储的站点**不需要做任何事**：

- 老库里没有 `storage.provider` 字段，后端会按 `storage.kind` 与端点后缀自动反推
  （例如 `kind=qiniu` → 七牛；`kind=s3` + `oss-cn-hangzhou.aliyuncs.com` → 阿里云）；
- 反推不出来的（比如自建 MinIO 的内网域名）会显示为「自定义 / S3 通用」，
  行为与升级前完全一致。

---

## 8. 实现位置速查（给改代码的人）

| 想做什么 | 改哪里 |
|---|---|
| 新增一家服务商 | `server/internal/service/storage_provider.go` 的 `providers` 列表加一项 |
| 改某家的默认端点/区域 | 同上，改它的 `EndpointTemplate` / `DefaultRegion` |
| 改寻址风格 | 同上，改 `AddressStyle`（`StyleVirtualHost` / `StylePath` / `StyleAuto`） |
| 新增一种协议 | `Protocol` 加常量 + `NewStorage` 分派 + 新实现文件 + `Probe` 分支 |
| 改探测逻辑 | `server/internal/service/storage_probe.go` |
| 改后台表单 | `web/src/views/admin/AdminView.vue` 存储区块（由注册表驱动，通常不用改） |

> 前端**不硬编码任何厂商信息**：它从 `GET /api/admin/settings` 的 `storageProviders`
> 读取服务商清单并动态渲染表单。新增一家只需改后端注册表，前端自动出现。
