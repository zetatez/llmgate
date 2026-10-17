-- 020: 为历史账号补齐注册时间（优先取首次请求时间，其次更新时间，最后当前时间）
UPDATE users
SET created_at = COALESCE(
    (SELECT MIN(r.created_at) FROM request_logs r WHERE r.user_id = users.id),
    NULLIF(updated_at, 0),
    CAST(strftime('%s','now') AS INTEGER)
)
WHERE created_at = 0;
