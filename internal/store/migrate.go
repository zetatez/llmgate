package store

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate 按版本号应用 migrations/ 目录下的 *_init.sql 等迁移脚本。
// 已应用的版本记录在 schema_migrations 表。
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations embed: %w", err)
	}

	var pending []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			pending = append(pending, e.Name())
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return versionOf(pending[i]) < versionOf(pending[j])
	})

	return withTx(db, func(tx *sql.Tx) error {
		for _, name := range pending {
			ver := versionOf(name)
			var exists int
			if err := tx.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, ver).Scan(&exists); err != nil {
				return err
			}
			if exists > 0 {
				continue
			}
			body, err := migrationsFS.ReadFile("migrations/" + name)
			if err != nil {
				return fmt.Errorf("read migration %s: %w", name, err)
			}
			if _, err := tx.Exec(string(body)); err != nil {
				return fmt.Errorf("apply migration %s: %w", name, err)
			}
			if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, ver, time.Now().Unix()); err != nil {
				return err
			}
		}
		return nil
	})
}

// versionOf 从文件名 "001_init.sql" 解析出版本号。
func versionOf(name string) int64 {
	idx := strings.IndexByte(name, '_')
	if idx < 0 {
		return 0
	}
	v, err := strconv.ParseInt(name[:idx], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func withTx(db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
