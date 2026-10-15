package store

import (
	"database/sql"
	"errors"
	"sort"
	"strings"
	"testing"
)

var errSentinel = errors.New("sentinel")

func TestMigrateIdempotent(t *testing.T) {
	db := setupDB(t) // 首次迁移已在 setupDB 完成
	// 再次迁移应幂等，不报错、不重复应用。
	if err := Migrate(db); err != nil {
		t.Fatalf("re-run migrate: %v", err)
	}
	// 校验 schema_migrations 中记录了所有版本。
	var n int
	if err := db.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n < 9 {
		t.Fatalf("expected >=9 applied migrations, got %d", n)
	}
}

func TestMigrateBackfillsLegacyTokens(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	// 只应用 1..8 的历史迁移，模拟升级前的旧库
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") && versionOf(e.Name()) <= 8 {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool { return versionOf(names[i]) < versionOf(names[j]) })
	for _, name := range names {
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(sqlBytes)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	// 旧式用户：只有 token_hash/token_enc
	if _, err := db.Exec(`INSERT INTO users (name, token_hash, token_enc, status, created_at, updated_at)
		VALUES ('old', 'legacyhash', 'legacyenc', 1, 0, 0)`); err != nil {
		t.Fatal(err)
	}

	// 升级到最新（应用 009/010，等价于 Migrate 恰好要跑的那几步）
	for _, name := range []string{"009_multiuser.sql", "010_sessions.sql"} {
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(sqlBytes)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}

	// 旧令牌被回填进 user_tokens，升级后客户端继续可用
	var h, enc string
	if err := db.QueryRow(`SELECT token_hash, token_enc FROM user_tokens WHERE token_enc='legacyenc'`).Scan(&h, &enc); err != nil {
		t.Fatalf("legacy token not backfilled: %v", err)
	}
	if h != "legacyhash" {
		t.Fatalf("backfilled hash = %q, want legacyhash", h)
	}
	// ResolveUserToken 可解析回填令牌
	u, tok, err := ResolveUserToken(db, "legacyhash")
	if err != nil || u == nil || tok == nil || u.Name != "old" {
		t.Fatalf("resolve backfilled: user=%+v token=%+v err=%v", u, tok, err)
	}
}

func TestVersionOf(t *testing.T) {
	cases := []struct {
		name string
		want int64
	}{
		{"001_init.sql", 1},
		{"008_route_price.sql", 8},
		{"abc.sql", 0},     // 无 '_' 前缀版本
		{"xxx_foo.sql", 0}, // 前缀非数字，ParseInt 失败
		{"no_sep.sql", 0},  // '_' 前为空 -> ParseInt 失败
	}
	for _, c := range cases {
		if got := versionOf(c.name); got != c.want {
			t.Fatalf("versionOf(%q) = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestWithTxRollback(t *testing.T) {
	db := setupDB(t)
	// 事务内 fn 出错应 rollback，并返回该错误。
	err := withTx(db, func(tx *sql.Tx) error {
		// 先写一行再返回错误，验证被回滚。
		if _, e := tx.Exec(`INSERT INTO settings (key, value) VALUES ('rollback_test', '1')`); e != nil {
			return e
		}
		return errSentinel
	})
	if err != errSentinel {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	// 回滚后 settings 不应包含该行。
	var val string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key='rollback_test'`).Scan(&val); err == nil {
		t.Fatalf("expected no row after rollback, got value %q", val)
	}
}
