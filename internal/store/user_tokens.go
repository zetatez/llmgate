package store

import (
	"database/sql"
	"fmt"

	"llmgate/internal/models"
)

const userTokenCols = `id, user_id, name, token_hash, token_enc, enabled, expires_at, last_used_at, created_at`

func scanUserToken(row interface{ Scan(...any) error }) (*models.UserToken, error) {
	t := &models.UserToken{}
	err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.TokenHash, &t.TokenEnc, &t.Enabled, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// ListUserTokens 返回某用户的全部消费令牌（最新在前）。
func ListUserTokens(db *sql.DB, userID int64) ([]*models.UserToken, error) {
	rows, err := db.Query(`SELECT `+userTokenCols+` FROM user_tokens WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.UserToken
	for rows.Next() {
		t, err := scanUserToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetUserToken 按 id 获取令牌。
func GetUserToken(db *sql.DB, id int64) (*models.UserToken, error) {
	row := db.QueryRow(`SELECT `+userTokenCols+` FROM user_tokens WHERE id=?`, id)
	t, err := scanUserToken(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// CreateUserToken 新建消费令牌，返回新 id。
func CreateUserToken(db *sql.DB, t *models.UserToken) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO user_tokens (user_id, name, token_hash, token_enc, enabled, expires_at, last_used_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?)`,
		t.UserID, t.Name, t.TokenHash, t.TokenEnc, t.Enabled, t.ExpiresAt, models.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateUserToken 更新令牌名称、启用状态与过期时间。
func UpdateUserToken(db *sql.DB, t *models.UserToken) error {
	res, err := db.Exec(`UPDATE user_tokens SET name=?, enabled=?, expires_at=? WHERE id=?`, t.Name, t.Enabled, t.ExpiresAt, t.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: user token %d", ErrNotFound, t.ID)
	}
	return nil
}

// DeleteUserToken 删除令牌。
func DeleteUserToken(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM user_tokens WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: user token %d", ErrNotFound, id)
	}
	return nil
}

// TouchUserToken 更新令牌最近使用时间。
func TouchUserToken(db *sql.DB, id int64) error {
	_, err := db.Exec(`UPDATE user_tokens SET last_used_at=? WHERE id=?`, models.Now(), id)
	return err
}

// ResolveUserToken 用令牌哈希定位所属用户与其令牌（网关鉴权）。
// 令牌不存在、被禁用、已过期，或用户被禁用时返回 (nil, token, nil)（token 可能非空便于区分原因）。
func ResolveUserToken(db *sql.DB, hash string) (*models.User, *models.UserToken, error) {
	row := db.QueryRow(`SELECT `+userTokenCols+` FROM user_tokens WHERE token_hash=?`, hash)
	t, err := scanUserToken(row)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if t.Enabled != 1 || (t.ExpiresAt > 0 && t.ExpiresAt <= models.Now()) {
		return nil, t, nil
	}
	u, err := GetUser(db, t.UserID)
	if err != nil {
		return nil, t, err
	}
	if u == nil || u.Status != 1 {
		return nil, t, nil
	}
	return u, t, nil
}
