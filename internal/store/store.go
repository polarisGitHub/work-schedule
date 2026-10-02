package store

import (
	_ "embed"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/jmoiron/sqlx"
)

// schemaSQL 元数据库表结构，编译期嵌入。
//
//go:embed schema.sql
var schemaSQL string

// Store 封装 SQLite 数据库连接。
// 使用 modernc.org/sqlite 纯 Go 驱动，无需 CGO，便于跨平台编译。
type Store struct {
	db *sqlx.DB
}

// Open 打开（必要时创建）SQLite 数据库，并同步表结构。
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	dsn := path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)"

	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// migrate 建表建索引，可重复执行；对旧库补齐后加的列。
func (s *Store) migrate() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return err
	}
	return s.ensureColumn("t_rule", "param", "TEXT")
}

// ensureColumn 幂等补列：已存在则什么都不做。
// SQLite 没有 ADD COLUMN IF NOT EXISTS，所以先查 PRAGMA。
func (s *Store) ensureColumn(table, column, decl string) error {
	rows := []struct {
		Name string `db:"name"`
	}{}
	if err := s.db.Unsafe().Select(&rows, "PRAGMA table_info("+table+")"); err != nil {
		return err
	}
	for _, r := range rows {
		if r.Name == column {
			return nil
		}
	}
	_, err := s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + decl)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

// DB 暴露底层连接供服务层使用。
func (s *Store) DB() *sqlx.DB { return s.db }
