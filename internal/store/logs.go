package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"llmgate/internal/models"
)

const logCols = `id, user_id, display_model, channel_id, key_id, upstream_model, prompt_tokens, completion_tokens, total_tokens, cost, latency_ms, status, error_code, stream, created_at`

// queryLogSelect 附带渠道名/用户名（联表，仅查询用）
const queryLogSelect = `lg.id, lg.user_id, COALESCE(NULLIF(u.username,''), u.name, ''), lg.display_model, lg.channel_id, lg.key_id, lg.key_owner_id, COALESCE(ch.name,''), lg.upstream_model, lg.prompt_tokens, lg.completion_tokens, lg.total_tokens, lg.cost, lg.latency_ms, lg.status, lg.error_code, lg.stream, lg.created_at`

// insertLogCols 不含 id，交由 AUTOINCREMENT 分配。
const insertLogCols = `user_id, display_model, channel_id, key_id, key_owner_id, upstream_model, prompt_tokens, completion_tokens, total_tokens, cost, latency_ms, status, error_code, stream, created_at`

// LogFilter 请求日志查询条件。
type LogFilter struct {
	UserID       int64 // >0 时仅返回该用户的日志
	DisplayModel string
	Status       string
	ChannelID    int64
	From         int64 // unix 秒
	To           int64
	Offset       int
	Limit        int
}

// InsertLogs 批量写入请求日志（供异步 logbus 调用）。
func InsertLogs(db *sql.DB, logs []*models.RequestLog) error {
	if len(logs) == 0 {
		return nil
	}
	var sb strings.Builder
	args := make([]any, 0, len(logs)*15)
	sb.WriteString(`INSERT INTO request_logs (` + insertLogCols + `) VALUES `)
	for i, l := range logs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)")
		args = append(args, l.UserID, l.DisplayModel, l.ChannelID, l.KeyID, l.KeyOwnerID, l.UpstreamModel,
			l.PromptTokens, l.CompletionTokens, l.TotalTokens, l.Cost, l.LatencyMS,
			l.Status, l.ErrorCode, l.Stream, l.CreatedAt)
	}
	_, err := db.Exec(sb.String(), args...)
	return err
}

// QueryLogs 查询请求日志。
func QueryLogs(db *sql.DB, f LogFilter) ([]*models.RequestLog, error) {
	where := []string{"1=1"}
	args := []any{}
	if f.UserID > 0 {
		where = append(where, "lg.user_id = ?")
		args = append(args, f.UserID)
	}
	if f.DisplayModel != "" {
		where = append(where, "lg.display_model = ?")
		args = append(args, f.DisplayModel)
	}
	if f.Status != "" {
		where = append(where, "lg.status = ?")
		args = append(args, f.Status)
	}
	if f.ChannelID > 0 {
		where = append(where, "lg.channel_id = ?")
		args = append(args, f.ChannelID)
	}
	if f.From > 0 {
		where = append(where, "lg.created_at >= ?")
		args = append(args, f.From)
	}
	if f.To > 0 {
		where = append(where, "lg.created_at <= ?")
		args = append(args, f.To)
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := fmt.Sprintf(`SELECT %s FROM request_logs lg LEFT JOIN channels ch ON ch.id = lg.channel_id LEFT JOIN users u ON u.id = lg.user_id WHERE %s ORDER BY lg.id DESC LIMIT ? OFFSET ?`,
		queryLogSelect, strings.Join(where, " AND "))
	args = append(args, limit, f.Offset)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.RequestLog
	for rows.Next() {
		l := &models.RequestLog{}
		if err := rows.Scan(&l.ID, &l.UserID, &l.UserName, &l.DisplayModel, &l.ChannelID, &l.KeyID, &l.KeyOwnerID, &l.ChannelName, &l.UpstreamModel,
			&l.PromptTokens, &l.CompletionTokens, &l.TotalTokens, &l.Cost, &l.LatencyMS,
			&l.Status, &l.ErrorCode, &l.Stream, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// CountLogs 返回满足条件（不含分页）的日志总数，供前端分页。
func CountLogs(db *sql.DB, f LogFilter) (int64, error) {
	where := []string{"1=1"}
	args := []any{}
	if f.UserID > 0 {
		where = append(where, "user_id = ?")
		args = append(args, f.UserID)
	}
	if f.DisplayModel != "" {
		where = append(where, "display_model = ?")
		args = append(args, f.DisplayModel)
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	if f.ChannelID > 0 {
		where = append(where, "channel_id = ?")
		args = append(args, f.ChannelID)
	}
	if f.From > 0 {
		where = append(where, "created_at >= ?")
		args = append(args, f.From)
	}
	if f.To > 0 {
		where = append(where, "created_at <= ?")
		args = append(args, f.To)
	}
	var n int64
	err := db.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE `+strings.Join(where, " AND "), args...).Scan(&n)
	return n, err
}

// UsageHourPoint 每小时的用量。
type UsageHourPoint struct {
	Hour     string  `json:"hour"` // HH:00
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
}

// UsageByHour 返回最近 hours 小时（含当前小时）按小时聚合，缺失补零。
func UsageByHour(db *sql.DB, hours int) ([]UsageHourPoint, error) {
	if hours <= 0 {
		hours = 24
	}
	now := time.Now()
	start := now.Add(time.Duration(-(hours - 1)) * time.Hour)
	startHour := time.Date(start.Year(), start.Month(), start.Day(), start.Hour(), 0, 0, 0, start.Location())
	rows, err := db.Query(`
		SELECT strftime('%Y-%m-%d %H', created_at, 'unixepoch', 'localtime') AS hr,
			COUNT(*), SUM(total_tokens), SUM(cost)
		FROM request_logs WHERE created_at >= ? GROUP BY hr`, startHour.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]UsageHourPoint{}
	for rows.Next() {
		var hr string
		var req, tok sql.NullInt64
		var cost sql.NullFloat64
		if err := rows.Scan(&hr, &req, &tok, &cost); err != nil {
			return nil, err
		}
		m[hr] = UsageHourPoint{Requests: req.Int64, Tokens: tok.Int64, Cost: cost.Float64}
	}
	out := make([]UsageHourPoint, 0, hours)
	for i := 0; i < hours; i++ {
		t := startHour.Add(time.Duration(i) * time.Hour)
		p := m[t.Format("2006-01-02 15")]
		p.Hour = t.Format("15:00")
		out = append(out, p)
	}
	return out, rows.Err()
}

// UsagePoint 按天聚合的用量。
type UsagePoint struct {
	Date         string  `json:"date"` // YYYY-MM-DD
	Requests     int64   `json:"requests"`
	Success      int64   `json:"success"`
	Errors       int64   `json:"errors"`
	TotalTokens  int64   `json:"total_tokens"`
	PromptTokens int64   `json:"prompt_tokens"`
	Cost         float64 `json:"cost"`
}

// UsageModelPoint 每日×模型用量（用于仪表盘堆叠占比图）。
type UsageModelPoint struct {
	Date     string `json:"date"` // YYYY-MM-DD
	Model    string `json:"model"`
	Requests int64  `json:"requests"`
	Tokens   int64  `json:"tokens"`
}

// UsageModelByDay 返回最近 days 天按「日期 × 模型」拆分的用量，按日期升序、模型名升序。
func UsageModelByDay(db *sql.DB, days int) ([]UsageModelPoint, error) {
	if days <= 0 {
		days = 7
	}
	rows, err := db.Query(`
		SELECT strftime('%Y-%m-%d', created_at, 'unixepoch', 'localtime') AS day,
			COALESCE(display_model, 'unknown'),
			COUNT(*), SUM(total_tokens)
		FROM request_logs
		WHERE created_at >= ?
		GROUP BY day, display_model
		ORDER BY day ASC, display_model ASC`, unixDayStart(days-1))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageModelPoint{}
	for rows.Next() {
		var p UsageModelPoint
		var reqs, tok sql.NullInt64
		if err := rows.Scan(&p.Date, &p.Model, &reqs, &tok); err != nil {
			return nil, err
		}
		p.Requests, p.Tokens = reqs.Int64, tok.Int64
		out = append(out, p)
	}
	return out, rows.Err()
}

// UsageByDay 返回最近 days 天（含今天）每天聚合，不足的日期补零。
func UsageByDay(db *sql.DB, days int) ([]UsagePoint, error) {
	if days <= 0 {
		days = 7
	}
	rows, err := db.Query(`
		SELECT strftime('%Y-%m-%d', created_at, 'unixepoch', 'localtime') AS day,
			COUNT(*),
			SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status!='success' THEN 1 ELSE 0 END),
			SUM(total_tokens),
			SUM(prompt_tokens),
			SUM(cost)
		FROM request_logs
		WHERE created_at >= ?
		GROUP BY day
		ORDER BY day ASC`, unixDayStart(days-1))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]UsagePoint{}
	for rows.Next() {
		var p UsagePoint
		var req, suc, errs, tok, ptok sql.NullInt64
		var cost sql.NullFloat64
		if err := rows.Scan(&p.Date, &req, &suc, &errs, &tok, &ptok, &cost); err != nil {
			return nil, err
		}
		p.Requests, p.Success, p.Errors, p.TotalTokens, p.PromptTokens = req.Int64, suc.Int64, errs.Int64, tok.Int64, ptok.Int64
		p.Cost = cost.Float64
		m[p.Date] = p
	}
	// 补齐缺日期
	out := make([]UsagePoint, 0, days)
	now := time.Now()
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		if p, ok := m[d]; ok {
			out = append(out, p)
		} else {
			out = append(out, UsagePoint{Date: d})
		}
	}
	return out, rows.Err()
}

// UsageSummary 汇总统计。
type UsageSummary struct {
	TotalRequests    int64   `json:"total_requests"`
	Success          int64   `json:"success"`
	Errors           int64   `json:"errors"`
	TotalTokens      int64   `json:"total_tokens"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
}

// UsageSummaryRange 返回 [from,to] 区间汇总；from<=0 则统计全部。
func UsageSummaryRange(db *sql.DB, from int64) (UsageSummary, error) {
	var s UsageSummary
	q := `
		SELECT COUNT(*),
			SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status!='success' THEN 1 ELSE 0 END),
			SUM(total_tokens),
			SUM(prompt_tokens),
			SUM(completion_tokens),
			SUM(cost)
		FROM request_logs`
	args := []any{}
	if from > 0 {
		q += ` WHERE created_at >= ?`
		args = append(args, from)
	}
	// 无日志时 SUM 返回 NULL，需用 Null 类型承接
	var req, suc, errs, tok, ptok, ctok sql.NullInt64
	var cost sql.NullFloat64
	err := db.QueryRow(q, args...).Scan(&req, &suc, &errs, &tok, &ptok, &ctok, &cost)
	if err != nil && err != sql.ErrNoRows {
		return s, err
	}
	s.TotalRequests, s.Success, s.Errors = req.Int64, suc.Int64, errs.Int64
	s.TotalTokens, s.PromptTokens, s.CompletionTokens = tok.Int64, ptok.Int64, ctok.Int64
	s.Cost = cost.Float64
	return s, nil
}

// ModelUsage 单模型在区间内的聚合。
type ModelUsage struct {
	Model          string  `json:"model"`
	Requests       int64   `json:"requests"`
	TotalTokens    int64   `json:"total_tokens"`
	PromptTokens   int64   `json:"prompt_tokens"`
	CompletionToks int64   `json:"completion_tokens"`
	Cost           float64 `json:"cost"`
}

// UsageByModel 按模型聚合区间 [from,to) 的用量，返回各模型降序。
func UsageByModel(db *sql.DB, from, to int64) ([]ModelUsage, error) {
	args := []any{}
	q := `
		SELECT display_model,
			COUNT(*),
			SUM(CASE WHEN status='success' THEN total_tokens ELSE 0 END),
			SUM(CASE WHEN status='success' THEN prompt_tokens ELSE 0 END),
			SUM(CASE WHEN status='success' THEN completion_tokens ELSE 0 END),
			SUM(CASE WHEN status='success' THEN cost ELSE 0 END)
		FROM request_logs WHERE 1=1`
	if from > 0 {
		q += ` AND created_at >= ?`
		args = append(args, from)
	}
	if to > 0 {
		q += ` AND created_at < ?`
		args = append(args, to)
	}
	q += ` GROUP BY display_model ORDER BY SUM(CASE WHEN status='success' THEN total_tokens ELSE 0 END) DESC`

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelUsage
	for rows.Next() {
		var m ModelUsage
		var req, tok, ptok, ctok sql.NullInt64
		var cost sql.NullFloat64
		if err := rows.Scan(&m.Model, &req, &tok, &ptok, &ctok, &cost); err != nil {
			return nil, err
		}
		m.Requests, m.TotalTokens = req.Int64, tok.Int64
		m.PromptTokens, m.CompletionToks = ptok.Int64, ctok.Int64
		m.Cost = cost.Float64
		out = append(out, m)
	}
	return out, rows.Err()
}

// UserModelUsage 单个用户对单个模型的用量聚合。
type UserModelUsage struct {
	UserID         int64   `json:"user_id"`
	UserName       string  `json:"user_name"`
	Model          string  `json:"model"`
	Requests       int64   `json:"requests"`
	PromptTokens   int64   `json:"prompt_tokens"`
	CompletionToks int64   `json:"completion_tokens"`
	TotalTokens    int64   `json:"total_tokens"`
	Cost           float64 `json:"cost"`
}

// UsageByUserModel 按 用户×模型 聚合区间 [from,to) 的用量（请求次数 / 上传token / 下载token / 费用）。
func UsageByUserModel(db *sql.DB, from, to int64) ([]UserModelUsage, error) {
	q := `
		SELECT lg.user_id, COALESCE(NULLIF(u.username,''), u.name, ''), lg.display_model,
			COUNT(*),
			SUM(lg.prompt_tokens), SUM(lg.completion_tokens), SUM(lg.total_tokens), SUM(lg.cost)
		FROM request_logs lg
		LEFT JOIN users u ON u.id = lg.user_id
		WHERE 1=1`
	args := []any{}
	if from > 0 {
		q += ` AND lg.created_at >= ?`
		args = append(args, from)
	}
	if to > 0 {
		q += ` AND lg.created_at < ?`
		args = append(args, to)
	}
	q += ` GROUP BY lg.user_id, lg.display_model ORDER BY lg.user_id ASC, SUM(lg.total_tokens) DESC`

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserModelUsage
	for rows.Next() {
		var m UserModelUsage
		var req, ptok, ctok, ttok sql.NullInt64
		var cost sql.NullFloat64
		if err := rows.Scan(&m.UserID, &m.UserName, &m.Model, &req, &ptok, &ctok, &ttok, &cost); err != nil {
			return nil, err
		}
		m.Requests, m.PromptTokens = req.Int64, ptok.Int64
		m.CompletionToks, m.TotalTokens = ctok.Int64, ttok.Int64
		m.Cost = cost.Float64
		out = append(out, m)
	}
	return out, rows.Err()
}

// UserUsage 单个用户在区间内的用量聚合（含成功次数）。
type UserUsage struct {
	UserID           int64   `json:"user_id"`
	UserName         string  `json:"user_name"`
	Requests         int64   `json:"requests"`
	Success          int64   `json:"success"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	Cost             float64 `json:"cost"`
}

// UsageByUser 按用户聚合区间 [from,to) 的用量（请求次数/成功次数/上传/下载/合计/费用），按 token 降序。
// 供"团队共享统计"使用（所有用户可见）。
func UsageByUser(db *sql.DB, from, to int64) ([]UserUsage, error) {
	q := `
		SELECT lg.user_id, COALESCE(NULLIF(u.username,''), u.name, ''),
			COUNT(*),
			SUM(CASE WHEN lg.status='success' THEN 1 ELSE 0 END),
			SUM(lg.prompt_tokens), SUM(lg.completion_tokens), SUM(lg.total_tokens), SUM(lg.cost)
		FROM request_logs lg
		LEFT JOIN users u ON u.id = lg.user_id
		WHERE 1=1`
	args := []any{}
	if from > 0 {
		q += ` AND lg.created_at >= ?`
		args = append(args, from)
	}
	if to > 0 {
		q += ` AND lg.created_at < ?`
		args = append(args, to)
	}
	q += ` GROUP BY lg.user_id ORDER BY SUM(lg.total_tokens) DESC`
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserUsage
	for rows.Next() {
		var m UserUsage
		var req, suc, ptok, ctok, ttok sql.NullInt64
		var cost sql.NullFloat64
		if err := rows.Scan(&m.UserID, &m.UserName, &req, &suc, &ptok, &ctok, &ttok, &cost); err != nil {
			return nil, err
		}
		m.Requests, m.Success = req.Int64, suc.Int64
		m.PromptTokens, m.CompletionTokens, m.TotalTokens = ptok.Int64, ctok.Int64, ttok.Int64
		m.Cost = cost.Float64
		out = append(out, m)
	}
	return out, rows.Err()
}

// UsageEarned 某用户作为"贡献者"在区间内被他人使用的聚合。
type UsageEarned struct {
	Calls int64   `json:"calls"`
	In    int64   `json:"in_tokens"`
	Out   int64   `json:"out_tokens"`
	Cost  float64 `json:"cost"`
}

// UsageEarnedByUser 按贡献者(key_owner_id)聚合区间 [from,to) 的成功请求。
func UsageEarnedByUser(db *sql.DB, from, to int64) (map[int64]UsageEarned, error) {
	q := `SELECT key_owner_id, COUNT(*), SUM(prompt_tokens), SUM(completion_tokens), SUM(cost)
		FROM request_logs WHERE status='success' AND key_owner_id > 0`
	args := []any{}
	if from > 0 {
		q += ` AND created_at >= ?`
		args = append(args, from)
	}
	if to > 0 {
		q += ` AND created_at < ?`
		args = append(args, to)
	}
	q += ` GROUP BY key_owner_id`
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]UsageEarned{}
	for rows.Next() {
		var id int64
		var calls, in, oc sql.NullInt64
		var cost sql.NullFloat64
		if err := rows.Scan(&id, &calls, &in, &oc, &cost); err != nil {
			return nil, err
		}
		out[id] = UsageEarned{Calls: calls.Int64, In: in.Int64, Out: oc.Int64, Cost: cost.Float64}
	}
	return out, rows.Err()
}

// ChannelDayStats 各渠道当日请求/错误数（用于健康卡片）。
type ChannelDayStats struct {
	ChannelID int64  `json:"channel_id"`
	Name      string `json:"name"`
	Health    string `json:"health"`
	TodayReq  int64  `json:"today_requests"`
	TodayErr  int64  `json:"today_errors"`
}

// ChannelStatsToday 返回各渠道今日请求统计（含未发生请求的渠道）。
func ChannelStatsToday(db *sql.DB) ([]ChannelDayStats, error) {
	rows, err := db.Query(`
		SELECT ch.id, ch.name, ch.health_state,
			SUM(CASE WHEN lg.created_at >= ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN lg.created_at >= ? AND lg.status != 'success' THEN 1 ELSE 0 END)
		FROM channels ch
		LEFT JOIN request_logs lg ON lg.channel_id = ch.id
		GROUP BY ch.id
		ORDER BY ch.id ASC`, dayStart(), dayStart())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelDayStats
	for rows.Next() {
		var s ChannelDayStats
		var req, errs sql.NullInt64
		if err := rows.Scan(&s.ChannelID, &s.Name, &s.Health, &req, &errs); err != nil {
			return nil, err
		}
		s.TodayReq, s.TodayErr = req.Int64, errs.Int64
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteOldLogs 删除创建时间早于 cutoff 的请求日志，返回删除条数（日志保留清理任务）。
func DeleteOldLogs(db *sql.DB, cutoff int64) (int64, error) {
	res, err := db.Exec(`DELETE FROM request_logs WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RecentFailures 最近 N 条失败日志。
func RecentFailures(db *sql.DB, n int) ([]*models.RequestLog, error) {
	if n <= 0 || n > 50 {
		n = 10
	}
	rows, err := db.Query(`SELECT `+queryLogSelect+` FROM request_logs lg
		LEFT JOIN channels ch ON ch.id = lg.channel_id
		LEFT JOIN users u ON u.id = lg.user_id
		WHERE lg.status != 'success' ORDER BY lg.id DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.RequestLog
	for rows.Next() {
		l := &models.RequestLog{}
		if err := rows.Scan(&l.ID, &l.UserID, &l.UserName, &l.DisplayModel, &l.ChannelID, &l.KeyID, &l.KeyOwnerID, &l.ChannelName, &l.UpstreamModel,
			&l.PromptTokens, &l.CompletionTokens, &l.TotalTokens, &l.Cost, &l.LatencyMS,
			&l.Status, &l.ErrorCode, &l.Stream, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// dayStart 返回今天 0 点的 unix 秒（本地时区）。
func dayStart() int64 {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
}

// unixDayStart 返回 N 天前 0 点的 unix 秒。
func unixDayStart(n int) int64 {
	now := time.Now().AddDate(0, 0, -n)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
}
