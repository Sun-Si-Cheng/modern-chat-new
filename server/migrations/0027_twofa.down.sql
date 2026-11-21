-- 0027 回滚：移除两步验证
DROP TABLE IF EXISTS `twofa_recovery_codes`;

ALTER TABLE `users`
    DROP COLUMN `twofa_secret`,
    DROP COLUMN `twofa_method`,
    DROP COLUMN `twofa_enabled`;
