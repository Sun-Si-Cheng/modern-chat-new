/**
 * 插件 API / 事件的权限映射表（§8.3）。
 *
 * 沙箱每条出站调用都按这里的表查权限：插件 manifest 里没声明对应权限就拒答。
 * 表是纯数据——新增 API 时在此登记，判定逻辑（宿主 frame 分发）不感知具体条目。
 */

/** API 名 -> 所需权限 */
export const API_PERMISSION: Record<string, string> = {
  // 注意：api.on 不走 RPC（沙箱内纯本地注册），不在此表——事件门槛见 EVENT_PERMISSION
  send: 'message:send',
  registerMessageRenderer: 'message:render',
  addSidePanel: 'ui:panel',
  removeSidePanel: 'ui:panel',
  'storage.get': 'storage',
  'storage.set': 'storage',
  request: 'net',
  'theme.apply': 'ui:theme',
  // 扩展点（§8.3）
  registerMessageAction: 'ui:actions',
  me: 'conv:read',
  'conv.current': 'conv:read',
  'conv.members': 'conv:read',
  'messages.recent': 'conv:read',
  'users.search': 'conv:read',
  'nav.openConversation': 'ui:host',
  'nav.jumpToMessage': 'ui:host',
  toast: 'ui:host',
}

/**
 * 事件 -> 接收方必须持有的权限（订阅与投递共用同一张表）。
 * message:new/recall 的 payload 是整条消息（含正文），只发给声明 message:read 的插件；
 * presence 同样带 userId 列表，按同一权限收敛。
 *
 * api.on 在沙箱内是纯本地注册、无权限门槛，真正把关的是这里的投递过滤——
 * 没登记的事件要么本身定向投递（只发给该插件），要么 payload 无敏感数据。
 */
export const EVENT_PERMISSION: Record<string, string> = {
  'message:new': 'message:read',
  'message:recall': 'message:read',
  presence: 'message:read',
  // ui:action 由 panelAction 定向投递给「拥有该面板」的插件；
  // 登记进表固化语义：能创建面板的插件才可能收到自己的面板动作
  'ui:action': 'ui:panel',
  // message:action 由 runMessageAction 定向投递，投递点已校验 ui:actions，
  // 无 message:read 时 payload 降级为消息元信息（见 usePluginHost.messageMeta）
  'message:action': 'ui:actions',
  // conv:open 故意不设门槛：payload 只有 convId、不含内容；设门槛（如 conv:read）
  // 会逼插件为收会话切换信号申请更宽的权限——与最小权限相反
}
