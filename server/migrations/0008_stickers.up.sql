-- 个人表情库：只存 URL，文件在 Storage（本地或对象存储）
CREATE TABLE IF NOT EXISTS stickers (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id    BIGINT UNSIGNED NOT NULL,
  url        VARCHAR(255)    NOT NULL,
  created_at DATETIME(3)     NOT NULL,
  PRIMARY KEY (id),
  KEY idx_stickers_user (user_id),
  UNIQUE KEY uk_stickers_user_url (user_id, url(191))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;
