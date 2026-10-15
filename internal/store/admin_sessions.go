package store

import (
	"database/sql"

	"llmgate/internal/models"
)

// CreateAdminSession 建立一条管理员登录会话（库内只存令牌哈希）。
func CreateAdminSession(db *sql.DB, tokenHash string, expiresAt int64) error {
	_, err := db.Exec(`INSERT INTO admin_sessions (token_hash, created_at, expires_at) VALUES (?, ?, ?)`,
		tokenHash, models.Now(), expiresAt)
	return err
}

// AdminSessionValid 校验管理员会话令牌是否有效（未过期）。无效/过期返回 false。
func AdminSessionValid(db *sql.DB, hash string) (bool, error) {
	var expiresAt int64
	err := db.QueryRow(`SELECT expires_at FROM admin_sessions WHERE token_hash=?`, hash).Scan(&expiresAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if expiresAt > 0 && expiresAt <= models.Now() {
		_, _ = db.Exec(`DELETE FROM admin_sessions WHERE token_hash=?`, hash)
		return false, nil
	}
	return true, nil
}

// DeleteAdminSession 删除单条管理员会话（登出）。
func DeleteAdminSession(db *sql.DB, hash string) error {
	_, err := db.Exec(`DELETE FROM admin_sessions WHERE token_hash=?`, hash)
	return err
}

// DeleteAllAdminSessions 吊销全部管理员会话（轮换令牌时调用）。
func DeleteAllAdminSessions(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM admin_sessions`)
	return err
}
