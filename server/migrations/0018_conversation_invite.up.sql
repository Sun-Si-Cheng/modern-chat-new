-- 群邀请链接：token 全局唯一，可设有效期，撤销后立即失效
CREATE TABLE IF NOT EXISTS `conversation_invites` (
    `token`           CHAR(32)        NOT NULL,
    `conversation_id` BIGINT UNSIGNED NOT NULL,
    `created_by`      BIGINT UNSIGNED NOT NULL,
    `expires_at`      DATETIME(3)     NULL DEFAULT NULL,
    `revoked`         BOOLEAN         NOT NULL DEFAULT FALSE,
    `created_at`      DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`token`),
    INDEX `idx_conversation_invites_conversation_id` (`conversation_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='群邀请链接';
