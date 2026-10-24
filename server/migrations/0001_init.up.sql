-- ModernChat 初始建表（与 GORM AutoMigrate 结构一致）
-- 回滚：执行 0001_init.down.sql

CREATE TABLE IF NOT EXISTS `users` (
    `id`             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    `created_at`     DATETIME(3) NULL,
    `updated_at`     DATETIME(3) NULL,
    `username`       VARCHAR(20)  NOT NULL,
    `password_hash`  VARCHAR(60)  NOT NULL,
    `nickname`       VARCHAR(20)  NOT NULL,
    `avatar_color`   VARCHAR(16)  NOT NULL DEFAULT '',
    `email`          VARCHAR(254) NULL,
    `email_verified` TINYINT(1)   NOT NULL DEFAULT 0,
    `role`           VARCHAR(10)  NOT NULL DEFAULT 'user',
    `token_version`  BIGINT       NOT NULL DEFAULT 1,
    `banned_until`   DATETIME(3)  NULL,
    `ban_reason`     VARCHAR(255) NULL,
    UNIQUE INDEX `idx_users_username` (`username`),
    UNIQUE INDEX `idx_users_email` (`email`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `conversations` (
    `id`         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    `created_at` DATETIME(3) NULL,
    `type`       TINYINT     NOT NULL COMMENT '1=单聊 2=群聊',
    `name`       VARCHAR(30) NULL,
    `owner_id`   BIGINT UNSIGNED NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `conversation_members` (
    `id`                   BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    `conversation_id`      BIGINT UNSIGNED NOT NULL,
    `user_id`              BIGINT UNSIGNED NOT NULL,
    `role`                 VARCHAR(10) NOT NULL DEFAULT 'member',
    `last_read_message_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `joined_at`            DATETIME(3) NULL,
    UNIQUE INDEX `idx_conv_user` (`conversation_id`, `user_id`),
    INDEX `idx_conversation_members_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `messages` (
    `id`             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    `created_at`     DATETIME(3) NULL,
    `conversation_id` BIGINT UNSIGNED NOT NULL,
    `sender_id`      BIGINT UNSIGNED NOT NULL,
    `client_msg_id`  VARCHAR(36) NOT NULL,
    `msg_type`       VARCHAR(32) NOT NULL DEFAULT 'text',
    `content`        VARCHAR(1000) NOT NULL,
    `is_recalled`    TINYINT(1)  NOT NULL DEFAULT 0,
    INDEX `idx_messages_conversation_id` (`conversation_id`),
    UNIQUE INDEX `idx_conv_clientmsg` (`conversation_id`, `client_msg_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `user_conversations` (
    `user_id`             BIGINT UNSIGNED NOT NULL,
    `conversation_id`     BIGINT UNSIGNED NOT NULL,
    `last_message_id`     BIGINT UNSIGNED NULL,
    `last_message_at`     DATETIME(3) NULL,
    `unread_count`        BIGINT      NOT NULL DEFAULT 0,
    `last_read_message_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
    PRIMARY KEY (`user_id`, `conversation_id`),
    INDEX `idx_user_conversations_conversation_id` (`conversation_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `friends` (
    `id`         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    `created_at` DATETIME(3) NULL,
    `user_id`    BIGINT UNSIGNED NOT NULL,
    `friend_id`  BIGINT UNSIGNED NOT NULL,
    `status`     TINYINT NOT NULL DEFAULT 0 COMMENT '0=待申请 1=已通过 2=已拒绝',
    INDEX `idx_uf` (`user_id`, `friend_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `plugins` (
    `id`              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    `created_at`      DATETIME(3) NULL,
    `name`            VARCHAR(30) NOT NULL,
    `version`         VARCHAR(16) NOT NULL DEFAULT '0.0.0',
    `description`     VARCHAR(255) NOT NULL DEFAULT '',
    `code_text`       MEDIUMTEXT NOT NULL,
    `permissions`     TEXT NULL,
    `allowed_hosts`   TEXT NULL,
    `enabled`         TINYINT(1) NOT NULL DEFAULT 0,
    `global_disabled` TINYINT(1) NOT NULL DEFAULT 0,
    `installed_by`    BIGINT UNSIGNED NOT NULL,
    INDEX `idx_plugins_installed_by` (`installed_by`),
    INDEX `idx_plugins_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `password_resets` (
    `id`         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    `created_at` DATETIME(3) NULL,
    `user_id`    BIGINT UNSIGNED NOT NULL,
    `token`      VARCHAR(64) NOT NULL,
    `expires_at` DATETIME(3) NOT NULL,
    `used`       TINYINT(1)  NOT NULL DEFAULT 0,
    UNIQUE INDEX `idx_password_resets_token` (`token`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
