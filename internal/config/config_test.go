package config

import "testing"

func unsetAll(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"LGM_HTTP_ADDR", "LGM_DB_PATH", "LGM_ADMIN_TOKEN", "LGM_ENCRYPT_SEED",
		"LGM_GATEWAY_PREFIX", "LGM_LOG_LEVEL", "LGM_TZ", "LGM_PROXY",
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
