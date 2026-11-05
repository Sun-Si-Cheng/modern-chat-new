-- 注册邮箱验证码。
-- 只存验证码的哈希，不存明文：拿到数据库也读不出正在生效的验证码。
-- 回滚：0020_email_codes.down.sql（DROP TABLE email_codes）
CREATE TABLE IF NOT EXISTS email_codes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  email VARCHAR(254) NOT NULL,
  code_hash CHAR(64) NOT NULL,
  purpose VARCHAR(16) NOT NULL DEFAULT 'register',
  attempts INT NOT NULL DEFAULT 0,
  expires_at DATETIME(3) NOT NULL,
  used_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_email_purpose_created (email, purpose, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
