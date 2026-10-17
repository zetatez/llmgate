-- 015: 账本基线——历史消费不形成欠账
--
-- 014 的回填中，历史请求的 Key 多为管理员/全局（key_owner_id=0），只记了消费、
-- 没有贡献，会让老用户的可调用次数/金额余额变成很大的负数并立即触发债务下限。
-- 这里把"历史消费"同时记为"历史贡献"（earned := spent），使迁移时刻余额归零；
-- 之后的请求仍按真实的 Key 归属双向记账。

UPDATE users SET
  calls_earned  = calls_spent,
  in_served     = in_spent,
  out_served    = out_spent,
  credit_earned = credit_spent;
