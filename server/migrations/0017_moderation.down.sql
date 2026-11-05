ALTER TABLE messages
  DROP COLUMN admin_deleted_by,
  DROP COLUMN admin_deleted_at;

DROP TABLE IF EXISTS `message_reports`;
DROP TABLE IF EXISTS `user_blocks`;
