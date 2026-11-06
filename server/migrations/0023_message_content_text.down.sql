-- 0023 回滚：缩回 VARCHAR(1000)
-- 注意：已有超过 1000 字符的消息会被截断或导致 ALTER 失败，回滚前请先备份。
ALTER TABLE messages MODIFY COLUMN content VARCHAR(1000);
