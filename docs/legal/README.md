# 法律文档（用户协议 / 隐私协议）

本目录说明法律文档的维护方式。协议正文**不在本目录**，唯一来源是前端内容文件：

- 正文（中英双语）：[`web/src/content/legal.ts`](../../web/src/content/legal.ts)
- 在线页面：`/terms`（用户协议）、`/privacy`（隐私协议），登录页与个人资料弹窗均有入口

## 部署者必做

打开 `web/src/content/legal.ts`，替换文件顶部 `legalMeta` 中的占位符：

| 字段 | 说明 | 示例 |
|---|---|---|
| `operator` | 运营主体名称（个人或公司全称） | `杭州某某科技有限公司` |
| `contact` | 有效联系邮箱（用于隐私请求与投诉） | `support@example.com` |
| `domain` | 服务对外域名 | `chat.example.com` |
| `effectiveDate` | 生效日期 `YYYY-MM-DD` | `2026-10-24` |

修改后同时更新各文档的 `updatedAt` 字段，并重新构建前端。

## 内容与实现的对应关系

协议中关于数据处理的描述均对应实际代码，修改功能时请同步核对：

| 协议描述 | 实现位置 |
|---|---|
| 密码 bcrypt（cost 10）散列 | `server/internal/handler/common.go` `hashPassword` |
| JWT 7 天、HS256、`token_version` 失效 | `server/internal/middleware/jwt.go` |
| 浏览器本地存储 `mc_token` / `mc_lang` / `mc_theme` | `web/src/api/client.ts`、`web/src/i18n/index.ts`、`web/src/stores/prefs.ts` |
| 找回密码令牌 15 分钟、一次性 | `server/internal/handler/auth.go` `Forgot` / `Reset` |
| 消息 2 分钟内可撤回 | `web/src/components/chat/MessagePane.vue`、`server/internal/ws/dispatch.go` |
| 插件沙箱（无 same-origin）、权限白名单 | `web/src/composables/usePluginHost.ts` |
| 插件 URL 安装的 SSRF 防护 | `server/internal/plugin/fetch.go` |
| 在线状态仅存内存 | `server/internal/ws/hub.go`（无持久化） |
| 站长后台无阅读消息功能 | `server/internal/handler/admin.go`（仅用户管理与插件下架） |

## 免责与合规提示

- 本文本为开源项目自带的**模板**，不构成法律意见；正式对外运营前应由法务审阅。
- 不同法域要求不同（如中国《个人信息保护法》要求的单独同意、告知同意清单；GDPR 的数据主体权利与 DPO）。若面向特定地区提供服务，请补充相应条款。
- 若启用邮箱找回、接入第三方统计或 CDN，需在本协议「共享与披露」一节补充相应第三方。
- 英文版仅为阅读便利，页面已标注「与中文版冲突时以中文版为准」；如需英文版具备同等效力，请由法务出具正式译本。

## 与软件许可的关系

- **软件代码**：Apache License 2.0（见根目录 [LICENSE](../../LICENSE)），规范"如何使用这份代码"。
- **服务条款**：本目录的用户协议与隐私协议，规范"使用这个已部署实例"。
- 两者相互独立：你可以依据 Apache 2.0 自建实例，但自建实例的服务条款由你自己提供并承担运营责任。

## 修改流程

1. 编辑 `web/src/content/legal.ts`（中英两份保持一致）
2. 更新 `updatedAt` 与（如有必要）`legalMeta.effectiveDate`
3. `cd web && npm run type-check && npm run lint && npm run build`
4. 提交，并在发布说明中标注协议变更
