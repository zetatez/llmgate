package store

import (
	"database/sql"
	"time"
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
