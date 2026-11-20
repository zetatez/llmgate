package config

import (
	"os"
	"testing"
)

func unsetAll(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"LGM_HTTP_ADDR", "LGM_DB_PATH", "LGM_ADMIN_TOKEN", "LGM_ENCRYPT_SEED",
		"LGM_GATEWAY_PREFIX", "LGM_LOG_LEVEL", "LGM_TZ", "LGM_PROXY", "LGM_TRUSTED_PROXIES", "LGM_SESSION_TTL_HOURS",
	} {
		t.Setenv(k, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	unsetAll(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.DBPath != "./data/llmgate.db" {
		t.Errorf("DBPath = %q, want default", cfg.DBPath)
	}
	if cfg.GatewayPrefix != "/" {
		t.Errorf("GatewayPrefix = %q, want /", cfg.GatewayPrefix)
	}
	if cfg.LogLevel != "release" {
		t.Errorf("LogLevel = %q, want release", cfg.LogLevel)
	}
	if cfg.TZ != "Asia/Shanghai" {
		t.Errorf("TZ = %q, want Asia/Shanghai", cfg.TZ)
	}
	if cfg.AdminToken != "" || cfg.EncryptSeed != "" || cfg.Proxy != "" {
		t.Errorf("secrets/proxy should be empty, got %q/%q/%q", cfg.AdminToken, cfg.EncryptSeed, cfg.Proxy)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Errorf("TrustedProxies default should be empty, got %v", cfg.TrustedProxies)
	}
	if cfg.SessionTTLHours != 720 {
		t.Errorf("SessionTTLHours default = %d, want 720", cfg.SessionTTLHours)
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	unsetAll(t)
	t.Setenv("LGM_TRUSTED_PROXIES", "10.0.0.1, 127.0.0.1/8 ,")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []string{"10.0.0.1", "127.0.0.1/8"}
	if len(cfg.TrustedProxies) != len(want) {
		t.Fatalf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
	}
	for i := range want {
		if cfg.TrustedProxies[i] != want[i] {
			t.Fatalf("TrustedProxies[%d] = %q, want %q", i, cfg.TrustedProxies[i], want[i])
		}
	}
}

func TestLoadFromEnv(t *testing.T) {
	unsetAll(t)
	t.Setenv("LGM_HTTP_ADDR", ":9999")
	t.Setenv("LGM_DB_PATH", "/tmp/x.db")
	t.Setenv("LGM_ADMIN_TOKEN", "tok123")
	t.Setenv("LGM_ENCRYPT_SEED", "deadbeef")
	t.Setenv("LGM_GATEWAY_PREFIX", "/api")
	t.Setenv("LGM_LOG_LEVEL", "debug")
	t.Setenv("LGM_TZ", "UTC")
	t.Setenv("LGM_PROXY", "socks5://127.0.0.1:7891")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HTTPAddr != ":9999" || cfg.DBPath != "/tmp/x.db" || cfg.AdminToken != "tok123" ||
		cfg.EncryptSeed != "deadbeef" || cfg.GatewayPrefix != "/api" || cfg.LogLevel != "debug" ||
		cfg.TZ != "UTC" || cfg.Proxy != "socks5://127.0.0.1:7891" {
		t.Errorf("env values not respected: %+v", cfg)
	}
}

func TestLoadPrefixValidation(t *testing.T) {
	unsetAll(t)
	t.Setenv("LGM_GATEWAY_PREFIX", "api") // 缺少前导 /
	if _, err := Load(); err == nil {
		t.Fatal("prefix without leading slash should error")
	}
	// 空串应回落默认 /
	unsetAll(t)
	if cfg, err := Load(); err != nil || cfg.GatewayPrefix != "/" {
		t.Fatalf("empty prefix should default to /, got %q err=%v", cfg.GatewayPrefix, err)
	}
}

func writeConfigFile(t *testing.T, body string) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.WriteFile(ConfigPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestLoadFromFile(t *testing.T) {
	unsetAll(t)
	writeConfigFile(t, `
admin_token: "filetok"
encrypt_seed: "deadbeef"
http_addr: ":1234"
log_level: debug
trusted_proxies:
  - 10.0.0.1
  - 127.0.0.1/8
session_ttl_hours: 12
`)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Source != ConfigPath {
		t.Errorf("Source = %q, want %q", cfg.Source, ConfigPath)
	}
	if cfg.AdminToken != "filetok" || cfg.EncryptSeed != "deadbeef" {
		t.Errorf("secrets from file not applied: %+v", cfg)
	}
	if cfg.HTTPAddr != ":1234" || cfg.LogLevel != "debug" || cfg.SessionTTLHours != 12 {
		t.Errorf("scalars from file not applied: %+v", cfg)
	}
	want := []string{"10.0.0.1", "127.0.0.1/8"}
	if len(cfg.TrustedProxies) != len(want) {
		t.Fatalf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
	}
	for i := range want {
		if cfg.TrustedProxies[i] != want[i] {
			t.Fatalf("TrustedProxies[%d] = %q, want %q", i, cfg.TrustedProxies[i], want[i])
		}
	}
	// 文件里没写的字段保持默认
	if cfg.DBPath != "./data/llmgate.db" || cfg.GatewayPrefix != "/" || cfg.TZ != "Asia/Shanghai" {
		t.Errorf("untouched defaults changed: %+v", cfg)
	}
}

func TestFileOverridesEnv(t *testing.T) {
	unsetAll(t)
	t.Setenv("LGM_LOG_LEVEL", "release")
	writeConfigFile(t, "log_level: debug\n")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug (file wins over env)", cfg.LogLevel)
	}
}

func TestFileRejectsUnknownField(t *testing.T) {
	unsetAll(t)
	writeConfigFile(t, "admin_tokne: typo\n")
	if _, err := Load(); err == nil {
		t.Fatal("unknown field should be rejected")
	}
}

func TestMissingFileUsesDefaults(t *testing.T) {
	unsetAll(t)
	t.Chdir(t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Source != "" {
		t.Errorf("Source = %q, want empty", cfg.Source)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want default :8080", cfg.HTTPAddr)
	}
}
