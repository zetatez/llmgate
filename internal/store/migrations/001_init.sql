-- 001_init: 初始表结构
-- 时间字段统一为 INTEGER unix 秒（Go 侧写入），避免驱动 timestamp 解析差异。

CREATE TABLE IF NOT EXISTS users (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL DEFAULT '',
    remark      TEXT    NOT NULL DEFAULT '',
    token_hash  TEXT    NOT NULL UNIQUE,
    quota_limit REAL    NOT NULL DEFAULT 0,   -- 0 = 不限
    quota_used  REAL    NOT NULL DEFAULT 0,
    status      INTEGER NOT NULL DEFAULT 1,   -- 1 启用 / 0 禁用
    created_at  INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS channels (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    base_url     TEXT    NOT NULL,
    adapter      TEXT    NOT NULL DEFAULT 'openai',
    priority     INTEGER NOT NULL DEFAULT 0,
    weight       INTEGER NOT NULL DEFAULT 1,
    timeout_ms   INTEGER NOT NULL DEFAULT 60000,
    enabled      INTEGER NOT NULL DEFAULT 1,
    health_state TEXT    NOT NULL DEFAULT 'healthy',   -- healthy / cooldown
    note         TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL DEFAULT 0,
    updated_at   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS channel_keys (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id   INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    api_key_enc  TEXT    NOT NULL,                     -- AES-256-GCM 加密后的 hex
    enabled      INTEGER NOT NULL DEFAULT 1,
    weight       INTEGER NOT NULL DEFAULT 1,
    last_used_at INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_channel_keys_channel ON channel_keys(channel_id);

CREATE TABLE IF NOT EXISTS model_routes (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    display_name   TEXT    NOT NULL,
    channel_id     INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    upstream_model TEXT    NOT NULL,
    priority       INTEGER NOT NULL DEFAULT 0,
    weight         INTEGER NOT NULL DEFAULT 1,
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     INTEGER NOT NULL DEFAULT 0,
    updated_at     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_model_routes_display ON model_routes(display_name);

CREATE TABLE IF NOT EXISTS request_logs (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id           INTEGER NOT NULL DEFAULT 0,
    display_model     TEXT    NOT NULL DEFAULT '',
    channel_id        INTEGER NOT NULL DEFAULT 0,
    key_id            INTEGER NOT NULL DEFAULT 0,
    upstream_model    TEXT    NOT NULL DEFAULT '',
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    cost              REAL    NOT NULL DEFAULT 0,
    latency_ms        INTEGER NOT NULL DEFAULT 0,
    status            TEXT    NOT NULL DEFAULT '',     -- success / error
    error_code        TEXT    NOT NULL DEFAULT '',
    stream            INTEGER NOT NULL DEFAULT 0,
    created_at        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_request_logs_created ON request_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_request_logs_model ON request_logs(display_model);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL DEFAULT 0
);