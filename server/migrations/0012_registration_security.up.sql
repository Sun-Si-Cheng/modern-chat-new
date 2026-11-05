-- 注册邀请码 + 登录失败锁定

CREATE TABLE IF NOT EXISTS invite_codes (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  code        VARCHAR(32)     NOT NULL COMMENT '12 字节随机数 hex',
  created_by  BIGINT UNSIGNED NOT NULL,
  max_uses    INT             NOT NULL DEFAULT 1,
  uses        INT             NOT NULL DEFAULT 0,
  expires_at  DATETIME(3)     NULL DEFAULT NULL COMMENT 'NULL = 永不过期',
  disabled    TINYINT(1)      NOT NULL DEFAULT 0,
  created_at  DATETIME(3)     NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_invite_code (code),
  KEY idx_invite_creator (created_by)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

CREATE TABLE IF NOT EXISTS login_attempts (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  username     VARCHAR(64)     NOT NULL,
  failed_count INT             NOT NULL DEFAULT 0,
  locked_until DATETIME(3)     NULL DEFAULT NULL,
  updated_at   DATETIME(3)     NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_login_attempt_username (username)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;
