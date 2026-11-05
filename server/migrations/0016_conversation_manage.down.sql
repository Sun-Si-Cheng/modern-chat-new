ALTER TABLE conversations
  DROP COLUMN mute_all;

ALTER TABLE user_conversations
  DROP COLUMN cleared_before_id,
  DROP COLUMN archived;
