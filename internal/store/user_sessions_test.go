package store

import (
	"testing"

	"llmgate/internal/models"
)

func TestUserSessionsLifecycle(t *testing.T) {
	db := setupDB(t)
	uid, err := CreateUser(db, &models.User{Name: "u", Username: "u", TokenHash: "ph", Status: 1})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// 有效会话
	if err := CreateUserSession(db, uid, "sh1", models.Now()+1000); err != nil {
		t.Fatalf("create session: %v", err)
	}
	u, err := GetUserBySessionHash(db, "sh1")
	if err != nil || u == nil || u.ID != uid {
		t.Fatalf("resolve session: %+v err %v", u, err)
	}

	// 未知会话
	if u, _ := GetUserBySessionHash(db, "nope"); u != nil {
		t.Fatalf("unknown session should be nil, got %+v", u)
	}

	// 过期会话 → nil，并被清理
	if err := CreateUserSession(db, uid, "expired", models.Now()-1); err != nil {
		t.Fatalf("create expired: %v", err)
	}
	if u, _ := GetUserBySessionHash(db, "expired"); u != nil {
		t.Fatalf("expired session should be nil, got %+v", u)
	}
	var cnt int
	_ = db.QueryRow(`SELECT COUNT(*) FROM user_sessions WHERE token_hash='expired'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("expired session should be purged, got %d", cnt)
	}

	// 禁用用户 → 会话不解析
	_ = UpdateUser(db, &models.User{ID: uid, Username: "u", Status: 0, TokenHash: "ph"})
	if u, _ := GetUserBySessionHash(db, "sh1"); u != nil {
		t.Fatalf("disabled user session should not resolve, got %+v", u)
	}
	_ = UpdateUser(db, &models.User{ID: uid, Username: "u", Status: 1, TokenHash: "ph"})

	// 删除单个 / 删除某用户全部
	if err := CreateUserSession(db, uid, "sh2", models.Now()+1000); err != nil {
		t.Fatalf("create session 2: %v", err)
	}
	if err := DeleteUserSession(db, "sh1"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if u, _ := GetUserBySessionHash(db, "sh1"); u != nil {
		t.Fatal("deleted session should be nil")
	}
	if err := DeleteUserSessionsForUser(db, uid); err != nil {
		t.Fatalf("delete all sessions: %v", err)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM user_sessions WHERE user_id=?`, uid).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("expected all sessions removed, got %d", cnt)
	}

	// CleanupExpiredSessions 不报错
	if err := CreateUserSession(db, uid, "old", models.Now()-5); err != nil {
		t.Fatalf("create old: %v", err)
	}
	if err := CleanupExpiredSessions(db); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM user_sessions WHERE token_hash='old'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("cleanup should purge expired, got %d", cnt)
	}
}
