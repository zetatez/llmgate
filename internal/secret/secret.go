// Package secret 提供 API Key 的 AES-256-GCM 加解密。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

// Manager 持有 AEAD 实例，负责加解密。
type Manager struct {
	aead cipher.AEAD
}

// New 从 32 字节 hex 种子创建 Manager；种子为空时自动生成随机种子。
// 返回的实际种子 hex（应与输入一致），调用方将其持久化（settings 表）以便重启后仍能解密。
func New(seedHex string) (*Manager, string, error) {
	var seed []byte
	if seedHex != "" {
		b, err := hex.DecodeString(seedHex)
		if err != nil {
			return nil, "", errors.New("encrypt seed 必须是 hex 编码")
		}
		if len(b) != 32 {
			return nil, "", errors.New("encrypt seed 必须是 32 字节")
		}
		seed = b
	} else {
		seed = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, seed); err != nil {
			return nil, "", err
		}
	}
	block, err := aes.NewCipher(seed)
	if err != nil {
		return nil, "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, "", err
	}
	return &Manager{aead: aead}, hex.EncodeToString(seed), nil
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
