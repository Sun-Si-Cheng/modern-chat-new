-- 0027 两步验证（2FA，默认关闭）：用户因子配置 + 一次性恢复码
-- users: TOTP 密钥 / 因子方式（''|app|email）/ 启用开关（不进任何对外 JSON）
ALTER TABLE `users`
    ADD COLUMN `twofa_secret`  VARCHAR(64)   NOT NULL DEFAULT '' COMMENT 'TOTP 密钥（base32），邮箱方式为空',
    ADD COLUMN `twofa_method`  VARCHAR(8)    NOT NULL DEFAULT '' COMMENT '两步验证方式：空=未启用 | app=验证器应用 | email=邮箱验证码',
    ADD COLUMN `twofa_enabled` TINYINT(1)    NOT NULL DEFAULT 0  COMMENT '两步验证是否启用';

-- twofa_recovery_codes: 启用时生成 8 个，单次有效；只存 sha256（明文仅展示一次）
CREATE TABLE IF NOT EXISTS `twofa_recovery_codes` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `user_id`    BIGINT UNSIGNED NOT NULL COMMENT '所属用户',
    `code_hash`  CHAR(64)        NOT NULL COMMENT '恢复码 sha256（归一化后）',
    `used_at`    DATETIME(3)     NULL DEFAULT NULL COMMENT '消费时间；NULL=未使用',
    `created_at` DATETIME(3)     NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_twofa_recovery_user` (`user_id`),
    UNIQUE INDEX `uk_twofa_recovery_hash` (`code_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='两步验证一次性恢复码';
