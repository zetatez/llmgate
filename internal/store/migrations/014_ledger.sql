-- 014: 共享 Key 池的双向账本（次数 / 上传下载 token / 金额）+ 债务下限
--
-- 每个成功请求记录贡献者 key_owner_id（被选中 Key 的归属用户，0=平台）。
-- 消费者累加 *_spent；贡献者累加 *_earned（自用相抵 → 净 0）。

ALTER TABLE request_logs ADD COLUMN key_owner_id INTEGER NOT NULL DEFAULT 0;

ALTER TABLE users ADD COLUMN calls_spent  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN calls_earned INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN in_spent     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN out_spent    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN in_served    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN out_served   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN credit_spent REAL    NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN credit_earned REAL   NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN debt_floor_calls  INTEGER NOT NULL DEFAULT -50;
ALTER TABLE users ADD COLUMN debt_floor_credit REAL    NOT NULL DEFAULT -5;

CREATE INDEX IF NOT EXISTS idx_request_logs_key_owner ON request_logs(key_owner_id);

-- 回填历史贡献者
UPDATE request_logs SET key_owner_id = COALESCE(
  (SELECT ck.user_id FROM channel_keys ck WHERE ck.id = request_logs.key_id), 0)
WHERE key_id > 0;

-- 历史账本：按成功日志双向回填（自用两侧相抵 → 净 0）
UPDATE users SET
  calls_spent   = (SELECT COUNT(*) FROM request_logs r WHERE r.user_id = users.id AND r.status='success'),
  in_spent      = (SELECT COALESCE(SUM(r.prompt_tokens),0)     FROM request_logs r WHERE r.user_id = users.id AND r.status='success'),
  out_spent     = (SELECT COALESCE(SUM(r.completion_tokens),0) FROM request_logs r WHERE r.user_id = users.id AND r.status='success'),
  credit_spent  = (SELECT COALESCE(SUM(r.cost),0)              FROM request_logs r WHERE r.user_id = users.id AND r.status='success'),
  calls_earned  = (SELECT COUNT(*) FROM request_logs r WHERE r.key_owner_id = users.id AND r.status='success'),
  in_served     = (SELECT COALESCE(SUM(r.prompt_tokens),0)     FROM request_logs r WHERE r.key_owner_id = users.id AND r.status='success'),
  out_served    = (SELECT COALESCE(SUM(r.completion_tokens),0) FROM request_logs r WHERE r.key_owner_id = users.id AND r.status='success'),
  credit_earned = (SELECT COALESCE(SUM(r.cost),0)              FROM request_logs r WHERE r.key_owner_id = users.id AND r.status='success');
