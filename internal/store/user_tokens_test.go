package store

import (
	"testing"

	"llmgate/internal/models"
)

func TestUserTokensCRUDAndResolve(t *testing.T) {
	db := setupDB(t)
	uid, err := CreateUser(db, &models.User{Name: "u", Username: "u", TokenHash: "placeholder", Status: 1})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if list, _ := ListUserTokens(db, uid); len(list) != 0 {
		t.Fatalf("expected no tokens, got %d", len(list))
	}

	id1, err := CreateUserToken(db, &models.UserToken{UserID: uid, Name: "a", TokenHash: "h1", TokenEnc: "e1", Enabled: 1})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if _, err := CreateUserToken(db, &models.UserToken{UserID: uid, Name: "b", TokenHash: "h2", Enabled: 1}); err != nil {
		t.Fatalf("create token 2: %v", err)
	}

	// 列表按 id 倒序
	list, err := ListUserTokens(db, uid)
	if err != nil || len(list) != 2 {
		t.Fatalf("list tokens: %+v err %v", list, err)
	}
	if list[0].Name != "b" {
		t.Fatalf("expected newest first, got %+v", list)
	}

	// 解析令牌 → 用户
	u, tok, err := ResolveUserToken(db, "h1")
	if err != nil || u == nil || tok == nil || u.ID != uid || tok.ID != id1 {
		t.Fatalf("resolve: user=%+v token=%+v err=%v", u, tok, err)
	}
	// 未知令牌
	if u, tok, _ := ResolveUserToken(db, "nope"); u != nil || tok != nil {
		t.Fatalf("unknown token should resolve nil, got %+v %+v", u, tok)
	}
	// 禁用令牌 → user 为 nil，token 非 nil（便于区分原因）
	_ = UpdateUserToken(db, &models.UserToken{ID: id1, Name: "a", Enabled: 0})
	if u, tok, _ := ResolveUserToken(db, "h1"); u != nil || tok == nil {
		t.Fatalf("disabled token should not resolve user, got %+v %+v", u, tok)
	}
	_ = UpdateUserToken(db, &models.UserToken{ID: id1, Name: "a", Enabled: 1})

	// 禁用用户 → 不解析
	_ = UpdateUser(db, &models.User{ID: uid, Username: "u", Status: 0, TokenHash: "placeholder"})
	if u, _, _ := ResolveUserToken(db, "h1"); u != nil {
		t.Fatalf("disabled user token should not resolve, got %+v", u)
	}
	_ = UpdateUser(db, &models.User{ID: uid, Username: "u", Status: 1, TokenHash: "placeholder"})

	// Touch / Delete / 不存在
	if err := TouchUserToken(db, id1); err != nil {
		t.Fatalf("touch: %v", err)
	}
	if err := DeleteUserToken(db, id1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := DeleteUserToken(db, id1); err == nil {
		t.Fatal("expected error deleting missing token")
	}
	if got, _ := GetUserToken(db, id1); got != nil {
		t.Fatal("deleted token should be gone")
	}
}
