package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"llmgate/internal/models"
)

const logCols = `id, user_id, display_model, channel_id, key_id, upstream_model, prompt_tokens, completion_tokens, total_tokens, cost, latency_ms, status, error_code, stream, created_at`

// insertLogCols 不含 id，交由 AUTOINCREMENT 分配。
const insertLogCols = `user_id, display_model, channel_id, key_id, upstream_model, prompt_tokens, completion_tokens, total_tokens, cost, latency_ms, status, error_code, stream, created_at`

// LogFilter 请求日志查询条件。
type LogFilter struct {
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
	args := make([]any, 0, len(logs)*14)
	sb.WriteString(`INSERT INTO request_logs (` + insertLogCols + `) VALUES `)
	for i, l := range logs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("(?,?,?,?,?,?,?,?,?,?,?,?,?,?)")
		args = append(args, l.UserID, l.DisplayModel, l.ChannelID, l.KeyID, l.UpstreamModel,
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
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := fmt.Sprintf(`SELECT %s FROM request_logs WHERE %s ORDER BY id DESC LIMIT ? OFFSET ?`,
		logCols, strings.Join(where, " AND "))
	args = append(args, limit, f.Offset)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.RequestLog
	for rows.Next() {
		l := &models.RequestLog{}
		if err := rows.Scan(&l.ID, &l.UserID, &l.DisplayModel, &l.ChannelID, &l.KeyID, &l.UpstreamModel,
			&l.PromptTokens, &l.CompletionTokens, &l.TotalTokens, &l.Cost, &l.LatencyMS,
			&l.Status, &l.ErrorCode, &l.Stream, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
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
		SELECT lg.user_id, COALESCE(u.name, ''), lg.display_model,
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

// RecentFailures 最近 N 条失败日志。
func RecentFailures(db *sql.DB, n int) ([]*models.RequestLog, error) {
	if n <= 0 || n > 50 {
		n = 10
	}
	rows, err := db.Query(`SELECT `+logCols+` FROM request_logs WHERE status != 'success' ORDER BY id DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.RequestLog
	for rows.Next() {
		l := &models.RequestLog{}
		if err := rows.Scan(&l.ID, &l.UserID, &l.DisplayModel, &l.ChannelID, &l.KeyID, &l.UpstreamModel,
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
