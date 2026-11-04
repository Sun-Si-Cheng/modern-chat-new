-- 撤回时间：前端据此显示「XXX 撤回了一条消息」并在 3 分钟后把提示从聊天记录移除。
-- 历史已撤回的消息 recalled_at 为 NULL，前端按「已过期」处理（不再显示提示）。
ALTER TABLE messages
  ADD COLUMN recalled_at DATETIME(3) NULL DEFAULT NULL COMMENT '撤回时间（服务端）' AFTER is_recalled;
