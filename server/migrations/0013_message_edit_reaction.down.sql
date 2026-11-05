DROP TABLE IF EXISTS `message_reactions`;

ALTER TABLE messages
  DROP COLUMN edited_at;
