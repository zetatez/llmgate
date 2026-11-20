package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// Config 是 llmgate 的全局配置。
//
// 加载顺序（后者覆盖前者）：
//  1. 代码内置默认值
//  2. 环境变量 LGM_*（仅为兼容旧部署而保留，可完全不使用）
//  3. 配置文件 config.yaml（推荐方式；相对进程工作目录，容器内即 /app/config.yaml）
type Config struct {
	// HTTPAddr 监听地址，默认 ":8080"
	HTTPAddr string `yaml:"http_addr"`
	// DBPath SQLite 数据库文件路径
	DBPath string `yaml:"db_path"`
	// AdminToken 管理端访问令牌（首次启动时写入 settings，可留空自动生成）
	AdminToken string `yaml:"admin_token"`
	// EncryptSeed API Key 加密种子（32 字节 hex），留空则首次启动自动生成
	EncryptSeed string `yaml:"encrypt_seed"`
	// GatewayPrefix OpenAPI 网关前缀，默认 "/"
	GatewayPrefix string `yaml:"gateway_prefix"`
	// LogLevel gin 日志级别：debug / release
	LogLevel string `yaml:"log_level"`
	// TZ 应用时区，默认 Asia/Shanghai
	TZ string `yaml:"tz"`
	// Proxy 上游出站代理，如 socks5://127.0.0.1:7891 或 http://host:port；空=直连
	Proxy string `yaml:"proxy"`
	// TrustedProxies 受信反向代理（CIDR/IP 列表）。留空=不信任任何代理（ClientIP 取 TCP 对端，
	// 不采信 X-Forwarded-For，避免被伪造绕过登录限速）。反代部署时填写代理地址。
	TrustedProxies []string `yaml:"trusted_proxies"`
	// SessionTTLHours 会话有效期（小时），默认 720（30 天）。
	SessionTTLHours int `yaml:"session_ttl_hours"`

	// Source 记录实际加载的配置文件路径（未加载文件时为空）。便于启动日志确认。
	Source string `yaml:"-"`
}

// ConfigPath 是默认配置文件路径，相对进程工作目录（容器内 WORKDIR 为 /app）。
const ConfigPath = "config.yaml"

// Load 读取配置。配置文件不存在时静默使用默认值/环境变量。
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:        getenv("LGM_HTTP_ADDR", ":8080"),
		DBPath:          getenv("LGM_DB_PATH", "./data/llmgate.db"),
		AdminToken:      os.Getenv("LGM_ADMIN_TOKEN"),
		EncryptSeed:     os.Getenv("LGM_ENCRYPT_SEED"),
		GatewayPrefix:   getenv("LGM_GATEWAY_PREFIX", "/"),
		LogLevel:        getenv("LGM_LOG_LEVEL", "release"),
		TZ:              getenv("LGM_TZ", "Asia/Shanghai"),
		Proxy:           os.Getenv("LGM_PROXY"),
		SessionTTLHours: getenvInt("LGM_SESSION_TTL_HOURS", 720),
	}
	if raw := os.Getenv("LGM_TRUSTED_PROXIES"); raw != "" {
		cfg.TrustedProxies = splitList(raw)
	}

	if err := loadFile(cfg, ConfigPath); err != nil {
		return nil, err
	}

	// 文件里写 0/负数视为无效，回退默认。
	if cfg.SessionTTLHours <= 0 {
		cfg.SessionTTLHours = 720
	}
	if cfg.TZ == "" {
		cfg.TZ = "Asia/Shanghai"
	}
	if cfg.GatewayPrefix != "/" && (cfg.GatewayPrefix == "" || cfg.GatewayPrefix[0] != '/') {
		return nil, fmt.Errorf("gateway_prefix must start with /")
	}
	return cfg, nil
}

// loadFile 把 config.yaml 的字段覆盖到 cfg 上；文件缺失/是空文件时不做任何事。
func loadFile(cfg *Config, path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if fi.IsDir() { // 例如 Docker 在源文件缺失时把 bind mount 建成目录
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	// 严格模式：写错字段名（如 admin_tokne）直接报错，而不是被静默忽略。
	if err := yaml.UnmarshalWithOptions(data, cfg, yaml.DisallowUnknownField()); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.Source = path
	return nil
}

func splitList(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// getenvInt 读取整数环境变量；空/非法回退默认。
func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
