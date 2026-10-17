-- 018: 为历史遗留的空用户名账号补齐唯一用户名
-- 优先使用显示名（小写），冲突或为空时回退到 user<id>。
UPDATE users
SET username = lower(trim(name))
WHERE username = ''
  AND trim(name) != ''
  AND lower(trim(name)) NOT IN (SELECT lower(username) FROM users WHERE username != '');

UPDATE users
SET username = 'user' || id
WHERE username = '';
