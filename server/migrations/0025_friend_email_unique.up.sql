-- 0025: 好友关系与邮箱验证码补唯一约束
--
-- friends：并发发送好友申请（双击 / 双方同时发）会插出同方向的重复行，
-- 导致「同一个人重复出现在申请列表 / 好友列表」。加 (user_id, friend_id)
-- 唯一索引前先清理同方向的重复行：保留优先级最高的一条
-- （status 1 已通过 > 0 待处理 > 2 已拒绝；同优先级保留 id 最小的一条）。
DELETE f FROM friends f
JOIN friends g
  ON f.user_id = g.user_id
 AND f.friend_id = g.friend_id
 AND (
      (CASE g.status WHEN 1 THEN 0 WHEN 0 THEN 1 ELSE 2 END)
        < (CASE f.status WHEN 1 THEN 0 WHEN 0 THEN 1 ELSE 2 END)
   OR ((CASE g.status WHEN 1 THEN 0 WHEN 0 THEN 1 ELSE 2 END)
        = (CASE f.status WHEN 1 THEN 0 WHEN 0 THEN 1 ELSE 2 END)
       AND g.id < f.id)
     );
CREATE UNIQUE INDEX uk_friends_pair ON friends (user_id, friend_id);

-- email_codes：(email, purpose) 只能有一行，发码改为原地重置该行，
-- 避免并发请求各插一条有效码、绕过 60 秒冷却。保留最新一条即可。
DELETE f FROM email_codes f
JOIN email_codes g
  ON f.email = g.email
 AND f.purpose = g.purpose
 AND f.id < g.id;
CREATE UNIQUE INDEX uk_email_codes_email_purpose ON email_codes (email, purpose);
