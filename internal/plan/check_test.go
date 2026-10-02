package plan

import "testing"

// mkProblem 构造两单元的最小问题，便于逐条验证检查器。
func mkProblem() *Problem {
	u1 := Unit{ID: 1, Key: "d1|1|c1", Cells: []Cell{{Day: "d1", ShiftID: 1, ClassID: 1}}}
	u2 := Unit{ID: 2, Key: "d1|1|c2", Cells: []Cell{{Day: "d1", ShiftID: 1, ClassID: 2}}}
	return &Problem{
		Units: []Unit{u1, u2},
		Candidates: map[UnitID][]TeacherID{
			1: {10, 11},
			2: {11},
		},
	}
}

func hasViolation(vs []Violation, kind string) bool {
	for _, v := range vs {
		if v.Kind == kind {
			return true
		}
	}
	return false
}

func TestCheckCover(t *testing.T) {
	p := mkProblem()
	vs := Check(p, &Result{Assignments: []Assignment{{Unit: 1, Teacher: 10}}})
	if !hasViolation(vs, "cover") {
		t.Fatalf("期望 cover 违规，得到 %+v", vs)
	}
}

func TestCheckCandidate(t *testing.T) {
	p := mkProblem()
	vs := Check(p, &Result{Assignments: []Assignment{
		{Unit: 1, Teacher: 10},
		{Unit: 2, Teacher: 10},
	}})
	if !hasViolation(vs, "candidate") {
		t.Fatalf("期望 candidate 违规，得到 %+v", vs)
	}
}

func TestCheckCountBound(t *testing.T) {
	p := mkProblem()
	day := "d1"
	p.Hard = []Constraint{CountBound{
		Scope: SelectionScope{Teacher: Ptr(TeacherID(11)), Day: &day},
		Min:   -1, Max: 1, Reason: "每人每天最多1",
	}}
	vs := Check(p, &Result{Assignments: []Assignment{
		{Unit: 1, Teacher: 11},
		{Unit: 2, Teacher: 11},
	}})
	if !hasViolation(vs, "count_bound") {
		t.Fatalf("期望 count_bound 违规，得到 %+v", vs)
	}
}

func TestCheckPairFix(t *testing.T) {
	p := mkProblem()
	p.Hard = []Constraint{
		PairFix{Unit: 1, Teacher: 10, Mode: ModeFix, Reason: "锁定"},
		PairFix{Unit: 2, Teacher: 11, Mode: ModeBan, Reason: "值班禁止"},
	}
	vs := Check(p, &Result{Assignments: []Assignment{{Unit: 1, Teacher: 11}, {Unit: 2, Teacher: 11}}})
	if !hasViolation(vs, "pair_fix") {
		t.Fatalf("期望 pair_fix 违规，得到 %+v", vs)
	}
}

func TestCheckBalance(t *testing.T) {
	p := mkProblem()
	p.Costs = []CostTerm{Balance{Group: []TeacherID{10, 11}, Weight: 1}}
	vs := Check(p, &Result{Assignments: []Assignment{{Unit: 1, Teacher: 10}, {Unit: 2, Teacher: 11}}})
	if hasViolation(vs, "balance") {
		t.Fatal("次数相等不应报偏差")
	}

	// 三单元、10 全包 → 偏差 3-0=3
	p2 := mkProblem()
	p2.Units = append(p2.Units, Unit{ID: 3, Key: "d1|1|c3", Cells: []Cell{{Day: "d1", ShiftID: 1, ClassID: 3}}})
	p2.Candidates[3] = []TeacherID{10, 11}
	p2.Costs = []CostTerm{Balance{Group: []TeacherID{10, 11}, Weight: 1}}
	vs2 := Check(p2, &Result{Assignments: []Assignment{
		{Unit: 1, Teacher: 10}, {Unit: 2, Teacher: 10}, {Unit: 3, Teacher: 10},
	}})
	if !hasViolation(vs2, "balance") {
		t.Fatalf("期望 balance 偏差，得到 %+v", vs2)
	}
}
