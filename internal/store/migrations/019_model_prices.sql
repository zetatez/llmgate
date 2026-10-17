-- 019: 独立的模型价格表（替代 settings.model_pricing 的单 JSON 维护方式）
CREATE TABLE IF NOT EXISTS model_prices (
    model      TEXT PRIMARY KEY,
    input      REAL NOT NULL DEFAULT 0, -- USD / 1M input tokens
    output     REAL NOT NULL DEFAULT 0, -- USD / 1M output tokens
    call       REAL NOT NULL DEFAULT 0, -- USD / request (optional)
    updated_at INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_model_prices_updated ON model_prices(updated_at);
