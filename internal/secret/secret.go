// Package secret 提供 API Key 及用户令牌的 AES-256-GCM 加解密。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

// Manager 持有 AEAD 实例，负责加解密。
type Manager struct {
	aead cipher.AEAD
}

// New 从 seed 创建 Manager；seed 为空时自动生成随机种子。
// seed 支持两种形式：
//   - 32 字节 hex：直接作为 AES-256 密钥（兼容既有密文，不改变行为）
//   - 其它任意字符串：视为口令，经 SHA-256 派生 32 字节密钥（KDF）
//
// 返回 (Manager, 用于持久化的种子 hex)。调用方**不应把种子写入数据库**，
// 否则"库失即全失"——建议由环境变量提供并在重启间保持一致。
func New(seed string) (*Manager, string, error) {
	var key []byte
	var shown string
	if seed == "" {
		b := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, b); err != nil {
			return nil, "", err
		}
		key = b
		shown = hex.EncodeToString(b)
	} else if b, err := hex.DecodeString(seed); err == nil && len(b) == 32 {
		// 32 字节 hex：raw 密钥（兼容既有数据）
		key = b
		shown = seed
	} else {
		// 任意口令：SHA-256 派生（一次性 KDF）
		sum := sha256.Sum256([]byte(seed))
		key = sum[:]
		shown = ""
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, "", err
	}
	return &Manager{aead: aead}, shown, nil
}

// Encrypt 加密明文，返回 hex(nonce || ciphertext)。
func (m *Manager) Encrypt(plain string) (string, error) {
	nonce := make([]byte, m.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := m.aead.Seal(nonce, nonce, []byte(plain), nil)
	return hex.EncodeToString(out), nil
}

// Decrypt 解密 Encrypt 的产物。
func (m *Manager) Decrypt(enc string) (string, error) {
	raw, err := hex.DecodeString(enc)
	if err != nil {
		return "", err
	}
	ns := m.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	plain, err := m.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", errors.New("decrypt failed (seed 不匹配?)")
	}
	return string(plain), nil
}
