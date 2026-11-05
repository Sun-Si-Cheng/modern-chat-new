-- 回滚 0021：删除发送者索引（删除后后台用户详情统计会退化为全表扫描）
ALTER TABLE messages DROP INDEX idx_messages_sender;
