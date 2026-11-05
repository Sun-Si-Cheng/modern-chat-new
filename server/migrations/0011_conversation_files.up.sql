-- 群文件：文件本体在 Storage，这里只记归属与展示信息
CREATE TABLE IF NOT EXISTS conversation_files (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  conversation_id BIGINT UNSIGNED NOT NULL,
  uploader_id     BIGINT UNSIGNED NOT NULL,
  name            VARCHAR(120)    NOT NULL COMMENT '用户上传时的文件名（展示用）',
  url             VARCHAR(255)    NOT NULL COMMENT '存储地址',
  size            BIGINT          NOT NULL DEFAULT 0,
  created_at      DATETIME(3)     NOT NULL,
  PRIMARY KEY (id),
  KEY idx_cf_conv (conversation_id, id),
  KEY idx_cf_uploader (uploader_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;
