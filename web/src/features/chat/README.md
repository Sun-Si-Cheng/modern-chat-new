# chat 模块（聊天）

聊天主界面：会话列表、消息流、消息输入、群聊、收藏、通知、公告。

## 归属
- 视图：ChatLayout、MessagePane、MessageComposer、ConvSidebar、DetailPanel、
  GroupPane、AppRail、NotificationDrawer、AnnouncementDialog、FavoriteDrawer、
  PluginDrawer、VoiceMessage
- 状态：stores/conversations、stores/favorites、stores/layout、stores/ws（连接状态机）
- 组合：composables/useWebSocket、notifications、announcement

## 边界规则（互不侵入）
1. 本模块**不得 import** `features/contacts`、`features/settings` 的内部文件；
   需要对方能力时只允许经由共享层（stores / core/ / components/ui / endpoints 用例）。
2. 模块间跳转用 router 与事件，不直接操作对方组件内部状态。
3. 新增文件直接放进本目录（`features/chat/components|composables/...`），
   不再往 `components/chat/` 续写——存量文件按上述清单随重构逐步迁入。

## 已知耦合（待解）
- ConvSidebar 同时承载会话列表与联系人视图（chat/contacts 混合）
- DetailPanel 含好友操作（拉黑/删除），归属 chat 但触达 contacts 能力
