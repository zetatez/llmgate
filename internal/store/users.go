package store

import (
	"database/sql"
	"fmt"

	"llmgate/internal/models"
)

const userCols = `id, name, remark, token_hash, token_enc, quota_limit, quota_used, status, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*models.User, error) {
	u := &models.User{}
	err := row.Scan(&u.ID, &u.Name, &u.Remark, &u.TokenHash, &u.TokenEnc, &u.QuotaLimit, &u.QuotaUsed, &u.Status, &u.CreatedAt, &u.UpdatedAt)
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

// GetUserByTokenHash 按令牌哈希查找用户。
func GetUserByTokenHash(db *sql.DB, hash string) (*models.User, error) {
	row := db.QueryRow(`SELECT `+userCols+` FROM users WHERE token_hash=?`, hash)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// CreateUser 创建用户，返回新 id。
func CreateUser(db *sql.DB, u *models.User) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO users (name, remark, token_hash, token_enc, quota_limit, quota_used, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?)`,
		u.Name, u.Remark, u.TokenHash, u.TokenEnc, u.QuotaLimit, u.Status, models.Now(), models.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateUser 更新用户基本信息（name/remark/quota_limit/status）。
func UpdateUser(db *sql.DB, u *models.User) error {
	res, err := db.Exec(`
		UPDATE users SET name=?, remark=?, quota_limit=?, status=?, updated_at=? WHERE id=?`,
		u.Name, u.Remark, u.QuotaLimit, u.Status, models.Now(), u.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user %d not found", u.ID)
	}
	return nil
}

// SetUserToken 重置用户令牌（同时更新哈希与加密值）。
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

// DeleteUser 删除用户。
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

// AddUserQuota 累加用户已用额度（当日清零逻辑由外部按需处理）。
func AddUserQuota(db *sql.DB, id int64, cost float64) error {
	_, err := db.Exec(`UPDATE users SET quota_used = quota_used + ? WHERE id=?`, cost, id)
	return err
}
