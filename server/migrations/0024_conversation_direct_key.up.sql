-- 0024: 单聊去重键 + 唯一索引
-- 并发发起单聊（双击、双方同时点开）此前会各自建一条会话；唯一索引兜底后
-- 只有一个插入成功，失败方回滚重查复用已有会话。
-- 存量单聊保持 NULL（MySQL 唯一索引允许多个 NULL），不打断现有数据；
-- 新会话由 EnsureSingle 写入 "小ID:大ID"。
ALTER TABLE conversations ADD COLUMN direct_key VARCHAR(64) NULL;
CREATE UNIQUE INDEX idx_conversations_direct_key ON conversations (direct_key);
