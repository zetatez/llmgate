-- 002: channel_keys 增加 name 字段（用于区分同一个渠道下的多个 Key）
ALTER TABLE channel_keys ADD COLUMN name TEXT NOT NULL DEFAULT '';