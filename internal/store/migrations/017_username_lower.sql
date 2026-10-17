-- 017: 用户名统一小写（配合 users.username 唯一索引，实现大小写不敏感的唯一性）
UPDATE users SET username = lower(trim(username)) WHERE username != '';
