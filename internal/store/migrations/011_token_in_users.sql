-- 011: 消费令牌回归 users 表（一人一 token，随用户行直接管理）
--
-- 之前把令牌拆到 user_tokens（支持多 token）；现收敛为"每人一个"，直接放回 users：
--   - users 增加 token_expires_at / token_last_used_at
--   - 从 user_tokens 回填每个用户最新的一条令牌
--   - 删除 user_tokens 表

ALTER TABLE users ADD COLUMN token_expires_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN token_last_used_at INTEGER NOT NULL DEFAULT 0;

UPDATE users SET
    token_hash = COALESCE((SELECT ut.token_hash FROM user_tokens ut WHERE ut.user_id = users.id ORDER BY ut.id DESC LIMIT 1), token_hash),
    token_enc = COALESCE((SELECT ut.token_enc FROM user_tokens ut WHERE ut.user_id = users.id ORDER BY ut.id DESC LIMIT 1), token_enc),
    token_expires_at = COALESCE((SELECT ut.expires_at FROM user_tokens ut WHERE ut.user_id = users.id ORDER BY ut.id DESC LIMIT 1), 0),
    token_last_used_at = COALESCE((SELECT ut.last_used_at FROM user_tokens ut WHERE ut.user_id = users.id ORDER BY ut.id DESC LIMIT 1), 0);

DROP TABLE IF EXISTS user_tokens;
