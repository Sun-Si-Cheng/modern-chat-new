-- 消息编辑与表情回应
-- edited_at: 消息被编辑的时间；NULL 表示从未编辑
-- message_reactions: 每人对每条消息最多一个表情；重复设置会替换旧表情

ALTER TABLE messages
  ADD COLUMN edited_at DATETIME(3) NULL DEFAULT NULL COMMENT '编辑时间（服务端）' AFTER content;

CREATE TABLE IF NOT EXISTS `message_reactions` (
    `message_id` BIGINT UNSIGNED NOT NULL,
    `user_id`    BIGINT UNSIGNED NOT NULL,
    `emoji`      VARCHAR(32)     NOT NULL,
    `created_at` DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`message_id`, `user_id`),
    INDEX `idx_message_reactions_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='消息表情回应';
