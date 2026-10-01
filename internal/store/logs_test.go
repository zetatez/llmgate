package store

import (
	"database/sql"
	"testing"

	"llmgate/internal/models"
)

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func createChannelFor(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	ch := &models.Channel{Name: "ch", BaseURL: "http://x", Adapter: "openai", Priority: 0, Weight: 1, TimeoutMS: 1000, Enabled: 1, HealthState: "healthy"}
	id, err := CreateChannel(db, ch)
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return id
}

func TestRouteExistsAndRemoveStale(t *testing.T) {
	db := setupDB(t)
	id := createChannelFor(t, db)

	add := func(disp, up string) {
		t.Helper()
		if _, err := CreateModelRoute(db, &models.ModelRoute{DisplayName: disp, ChannelID: id, UpstreamModel: up, Priority: 0, Weight: 1, Enabled: 1}); err != nil {
			t.Fatalf("create route: %v", err)
		}
	}
	add("deepseek-chat", "deepseek-chat") // 同名直通：应保留
	add("old-model", "old-model")         // 同名直通：应移除
	add("my-alias", "deepseek-chat")      // 手动别名：应保留

	if exists, _ := RouteExists(db, id, "deepseek-chat", "deepseek-chat"); !exists {
		t.Fatal("RouteExists should be true for same-name route")
	}
	if exists, _ := RouteExists(db, id, "my-alias", "deepseek-chat"); !exists {
		t.Fatal("RouteExists should be true for alias")
	}
	if exists, _ := RouteExists(db, id, "nope", "nope"); exists {
		t.Fatal("RouteExists should be false for missing")
	}

	removed, err := RemoveStaleRoutes(db, id, []string{"deepseek-chat", "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("RemoveStaleRoutes: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 stale route removed, got %d", removed)
	}

	names, err := ListModelRoutes(db)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range names {
		got[r.DisplayName] = true
	}
	if !got["deepseek-chat"] || !got["my-alias"] {
		t.Fatalf("valid/alias routes must remain, got %v", got)
	}
	if got["old-model"] {
		t.Fatal("stale route must be removed")
	}
}

func TestUsageByModel(t *testing.T) {
	db := setupDB(t)

	logs := []*models.RequestLog{
		{DisplayModel: "m-a", PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, Status: "success"},
		{DisplayModel: "m-a", PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6, Status: "success"},
		{DisplayModel: "m-b", PromptTokens: 3, CompletionTokens: 1, TotalTokens: 4, Status: "error"},
	}
	for _, l := range logs {
		l.CreatedAt = models.Now()
	}
	if err := InsertLogs(db, logs); err != nil {
		t.Fatalf("insert logs: %v", err)
	}

	rows, err := UsageByModel(db, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	byModel := map[string]ModelUsage{}
	for _, r := range rows {
		byModel[r.Model] = r
	}
	// m-a：requests=2, total=21
	if a, ok := byModel["m-a"]; !ok || a.Requests != 2 || a.TotalTokens != 21 {
		t.Fatalf("m-a aggregation wrong: %+v", a)
	}
	// m-b：成功 token 计 0
	if b, ok := byModel["m-b"]; !ok || b.TotalTokens != 0 {
		t.Fatalf("m-b error tokens should be 0: %+v", b)
	}
}

func TestDeleteOldLogs(t *testing.T) {
	db := setupDB(t)
	now := models.Now()
	logs := []*models.RequestLog{
		{DisplayModel: "m", TotalTokens: 1, Status: "success", CreatedAt: now - 40*86400}, // 40 天前
		{DisplayModel: "m", TotalTokens: 2, Status: "success", CreatedAt: now - 86400},    // 1 天前
	}
	if err := InsertLogs(db, logs); err != nil {
		t.Fatal(err)
	}
	n, err := DeleteOldLogs(db, now-30*86400)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 old log deleted, got %d", n)
	}
	list, err := QueryLogs(db, LogFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].CreatedAt != now-86400 {
		t.Fatalf("expected only recent log to remain, got %+v", list)
	}
}
