package store

import (
	"testing"

	"llmgate/internal/models"
)

func TestModelRouteCRUD(t *testing.T) {
	db := setupDB(t)
	chanID := createChannelFor(t, db)

	if list, _ := ListModelRoutes(db); len(list) != 0 {
		t.Fatalf("expected no routes, got %d", len(list))
	}

	pi := 0.5
	po := 1.5
	r := &models.ModelRoute{DisplayName: "gpt-4o", ChannelID: chanID, UpstreamModel: "gpt-4o", Priority: 1, Weight: 2, Enabled: 1, PriceInput: &pi, PriceOutput: &po}
	id, err := CreateModelRoute(db, r)
	if err != nil {
		t.Fatalf("create route: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero route id")
	}

	got, err := GetModelRoute(db, id)
	if err != nil || got == nil {
		t.Fatalf("get route: %+v err %v", got, err)
	}
	if got.DisplayName != "gpt-4o" || got.PriceInput == nil || *got.PriceInput != 0.5 || got.PriceOutput == nil || *got.PriceOutput != 1.5 {
		t.Fatalf("route mismatch: %+v", got)
	}
	if m, _ := GetModelRoute(db, 99999); m != nil {
		t.Fatal("get missing route should be nil")
	}

	// 更新路由。
	got.Priority = 3
	got.Enabled = 0
	if err := UpdateModelRoute(db, got); err != nil {
		t.Fatalf("update route: %v", err)
	}
	if err := UpdateModelRoute(db, &models.ModelRoute{ID: 99999}); err == nil {
		t.Fatal("expected error updating missing route")
	}
	up, _ := GetModelRoute(db, id)
	if up.Priority != 3 || up.Enabled != 0 {
		t.Fatalf("updated route mismatch: %+v", up)
	}

	// ListRoutesForModel 过滤。
	alias := &models.ModelRoute{DisplayName: "gpt-4o", ChannelID: createChannelFor(t, db), UpstreamModel: "v1/gpt-4o", Priority: 0, Weight: 1, Enabled: 1}
	if _, err := CreateModelRoute(db, alias); err != nil {
		t.Fatal(err)
	}
	other := &models.ModelRoute{DisplayName: "claude", ChannelID: chanID, UpstreamModel: "claude", Priority: 0, Weight: 1, Enabled: 1}
	if _, err := CreateModelRoute(db, other); err != nil {
		t.Fatal(err)
	}

	forModel, err := ListRoutesForModel(db, "gpt-4o")
	if err != nil {
		t.Fatalf("list routes for model: %v", err)
	}
	if len(forModel) != 2 {
		t.Fatalf("expected 2 gpt-4o routes, got %d", len(forModel))
	}

	if err := DeleteModelRoute(db, id); err != nil {
		t.Fatalf("delete route: %v", err)
	}
	if err := DeleteModelRoute(db, id); err == nil {
		t.Fatal("expected ErrNotFound on double delete")
	}
}

func TestDeleteAutoBare(t *testing.T) {
	db := setupDB(t)
	chanID := createChannelFor(t, db)

	add := func(disp, up string) {
		if _, err := CreateModelRoute(db, &models.ModelRoute{DisplayName: disp, ChannelID: chanID, UpstreamModel: up, Enabled: 1}); err != nil {
			t.Fatal(err)
		}
	}
	add("deepseek-chat", "deepseek-chat") // 纯裸名，应删
	add("deepseek/chat", "deepseek-chat") // 含 '/'，不应由 DeleteAutoBare 删
	add("my-alias", "deepseek-chat")      // 手动别名，不应删
	add("other-model", "other-model")     // 同渠道另一模型裸名，不该被删（upstream 不匹配）

	n, err := DeleteAutoBare(db, chanID, "deepseek-chat")
	if err != nil {
		t.Fatalf("delete auto bare: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 deleted, got %d", n)
	}

	left, _ := ListModelRoutes(db)
	if len(left) != 3 {
		t.Fatalf("expected 3 remaining routes, got %d", len(left))
	}
}

func TestProbeRouteModel(t *testing.T) {
	db := setupDB(t)
	chanID := createChannelFor(t, db)

	// 无路由返回空串。
	m, err := ProbeRouteModel(db, chanID)
	if err != nil || m != "" {
		t.Fatalf("expected empty probe, got %q err %v", m, err)
	}

	// 创建两个启用路由，应返回优先级最高的 upstream。
	if _, err := CreateModelRoute(db, &models.ModelRoute{DisplayName: "a", ChannelID: chanID, UpstreamModel: "up-a", Priority: 1, Enabled: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateModelRoute(db, &models.ModelRoute{DisplayName: "b", ChannelID: chanID, UpstreamModel: "up-b", Priority: 0, Enabled: 1}); err != nil {
		t.Fatal(err)
	}
	m, err = ProbeRouteModel(db, chanID)
	if err != nil || m != "up-b" {
		t.Fatalf("expected up-b (higher priority), got %q err %v", m, err)
	}

	// 禁用所有启用路由后返回空串。
	if err := UpdateModelRoute(db, &models.ModelRoute{ID: 2, DisplayName: "b", ChannelID: chanID, UpstreamModel: "up-b", Enabled: 0}); err != nil {
		t.Fatal(err)
	}
	m, _ = ProbeRouteModel(db, chanID)
	if m != "up-a" { // id=2 即 up-b 被禁用，up-a 成为首个启用路由
		t.Fatalf("expected up-a after disabling up-b, got %q", m)
	}
}

func TestRemoveStaleRoutesEmpty(t *testing.T) {
	db := setupDB(t)
	chanID := createChannelFor(t, db)
	n, err := RemoveStaleRoutes(db, chanID, nil)
	if err != nil || n != 0 {
		t.Fatalf("empty valid list should be no-op: n=%d err=%v", n, err)
	}
}
