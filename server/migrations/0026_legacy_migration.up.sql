-- 0026 旧站迁移：用户映射、迁移任务、一次性登录票据（契约 §5）
-- legacy_user_map: 旧站 users.id → 新站 users.id，幂等与 complete-existing 的依据
-- migration_jobs: 单个旧站用户的迁移任务与阶段进度
-- migration_sso_tokens: 迁移完成后的 token+key 一次性登录票据（只存 sha256 哈希）

CREATE TABLE IF NOT EXISTS `legacy_user_map` (
    `legacy_id`  INT         NOT NULL COMMENT '旧站 users.id',
    `user_id`    BIGINT      NOT NULL COMMENT '新站 users.id',
    `created_at` DATETIME(3) NULL DEFAULT NULL,
    PRIMARY KEY (`legacy_id`),
    UNIQUE INDEX `uk_legacy_user_map_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='旧站用户 id 映射';

CREATE TABLE IF NOT EXISTS `migration_jobs` (
    `id`             VARCHAR(32)  NOT NULL COMMENT '32 位 hex 任务 id',
    `legacy_user_id` INT          NOT NULL COMMENT '旧站用户 id',
    `phase`          VARCHAR(16)  NOT NULL COMMENT 'users/friends/groups/messages/inbox/done',
    `done`           INT          NOT NULL DEFAULT 0 COMMENT '已迁移消息条数',
    `total`          INT          NOT NULL DEFAULT 0 COMMENT '选择迁移的消息总条数',
    `cursor`         VARCHAR(64)  NULL DEFAULT NULL COMMENT 'messages 阶段游标',
    `exclude_private_ids` TEXT     NULL DEFAULT NULL COMMENT '排除迁移的旧私聊消息 id，逗号分隔',
    `exclude_group_ids`   TEXT     NULL DEFAULT NULL COMMENT '排除迁移的旧群消息 id，逗号分隔',
    `finished`       TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '是否完成',
    `created_at`     DATETIME(3)  NULL DEFAULT NULL,
    `updated_at`     DATETIME(3)  NULL DEFAULT NULL,
    PRIMARY KEY (`id`),
    INDEX `idx_migration_jobs_legacy_user` (`legacy_user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='旧站迁移任务';

CREATE TABLE IF NOT EXISTS `migration_sso_tokens` (
    `token_hash` CHAR(64)    NOT NULL COMMENT 'sha256(token) hex',
    `key_hash`   CHAR(64)    NOT NULL COMMENT 'sha256(key) hex',
    `user_id`    BIGINT      NOT NULL COMMENT '新站 users.id',
    `expires_at` DATETIME(3) NOT NULL COMMENT '过期时间',
    `used_at`    DATETIME(3) NULL DEFAULT NULL COMMENT '使用时间（非空=已消费）',
    PRIMARY KEY (`token_hash`),
    INDEX `idx_migration_sso_tokens_user` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='迁移一次性登录票据';
