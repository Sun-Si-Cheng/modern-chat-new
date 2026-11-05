-- 转发标记：转发是"复制内容到新会话"，不保留原作者，只标注来源性质
ALTER TABLE messages
  ADD COLUMN forwarded TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否转发消息';
