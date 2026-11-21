/**
 * 插件 API / 事件的权限映射表（§8.3）。
 *
 * 沙箱每条出站调用都按这里的表查权限：插件 manifest 里没声明对应权限就拒答。
 * 表是纯数据——新增 API 时在此登记，判定逻辑（宿主 frame 分发）不感知具体条目。
 */

/** API 名 -> 所需权限 */
export const API_PERMISSION: Record<string, string> = {
  on: 'message:read',
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
 * 事件 -> 接收方必须持有的权限。
 * message:new/recall 的 payload 是整条消息（含正文），只发给声明 message:read 的插件；
 * presence 同样带 userId 列表，按同一权限收敛。
 */
export const EVENT_PERMISSION: Record<string, string> = {
  'message:new': 'message:read',
  'message:recall': 'message:read',
  presence: 'message:read',
}
