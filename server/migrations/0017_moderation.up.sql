-- 治理能力：拉黑、消息举报、管理员删除违规消息
-- user_blocks: 单向拉黑关系；(blocker_id, blocked_id) 唯一
-- message_reports: 每人对同一条消息只能举报一次
-- messages.admin_deleted_at/by: 管理员删除标记（对所有成员不可见）

CREATE TABLE IF NOT EXISTS `user_blocks` (
    `blocker_id` BIGINT UNSIGNED NOT NULL,
    `blocked_id` BIGINT UNSIGNED NOT NULL,
    `created_at` DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`blocker_id`, `blocked_id`),
    INDEX `idx_user_blocks_blocked_id` (`blocked_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户拉黑关系';

CREATE TABLE IF NOT EXISTS `message_reports` (
    `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `reporter_id` BIGINT UNSIGNED NOT NULL,
    `message_id`  BIGINT UNSIGNED NOT NULL,
    `reason`      VARCHAR(200)    NOT NULL,
    `status`      TINYINT         NOT NULL DEFAULT 0 COMMENT '0=待处理 1=已处理 2=已忽略',
    `created_at`  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    UNIQUE INDEX `uk_message_reports_reporter_message` (`reporter_id`, `message_id`),
    INDEX `idx_message_reports_message_id` (`message_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='消息举报';

ALTER TABLE messages
  ADD COLUMN admin_deleted_at DATETIME(3) NULL DEFAULT NULL COMMENT '管理员删除时间' AFTER edited_at,
  ADD COLUMN admin_deleted_by BIGINT UNSIGNED NULL DEFAULT NULL COMMENT '删除操作人' AFTER admin_deleted_at;
