-- 提高默认透支额度（旧默认值：-50 calls / -5 credit），避免正常使用很快触发
-- "call credits exhausted"。仅升级仍在使用旧默认值的用户，保留管理员的自定义配置。
UPDATE users SET debt_floor_calls = -1000 WHERE debt_floor_calls = -50;
UPDATE users SET debt_floor_credit = -50 WHERE debt_floor_credit = -5;

-- 将 shiyi 的 calls 余额重置为 0（保留历史消耗/贡献统计，仅调整基准）。
UPDATE users SET base_calls = calls_spent - calls_earned WHERE username = 'shiyi';
