-- 0022 回滚：删除消息正文全文索引
ALTER TABLE messages DROP INDEX idx_messages_content_ft;
