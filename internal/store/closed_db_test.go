package store

import (
	"database/sql"
	"testing"

	"llmgate/internal/models"
)

// newClosedDB 返回一个已关闭的 *sql.DB，用于触发各函数的 DB 错误分支。
func newClosedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.Close()
	return db
}

func TestChannelFunctionsClosedDB(t *testing.T) {
	db := newClosedDB(t)
	if _, err := ListChannels(db); err == nil {
		t.Fatal("ListChannels should error on closed db")
	}
	if _, err := CreateChannel(db, &models.Channel{Name: "x"}); err == nil {
		t.Fatal("CreateChannel should error on closed db")
	}
	if err := UpdateChannel(db, &models.Channel{ID: 1}); err == nil {
		t.Fatal("UpdateChannel should error on closed db")
	}
	if err := DeleteChannel(db, 1); err == nil {
		t.Fatal("DeleteChannel should error on closed db")
	}
	if _, err := ListChannelKeys(db, 1); err == nil {
		t.Fatal("ListChannelKeys should error on closed db")
	}
	// AddChannelKeys 的 db.Begin 应失败。
	if err := AddChannelKeys(db, 1, []KeyInput{{Encrypted: "e"}}); err == nil {
		t.Fatal("AddChannelKeys should error on closed db")
	}
	if err := UpdateChannelKey(db, &models.ChannelKey{ID: 1}); err == nil {
		t.Fatal("UpdateChannelKey should error on closed db")
	}
	if err := DeleteChannelKey(db, 1); err == nil {
		t.Fatal("DeleteChannelKey should error on closed db")
	}
}

func TestUserFunctionsClosedDB(t *testing.T) {
	db := newClosedDB(t)
	if _, err := ListUsers(db); err == nil {
		t.Fatal("ListUsers should error on closed db")
	}
	if _, err := CreateUser(db, &models.User{Name: "x"}); err == nil {
		t.Fatal("CreateUser should error on closed db")
	}
	if err := UpdateUser(db, &models.User{ID: 1}); err == nil {
		t.Fatal("UpdateUser should error on closed db")
	}
}

func TestRouteFunctionsClosedDB(t *testing.T) {
	db := newClosedDB(t)
	if _, err := ListModelRoutes(db); err == nil {
		t.Fatal("ListModelRoutes should error on closed db")
	}
	if _, err := ListRoutesForModel(db, "gpt"); err == nil {
		t.Fatal("ListRoutesForModel should error on closed db")
	}
	if _, err := CreateModelRoute(db, &models.ModelRoute{DisplayName: "a"}); err == nil {
		t.Fatal("CreateModelRoute should error on closed db")
	}
	if err := UpdateModelRoute(db, &models.ModelRoute{ID: 1}); err == nil {
		t.Fatal("UpdateModelRoute should error on closed db")
	}
	if err := DeleteModelRoute(db, 1); err == nil {
		t.Fatal("DeleteModelRoute should error on closed db")
	}
	if _, err := RemoveStaleRoutes(db, 1, []string{"m"}); err == nil {
		t.Fatal("RemoveStaleRoutes should error on closed db")
	}
}

func TestLogFunctionsClosedDB(t *testing.T) {
	db := newClosedDB(t)
	if _, err := QueryLogs(db, LogFilter{}); err == nil {
		t.Fatal("QueryLogs should error on closed db")
	}
	if _, err := UsageModelByDay(db, 7); err == nil {
		t.Fatal("UsageModelByDay should error on closed db")
	}
	if _, err := UsageByDay(db, 7); err == nil {
		t.Fatal("UsageByDay should error on closed db")
	}
	if _, err := UsageByModel(db, 0, 0); err == nil {
		t.Fatal("UsageByModel should error on closed db")
	}
	if _, err := UsageByUserModel(db, 0, 0); err == nil {
		t.Fatal("UsageByUserModel should error on closed db")
	}
	if _, err := ChannelStatsToday(db); err == nil {
		t.Fatal("ChannelStatsToday should error on closed db")
	}
	if _, err := DeleteOldLogs(db, 0); err == nil {
		t.Fatal("DeleteOldLogs should error on closed db")
	}
	if _, err := RecentFailures(db, 10); err == nil {
		t.Fatal("RecentFailures should error on closed db")
	}
}
