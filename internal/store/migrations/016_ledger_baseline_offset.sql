-- 016: 账本基线偏移（替代 015 的 earned:=spent 近似）
--
-- 目标：
--   * 迁移时刻余额从 0 起算（历史不形成欠账/溢账）；
--   * earned/served 仍按真实 Key 归属（key_owner_id）统计，不虚增贡献。
-- 余额公式：balance = earned - spent + base（base = 迁移时刻 spent - earned）。

ALTER TABLE users ADD COLUMN base_calls  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN base_in     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN base_out    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN base_credit REAL    NOT NULL DEFAULT 0;

-- 重算真实贡献（撤销 015 的近似）
UPDATE users SET
  calls_earned  = (SELECT COUNT(*) FROM request_logs r WHERE r.key_owner_id = users.id AND r.status='success'),
  in_served     = (SELECT COALESCE(SUM(r.prompt_tokens),0)     FROM request_logs r WHERE r.key_owner_id = users.id AND r.status='success'),
  out_served    = (SELECT COALESCE(SUM(r.completion_tokens),0) FROM request_logs r WHERE r.key_owner_id = users.id AND r.status='success'),
  credit_earned = (SELECT COALESCE(SUM(r.cost),0)              FROM request_logs r WHERE r.key_owner_id = users.id AND r.status='success');

-- 基线偏移，使迁移时刻余额归零
UPDATE users SET
  base_calls  = calls_spent  - calls_earned,
  base_in     = in_spent     - in_served,
  base_out    = out_spent    - out_served,
  base_credit = credit_spent - credit_earned;
