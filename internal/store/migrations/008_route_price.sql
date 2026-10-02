-- 008: 路由级自定义计费单价（USD / 百万 tokens）。
-- price_input / price_output 为 NULL 时回退到全局 model_pricing（按对外模型名计费）。
ALTER TABLE model_routes ADD COLUMN price_input REAL;
ALTER TABLE model_routes ADD COLUMN price_output REAL;