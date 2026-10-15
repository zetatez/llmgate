package store

import (
	"testing"

	"llmgate/internal/models"
)

func TestSetUserPassword(t *testing.T) {
	db := setupDB(t)
	id, err := CreateUser(db, &models.User{Name: "u", Username: "u", PasswordHash: "old-hash", TokenHash: "ph", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := SetUserPassword(db, id, "new-hash"); err != nil {
		t.Fatalf("set password: %v", err)
	}
	u, err := GetUser(db, id)
	if err != nil || u == nil || u.PasswordHash != "new-hash" {
		t.Fatalf("password not updated: %+v err %v", u, err)
	}
	if err := SetUserPassword(db, 99999, "x"); err == nil {
		t.Fatal("expected error for missing user")
	}
}
