package store

import (
	"testing"

	"llmgate/internal/models"
)

func TestListKeysByOwner(t *testing.T) {
	db := setupDB(t)
	ch1, err := CreateChannel(db, &models.Channel{Name: "c1", BaseURL: "http://a", Adapter: "openai", Enabled: 1, HealthState: "healthy"})
	if err != nil {
		t.Fatal(err)
	}
	ch2, err := CreateChannel(db, &models.Channel{Name: "c2", BaseURL: "http://b", Adapter: "openai", Enabled: 1, HealthState: "healthy"})
	if err != nil {
		t.Fatal(err)
	}

	if err := AddChannelKeys(db, ch1, []KeyInput{
		{UserID: 7, Name: "a1", Encrypted: "e"},
		{UserID: 8, Name: "b1", Encrypted: "e"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AddChannelKeys(db, ch2, []KeyInput{{UserID: 7, Name: "a2", Encrypted: "e"}}); err != nil {
		t.Fatal(err)
	}

	// 按渠道 + 归属过滤
	got, err := ListChannelKeysByUser(db, ch1, 7)
	if err != nil || len(got) != 1 || got[0].Name != "a1" || got[0].UserID != 7 {
		t.Fatalf("ListChannelKeysByUser(7) = %+v err %v", got, err)
	}
	// 跨渠道
	all, err := ListKeysByUser(db, 7)
	if err != nil || len(all) != 2 {
		t.Fatalf("ListKeysByUser(7) = %+v err %v", all, err)
	}
	if none, _ := ListKeysByUser(db, 99); len(none) != 0 {
		t.Fatalf("expected no keys for unknown user, got %+v", none)
	}
}

func TestGetUserByUsernameAndTouchLogin(t *testing.T) {
	db := setupDB(t)
	id, err := CreateUser(db, &models.User{Name: "A", Username: "alice", PasswordHash: "h", TokenHash: "ph", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	u, err := GetUserByUsername(db, "alice")
	if err != nil || u == nil || u.ID != id {
		t.Fatalf("GetUserByUsername = %+v err %v", u, err)
	}
	if u, _ := GetUserByUsername(db, "ghost"); u != nil {
		t.Fatalf("unknown username should be nil, got %+v", u)
	}
	if u, _ := GetUserByUsername(db, ""); u != nil {
		t.Fatalf("empty username should be nil, got %+v", u)
	}

	if err := TouchUserLogin(db, id); err != nil {
		t.Fatal(err)
	}
	u, _ = GetUser(db, id)
	if u.LastLoginAt == 0 {
		t.Fatal("last_login_at should be set")
	}
}

func TestUserUsageAggregates(t *testing.T) {
	db := setupDB(t)
	if err := InsertLogs(db, []*models.RequestLog{
		{UserID: 1, DisplayModel: "m1", Status: "success", TotalTokens: 100, PromptTokens: 80, CompletionTokens: 20, Cost: 1.5, CreatedAt: models.Now()},
		{UserID: 1, DisplayModel: "m2", Status: "error", TotalTokens: 0, Cost: 0, CreatedAt: models.Now()},
		{UserID: 2, DisplayModel: "m1", Status: "success", TotalTokens: 50, Cost: 0.5, CreatedAt: models.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	week, err := UserUsageByDay(db, 1, 7)
	if err != nil || len(week) != 7 {
		t.Fatalf("UserUsageByDay = %+v err %v", week, err)
	}
	today := week[len(week)-1]
	if today.Requests != 2 || today.Success != 1 || today.Errors != 1 || today.TotalTokens != 100 {
		t.Fatalf("today bucket = %+v", today)
	}

	sum, err := UserUsageSummary(db, 1, 0)
	if err != nil || sum.TotalRequests != 2 || sum.TotalTokens != 100 {
		t.Fatalf("summary = %+v err %v", sum, err)
	}
	// 只统计该用户
	sum2, _ := UserUsageSummary(db, 2, 0)
	if sum2.TotalRequests != 1 || sum2.TotalTokens != 50 {
		t.Fatalf("summary user2 = %+v", sum2)
	}

	bym, err := UserUsageByModel(db, 1, 0, 0)
	if err != nil || len(bym) != 2 {
		t.Fatalf("UserUsageByModel = %+v err %v", bym, err)
	}
	if bym[0].Model != "m1" { // tokens 降序，m1 在前
		t.Fatalf("expected m1 first, got %+v", bym)
	}
}

func TestUsageByHour(t *testing.T) {
	db := setupDB(t)
	if err := InsertLogs(db, []*models.RequestLog{
		{UserID: 1, DisplayModel: "m", Status: "success", TotalTokens: 10, Cost: 0.5, CreatedAt: models.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	pts, err := UsageByHour(db, 24)
	if err != nil || len(pts) != 24 {
		t.Fatalf("UsageByHour = %d pts, err %v", len(pts), err)
	}
	last := pts[len(pts)-1]
	if last.Requests != 1 || last.Tokens != 10 || last.Cost != 0.5 {
		t.Fatalf("last hour bucket = %+v", last)
	}
	for i, p := range pts {
		if p.Hour == "" {
			t.Fatalf("bucket %d missing hour label", i)
		}
	}
}
