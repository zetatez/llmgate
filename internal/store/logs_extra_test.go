package store

import (
	"testing"

	"llmgate/internal/models"
)

func TestInsertLogsEmptyAndQueryFilters(t *testing.T) {
	db := setupDB(t)

	// 空输入应直接返回 nil。
	if err := InsertLogs(db, nil); err != nil {
		t.Fatalf("empty insert should be no-op: %v", err)
	}
	if err := InsertLogs(db, []*models.RequestLog{}); err != nil {
		t.Fatalf("empty slice insert should be no-op: %v", err)
	}

	chanID := createChannelFor(t, db)
	now := models.Now()
	logs := []*models.RequestLog{
		{UserID: 1, DisplayModel: "gpt", ChannelID: chanID, KeyID: 1, UpstreamModel: "u-gpt", PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, Cost: 0.1, LatencyMS: 100, Status: "success", CreatedAt: now - 86400},
		{UserID: 1, DisplayModel: "gpt", ChannelID: chanID, KeyID: 2, UpstreamModel: "u-gpt", TotalTokens: 8, Status: "error", ErrorCode: "E1", CreatedAt: now - 3600},
		{UserID: 2, DisplayModel: "claude", ChannelID: 0, UpstreamModel: "u-claude", TotalTokens: 30, Status: "success", Stream: 1, CreatedAt: now},
	}
	if err := InsertLogs(db, logs); err != nil {
		t.Fatalf("insert logs: %v", err)
	}

	// 无过滤查询（默认 limit 50）。
	all, err := QueryLogs(db, LogFilter{})
	if err != nil {
		t.Fatalf("query all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(all))
	}
	// 联表应填充 ChannelName。
	foundChanName := false
	for _, l := range all {
		if l.ChannelID == chanID && l.ChannelName == "ch" {
			foundChanName = true
		}
	}
	if !foundChanName {
		t.Fatalf("expected channel name joined, got %+v", all)
	}

	// 按状态过滤。
	errors, _ := QueryLogs(db, LogFilter{Status: "error"})
	if len(errors) != 1 || errors[0].ErrorCode != "E1" {
		t.Fatalf("expected 1 error log, got %+v", errors)
	}

	// 按模型过滤。
	gpts, _ := QueryLogs(db, LogFilter{DisplayModel: "gpt"})
	if len(gpts) != 2 {
		t.Fatalf("expected 2 gpt logs, got %d", len(gpts))
	}

	// 按渠道过滤。
	chanLogs, _ := QueryLogs(db, LogFilter{ChannelID: chanID})
	if len(chanLogs) != 2 {
		t.Fatalf("expected 2 channel logs, got %d", len(chanLogs))
	}

	// 按时间范围过滤。
	recent, _ := QueryLogs(db, LogFilter{From: now - 7200})
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent logs, got %d", len(recent))
	}
	old, _ := QueryLogs(db, LogFilter{From: 0, To: now - 7200})
	if len(old) != 1 {
		t.Fatalf("expected 1 old log, got %d", len(old))
	}

	// limit<=0 或 >500 时回退到 50；offset 生效。
	page, _ := QueryLogs(db, LogFilter{Limit: 1, Offset: 0})
	if len(page) != 1 {
		t.Fatalf("expected 1 log with limit=1, got %d", len(page))
	}
}

func TestUsageAggregations(t *testing.T) {
	db := setupDB(t)
	now := models.Now()
	logs := []*models.RequestLog{
		{UserID: 1, DisplayModel: "gpt", PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, Cost: 0.10, Status: "success", CreatedAt: now},
		{UserID: 1, DisplayModel: "gpt", TotalTokens: 7, Cost: 0.05, Status: "error", CreatedAt: now},
		{UserID: 2, DisplayModel: "claude", TotalTokens: 20, Cost: 0.20, Status: "success", CreatedAt: now},
	}
	if err := InsertLogs(db, logs); err != nil {
		t.Fatal(err)
	}

	// UsageByDay 默认 7 天补齐。
	byDay, err := UsageByDay(db, 0)
	if err != nil || len(byDay) != 7 {
		t.Fatalf("usage by day: len=%d err=%v", len(byDay), err)
	}
	// 今天应该有数据，其余补零。
	var todayReq int64
	for _, p := range byDay {
		todayReq += p.Requests
	}
	if todayReq != 3 {
		t.Fatalf("expected 3 total requests across days, got %d", todayReq)
	}

	// UsageModelByDay。
	modelByDay, err := UsageModelByDay(db, 7)
	if err != nil {
		t.Fatalf("usage model by day: %v", err)
	}
	if len(modelByDay) < 2 {
		t.Fatalf("expected >=2 model-day rows, got %d", len(modelByDay))
	}

	// UsageSummaryRange：from=0 统计全部。
	s, err := UsageSummaryRange(db, 0)
	if err != nil {
		t.Fatalf("usage summary: %v", err)
	}
	if s.TotalRequests != 3 || s.Success != 2 || s.Errors != 1 || s.TotalTokens != 42 || s.PromptTokens != 10 || s.CompletionTokens != 5 {
		t.Fatalf("summary mismatch: %+v", s)
	}

	// from 过滤后的空库（未来时间）应返回全 0。
	s2, err := UsageSummaryRange(db, now+100000)
	if err != nil {
		t.Fatalf("usage summary empty: %v", err)
	}
	if s2.TotalRequests != 0 {
		t.Fatalf("expected empty summary, got %+v", s2)
	}

	// UsageByUserModel。
	byUser, err := UsageByUserModel(db, 0, 0)
	if err != nil {
		t.Fatalf("usage by user model: %v", err)
	}
	if len(byUser) != 2 { // 用户1×gpt 聚合为一行，用户2×claude 一行
		t.Fatalf("expected 2 user-model rows, got %d", len(byUser))
	}
	// 用户 1 x gpt 聚合。
	for _, m := range byUser {
		if m.UserID == 1 && m.Model == "gpt" {
			if m.Requests != 2 || m.TotalTokens != 22 {
				t.Fatalf("user-model agg mismatch: %+v", m)
			}
		}
	}
}

func TestChannelStatsTodayAndRecentFailures(t *testing.T) {
	db := setupDB(t)
	chanID := createChannelFor(t, db)
	now := models.Now()
	logs := []*models.RequestLog{
		{DisplayModel: "gpt", ChannelID: chanID, TotalTokens: 1, Status: "success", CreatedAt: now},
		{DisplayModel: "gpt", ChannelID: chanID, TotalTokens: 1, Status: "error", CreatedAt: now},
		{DisplayModel: "gpt", ChannelID: 0, TotalTokens: 1, Status: "error", CreatedAt: now - 10*86400}, // 旧日志不计入今日
	}
	if err := InsertLogs(db, logs); err != nil {
		t.Fatal(err)
	}

	stats, err := ChannelStatsToday(db)
	if err != nil {
		t.Fatalf("channel stats: %v", err)
	}
	if len(stats) != 1 || stats[0].ChannelID != chanID || stats[0].TodayReq != 2 || stats[0].TodayErr != 1 {
		t.Fatalf("channel stats mismatch: %+v", stats)
	}

	// RecentFailures 默认上限 10。
	fails, err := RecentFailures(db, 0)
	if err != nil {
		t.Fatalf("recent failures: %v", err)
	}
	if len(fails) != 2 {
		t.Fatalf("expected 2 failures, got %d", len(fails))
	}
	// limit>50 也回退。
	fails2, _ := RecentFailures(db, 100)
	if len(fails2) != 2 {
		t.Fatalf("expected 2 failures with big limit, got %d", len(fails2))
	}
}
