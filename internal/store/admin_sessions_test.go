package store

import (
	"testing"

	"llmgate/internal/models"
)

func TestAdminSessionsLifecycle(t *testing.T) {
	db := setupDB(t)

	if ok, _ := AdminSessionValid(db, "unknown"); ok {
		t.Fatal("unknown session should be invalid")
	}
	if err := CreateAdminSession(db, "h1", models.Now()+1000); err != nil {
		t.Fatal(err)
	}
	if ok, err := AdminSessionValid(db, "h1"); err != nil || !ok {
		t.Fatalf("valid session expected, ok=%v err=%v", ok, err)
	}

	// 过期会话无效，并被清理
	if err := CreateAdminSession(db, "expired", models.Now()-1); err != nil {
		t.Fatal(err)
	}
	if ok, _ := AdminSessionValid(db, "expired"); ok {
		t.Fatal("expired session should be invalid")
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM admin_sessions WHERE token_hash='expired'`).Scan(&n)
	if n != 0 {
		t.Fatal("expired admin session should be purged on access")
	}

	// 删除单条
	if err := DeleteAdminSession(db, "h1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := AdminSessionValid(db, "h1"); ok {
		t.Fatal("deleted session should be invalid")
	}

	// 吊销全部
	_ = CreateAdminSession(db, "h2", models.Now()+1000)
	if err := DeleteAllAdminSessions(db); err != nil {
		t.Fatal(err)
	}
	if ok, _ := AdminSessionValid(db, "h2"); ok {
		t.Fatal("all sessions should be revoked")
	}

	// CleanupExpiredSessions 同时清理过期管理员会话
	_ = CreateAdminSession(db, "exp2", models.Now()-5)
	if err := CleanupExpiredSessions(db); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM admin_sessions WHERE token_hash='exp2'`).Scan(&n)
	if n != 0 {
		t.Fatal("cleanup should purge expired admin sessions")
	}
}
