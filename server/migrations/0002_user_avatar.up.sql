-- 用户自定义头像：前端压缩后的 data URL，空表示使用默认头像
ALTER TABLE users ADD COLUMN avatar MEDIUMTEXT NULL AFTER avatar_color;
