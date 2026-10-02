package store

import (
	"path/filepath"
	"testing"
)

func TestOpenAddsRuleParamColumn(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	cols := []struct {
		Name string `db:"name"`
	}{}
	if err := st.DB().Unsafe().Select(&cols, "PRAGMA table_info(t_rule)"); err != nil {
		t.Fatalf("读取表结构失败: %v", err)
	}
	found := false
	for _, c := range cols {
		if c.Name == "param" {
			found = true
		}
	}
	if !found {
		t.Fatalf("t_rule 应有 param 列，实际列: %+v", cols)
	}
}

func TestEnsureColumnIsIdempotent(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	if _, err := st.DB().Exec(`CREATE TABLE old_style (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("建临时表失败: %v", err)
	}
	if err := st.ensureColumn("old_style", "param", "TEXT"); err != nil {
		t.Fatalf("ensureColumn 失败: %v", err)
	}
	// 再补一次应幂等、不报错
	if err := st.ensureColumn("old_style", "param", "TEXT"); err != nil {
		t.Fatalf("ensureColumn 应幂等: %v", err)
	}
}
