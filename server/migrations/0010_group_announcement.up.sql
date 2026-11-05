-- 群公告：仅群聊使用，存在 conversations 上（所有成员共享一份）
ALTER TABLE conversations
  ADD COLUMN announcement    VARCHAR(500) NULL DEFAULT NULL COMMENT '群公告内容，NULL 表示无公告',
  ADD COLUMN announcement_at DATETIME(3)  NULL DEFAULT NULL COMMENT '公告发布时间',
  ADD COLUMN announcement_by BIGINT UNSIGNED NULL DEFAULT NULL COMMENT '发布者 user_id';
