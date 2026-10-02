package solver

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"work-schedule/internal/plan"
)

func u(id plan.UnitID, day string, shift, class int64) plan.Unit {
	return plan.Unit{
		ID:    id,
		Key:   fmt.Sprintf("%s|%d|%d", day, shift, class),
		Cells: []plan.Cell{{Day: day, ShiftID: shift, ClassID: class}},
	}
}

func TestGreedyRespectsLockAndSlot(t *testing.T) {
	p := &plan.Problem{
		Units:      []plan.Unit{u(1, "d1", 1, 1), u(2, "d1", 1, 2)},
		Candidates: map[plan.UnitID][]plan.TeacherID{1: {10, 11}, 2: {10, 11}},
		Hard: []plan.Constraint{
			plan.ExclusiveSlot{},
			plan.PairFix{Unit: 1, Teacher: 11, Mode: plan.ModeFix},
		},
		Locks: map[plan.UnitID]plan.TeacherID{1: 11},
	}
	res, err := GreedySwap{}.Solve(context.Background(), p)
	if err != nil {
		t.Fatalf("Solve 失败: %v", err)
	}
	got := map[plan.UnitID]plan.Assignment{}
	for _, a := range res.Assignments {
		got[a.Unit] = a
	}
	if got[1].Teacher != 11 || !got[1].Locked {
		t.Fatalf("单元1 应保留锁定老师 11 且 Locked=true，得到 %+v", got[1])
	}
	if got[2].Teacher != 10 {
		t.Fatalf("单元2 应为 10（11 被同时段占用），得到 %+v", got[2])
	}
}

func TestGreedySlotLeavesUnassigned(t *testing.T) {
	// 两个同时段单元，只有一个老师 → 只能排一个
	p := &plan.Problem{
		Units:      []plan.Unit{u(1, "d1", 1, 1), u(2, "d1", 1, 2)},
		Candidates: map[plan.UnitID][]plan.TeacherID{1: {10}, 2: {10}},
		Hard:       []plan.Constraint{plan.ExclusiveSlot{}},
	}
	res, err := GreedySwap{}.Solve(context.Background(), p)
	if err != nil {
		t.Fatalf("Solve 失败: %v", err)
	}
	if len(res.Assignments) != 1 {
		t.Fatalf("应只排上 1 个，得到 %+v", res.Assignments)
	}
	if len(res.Unassigned) != 1 || res.Unassigned[0].Reason == "" {
		t.Fatalf("应有 1 个未排上且带原因，得到 %+v", res.Unassigned)
	}
}

func TestGreedyBalanceSpreads(t *testing.T) {
	p := &plan.Problem{
		Units:      []plan.Unit{u(1, "d1", 1, 1), u(2, "d1", 2, 1)},
		Candidates: map[plan.UnitID][]plan.TeacherID{1: {10, 11}, 2: {10, 11}},
		Hard:       []plan.Constraint{plan.ExclusiveSlot{}},
		Costs:      []plan.CostTerm{plan.Balance{Group: []plan.TeacherID{10, 11}, Weight: 1}},
	}
	res, _ := GreedySwap{}.Solve(context.Background(), p)
	seen := map[plan.TeacherID]int{}
	for _, a := range res.Assignments {
		seen[a.Teacher]++
	}
	if seen[10] != 1 || seen[11] != 1 {
		t.Fatalf("Balance 应让两人各 1 次，得到 %+v", seen)
	}
}

func TestGreedyCountBoundMaxZero(t *testing.T) {
	day, shift, teacher := "d1", int64(1), plan.TeacherID(10)
	p := &plan.Problem{
		Units:      []plan.Unit{u(1, day, shift, 1)},
		Candidates: map[plan.UnitID][]plan.TeacherID{1: {10}},
		Hard: []plan.Constraint{
			plan.CountBound{Scope: plan.SelectionScope{Teacher: &teacher, Day: &day, ShiftID: &shift}, Min: -1, Max: 0},
		},
	}
	res, _ := GreedySwap{}.Solve(context.Background(), p)
	if len(res.Assignments) != 0 {
		t.Fatalf("Max:0 的老师不该被排上，得到 %+v", res.Assignments)
	}
}

func TestGreedySwapFreesBlockedTeacher(t *testing.T) {
	// 三个同时段单元、三个老师；贪心按 Key 先选小号老师，会让单元3 没得排。
	// 局部交换应把单元1 挪到空闲的 12，把 10 让给单元3。
	p := &plan.Problem{
		Units: []plan.Unit{u(1, "d1", 1, 1), u(2, "d1", 1, 2), u(3, "d1", 1, 3)},
		Candidates: map[plan.UnitID][]plan.TeacherID{
			1: {10, 12},
			2: {11, 12},
			3: {10, 11},
		},
		Hard:  []plan.Constraint{plan.ExclusiveSlot{}},
		Costs: []plan.CostTerm{plan.Balance{Group: []plan.TeacherID{10, 11, 12}, Weight: 1}},
	}
	res, err := GreedySwap{}.Solve(context.Background(), p)
	if err != nil {
		t.Fatalf("Solve 失败: %v", err)
	}
	if len(res.Unassigned) != 0 {
		t.Fatalf("局部交换后应全部排上，得到 unassigned=%+v assignments=%+v", res.Unassigned, res.Assignments)
	}
	used := map[plan.TeacherID]bool{}
	for _, a := range res.Assignments {
		used[a.Teacher] = true
	}
	if !used[10] || !used[11] || !used[12] {
		t.Fatalf("三个老师都应被用上，得到 %+v", used)
	}
}

func TestGreedyUnsatisfiableMinKeepsGoing(t *testing.T) {
	// Min:1 指定的老师不在任何候选里 —— 无解也不阻断，照常返回并由检查器报出
	day, shift, missing := "d1", int64(1), plan.TeacherID(99)
	p := &plan.Problem{
		Units:      []plan.Unit{u(1, day, shift, 1)},
		Candidates: map[plan.UnitID][]plan.TeacherID{1: {10}},
		Hard: []plan.Constraint{
			plan.ExclusiveSlot{},
			plan.CountBound{Scope: plan.SelectionScope{Teacher: &missing, Day: &day, ShiftID: &shift}, Min: 1, Max: -1},
		},
	}
	res, err := GreedySwap{}.Solve(context.Background(), p)
	if err != nil {
		t.Fatalf("无解也不该报错: %v", err)
	}
	if len(res.Assignments) != 1 {
		t.Fatalf("普通单元仍应排上，得到 %+v", res.Assignments)
	}
	vs := plan.Check(p, res)
	found := false
	for _, v := range vs {
		if v.Kind == "count_bound" {
			found = true
		}
	}
	if !found {
		t.Fatalf("检查器应报出 count_bound 违规，得到 %+v", vs)
	}
}

func TestGreedyDeterministic(t *testing.T) {
	p := &plan.Problem{
		Units: []plan.Unit{u(1, "d1", 1, 1), u(2, "d1", 1, 2), u(3, "d1", 2, 1)},
		Candidates: map[plan.UnitID][]plan.TeacherID{
			1: {11, 10},
			2: {10, 11},
			3: {10, 11, 12},
		},
		Hard:  []plan.Constraint{plan.ExclusiveSlot{}},
		Costs: []plan.CostTerm{plan.Balance{Group: []plan.TeacherID{10, 11, 12}, Weight: 1}},
	}
	r1, _ := GreedySwap{}.Solve(context.Background(), p)
	r2, _ := GreedySwap{}.Solve(context.Background(), p)
	if !reflect.DeepEqual(r1.Assignments, r2.Assignments) {
		t.Fatalf("同输入应给同输出：\n%+v\n%+v", r1.Assignments, r2.Assignments)
	}
}
