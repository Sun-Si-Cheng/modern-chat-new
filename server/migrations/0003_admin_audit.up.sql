-- 站长操作审计：记录后台的每一次写操作（谁、何时、对谁、做了什么）
CREATE TABLE IF NOT EXISTS admin_audits (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  admin_id BIGINT UNSIGNED NOT NULL COMMENT '操作人（users.id）',
  action VARCHAR(40) NOT NULL COMMENT 'ban / unban / kick / reset_password / set_role / purge_plugin / announce',
  target_type VARCHAR(20) NOT NULL COMMENT 'user / plugin / system',
  target_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '目标 id（插件类为 0，名称写在 detail）',
  target_name VARCHAR(120) NOT NULL DEFAULT '' COMMENT '用户名 / 插件名等可读标识',
  detail VARCHAR(500) NOT NULL DEFAULT '' COMMENT '附加说明（原因、天数、角色等）',
  ip VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_admin_audits_created (created_at),
  KEY idx_admin_audits_admin (admin_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='站长操作审计';
