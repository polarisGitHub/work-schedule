package services

import (
	"path/filepath"
	"testing"

	"work-schedule/internal/store"
)

func newScheduleStore(t *testing.T) *store.Store {
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

func seedDatasetSvc(t *testing.T, st *store.Store, typ, name string) int64 {
	t.Helper()
	res, err := st.DB().Exec(
		`INSERT INTO t_dataset(scope_id, type, col1, created_at, updated_at) VALUES(1, ?, ?, 1, 1)`, typ, name)
	if err != nil {
		t.Fatalf("插入 %s 失败: %v", typ, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestSetListClearAssignment(t *testing.T) {
	st := newScheduleStore(t)
	svc := NewScheduleService(st)
	shiftID := seedDatasetSvc(t, st, "shift", "晚1")
	classID := seedDatasetSvc(t, st, "class", "高二3班")
	teacherID := seedDatasetSvc(t, st, "teacher", "张老师")

	if err := svc.SetAssignment(1, "2026-10-06", shiftID, classID, teacherID, true); err != nil {
		t.Fatalf("SetAssignment 失败: %v", err)
	}
	// 重复设置应 upsert，不报错
	if err := svc.SetAssignment(1, "2026-10-06", shiftID, classID, teacherID, false); err != nil {
		t.Fatalf("重复 SetAssignment 应 upsert: %v", err)
	}

	list, err := svc.ListAssignments(1, 0)
	if err != nil {
		t.Fatalf("ListAssignments 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("应有 1 条排班，得到 %d: %+v", len(list), list)
	}
	if list[0].TeacherID != teacherID {
		t.Fatalf("老师应为 %d，得到 %d", teacherID, list[0].TeacherID)
	}
	if list[0].Locked {
		t.Fatal("第二次把 locked 设为 false，应生效")
	}

	if err := svc.ClearAssignment(1, "2026-10-06", shiftID, classID); err != nil {
		t.Fatalf("ClearAssignment 失败: %v", err)
	}
	list, _ = svc.ListAssignments(1, 0)
	if len(list) != 0 {
		t.Fatalf("清除后应无排班，得到 %+v", list)
	}
}

func TestRuleTypesIncludesFairRules(t *testing.T) {
	st := newScheduleStore(t)
	svc := NewScheduleService(st)
	types := svc.RuleTypes()
	found := false
	for _, r := range types {
		if r.Type == "fair_count" {
			found = true
			if len(r.ParamSchema) == 0 {
				t.Fatal("fair_count 应带参数表，供前端生成表单")
			}
		}
	}
	if !found {
		t.Fatalf("RuleTypes 应包含 fair_count，得到 %+v", types)
	}
}
