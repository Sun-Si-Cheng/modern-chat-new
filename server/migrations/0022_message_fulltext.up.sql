-- 0022: 消息正文全文索引（ngram 分词，支持中文短词）
-- 回滚：ALTER TABLE messages DROP INDEX idx_messages_content_ft;
ALTER TABLE messages ADD FULLTEXT INDEX idx_messages_content_ft (content) WITH PARSER ngram;
