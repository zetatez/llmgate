-- 004: users 增加 token_enc，以 AES 加密存储用户令牌（支持管理员随时回显复制）；
-- 鉴权仍使用 token_hash（单向哈希），两者并存。
ALTER TABLE users ADD COLUMN token_enc TEXT NOT NULL DEFAULT '';