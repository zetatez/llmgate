-- 005: 删除旧格式用户（只有 token_hash、无加密令牌 token_enc 的）。
-- 旧格式令牌无法回显复制，平台不再兼容旧版用户，直接清理。
DELETE FROM users WHERE token_enc = '' OR token_enc IS NULL;