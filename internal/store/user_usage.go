package store

import (
	"database/sql"
	"time"

	"llmgate/internal/models"
)

// UserUsageByDay 返回某用户最近 days 天（含今天）每天聚合，不足的日期补零。
func UserUsageByDay(db *sql.DB, userID int64, days int) ([]UsagePoint, error) {
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
		WHERE user_id = ? AND created_at >= ?
		GROUP BY day
		ORDER BY day ASC`, userID, unixDayStart(days-1))
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

// UserUsageSummary 返回某用户在 [from,∞) 的汇总；from<=0 统计全部。
func UserUsageSummary(db *sql.DB, userID int64, from int64) (UsageSummary, error) {
	var s UsageSummary
	q := `
		SELECT COUNT(*),
			SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status!='success' THEN 1 ELSE 0 END),
			SUM(total_tokens),
			SUM(prompt_tokens),
			SUM(completion_tokens),
			SUM(cost)
		FROM request_logs WHERE user_id = ?`
	args := []any{userID}
	if from > 0 {
		q += ` AND created_at >= ?`
		args = append(args, from)
	}
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

// UserUsageByModel 按模型聚合某用户区间 [from,to) 用量（按 tokens 降序）。
func UserUsageByModel(db *sql.DB, userID, from, to int64) ([]ModelUsage, error) {
	args := []any{userID}
	q := `
		SELECT display_model,
			COUNT(*),
			SUM(CASE WHEN status='success' THEN total_tokens ELSE 0 END),
			SUM(CASE WHEN status='success' THEN prompt_tokens ELSE 0 END),
			SUM(CASE WHEN status='success' THEN completion_tokens ELSE 0 END),
			SUM(CASE WHEN status='success' THEN cost ELSE 0 END)
		FROM request_logs WHERE user_id = ?`
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
	out := []ModelUsage{}
	for rows.Next() {
		var m ModelUsage
		var req, ttok, ptok, ctok sql.NullInt64
		var cost sql.NullFloat64
		if err := rows.Scan(&m.Model, &req, &ttok, &ptok, &ctok, &cost); err != nil {
			return nil, err
		}
		m.Requests, m.TotalTokens, m.PromptTokens, m.CompletionToks = req.Int64, ttok.Int64, ptok.Int64, ctok.Int64
		m.Cost = cost.Float64
		out = append(out, m)
	}
	return out, rows.Err()
}

// UserUsageModelByDay 返回某用户最近 days 天按「日期 × 模型」拆分的用量。
func UserUsageModelByDay(db *sql.DB, userID int64, days int) ([]UsageModelPoint, error) {
	if days <= 0 {
		days = 7
	}
	rows, err := db.Query(`
		SELECT strftime('%Y-%m-%d', created_at, 'unixepoch', 'localtime') AS day,
			COALESCE(display_model, 'unknown'),
			COUNT(*), SUM(total_tokens)
		FROM request_logs
		WHERE user_id = ? AND created_at >= ?
		GROUP BY day, display_model
		ORDER BY day ASC, display_model ASC`, userID, unixDayStart(days-1))
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

// UserUsageByHour 返回某用户最近 hours 小时按小时聚合，缺失补零。
func UserUsageByHour(db *sql.DB, userID int64, hours int) ([]UsageHourPoint, error) {
	if hours <= 0 {
		hours = 24
	}
	now := time.Now()
	start := now.Add(time.Duration(-(hours - 1)) * time.Hour)
	startHour := time.Date(start.Year(), start.Month(), start.Day(), start.Hour(), 0, 0, 0, start.Location())
	rows, err := db.Query(`
		SELECT strftime('%Y-%m-%d %H', created_at, 'unixepoch', 'localtime') AS hr,
			COUNT(*), SUM(total_tokens), SUM(cost)
		FROM request_logs WHERE user_id = ? AND created_at >= ? GROUP BY hr`, userID, startHour.Unix())
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

// UserChannelStatsToday 返回某用户今日使用过的渠道统计（按请求数降序）。
func UserChannelStatsToday(db *sql.DB, userID int64) ([]ChannelDayStats, error) {
	rows, err := db.Query(`
		SELECT ch.id, ch.name, ch.health_state,
			SUM(CASE WHEN lg.created_at >= ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN lg.created_at >= ? AND lg.status != 'success' THEN 1 ELSE 0 END)
		FROM request_logs lg
		JOIN channels ch ON ch.id = lg.channel_id
		WHERE lg.user_id = ?
		GROUP BY ch.id
		ORDER BY SUM(CASE WHEN lg.created_at >= ? THEN 1 ELSE 0 END) DESC, ch.id ASC`,
		dayStart(), dayStart(), userID, dayStart())
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

// UserRecentFailures 返回某用户最近 N 条失败日志。
func UserRecentFailures(db *sql.DB, userID int64, n int) ([]*models.RequestLog, error) {
	if n <= 0 || n > 50 {
		n = 10
	}
	rows, err := db.Query(`SELECT `+queryLogSelect+` FROM request_logs lg
		LEFT JOIN channels ch ON ch.id = lg.channel_id
		LEFT JOIN users u ON u.id = lg.user_id
		WHERE lg.user_id = ? AND lg.status != 'success'
		ORDER BY lg.id DESC LIMIT ?`, userID, n)
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
