package rules

import (
	"path/filepath"
	"testing"

	"work-schedule/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.DB().Exec(`INSERT INTO t_scope(id, name, created_at, updated_at) VALUES(1, 's1', 1, 1)`); err != nil {
		t.Fatalf("插入 scope 失败: %v", err)
	}
	return st
}

// seedRule 插一条规则并挂到指定连线上。mountType: global_rule / personal_rule / rule_mount
func seedRule(t *testing.T, st *store.Store, scopeID int64, typ, param, mountType string, fromID int64) int64 {
	t.Helper()
	res, err := st.DB().Exec(
		`INSERT INTO t_rule(scope_id, type, param, created_at, updated_at) VALUES(?, ?, ?, 1, 1)`,
		scopeID, typ, param)
	if err != nil {
		t.Fatalf("插入规则失败: %v", err)
	}
	ruleID, _ := res.LastInsertId()
	var from any
	if fromID != 0 {
		from = fromID
	}
	if _, err := st.DB().Exec(
		`INSERT INTO t_mapping(scope_id, type, from_id, rule_id, created_at, updated_at) VALUES(?, ?, ?, ?, 1, 1)`,
		scopeID, mountType, from, ruleID); err != nil {
		t.Fatalf("插入挂载失败: %v", err)
	}
	return ruleID
}

// seedDataset 插一条实体（scope=1），id 显式指定，便于被外键引用。
func seedDataset(t *testing.T, st *store.Store, typ, name string, id int64) int64 {
	t.Helper()
	if _, err := st.DB().Exec(
		`INSERT INTO t_dataset(id, scope_id, type, col1, created_at, updated_at) VALUES(?, 1, ?, ?, 1, 1)`,
		id, typ, name); err != nil {
		t.Fatalf("插入 %s 失败: %v", typ, err)
	}
	return id
}

func TestLoadGlobalAndPersonal(t *testing.T) {
	st := newTestStore(t)
	seedRule(t, st, 1, "fair_count", `{"weight":2}`, "global_rule", 0)
	// t_mapping.from_id 有指向 t_dataset 的外键，个人挂载必须先有目标老师。
	seedDataset(t, st, "teacher", "李老师", 42)
	seedRule(t, st, 1, "fair_interval", ``, "personal_rule", 42)

	got, err := Load(st, 1)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("应加载 2 条规则，得到 %d: %+v", len(got), got)
	}
	if got[0].Mount != MountGlobal || got[0].TargetID != 0 {
		t.Fatalf("第一条应为全局，得到 %+v", got[0])
	}
	if got[0].Instance.Meta().Type != "fair_count" {
		t.Fatalf("第一条类型应为 fair_count，得到 %s", got[0].Instance.Meta().Type)
	}
	if got[1].Mount != MountTeacher || got[1].TargetID != 42 {
		t.Fatalf("第二条应为个人（target=42），得到 %+v", got[1])
	}
}

func TestLoadSkipsUnknownType(t *testing.T) {
	st := newTestStore(t)
	seedRule(t, st, 1, "fair_count", ``, "global_rule", 0)
	seedRule(t, st, 1, "unknown_type", ``, "global_rule", 0)

	got, err := Load(st, 1)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("未知类型应被跳过，得到 %d 条: %+v", len(got), got)
	}
}
