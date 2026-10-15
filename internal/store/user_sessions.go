package store

import (
	"database/sql"

	"llmgate/internal/models"
)

// CreateUserSession 建立一条登录会话（库内只存令牌哈希）。
func CreateUserSession(db *sql.DB, userID int64, tokenHash string, expiresAt int64) error {
	_, err := db.Exec(`INSERT INTO user_sessions (user_id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		userID, tokenHash, models.Now(), expiresAt)
	return err
}

// GetUserBySessionHash 用会话令牌哈希定位用户（并校验未过期、账号启用）。
// 无效/过期返回 (nil, nil)。
func GetUserBySessionHash(db *sql.DB, hash string) (*models.User, error) {
	var userID int64
	var expiresAt int64
	err := db.QueryRow(`SELECT user_id, expires_at FROM user_sessions WHERE token_hash=?`, hash).Scan(&userID, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if expiresAt > 0 && expiresAt <= models.Now() {
		_, _ = db.Exec(`DELETE FROM user_sessions WHERE token_hash=?`, hash)
		return nil, nil
	}
	u, err := GetUser(db, userID)
	if err != nil {
		return nil, err
	}
	if u == nil || u.Status != 1 {
		return nil, nil
	}
	return u, nil
}

// DeleteUserSession 删除单条会话（登出）。
func DeleteUserSession(db *sql.DB, hash string) error {
	_, err := db.Exec(`DELETE FROM user_sessions WHERE token_hash=?`, hash)
	return err
}

// DeleteUserSessionsForUser 删除某用户全部会话（禁用/改密时强制下线）。
func DeleteUserSessionsForUser(db *sql.DB, userID int64) error {
	_, err := db.Exec(`DELETE FROM user_sessions WHERE user_id=?`, userID)
	return err
}

// CleanupExpiredSessions 清理过期的用户会话与管理员会话（启动时 + 后台定时调用）。
func CleanupExpiredSessions(db *sql.DB) error {
	now := models.Now()
	if _, err := db.Exec(`DELETE FROM user_sessions WHERE expires_at > 0 AND expires_at <= ?`, now); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM admin_sessions WHERE expires_at > 0 AND expires_at <= ?`, now)
	return err
}
