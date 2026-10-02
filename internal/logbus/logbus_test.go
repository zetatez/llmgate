package logbus

import (
	"database/sql"
	"testing"
	"time"

	"llmgate/internal/models"
	"llmgate/internal/store"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func countLogs(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(1) FROM request_logs`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// waitLogs 轮询直到日志条数达到 want（batch 满 100 或 1s ticker 都会触发刷盘），
// 避免依赖 Close 排空 channel 的竞态。
func waitLogs(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if countLogs(t, db) >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d logs, got %d", want, countLogs(t, db))
}

func mkLog() *models.RequestLog {
	return &models.RequestLog{
		UserID: 1, DisplayModel: "m", ChannelID: 2, KeyID: 3, UpstreamModel: "up",
		PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30, Cost: 0.5,
		LatencyMS: 100, Status: "success", Stream: 0, CreatedAt: 123456,
	}
}

// 少量日志走 1s 定时刷盘：应全部落库。
func TestTickerFlush(t *testing.T) {
	db := openDB(t)
	b := New(db, 0) // buffer<=0 → 默认 1024
	for i := 0; i < 5; i++ {
		b.Write(mkLog())
	}
	waitLogs(t, db, 5)
	b.Close()
	if got := countLogs(t, db); got != 5 {
		t.Fatalf("after close, inserted=%d want 5", got)
	}
}

// 达到批量阈值(100)即刷盘。
func TestBatchFlushAtSize(t *testing.T) {
	db := openDB(t)
	b := New(db, 100)
	for i := 0; i < 100; i++ {
		b.Write(mkLog())
	}
	waitLogs(t, db, 100)
	b.Close()
}

// 批量再上一档：105 条应全部落库。
func TestBatchOverSize(t *testing.T) {
	db := openDB(t)
	b := New(db, 128)
	for i := 0; i < 105; i++ {
		b.Write(mkLog())
	}
	waitLogs(t, db, 105)
	b.Close()
}

func TestCloseStopsWorker(t *testing.T) {
	db := openDB(t)
	b := New(db, 0)
	b.Write(mkLog())
	waitLogs(t, db, 1)
	b.Close() // 不 panic、能正常结束后台协程
}

// 缓冲满时 Write 应丢弃而非阻塞/panic。
func TestWriteWhenFullDrops(t *testing.T) {
	db := openDB(t)
	b := New(db, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5000; i++ {
			b.Write(mkLog())
		}
	}()
	<-done
	b.Close()
}
