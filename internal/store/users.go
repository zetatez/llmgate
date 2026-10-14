package store

import (
	"database/sql"
	"fmt"

	"llmgate/internal/models"
)

const userCols = `id, name, remark, username, password_hash, token_hash, token_enc, quota_limit, quota_used, status, last_login_at, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*models.User, error) {
	u := &models.User{}
	err := row.Scan(&u.ID, &u.Name, &u.Remark, &u.Username, &u.PasswordHash, &u.TokenHash, &u.TokenEnc,
		&u.QuotaLimit, &u.QuotaUsed, &u.Status, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
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

// GetUserByTokenHash 按遗留令牌哈希查找用户（保留兼容；网关鉴权已改用 user_tokens）。
func GetUserByTokenHash(db *sql.DB, hash string) (*models.User, error) {
	row := db.QueryRow(`SELECT `+userCols+` FROM users WHERE token_hash=?`, hash)
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
		INSERT INTO users (name, remark, username, password_hash, token_hash, token_enc, quota_limit, quota_used, status, last_login_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		u.Name, u.Remark, u.Username, u.PasswordHash, u.TokenHash, u.TokenEnc,
		u.QuotaLimit, u.Status, u.LastLoginAt, models.Now(), models.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateUser 更新用户基本信息（name/remark/username/password_hash/quota_limit/status）。
func UpdateUser(db *sql.DB, u *models.User) error {
	res, err := db.Exec(`
		UPDATE users SET name=?, remark=?, username=?, password_hash=?, quota_limit=?, status=?, updated_at=? WHERE id=?`,
		u.Name, u.Remark, u.Username, u.PasswordHash, u.QuotaLimit, u.Status, models.Now(), u.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user %d not found", u.ID)
	}
	return nil
}

// SetUserToken 重置用户名下的遗留令牌字段（兼容旧接口；新模型请用 user_tokens）。
func SetUserToken(db *sql.DB, id int64, hash, encrypted string) error {
	res, err := db.Exec(`UPDATE users SET token_hash=?, token_enc=?, updated_at=? WHERE id=?`, hash, encrypted, models.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return nil
}

// TouchUserLogin 记录最近登录时间。
func TouchUserLogin(db *sql.DB, id int64) error {
	_, err := db.Exec(`UPDATE users SET last_login_at=? WHERE id=?`, models.Now(), id)
	return err
}

// DeleteUser 删除用户（user_tokens / user_sessions 级联删除）。
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
