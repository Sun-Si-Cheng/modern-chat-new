-- 「仅自己删除」与消息收藏：均为每用户独立的关系表，不删除/修改消息本体。
-- 与 internal/model/chat.go 的 MessageDeletion / MessageFavorite 保持一致。

CREATE TABLE IF NOT EXISTS `message_deletions` (
    `user_id`    BIGINT UNSIGNED NOT NULL,
    `message_id` BIGINT UNSIGNED NOT NULL,
    `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`user_id`, `message_id`),
    INDEX `idx_message_deletions_message_id` (`message_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户仅自己删除的消息';

CREATE TABLE IF NOT EXISTS `message_favorites` (
    `user_id`    BIGINT UNSIGNED NOT NULL,
    `message_id` BIGINT UNSIGNED NOT NULL,
    `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`user_id`, `message_id`),
    INDEX `idx_message_favorites_message_id` (`message_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户收藏的消息';
