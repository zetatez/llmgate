package secret

import "testing"

// 口令式种子：任意字符串经 SHA-256 派生（KDF），持久化种子应为空。
func TestPassphraseSeedKDF(t *testing.T) {
	m, shown, err := New("my-secret-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if shown != "" {
		t.Fatalf("passphrase seed should return empty persisted seed, got %q", shown)
	}
	enc, err := m.Encrypt("abc")
	if err != nil {
		t.Fatal(err)
	}
	// 相同口令派生出一致密钥，能解密
	m2, _, _ := New("my-secret-passphrase")
	if p, err := m2.Decrypt(enc); err != nil || p != "abc" {
		t.Fatalf("same passphrase decrypt failed: %q %v", p, err)
	}
	// 不同口令应解密失败
	m3, _, _ := New("other-passphrase")
	if _, err := m3.Decrypt(enc); err == nil {
		t.Fatal("different passphrase must fail to decrypt")
	}
}

func TestNewInvalidHexFallbackToKDF(t *testing.T) {
	// 非 32 字节 hex → 按口令处理（不报错），persisted 为空
	if _, shown, err := New("00112233"); err != nil || shown != "" {
		t.Fatalf("short hex should be treated as passphrase, got err=%v shown=%q", err, shown)
	}
}

func TestDecryptInvalidInputs(t *testing.T) {
	m, _, _ := New("")
	// 非法 hex
	if _, err := m.Decrypt("zzz"); err == nil {
		t.Fatal("invalid hex should error")
	}
	// 太短的密文
	if _, err := m.Decrypt("abcdef01"); err == nil {
		t.Fatal("short ciphertext should error")
	}
}
