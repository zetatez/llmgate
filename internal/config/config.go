package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config 是 llmgate 的全局配置，全部来自环境变量。
// 本地运行时若存在 .env 文件会自动加载（已有环境变量优先，不会被覆盖）。
type Config struct {
	// HTTPAddr 监听地址，默认 ":8080"
	HTTPAddr string
	// DBPath SQLite 数据库文件路径
	DBPath string
	// AdminToken 管理端访问令牌（首次启动时写入 settings，可留空自动生成）
	AdminToken string
	// EncryptSeed API Key 加密种子（32 字节 hex），留空则首次启动自动生成
	EncryptSeed string
	// GatewayPrefix OpenAPI 网关前缀，默认 "/"
	GatewayPrefix string
	// LogLevel gin 日志级别：debug / release
	LogLevel string
	// TZ 应用时区，默认 Asia/Shanghai
	TZ string
}

// Load 从环境变量读取配置；自动加载 ./.env（可选）。
func Load() (*Config, error) {
	// .env 不存在时忽略；已导出的环境变量优先级高于 .env 文件
	_ = godotenv.Load()

	cfg := &Config{
		HTTPAddr:      getenv("LGM_HTTP_ADDR", ":8080"),
		DBPath:        getenv("LGM_DB_PATH", "./data/llmgate.db"),
		AdminToken:    os.Getenv("LGM_ADMIN_TOKEN"),
		EncryptSeed:   os.Getenv("LGM_ENCRYPT_SEED"),
		GatewayPrefix: getenv("LGM_GATEWAY_PREFIX", "/"),
		LogLevel:      getenv("LGM_LOG_LEVEL", "release"),
		TZ:            getenv("LGM_TZ", "Asia/Shanghai"),
	}
	if cfg.GatewayPrefix != "/" && (cfg.GatewayPrefix == "" || cfg.GatewayPrefix[0] != '/') {
		return nil, fmt.Errorf("LGM_GATEWAY_PREFIX 必须以 / 开头")
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
