# contacts 模块（联系人）

好友关系：申请 / 同意 / 删除、好友列表、用户资料卡。

## 归属
- 状态：stores/friends
- 组件：AddFriendDialog、ProfileCard（现居 components/chat/，迁移时移入）
- 视图：无独立页面（嵌在聊天侧栏与详情面板中呈现）

## 边界规则
1. 不 import `features/chat`、`features/settings` 内部；对聊天侧只暴露
   store 动作与组件 props。
2. 好友相关的 REST 调用只走 endpoints 用例（friendApi）。

## 已知耦合（待解）
- 聊天侧的拉黑/删除好友入口在 DetailPanel 内联实现
