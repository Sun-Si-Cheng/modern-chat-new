-- 回滚 0025：只删除唯一索引。
-- 被清理掉的重复好友申请 / 历史验证码不会恢复（它们本就是脏数据）。
DROP INDEX uk_email_codes_email_purpose ON email_codes;
DROP INDEX uk_friends_pair ON friends;
