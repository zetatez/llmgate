-- 007: channels 增加 extra_headers（JSON 对象），用于给该渠道的请求附加自定义头（如 x-opencode-session）
ALTER TABLE channels ADD COLUMN extra_headers TEXT NOT NULL DEFAULT '';