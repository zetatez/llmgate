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

	u := &models.User{Name: "u1", Remark: "r", TokenHash: "h1", TokenEnc: "e1", Status: 1}
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
	if got.Name != "u1" || got.TokenHash != "h1" || got.TokenEnc != "e1" {
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
	got.Status = 0
	if err := UpdateUser(db, got); err != nil {
		t.Fatalf("update user: %v", err)
	}
	if err := UpdateUser(db, &models.User{ID: 99999}); err == nil {
		t.Fatal("expected error updating missing user")
	}
	up, _ := GetUser(db, id)
	if up.Name != "u1-upd" || up.Status != 0 {
		t.Fatalf("updated user mismatch: %+v", up)
	}

	// 重置令牌（含过期时间）。
	if err := SetUserToken(db, id, "h2", "e2", 12345); err != nil {
		t.Fatalf("set user token: %v", err)
	}
	if err := SetUserToken(db, 99999, "h", "e", 0); err == nil {
		t.Fatal("expected error setting token on missing user")
	}
	ut, _ := GetUser(db, id)
	if ut.TokenHash != "h2" || ut.TokenEnc != "e2" || ut.TokenExpiresAt != 12345 {
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

func TestApplyUsage(t *testing.T) {
	db := setupDB(t)

	consumer := &models.User{Name: "consumer", TokenHash: "h-consumer", Status: 1}
	consumerID, err := CreateUser(db, consumer)
	if err != nil {
		t.Fatal(err)
	}
	owner := &models.User{Name: "owner", TokenHash: "h-owner", Status: 1}
	ownerID, err := CreateUser(db, owner)
	if err != nil {
		t.Fatal(err)
	}

	if err := ApplyUsage(db, consumerID, ownerID, 100, 20); err != nil {
		t.Fatalf("apply usage: %v", err)
	}
	got, _ := GetUser(db, consumerID)
	if got.CallsSpent != 1 || got.InSpent != 100 || got.OutSpent != 20 {
		t.Fatalf("consumer billing = %+v", got)
	}
	gotOwner, _ := GetUser(db, ownerID)
	if gotOwner.CallsEarned != 1 || gotOwner.InServed != 100 || gotOwner.OutServed != 20 {
		t.Fatalf("owner billing = %+v", gotOwner)
	}

	// 自用：两侧相抵，但计数仍各 +1（用于展示）。
	if err := ApplyUsage(db, consumerID, consumerID, 5, 5); err != nil {
		t.Fatalf("apply self usage: %v", err)
	}
	got, _ = GetUser(db, consumerID)
	if got.CallsSpent != 2 || got.CallsEarned != 1 || got.InSpent != 105 || got.InServed != 5 {
		t.Fatalf("self usage billing = %+v", got)
	}

	// 平台 Key（owner=0）只累计消费者。
	if err := ApplyUsage(db, consumerID, 0, 1, 2); err != nil {
		t.Fatalf("apply platform usage: %v", err)
	}
	got, _ = GetUser(db, consumerID)
	if got.CallsSpent != 3 || got.InSpent != 106 || got.OutSpent != 27 {
		t.Fatalf("platform usage billing = %+v", got)
	}
}
