-- 006: channels 增加 cooldown_until，将熔断冷却持久化到 DB（重启不丢，后台探活自动恢复）
ALTER TABLE channels ADD COLUMN cooldown_until INTEGER NOT NULL DEFAULT 0;