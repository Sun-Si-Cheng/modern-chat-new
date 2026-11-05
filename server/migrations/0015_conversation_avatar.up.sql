-- 群头像：存储上传接口返回的图片地址（本地 /uploads/ 或对象存储 URL）；
-- NULL 表示未设置，前端回退到「群名首字 + 颜色」的默认头像。

ALTER TABLE conversations
  ADD COLUMN avatar VARCHAR(255) NULL DEFAULT NULL COMMENT '群头像地址' AFTER name;
