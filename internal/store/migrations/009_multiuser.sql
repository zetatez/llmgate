-- 009: 多用户登录 + API Key 归属（BYOK 多租户）
--
-- 用户从"仅持有网关令牌的轻量账号"升级为"可登录的账号"：
--   * users 增加 username/password_hash/last_login_at（登录用）
--   * 网关消费令牌改由 user_tokens 承载（一个用户可创建多个，可单独吊销）
--   * 用户登录会话存 user_sessions（随机令牌的哈希 + 过期时间）
--   * channel_keys 增加 user_id，记录 Key 归属（0 = 管理员/历史遗留）

ALTER TABLE users ADD COLUMN username TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN last_login_at INTEGER NOT NULL DEFAULT 0;
-- 登录名非空时唯一（空串允许存在多条：历史/未分配用户名）
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users(username) WHERE username != '';

-- 归属用户；0 表示管理员录入的全局 Key（不参与普通用户路由）
ALTER TABLE channel_keys ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_channel_keys_user ON channel_keys(user_id);

-- 用户消费令牌（sk-xxx）：一个用户可创建多个
CREATE TABLE IF NOT EXISTS user_tokens (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL DEFAULT '',
    token_hash   TEXT    NOT NULL UNIQUE,
    token_enc    TEXT    NOT NULL DEFAULT '',
    enabled      INTEGER NOT NULL DEFAULT 1,
    last_used_at INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_user_tokens_user ON user_tokens(user_id);

-- 用户端登录会话（httpOnly Cookie 中为随机令牌，库内只存哈希）
CREATE TABLE IF NOT EXISTS user_sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT    NOT NULL UNIQUE,
    created_at INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_user_sessions_user ON user_sessions(user_id);

-- 兼容旧版：把既有用户令牌迁入 user_tokens，升级后既有 sk-xxx 客户端继续可用
INSERT INTO user_tokens (user_id, name, token_hash, token_enc, enabled, created_at)
SELECT id, 'legacy', token_hash, token_enc, status, created_at
FROM users
WHERE token_hash IS NOT NULL AND token_hash != '';
