package store

import (
	"database/sql"
)

// MemberStat 是面向所有登录用户公开的成员数据（不含密钥/备注等敏感信息）。
type MemberStat struct {
	ID            int64   `json:"id"`
	Username      string  `json:"username"`
	Name          string  `json:"name"`
	CreatedAt     int64   `json:"created_at"`
	CallsSpent    int64   `json:"calls_spent"`
	CallsEarned   int64   `json:"calls_earned"`
	CallsBalance  int64   `json:"calls_balance"`
	InSpent       int64   `json:"in_spent"`
	OutSpent      int64   `json:"out_spent"`
	InServed      int64   `json:"in_served"`
	OutServed     int64   `json:"out_served"`
	CreditSpent   float64 `json:"credit_spent"`
	CreditEarned  float64 `json:"credit_earned"`
	CreditBalance float64 `json:"credit_balance"`
}

// ListMemberStats 返回全部成员的公开消费/贡献与注册时间，按调用量降序。
func ListMemberStats(db *sql.DB) ([]MemberStat, error) {
	rows, err := db.Query(`
		SELECT id, username, name, created_at,
			calls_spent, calls_earned, calls_earned - calls_spent + base_calls,
			in_spent, out_spent, in_served, out_served,
			credit_spent, credit_earned, credit_earned - credit_spent + base_credit
		FROM users
		WHERE username != ''
		ORDER BY calls_spent DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MemberStat, 0)
	for rows.Next() {
		var m MemberStat
		if err := rows.Scan(&m.ID, &m.Username, &m.Name, &m.CreatedAt,
			&m.CallsSpent, &m.CallsEarned, &m.CallsBalance,
			&m.InSpent, &m.OutSpent, &m.InServed, &m.OutServed,
			&m.CreditSpent, &m.CreditEarned, &m.CreditBalance); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
