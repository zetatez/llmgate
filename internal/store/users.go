package store

import (
	"database/sql"
	"fmt"

	"llmgate/internal/models"
)

const userCols = `id, name, remark, username, password_hash, token_hash, token_enc, token_expires_at, token_last_used_at, quota_limit, quota_used, status,
	calls_spent, calls_earned, in_spent, out_spent, in_served, out_served, credit_spent, credit_earned, base_calls, base_in, base_out, base_credit, debt_floor_calls, debt_floor_credit,
	last_login_at, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*models.User, error) {
	u := &models.User{}
	err := row.Scan(&u.ID, &u.Name, &u.Remark, &u.Username, &u.PasswordHash, &u.TokenHash, &u.TokenEnc,
		&u.TokenExpiresAt, &u.TokenLastUsed, &u.QuotaLimit, &u.QuotaUsed, &u.Status,
		&u.CallsSpent, &u.CallsEarned, &u.InSpent, &u.OutSpent, &u.InServed, &u.OutServed, &u.CreditSpent, &u.CreditEarned,
		&u.BaseCalls, &u.BaseIn, &u.BaseOut, &u.BaseCredit, &u.DebtFloorCalls, &u.DebtFloorCredit,
		&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// ListUsers 返回全部用户。
func ListUsers(db *sql.DB) ([]*models.User, error) {
	rows, err := db.Query(`SELECT ` + userCols + ` FROM users ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// GetUser 按 id 获取用户。
func GetUser(db *sql.DB, id int64) (*models.User, error) {
	row := db.QueryRow(`SELECT `+userCols+` FROM users WHERE id=?`, id)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// GetUserByTokenHash 按消费令牌哈希查找启用且未过期的用户（网关鉴权）。
// 无令牌（token_enc 空）或已过期的不返回。
func GetUserByTokenHash(db *sql.DB, hash string) (*models.User, error) {
	row := db.QueryRow(`SELECT `+userCols+` FROM users
		WHERE token_hash=? AND token_enc != '' AND (token_expires_at = 0 OR token_expires_at > ?)`, hash, models.Now())
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// GetUserByUsername 按登录名查找用户（登录用）。空名或不存在返回 nil。
func GetUserByUsername(db *sql.DB, username string) (*models.User, error) {
	if username == "" {
		return nil, nil
	}
	row := db.QueryRow(`SELECT `+userCols+` FROM users WHERE username=?`, username)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// CreateUser 创建用户，返回新 id。
func CreateUser(db *sql.DB, u *models.User) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO users (name, remark, username, password_hash, token_hash, token_enc, token_expires_at, token_last_used_at, quota_limit, quota_used, status, last_login_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, 0, ?, ?, ?, ?)`,
		u.Name, u.Remark, u.Username, u.PasswordHash, u.TokenHash, u.TokenEnc, u.TokenExpiresAt,
		u.QuotaLimit, u.Status, u.LastLoginAt, models.Now(), models.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateUser 更新用户基本信息（name/remark/username/password_hash/quota_limit/status/债务下限）。
func UpdateUser(db *sql.DB, u *models.User) error {
	res, err := db.Exec(`
		UPDATE users SET name=?, remark=?, username=?, password_hash=?, quota_limit=?, status=?,
			debt_floor_calls=?, debt_floor_credit=?, updated_at=? WHERE id=?`,
		u.Name, u.Remark, u.Username, u.PasswordHash, u.QuotaLimit, u.Status,
		u.DebtFloorCalls, u.DebtFloorCredit, models.Now(), u.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user %d not found", u.ID)
	}
	return nil
}

// SetUserPassword 更新用户密码哈希（改密用）。
func SetUserPassword(db *sql.DB, id int64, hash string) error {
	res, err := db.Exec(`UPDATE users SET password_hash=?, updated_at=? WHERE id=?`, hash, models.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return nil
}

// SetUserToken 设置/轮换用户的消费令牌（hash 鉴权、encrypted 回显、expiresAt 过期时间）。
func SetUserToken(db *sql.DB, id int64, hash, encrypted string, expiresAt int64) error {
	res, err := db.Exec(`UPDATE users SET token_hash=?, token_enc=?, token_expires_at=?, updated_at=? WHERE id=?`,
		hash, encrypted, expiresAt, models.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return nil
}

// TouchUserToken 更新令牌最近使用时间。
func TouchUserToken(db *sql.DB, id int64) error {
	_, err := db.Exec(`UPDATE users SET token_last_used_at=? WHERE id=?`, models.Now(), id)
	return err
}

// TouchUserLogin 记录最近登录时间。
func TouchUserLogin(db *sql.DB, id int64) error {
	_, err := db.Exec(`UPDATE users SET last_login_at=? WHERE id=?`, models.Now(), id)
	return err
}

// DeleteUser 删除用户（user_sessions 级联删除；其贡献的 channel_keys 由调用方清理）。
func DeleteUser(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return nil
}

// ApplyUsage 在同一事务内记账：消费者累加 *_spent，贡献者（Key 归属）累加 *_earned。
// 自用（consumer==owner）时两侧相抵（净 0）。
func ApplyUsage(db *sql.DB, consumerID, ownerID int64, prompt, completion int64, cost float64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET
			calls_spent = calls_spent + 1,
			in_spent = in_spent + ?, out_spent = out_spent + ?,
			credit_spent = credit_spent + ?, quota_used = quota_used + ?
		WHERE id=?`, prompt, completion, cost, cost, consumerID); err != nil {
		return err
	}
	if ownerID > 0 {
		if _, err := tx.Exec(`UPDATE users SET
				calls_earned = calls_earned + 1,
				in_served = in_served + ?, out_served = out_served + ?,
				credit_earned = credit_earned + ?
			WHERE id=?`, prompt, completion, cost, ownerID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AddUserQuota 累加用户已用额度。设了上限时在事务性条件内累加，
// 避免并发请求"先查后增"造成大幅超额（至多一粒超限）。
func AddUserQuota(db *sql.DB, id int64, cost float64) error {
	var limit float64
	if err := db.QueryRow(`SELECT quota_limit FROM users WHERE id=?`, id).Scan(&limit); err != nil {
		return err
	}
	if limit > 0 {
		_, err := db.Exec(
			`UPDATE users SET quota_used = quota_used + ? WHERE id=? AND quota_used + ? <= ?`,
			cost, id, cost, limit)
		return err
	}
	_, err := db.Exec(`UPDATE users SET quota_used = quota_used + ? WHERE id=?`, cost, id)
	return err
}
