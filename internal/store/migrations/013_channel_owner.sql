-- 013: 渠道归属用户（用户可自建"套餐"渠道）
--   user_id = 0 → 管理员/全局渠道（历史数据默认归此）
--   user_id > 0 → 该用户自建渠道，仅本人可改，所有人只读可见
ALTER TABLE channels ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_channels_user ON channels(user_id);
