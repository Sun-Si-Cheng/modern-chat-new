-- 第一批 IM 体验增强：会话置顶/免打扰、消息引用、@提醒

ALTER TABLE messages
  ADD COLUMN reply_to_id BIGINT UNSIGNED NULL DEFAULT NULL COMMENT '引用的消息 id（同会话）',
  ADD COLUMN mentions TEXT NULL COMMENT '被 @ 的用户 id（JSON 数组，仅群聊）',
  ADD INDEX idx_messages_reply_to (reply_to_id);

ALTER TABLE user_conversations
  ADD COLUMN pinned TINYINT(1) NOT NULL DEFAULT 0 COMMENT '置顶（个人设置）',
  ADD COLUMN muted TINYINT(1) NOT NULL DEFAULT 0 COMMENT '免打扰（个人设置）',
  ADD COLUMN mention_unread TINYINT(1) NOT NULL DEFAULT 0 COMMENT '有未读的 @我 消息';
