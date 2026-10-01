package secret

import "testing"

func TestRoundTrip(t *testing.T) {
	m, _, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	enc, err := m.Encrypt("sk-abc123")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := m.Decrypt(enc)
	if err != nil || plain != "sk-abc123" {
		t.Fatalf("roundtrip failed: %q %v", plain, err)
	}
	if enc == "sk-abc123" {
		t.Fatal("ciphertext must not equal plaintext")
	}
}

func TestWrongSeedFails(t *testing.T) {
	m1, seed1, _ := New("") // 随机种子
	m2, _, err := New(seed1)
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := m1.Encrypt("secret")
	// 用相同种子解密应成功
	if _, err := m2.Decrypt(enc); err != nil {
		t.Fatalf("same seed decrypt failed: %v", err)
	}
	m3, other, _ := New("") // 另一个随机种子
	if other == seed1 {
		t.Fatal("random seeds should differ")
	}
	if _, err := m3.Decrypt(enc); err == nil {
		t.Fatal("decrypt with wrong seed should fail")
	}
}

func TestCustomSeedPersisted(t *testing.T) {
	seed := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	m, persisted, err := New(seed)
	if err != nil {
		t.Fatal(err)
	}
	if persisted != seed {
		t.Fatalf("persisted seed mismatch: %s", persisted)
	}
	enc, _ := m.Encrypt("x")
	m2, _, _ := New(seed)
	if p, err := m2.Decrypt(enc); err != nil || p != "x" {
		t.Fatalf("recreated manager decrypt failed: %q %v", p, err)
	}
}
