-- 0026 回滚：删除旧站迁移三张表
DROP TABLE IF EXISTS `migration_sso_tokens`;
DROP TABLE IF EXISTS `migration_jobs`;
DROP TABLE IF EXISTS `legacy_user_map`;
