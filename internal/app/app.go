// Package app 持有跨模块共享的运行时上下文（配置 + 数据库）。
package app

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"llmgate/internal/config"
	"llmgate/internal/models"
	"llmgate/internal/secret"
	"llmgate/internal/store"
)

// App 是各 handler 共享的依赖容器。
type App struct {
	Cfg    *config.Config
	DB     *sql.DB
	Secret *secret.Manager
}

// New 创建 App。管理令牌策略：
//   - 环境变量 LGM_ADMIN_TOKEN 始终优先，每次启动都会覆盖库内 admin_token_hash；
//   - 未提供环境变量时，首次启动自动生成并打印一次。
//
// 同时初始化 API Key 加密器（种子来自 env 或自动生成并持久化到 settings）。
func New(cfg *config.Config, db *sql.DB) (*App, error) {
	a := &App{Cfg: cfg, DB: db}

	hash, err := a.GetSetting("admin_token_hash")
	if err != nil {
		return nil, fmt.Errorf("read admin_token_hash: %w", err)
	}
	token := cfg.AdminToken
	if token == "" {
		// 无环境变量：仅当库内无令牌时才生成（保持既有登录）
		if hash == "" {
			token, err = GenSecret(24)
			if err != nil {
				return nil, err
			}
			fmt.Println("==================================================")
			fmt.Printf("  首次启动：管理员令牌 = %s\n", token)
			fmt.Println("  请立即保存，写入 LGM_ADMIN_TOKEN 环境变量")
			fmt.Println("==================================================")
		}
	}
	if token != "" {
		if err := a.SetSetting("admin_token_hash", HashToken(token)); err != nil {
			return nil, err
		}
	}

	seed, err := a.GetSetting("encrypt_seed")
	if err != nil {
		return nil, fmt.Errorf("read encrypt_seed: %w", err)
	}
	if seed == "" {
		seed = cfg.EncryptSeed
	}
	mgr, persistSeed, err := secret.New(seed)
	if err != nil {
		return nil, fmt.Errorf("init secret: %w", err)
	}
	if err := a.SetSetting("encrypt_seed", persistSeed); err != nil {
		return nil, err
	}
	a.Secret = mgr
	return a, nil
}

// HashToken 以 sha256 哈希令牌用于存储与比对。
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// VerifyToken 常量时间比对令牌哈希。
func (a *App) VerifyToken(token string, hash string) bool {
	return subtleCompare(HashToken(token), hash)
}

func subtleCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// GetSetting 读取设置，不存在返回空串。
func (a *App) GetSetting(key string) (string, error) {
	var v string
	err := a.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// FirstEnabledKey 返回渠道第一个启用 Key 的明文（连通性测试/拉取模型共用）。
func (a *App) FirstEnabledKey(channelID int64) (string, error) {
	keys, err := store.ListChannelKeys(a.DB, channelID)
	if err != nil {
		return "", err
	}
	for _, k := range keys {
		if k.Enabled != 1 {
			continue
		}
		plain, err := a.Secret.Decrypt(k.APIKeyEnc)
		if err == nil && plain != "" {
			return plain, nil
		}
	}
	return "", errors.New("渠道没有可用的 Key")
}

// KeyTail 返回密钥明文末 4 位（用于界面辨认该密钥），解密失败返回空串。
func (a *App) KeyTail(enc string) string {
	plain, err := a.Secret.Decrypt(enc)
	if err != nil || plain == "" {
		return ""
	}
	if len(plain) <= 4 {
		return "****"
	}
	return plain[len(plain)-4:]
}

// TokenTail 返回用户令牌明文末 4 位（界面临辨认），无密文返回空串。
func (a *App) TokenTail(u *models.User) string {
	if u == nil || u.TokenEnc == "" {
		return ""
	}
	plain, err := a.Secret.Decrypt(u.TokenEnc)
	if err != nil || plain == "" {
		return ""
	}
	if len(plain) <= 4 {
		return "****"
	}
	return plain[len(plain)-4:]
}

// SetSetting 写入或更新设置。
func (a *App) SetSetting(key, value string) error {
	_, err := a.DB.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// GenSecret 生成 n 字节随机 hex（用于加密种子等）。
func GenSecret(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
