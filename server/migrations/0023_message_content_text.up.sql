-- 0023: 消息正文列扩为 TEXT，与后台「单条消息长度」上限（最大 5000 字符）匹配
-- 原列 VARCHAR(1000) 在严格模式下会报 1406（Data too long），非严格模式会静默截断。
-- TEXT 可容纳 5000 个多字节字符（UTF-8 下约 15KB），不会触及 64KB 上限。
ALTER TABLE messages MODIFY COLUMN content TEXT;
