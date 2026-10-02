package engine

import (
	"testing"

	"work-schedule/internal/plan"
)

func TestBuildProblemUnitsAndLocks(t *testing.T) {
	st := newTestStore(t)
	_ = seedDataset(t, st, "teacher", "张老师")
	t2 := seedDataset(t, st, "teacher", "李老师")
	class := seedDataset(t, st, "class", "高二3班")
	shift := seedDataset(t, st, "shift", "晚1")

	for _, d := range []string{"2026-10-06", "2026-10-07"} {
		if _, err := st.DB().Exec(
			`INSERT INTO t_schedule_day(scope_id, day, created_at, updated_at) VALUES(1, ?, 1, 1)`, d); err != nil {
			t.Fatalf("插入排班日失败: %v", err)
		}
	}
	// 人工锁定：2026-10-06 晚1 高二3班 = 李老师
	if _, err := st.DB().Exec(`
		INSERT INTO t_assignment(scope_id, day, shift_id, class_id, teacher_id, locked, created_at, updated_at)
		VALUES(1, '2026-10-06', ?, ?, ?, 1, 1, 1)`, shift, class, t2); err != nil {
		t.Fatalf("插入锁定失败: %v", err)
	}

	p, err := BuildProblem(st, 1)
	if err != nil {
		t.Fatalf("BuildProblem 失败: %v", err)
	}
	// 2 天 × 1 班次 × 1 班 = 2 个单元
	if len(p.Units) != 2 {
		t.Fatalf("应有 2 个单元，得到 %d: %+v", len(p.Units), p.Units)
	}
	lockedUnit, ok := p.UnitForCell(plan.Cell{Day: "2026-10-06", ShiftID: shift, ClassID: class})
	if !ok {
		t.Fatal("锁定的格子应能定位到单元")
	}
	if got, ok := p.Locks[lockedUnit.ID]; !ok || got != plan.TeacherID(t2) {
		t.Fatalf("Locks 应包含锁定老师 %d，得到 %+v", t2, p.Locks)
	}
	// 候选集默认是全体老师
	if len(p.Candidates[lockedUnit.ID]) != 2 {
		t.Fatalf("候选应默认含 2 位老师，得到 %+v", p.Candidates[lockedUnit.ID])
	}
}

func TestBuildProblemRunsFairRules(t *testing.T) {
	st := newTestStore(t)
	_ = seedDataset(t, st, "teacher", "张老师")
	_ = seedDataset(t, st, "class", "高二3班")
	_ = seedDataset(t, st, "shift", "晚1")
	if _, err := st.DB().Exec(
		`INSERT INTO t_schedule_day(scope_id, day, created_at, updated_at) VALUES(1, '2026-10-06', 1, 1)`); err != nil {
		t.Fatalf("插入排班日失败: %v", err)
	}
	seedRule(t, st, "fair_count", `{"weight":3}`)

	p, err := BuildProblem(st, 1)
	if err != nil {
		t.Fatalf("BuildProblem 失败: %v", err)
	}
	found := false
	for _, c := range p.Costs {
		if b, ok := c.(plan.Balance); ok && b.Weight == 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("Problem.Costs 应含 fair_count 产出的 Balance(weight=3)，得到 %+v", p.Costs)
	}
}

func TestBuildProblemDutyBounds(t *testing.T) {
	st := newTestStore(t)
	teacher := seedDataset(t, st, "teacher", "张老师")
	_ = seedDataset(t, st, "class", "高二3班")
	shift := seedDataset(t, st, "shift", "晚1")
	if _, err := st.DB().Exec(
		`INSERT INTO t_schedule_day(scope_id, day, created_at, updated_at) VALUES(1, '2026-10-06', 1, 1)`); err != nil {
		t.Fatalf("插入排班日失败: %v", err)
	}
	// 指定不值班
	if _, err := st.DB().Exec(`
		INSERT INTO t_duty(scope_id, day, shift_id, teacher_id, required, created_at, updated_at)
		VALUES(1, '2026-10-06', ?, ?, 0, 1, 1)`, shift, teacher); err != nil {
		t.Fatalf("插入值班失败: %v", err)
	}

	p, err := BuildProblem(st, 1)
	if err != nil {
		t.Fatalf("BuildProblem 失败: %v", err)
	}
	found := false
	for _, c := range p.Hard {
		if cb, ok := c.(plan.CountBound); ok && cb.Max == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("问题里应含「指定不值班」翻译出的 CountBound{Max:0}，得到 %+v", p.Hard)
	}
}
