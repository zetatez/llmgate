package store

import (
	"database/sql"
	"testing"

	"llmgate/internal/models"
)

func TestChannelCRUD(t *testing.T) {
	db := setupDB(t)

	// 空库 ListChannels 应返回空切片而非 nil/错误。
	empty, err := ListChannels(db)
	if err != nil {
		t.Fatalf("list empty channels: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no channels, got %d", len(empty))
	}

	ch := &models.Channel{Name: "c1", BaseURL: "http://c1", Adapter: "openai", Priority: 2, Weight: 5, TimeoutMS: 3000, Enabled: 1, HealthState: "healthy"}
	id, err := CreateChannel(db, ch)
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	// CreateChannel 使用 now() 填充时间字段。
	got, err := GetChannel(db, id)
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if got == nil || got.Name != "c1" || got.BaseURL != "http://c1" || got.TimeoutMS != 3000 {
		t.Fatalf("channel mismatch: %+v", got)
	}
	if got.CreatedAt == 0 || got.UpdatedAt == 0 {
		t.Fatalf("timestamps should be set: %+v", got)
	}

	// 更新。
	got.Name = "c1-renamed"
	got.Priority = 1
	got.Weight = 9
	got.Enabled = 0
	got.ExtraHeaders = `{"x-test":"1"}`
	if err := UpdateChannel(db, got); err != nil {
		t.Fatalf("update channel: %v", err)
	}
	updated, _ := GetChannel(db, id)
	if updated.Name != "c1-renamed" || updated.Priority != 1 || updated.Weight != 9 || updated.Enabled != 0 || updated.ExtraHeaders != `{"x-test":"1"}` {
		t.Fatalf("updated channel mismatch: %+v", updated)
	}

	// UpdateChannel 对不存在的 id 应报错。
	if err := UpdateChannel(db, &models.Channel{ID: 99999, Name: "nope"}); err == nil {
		t.Fatal("expected error updating missing channel")
	}

	// GetChannel 不存在返回 nil,nil。
	missing, err := GetChannel(db, 99999)
	if err != nil || missing != nil {
		t.Fatalf("get missing channel: got %+v err %v", missing, err)
	}

	// 删除并二次删除应报 ErrNotFound。
	if err := DeleteChannel(db, id); err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	if err := DeleteChannel(db, id); err == nil {
		t.Fatal("expected ErrNotFound on double delete")
	}
}

func TestChannelHealthAndCooldown(t *testing.T) {
	db := setupDB(t)
	id := createChannelFor(t, db)

	now := models.Now()
	if err := SetChannelCooldown(db, id, now+3600); err != nil {
		t.Fatalf("set cooldown: %v", err)
	}
	g := getChannelOrFail(t, db, id)
	if g.HealthState != "cooldown" || g.CooldownUntil != now+3600 {
		t.Fatalf("cooldown not set: %+v", g)
	}

	// 冷却中但未到期：ListCooldownChannels 不应返回。
	if list, _ := ListCooldownChannels(db, now); len(list) != 0 {
		t.Fatalf("expected no due cooldown channels, got %d", len(list))
	}

	// 到期后应返回。
	if list, _ := ListCooldownChannels(db, now+3600); len(list) != 1 {
		t.Fatalf("expected 1 due cooldown channel, got %d", len(list))
	}

	if err := SetChannelHealthy(db, id); err != nil {
		t.Fatalf("set healthy: %v", err)
	}
	g = getChannelOrFail(t, db, id)
	if g.HealthState != "healthy" || g.CooldownUntil != 0 {
		t.Fatalf("healthy not restored: %+v", g)
	}
}

func getChannelOrFail(t *testing.T, db *sql.DB, id int64) *models.Channel {
	t.Helper()
	g, err := GetChannel(db, id)
	if err != nil || g == nil {
		t.Fatalf("get channel %d: %v", id, err)
	}
	return g
}

func TestChannelKeys(t *testing.T) {
	db := setupDB(t)
	chanID := createChannelFor(t, db)

	// 空列表。
	if list, _ := ListChannelKeys(db, chanID); len(list) != 0 {
		t.Fatalf("expected no keys, got %d", len(list))
	}

	// 批量添加。
	items := []KeyInput{{Name: "k1", Source: "官网", Remark: "a", Encrypted: "ENC1"}}
	if err := AddChannelKeys(db, chanID, items); err != nil {
		t.Fatalf("add channel keys: %v", err)
	}
	items2 := []KeyInput{{Name: "k2", Encrypted: "ENC2"}}
	if err := AddChannelKeys(db, chanID, items2); err != nil {
		t.Fatalf("add channel keys 2: %v", err)
	}

	// 空输入应直接返回 nil。
	if err := AddChannelKeys(db, chanID, nil); err != nil {
		t.Fatalf("add empty keys should be no-op: %v", err)
	}

	list, err := ListChannelKeys(db, chanID)
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(list))
	}
	first := list[0]
	if first.APIKeyEnc != "ENC1" || first.Name != "k1" || first.Source != "官网" || first.Remark != "a" ||
		first.Enabled != 1 || first.Weight != 1 {
		t.Fatalf("key1 mismatch: %+v", first)
	}

	// GetChannelKey 命中与未命中。
	k, err := GetChannelKey(db, first.ID)
	if err != nil || k == nil || k.APIKeyEnc != "ENC1" {
		t.Fatalf("get key: %+v err %v", k, err)
	}
	if km, _ := GetChannelKey(db, 99999); km != nil {
		t.Fatal("get missing key should be nil")
	}

	// 更新。
	first.Name = "k1-upd"
	first.Enabled = 0
	first.Weight = 3
	if err := UpdateChannelKey(db, first); err != nil {
		t.Fatalf("update key: %v", err)
	}
	if err := UpdateChannelKey(db, &models.ChannelKey{ID: 99999}); err == nil {
		t.Fatal("expected error on update missing key")
	}
	u, _ := GetChannelKey(db, first.ID)
	if u.Enabled != 0 || u.Weight != 3 || u.Name != "k1-upd" {
		t.Fatalf("updated key mismatch: %+v", u)
	}

	// 更换密文。
	if err := UpdateChannelKeySecret(db, first.ID, "ENC-NEW"); err != nil {
		t.Fatalf("update key secret: %v", err)
	}
	if err := UpdateChannelKeySecret(db, 99999, "x"); err == nil {
		t.Fatal("expected error on updating missing key secret")
	}
	u2, _ := GetChannelKey(db, first.ID)
	if u2.APIKeyEnc != "ENC-NEW" {
		t.Fatalf("secret not updated: %+v", u2)
	}

	// 触碰最近使用时间。
	if err := TouchChannelKey(db, first.ID); err != nil {
		t.Fatalf("touch key: %v", err)
	}
	u3, _ := GetChannelKey(db, first.ID)
	if u3.LastUsedAt == 0 {
		t.Fatal("last_used_at should be set")
	}

	// 删除 Key 与二次删除。
	if err := DeleteChannelKey(db, first.ID); err != nil {
		t.Fatalf("delete key: %v", err)
	}
	if err := DeleteChannelKey(db, first.ID); err == nil {
		t.Fatal("expected error deleting missing key")
	}
	if left, _ := ListChannelKeys(db, chanID); len(left) != 1 {
		t.Fatalf("expected 1 key left, got %d", len(left))
	}
}

func TestChannelKeyCascadeOnChannelDelete(t *testing.T) {
	db := setupDB(t)
	chanID := createChannelFor(t, db)
	if err := AddChannelKeys(db, chanID, []KeyInput{{Encrypted: "E", Name: "k"}}); err != nil {
		t.Fatal(err)
	}
	// 删除渠道应级联删除其 key。
	if err := DeleteChannel(db, chanID); err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	if list, _ := ListChannelKeys(db, chanID); len(list) != 0 {
		t.Fatalf("keys should cascade-delete, got %d", len(list))
	}
}
