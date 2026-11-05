-- 回滚 0020：删除验证码表。未完成的注册流程会失效，已注册账号不受影响。
DROP TABLE IF EXISTS email_codes;
