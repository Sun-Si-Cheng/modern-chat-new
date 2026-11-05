-- 群管理员与禁言
-- role 复用现有 conversation_members.role：owner | admin | member
-- mute_until 为 NULL 表示未禁言；到时间后自动失效（无需定时任务）

ALTER TABLE conversation_members
  ADD COLUMN mute_until DATETIME(3) NULL DEFAULT NULL COMMENT '禁言截止时间，NULL=未禁言' AFTER role;
