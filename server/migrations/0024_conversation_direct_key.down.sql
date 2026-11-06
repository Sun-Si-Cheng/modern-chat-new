-- 0024 回滚
DROP INDEX idx_conversations_direct_key ON conversations;
ALTER TABLE conversations DROP COLUMN direct_key;
