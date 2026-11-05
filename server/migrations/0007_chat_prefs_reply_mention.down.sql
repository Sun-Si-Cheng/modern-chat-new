ALTER TABLE user_conversations
  DROP COLUMN mention_unread,
  DROP COLUMN muted,
  DROP COLUMN pinned;

ALTER TABLE messages
  DROP INDEX idx_messages_reply_to,
  DROP COLUMN mentions,
  DROP COLUMN reply_to_id;
