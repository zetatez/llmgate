-- 010: 管理员服务端会话（可吊销） + 消费令牌可选过期时间
--
-- 管理员会话从"Cookie 直接存 admin token（无法吊销）"改为服务端会话：
--   - 登录时生成随机会话令牌，库内只存哈希；登出/改令牌即可吊销
-- user_tokens 增加 expires_at：0 = 永不过期；到期后网关鉴权拒绝。

CREATE TABLE IF NOT EXISTS admin_sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT    NOT NULL UNIQUE,
    created_at INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL DEFAULT 0
);

ALTER TABLE user_tokens ADD COLUMN expires_at INTEGER NOT NULL DEFAULT 0;
