package store

import (
	"database/sql"
	"errors"
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
	if n < 8 {
		t.Fatalf("expected >=8 applied migrations, got %d", n)
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
