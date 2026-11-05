-- 会话管理：归档、清空记录（按用户）与全员禁言
-- archived: 归档后从默认会话列表收起，可在「归档」筛选里查看
-- cleared_before_id: 该用户清空记录的水位，早于该 id 的消息不再下发
-- mute_all: 群全员禁言（群主/管理员仍可发言）

ALTER TABLE user_conversations
  ADD COLUMN archived BOOLEAN NOT NULL DEFAULT FALSE COMMENT '是否归档' AFTER muted,
  ADD COLUMN cleared_before_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '清空记录水位' AFTER archived;

ALTER TABLE conversations
  ADD COLUMN mute_all BOOLEAN NOT NULL DEFAULT FALSE COMMENT '全员禁言' AFTER avatar;
