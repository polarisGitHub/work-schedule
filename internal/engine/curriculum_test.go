package engine

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

func seedDataset(t *testing.T, st *store.Store, typ, name string) int64 {
	t.Helper()
	res, err := st.DB().Exec(
		`INSERT INTO t_dataset(scope_id, type, col1, created_at, updated_at) VALUES(1, ?, ?, 1, 1)`, typ, name)
	if err != nil {
		t.Fatalf("插入 %s 失败: %v", typ, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedRule 在库里挂一条全局规则。
func seedRule(t *testing.T, st *store.Store, typ, param string) {
	t.Helper()
	res, err := st.DB().Exec(
		`INSERT INTO t_rule(scope_id, type, param, created_at, updated_at) VALUES(1, ?, ?, 1, 1)`, typ, param)
	if err != nil {
		t.Fatalf("插入规则失败: %v", err)
	}
	ruleID, _ := res.LastInsertId()
	if _, err := st.DB().Exec(
		`INSERT INTO t_mapping(scope_id, type, rule_id, created_at, updated_at) VALUES(1, 'global_rule', ?, 1, 1)`,
		ruleID); err != nil {
		t.Fatalf("插入挂载失败: %v", err)
	}
}

func TestLoadCurriculum(t *testing.T) {
	st := newTestStore(t)
	teacher := seedDataset(t, st, "teacher", "张老师")
	subject := seedDataset(t, st, "subject", "数学")
	class := seedDataset(t, st, "class", "高二3班")
	_ = seedDataset(t, st, "shift", "晚1")

	// 任课：张老师 × 数学 × 高二3班
	res, _ := st.DB().Exec(`INSERT INTO t_dataset(scope_id, type, created_at, updated_at) VALUES(1, 'binding', 1, 1)`)
	bindingID, _ := res.LastInsertId()
	for _, l := range []struct {
		typ      string
		from, to int64
	}{
		{"teacher_binding", teacher, bindingID},
		{"binding_subject", bindingID, subject},
		{"binding_class", bindingID, class},
	} {
		if _, err := st.DB().Exec(
			`INSERT INTO t_mapping(scope_id, type, from_id, to_id, created_at, updated_at) VALUES(1, ?, ?, ?, 1, 1)`,
			l.typ, l.from, l.to); err != nil {
			t.Fatalf("插入连线失败: %v", err)
		}
	}

	c, err := LoadCurriculum(st, 1)
	if err != nil {
		t.Fatalf("LoadCurriculum 失败: %v", err)
	}
	if len(c.Teachers) != 1 || c.Teachers[0].Name != "张老师" {
		t.Fatalf("老师加载不对: %+v", c.Teachers)
	}
	if len(c.Teachers[0].SubjectIDs) != 1 || c.Teachers[0].SubjectIDs[0] != subject {
		t.Fatalf("老师应带上所教学科: %+v", c.Teachers[0])
	}
	if len(c.Teachers[0].ClassIDs) != 1 || c.Teachers[0].ClassIDs[0] != class {
		t.Fatalf("老师应带上所教班级: %+v", c.Teachers[0])
	}
	if len(c.Classes) != 1 || c.Classes[0].Name != "高二3班" {
		t.Fatalf("班级加载不对: %+v", c.Classes)
	}
	if len(c.Shifts) != 1 || c.Shifts[0].Name != "晚1" {
		t.Fatalf("班次加载不对: %+v", c.Shifts)
	}
}
