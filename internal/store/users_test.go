package store

import (
	"testing"

	"llmgate/internal/models"
)

func TestUserCRUD(t *testing.T) {
	db := setupDB(t)

	// 空库。
	if list, _ := ListUsers(db); len(list) != 0 {
		t.Fatalf("expected no users, got %d", len(list))
	}

	u := &models.User{Name: "u1", Remark: "r", TokenHash: "h1", TokenEnc: "e1", QuotaLimit: 100, Status: 1}
	id, err := CreateUser(db, u)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero user id")
	}

	got, err := GetUser(db, id)
	if err != nil || got == nil {
		t.Fatalf("get user: %+v err %v", got, err)
	}
	if got.Name != "u1" || got.QuotaLimit != 100 || got.QuotaUsed != 0 || got.TokenHash != "h1" || got.TokenEnc != "e1" {
		t.Fatalf("user mismatch: %+v", got)
	}

	// 不存在。
	if m, _ := GetUser(db, 99999); m != nil {
		t.Fatal("get missing user should be nil")
	}

	// 按 token_hash 查询。
	byHash, err := GetUserByTokenHash(db, "h1")
	if err != nil || byHash == nil || byHash.ID != id {
		t.Fatalf("get by hash: %+v err %v", byHash, err)
	}
	if m, _ := GetUserByTokenHash(db, "missing"); m != nil {
		t.Fatal("get by missing hash should be nil")
	}
	// token_hash 唯一，重复创建同 hash 应报错。
	if _, err := CreateUser(db, &models.User{Name: "dup", TokenHash: "h1"}); err == nil {
		t.Fatal("expected unique constraint error on duplicate token_hash")
	}

	// 更新。
	got.Name = "u1-upd"
	got.QuotaLimit = 200
	got.Status = 0
	if err := UpdateUser(db, got); err != nil {
		t.Fatalf("update user: %v", err)
	}
	if err := UpdateUser(db, &models.User{ID: 99999}); err == nil {
		t.Fatal("expected error updating missing user")
	}
	up, _ := GetUser(db, id)
	if up.Name != "u1-upd" || up.QuotaLimit != 200 || up.Status != 0 {
		t.Fatalf("updated user mismatch: %+v", up)
	}

	// 重置令牌。
	if err := SetUserToken(db, id, "h2", "e2"); err != nil {
		t.Fatalf("set user token: %v", err)
	}
	if err := SetUserToken(db, 99999, "h", "e"); err == nil {
		t.Fatal("expected error setting token on missing user")
	}
	ut, _ := GetUser(db, id)
	if ut.TokenHash != "h2" || ut.TokenEnc != "e2" {
		t.Fatalf("token not reset: %+v", ut)
	}

	// 删除与二次删除。
	if err := DeleteUser(db, id); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if err := DeleteUser(db, id); err == nil {
		t.Fatal("expected error on double delete")
	}
}

func TestAddUserQuota(t *testing.T) {
	db := setupDB(t)

	// 无上限用户（limit=0）。
	u0 := &models.User{Name: "free", TokenHash: "h-free", QuotaLimit: 0}
	id0, err := CreateUser(db, u0)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddUserQuota(db, id0, 10.5); err != nil {
		t.Fatalf("add quota free: %v", err)
	}
	gu, _ := GetUser(db, id0)
	if gu.QuotaUsed != 10.5 {
		t.Fatalf("quota_used should be 10.5, got %v", gu.QuotaUsed)
	}

	// 有上限用户：可累加直到达到上限。
	u1 := &models.User{Name: "cap", TokenHash: "h-cap", QuotaLimit: 100}
	id1, err := CreateUser(db, u1)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddUserQuota(db, id1, 60); err != nil {
		t.Fatalf("add quota cap: %v", err)
	}
	if err := AddUserQuota(db, id1, 50); err != nil {
		t.Fatalf("add quota over cap: %v", err)
	}
	gc, _ := GetUser(db, id1)
	// 第二次 60+50>100，仅累加 60（60+40<=100 不成立），故为 60。
	if gc.QuotaUsed != 60 {
		t.Fatalf("expected capped quota 60, got %v", gc.QuotaUsed)
	}

	// 不存在用户。
	if err := AddUserQuota(db, 99999, 1); err == nil {
		t.Fatal("expected error adding quota for missing user")
	}
}
