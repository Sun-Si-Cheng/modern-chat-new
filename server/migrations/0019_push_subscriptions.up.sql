-- Web Push 订阅：每个浏览器端点一行，同一端点重复订阅时更新密钥
CREATE TABLE IF NOT EXISTS `push_subscriptions` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `user_id`    BIGINT UNSIGNED NOT NULL,
    `endpoint`   VARCHAR(500)    NOT NULL,
    `p256dh`     VARCHAR(255)    NOT NULL,
    `auth`       VARCHAR(128)    NOT NULL,
    `created_at` DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    UNIQUE INDEX `uk_push_subscriptions_endpoint` (`endpoint`(191)),
    INDEX `idx_push_subscriptions_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Web Push 订阅';
