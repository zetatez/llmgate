package app

import (
	"testing"

	"llmgate/internal/config"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

func newApp(t *testing.T, cfg *config.Config) *App {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a, err := New(cfg, db)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	return a
}

func TestNewWithAdminTokenEnv(t *testing.T) {
	cfg := &config.Config{AdminToken: "admintok", EncryptSeed: ""}
	a := newApp(t, cfg)
	hash, err := a.GetSetting("admin_token_hash")
	if err != nil {
		t.Fatal(err)
	}
	if hash != HashToken("admintok") {
		t.Fatalf("admin token hash = %q", hash)
	}
	if !a.VerifyToken("admintok", hash) {
		t.Fatal("verify should pass")
	}
	if a.VerifyToken("wrong", hash) {
		t.Fatal("verify wrong token should fail")
	}
}

func TestNewGeneratesAndPersistsSeed(t *testing.T) {
	cfg := &config.Config{EncryptSeed: "abc"}
	a := newApp(t, cfg)
	// 种子为口令式 KDF，不落库
	legacy, _ := a.GetSetting("encrypt_seed")
	if legacy != "" {
		t.Fatalf("encrypt_seed must not be persisted, got %q", legacy)
	}
	// 加密器可用
	enc, _ := a.Secret.Encrypt("sk-x")
	if plain, err := a.Secret.Decrypt(enc); err != nil || plain != "sk-x" {
		t.Fatalf("secret roundtrip failed: %q %v", plain, err)
	}
}

func TestHashTokenAndSubtleCompare(t *testing.T) {
	h := HashToken("tok")
	if h == "tok" || len(h) != 64 {
		t.Fatalf("hash malformed: %q", h)
	}
	if HashToken("a") == HashToken("b") {
		t.Fatal("distinct tokens should not collide")
	}
	a := &App{}
	if a.VerifyToken("x", HashToken("y")) { // 长度不同 → false
		t.Fatal("length mismatch should be false")
	}
}

func TestSetGetSetting(t *testing.T) {
	a := newApp(t, &config.Config{})
	if err := a.SetSetting("pull_interval_min", "30"); err != nil {
		t.Fatal(err)
	}
	v, err := a.GetSetting("pull_interval_min")
	if err != nil || v != "30" {
		t.Fatalf("get setting: %q %v", v, err)
	}
	// 覆盖写入
	_ = a.SetSetting("pull_interval_min", "5")
	if v, _ := a.GetSetting("pull_interval_min"); v != "5" {
		t.Fatalf("upsert failed: %q", v)
	}
	// 不存在 → ""
	if v, _ := a.GetSetting("nope"); v != "" {
		t.Fatalf("missing setting should be empty, got %q", v)
	}
}

func TestFirstEnabledKey(t *testing.T) {
	a := newApp(t, &config.Config{})
	chID, err := store.CreateChannel(a.DB, &models.Channel{Name: "c", BaseURL: "http://x", Adapter: "openai", Weight: 1, TimeoutMS: 1000, Enabled: 1})
	if err != nil {
		t.Fatal(err)
	}
	enc1, _ := a.Secret.Encrypt("disabled-plain")
	enc2, _ := a.Secret.Encrypt("enabled-plain")
	if err := store.AddChannelKeys(a.DB, chID, []store.KeyInput{{Name: "d", Encrypted: enc1}, {Name: "e", Encrypted: enc2}}); err != nil {
		t.Fatal(err)
	}
	keys, _ := store.ListChannelKeys(a.DB, chID)
	// 禁用第一个 key → FirstEnabledKey 应落到第二个
	if err := store.UpdateChannelKey(a.DB, &models.ChannelKey{ID: keys[0].ID, Enabled: 0}); err != nil {
		t.Fatal(err)
	}
	k, err := a.FirstEnabledKey(chID)
	if err != nil || k != "enabled-plain" {
		t.Fatalf("FirstEnabledKey = %q, %v (want enabled-plain)", k, err)
	}
	// 无 key → 报错
	ch2, _ := store.CreateChannel(a.DB, &models.Channel{Name: "c2", BaseURL: "http://x", Adapter: "openai", Weight: 1, TimeoutMS: 1000, Enabled: 1})
	if _, err := a.FirstEnabledKey(ch2); err == nil {
		t.Fatal("channel without keys should error")
	}
}

func TestKeyTailAndTokenTail(t *testing.T) {
	a := newApp(t, &config.Config{})
	enc, _ := a.Secret.Encrypt("sk-abcdefgh1234")
	if got := a.KeyTail(enc); got != "1234" {
		t.Fatalf("KeyTail = %q want 1234", got)
	}
	// 解密失败 / 空 → ""
	if got := a.KeyTail("zzz-not-hex"); got != "" {
		t.Fatalf("KeyTail(bad) = %q want empty", got)
	}
	if got := a.KeyTail(""); got != "" {
		t.Fatalf("KeyTail(empty) = %q", got)
	}

	if got := a.TokenTail(nil); got != "" {
		t.Fatalf("TokenTail(nil) = %q want empty", got)
	}
	u := &models.User{TokenEnc: enc}
	if got := a.TokenTail(u); got != "1234" {
		t.Fatalf("TokenTail = %q want 1234", got)
	}
	u2 := &models.User{TokenEnc: ""}
	if got := a.TokenTail(u2); got != "" {
		t.Fatalf("TokenTail(empty enc) = %q", got)
	}
}

func TestGenSecret(t *testing.T) {
	s1, err := GenSecret(32)
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := GenSecret(32)
	if len(s1) != 64 || s1 == s2 {
		t.Fatalf("GenSecret malformed: len=%d unique=%v", len(s1), s1 != s2)
	}
}
