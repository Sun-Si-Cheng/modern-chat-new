/**
 * 站外链接与版本号集中管理。
 * 版本号同时被 scripts/check-version.sh 校验（以 CHANGELOG 最新正式版为基准），
 * 发版时此处与 HomeView 的 Hero 徽标、package.json 一起改。
 */
export const VERSION = '0.10.3'

export const REPO = 'https://gitee.com/Sun-Si-Cheng/modern-chat-new'
export const BRANCH = 'main'
export const RELEASES = `${REPO}/releases`
export const LICENSE_URL = `${REPO}/blob/${BRANCH}/LICENSE`

/** 生产服务器一键安装（无需 Go / Node / git） */
export const INSTALL_CMD = `curl -fsSL ${REPO}/raw/${BRANCH}/scripts/install.sh | bash`

/**
 * 聊天应用入口：同源子路径 /app/。
 * 官网与聊天前端部署在同一个域名下——根路径是官网（纯静态、hash 路由），
 * /app/ 才是聊天 SPA（vite base '/app/'、history 路由）。
 * 写成同源绝对路径而不是外部域名，这样切换域名时不用改代码。
 */
export const CHAT_APP = '/app/'
