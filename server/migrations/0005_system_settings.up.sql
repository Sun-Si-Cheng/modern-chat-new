-- 运行时配置（后台「系统设置」写入的键值对，库内值优先于环境变量）
-- 与 internal/model/setting.go 的 SystemSetting 结构保持一致
CREATE TABLE IF NOT EXISTS `system_settings` (
    `key`        VARCHAR(64) NOT NULL,
    `value`      TEXT        NOT NULL,
    `encrypted`  TINYINT(1)  NOT NULL DEFAULT 0 COMMENT '1=value 为 AES-256-GCM 密文',
    `updated_by` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '最后一次修改人（users.id）',
    `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='运行时系统设置';
