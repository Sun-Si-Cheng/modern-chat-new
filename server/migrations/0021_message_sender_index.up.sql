-- messages.sender_id 索引。
--
-- 后台「用户详情」按发送者统计消息数（SELECT COUNT(*) WHERE sender_id = ?），
-- 此前没有索引 = 每次打开用户详情都扫全表；消息量到百万级时是明显的后台卡顿。
-- 回滚：0021_message_sender_index.down.sql
ALTER TABLE messages ADD INDEX idx_messages_sender (sender_id);
