-- 003: channel_keys 增加 source(来源)/remark(备注)，便于每个 Key 的维护管理
ALTER TABLE channel_keys ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE channel_keys ADD COLUMN remark TEXT NOT NULL DEFAULT '';