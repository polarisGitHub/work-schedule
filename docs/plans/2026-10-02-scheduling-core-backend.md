# 排班后端核心 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 搭出"元数据 + 挂载规则 → 声明式 `Problem` → 检查器复核结果"的后端核心，让**人工排班 + 实时体检**可用。

**范围说明（重要）：** 本计划覆盖 spec 实施顺序的**第 1~4 步的后端部分**。下面两项是**本计划有意拆出去的**（为控制单份计划的粒度），**不是 spec 排除的**：

- **贪心求解器**（spec 实施顺序**第 5 步**）→ 另出一份计划；
- **排班结果页**（spec 第 4 步里的前端部分）→ 另开会话。

**Architecture:** 声明式契约（`internal/plan`）→ 规则层把 `(type,param)` 翻译成契约数据（`internal/rules`）→ 引擎读库里元数据并跑规则产出 `Problem`（`internal/engine`）→ 检查器对结果按同一批契约数据复核。规则与算法都只依赖契约，互不认识。

**Tech Stack:** Go 1.27、`github.com/jmoiron/sqlx`、`modernc.org/sqlite`（纯 Go，免 CGO）、标准库 `testing`。

**依据 spec:** `docs/specs/2026-10-02-scheduling-architecture-spec.md`

---

## 对 spec 的一处修正（重要）

spec 里写 `internal/plan` 同时放 `problem.go` / `build.go` / `check.go`。实际会形成 **import 环**：

- `rules` 的 `BuildCtx` 需要 `plan.Unit` → `rules` 依赖 `plan`
- 而 `build.go` 要调用 `rules` → `plan` 依赖 `rules` ← 环

修正：**builder 移到 `internal/engine`**（位于 `plan` 与 `rules` 之上）。最终分层：

```
internal/plan    契约类型 + 检查器            （不依赖本项目其它包）
internal/rules   规则接口/注册表/实现/加载     （依赖 plan）
internal/engine  读库 + 组装 BuildCtx + 跑规则 → plan.Problem（依赖 plan、rules、store）
internal/services  暴露给前端                 （依赖全部）
```

---

## 约定

- 本仓库**目前没有任何测试**，本计划首次引入 Go 单测：测试文件与被测文件同目录，命名 `xxx_test.go`。
- 跑测试统一为 `go test ./internal/<pkg>/ -run <TestName> -v`（工作目录＝worktree 根）。
- 每个任务结束都 commit；消息用中文，前缀沿用仓库风格。
- 每个任务给的代码**完整可编译**；后续任务只新增，不回改前面任务的文件（除非明确写 "Modify"）。

---

## Task 1: 契约类型与 SelectionScope

**Files:**
- Create: `internal/plan/problem.go`
- Test: `internal/plan/problem_test.go`

**Step 1: Write the failing test**

创建 `internal/plan/problem_test.go`：

```go
package plan

import "testing"

func TestSelectionScopeMatchCell(t *testing.T) {
	teacher := TeacherID(7)
	day := "2026-10-06"
	shift := int64(1)

	cases := []struct {
		name  string
		scope SelectionScope
		tes   TeacherID
		cell  Cell
		want  bool
	}{
		{"空 scope 全匹配", SelectionScope{}, 7, Cell{Day: day, ShiftID: 1, ClassID: 3}, true},
		{"teacher 命中", SelectionScope{Teacher: &teacher}, 7, Cell{Day: day}, true},
		{"teacher 未命中", SelectionScope{Teacher: &teacher}, 8, Cell{Day: day}, false},
		{"day 命中", SelectionScope{Day: &day}, 7, Cell{Day: day}, true},
		{"day 未命中", SelectionScope{Day: &day}, 7, Cell{Day: "2026-10-07"}, false},
		{"shift 命中", SelectionScope{ShiftID: &shift}, 7, Cell{ShiftID: 1}, true},
		{"shift 未命中", SelectionScope{ShiftID: &shift}, 7, Cell{ShiftID: 2}, false},
		{"days 滑窗命中", SelectionScope{Days: []string{"2026-10-05", day}}, 7, Cell{Day: day}, true},
		{"days 滑窗未命中", SelectionScope{Days: []string{"2026-10-05"}}, 7, Cell{Day: day}, false},
	}
	for _, c := range cases {
		if got := c.scope.MatchCell(c.tes, c.cell); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestProblemUnitForCell(t *testing.T) {
	p := &Problem{Units: []Unit{
		{ID: 1, Key: "a", Cells: []Cell{{Day: "2026-10-06", ShiftID: 1, ClassID: 3}}},
		{ID: 2, Key: "b", Cells: []Cell{
			{Day: "2026-10-06", ShiftID: 2, ClassID: 3},
			{Day: "2026-10-06", ShiftID: 2, ClassID: 4},
		}},
	}}
	u, ok := p.UnitForCell(Cell{Day: "2026-10-06", ShiftID: 2, ClassID: 4})
	if !ok || u.ID != 2 {
		t.Fatalf("期望命中 unit 2，得到 ok=%v unit=%d", ok, u.ID)
	}
	if _, ok := p.UnitForCell(Cell{Day: "2026-10-06", ShiftID: 9, ClassID: 9}); ok {
		t.Fatal("不存在的格子不应命中")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/plan/ -run 'TestSelectionScopeMatchCell|TestProblemUnitForCell' -v`
Expected: FAIL —— `no Go files in .../internal/plan`（包还不存在）

**Step 3: Write minimal implementation**

创建 `internal/plan/problem.go`：

```go
// Package plan 定义排班的声明式契约：规则层产出它，求解器消费它，检查器复核它。
//
// 本包不依赖项目内其它包，也不含任何算法逻辑。
package plan

// UnitID / TeacherID 在契约内部用独立类型，避免与数据库 id、班次 id 混用。
type UnitID int64
type TeacherID int64

// Cell 一个物理课表格子：天 + 班次 + 班级。
type Cell struct {
	Day     string `json:"day"`
	ShiftID int64  `json:"shiftId"`
	ClassID int64  `json:"classId"`
}

// Unit 待排单元，覆盖一个或多个物理格子。
// 正常班单元恒为 1 个格子；合班定下来后某些单元会有多个格子，上层无需改动。
type Unit struct {
	ID    UnitID `json:"id"`
	Key   string `json:"key"` // 展示用，如 "2026-10-06|1|3"
	Cells []Cell `json:"cells"`
}

// SelectionScope 计数类约束/代价项的作用范围：对满足全部非空维度的分配计数。
// 各维度之间是 AND；留空表示不限。
type SelectionScope struct {
	Teacher *TeacherID `json:"teacher,omitempty"`
	Day     *string    `json:"day,omitempty"`
	ShiftID *int64     `json:"shiftId,omitempty"`
	Days    []string   `json:"days,omitempty"` // 多日窗口（如间隔滑窗）
}

// MatchCell 判断"某老师的某格子"是否落在范围内。
func (s SelectionScope) MatchCell(t TeacherID, c Cell) bool {
	if s.Teacher != nil && *s.Teacher != t {
		return false
	}
	if s.Day != nil && *s.Day != c.Day {
		return false
	}
	if s.ShiftID != nil && *s.ShiftID != c.ShiftID {
		return false
	}
	if len(s.Days) > 0 {
		found := false
		for _, d := range s.Days {
			if d == c.Day {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// FixMode PairFix 的方向。
type FixMode string

const (
	ModeFix FixMode = "fix" // 必须排这一对
	ModeBan FixMode = "ban" // 禁止这一对
)

// Constraint 硬约束。接口用未导出方法，只有本包能实现，保证词汇封闭。
type Constraint interface{ isConstraint() }

// CountBound 对某个选择子集的计数上下界；Min/Max 为 -1 表示该侧不限。
type CountBound struct {
	Scope  SelectionScope `json:"scope"`
	Min    int            `json:"min"`
	Max    int            `json:"max"`
	Reason string         `json:"reason,omitempty"`
}

func (CountBound) isConstraint() {}

// PairFix 指定或禁止"单元 × 老师"。
type PairFix struct {
	Unit    UnitID    `json:"unit"`
	Teacher TeacherID `json:"teacher"`
	Mode    FixMode   `json:"mode"`
	Reason  string    `json:"reason,omitempty"`
}

func (PairFix) isConstraint() {}

// Cover 这些单元必须有人。
type Cover struct {
	Units  []UnitID `json:"units"`
	Reason string   `json:"reason,omitempty"`
}

func (Cover) isConstraint() {}

// CostTerm 软代价。
type CostTerm interface{ isCostTerm() }

// Balance 组内每人次数尽量均等。
type Balance struct {
	Group  []TeacherID `json:"group"`
	Weight float64     `json:"weight"`
}

func (Balance) isCostTerm() {}

// Spread 范围内的点尽量在时间上散开。
type Spread struct {
	Scope  SelectionScope `json:"scope"`
	Weight float64        `json:"weight"`
}

func (Spread) isCostTerm() {}

// Prefer 线性偏好：Sign 为 1 鼓励被选中，-1 避免被选中。
type Prefer struct {
	Scope  SelectionScope `json:"scope"`
	Weight float64        `json:"weight"`
	Sign   int            `json:"sign"`
}

func (Prefer) isCostTerm() {}

// Assignment 一条排定：单元 → 老师。
type Assignment struct {
	Unit    UnitID    `json:"unit"`
	Teacher TeacherID `json:"teacher"`
	Locked  bool      `json:"locked"`
}

// Unassigned 没排上的单元及原因。
type Unassigned struct {
	Unit      UnitID   `json:"unit"`
	Reason    string   `json:"reason"`
	BlockedBy []string `json:"blockedBy,omitempty"`
}

// Result 求解器 / 人工排班的产出。
type Result struct {
	Assignments []Assignment `json:"assignments"`
	Unassigned  []Unassigned `json:"unassigned,omitempty"`
	Score       float64      `json:"score"`
	Optimal     bool         `json:"optimal"`
}

// Level 冲突级别。
type Level string

const (
	LevelHard Level = "hard"
	LevelSoft Level = "soft"
	LevelInfo Level = "info"
)

// Violation 检查器报出的一条冲突。
type Violation struct {
	Level     Level      `json:"level"`
	Kind      string     `json:"kind"`
	Unit      *UnitID    `json:"unit,omitempty"`
	Teacher   *TeacherID `json:"teacher,omitempty"`
	RuleLabel string     `json:"ruleLabel,omitempty"`
	Message   string     `json:"message"`
	Delta     float64    `json:"delta,omitempty"`
}

// Problem 规则层与求解器之间的唯一契约。
//
// Hard / Costs 是接口切片，暂不参与 JSON 序列化（json:"-"）。
// 将来接外部进程求解器时，再给 Constraint / CostTerm 加带类型判别字段的
// MarshalJSON / UnmarshalJSON——现在做属于过度设计。
type Problem struct {
	Units      []Unit                          `json:"units"`
	Candidates map[UnitID][]TeacherID          `json:"candidates"`
	Hard       []Constraint                    `json:"-"`
	Costs      []CostTerm                      `json:"-"`
	Locks      map[UnitID]TeacherID            `json:"locks,omitempty"`
	Reason     map[UnitID]map[TeacherID]string `json:"reason,omitempty"`
}

// UnitForCell 返回覆盖该格子的单元。
func (p *Problem) UnitForCell(c Cell) (Unit, bool) {
	for _, u := range p.Units {
		for _, cell := range u.Cells {
			if cell == c {
				return u, true
			}
		}
	}
	return Unit{}, false
}

// Ptr 返回 v 的指针，便于构造可选的指针字段。
func Ptr[T any](v T) *T { return &v }
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/plan/ -run 'TestSelectionScopeMatchCell|TestProblemUnitForCell' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/plan/problem.go internal/plan/problem_test.go
git commit -m "feat(plan): 排班声明式契约类型 + SelectionScope 匹配"
```

---

## Task 2: 检查器

**Files:**
- Create: `internal/plan/check.go`
- Test: `internal/plan/check_test.go`

**Step 1: Write the failing test**

创建 `internal/plan/check_test.go`：

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/plan/ -run 'TestCheck' -v`
Expected: FAIL —— `undefined: Check`

**Step 3: Write minimal implementation**

创建 `internal/plan/check.go`：

```go
package plan

import "fmt"

// Check 用 Problem 里的声明式数据复核一个结果，抛出冲突清单。
//
// 检查器不重写任何规则逻辑：候选集 / 硬约束 / 代价项都能对"某个具体结果"求值，
// 所以同一批数据既喂求解器、又能复核结果。纯函数，不碰数据库。
func Check(p *Problem, r *Result) []Violation {
	out := []Violation{}

	unitByID := make(map[UnitID]Unit, len(p.Units))
	for _, u := range p.Units {
		unitByID[u.ID] = u
	}
	assigned := make(map[UnitID]TeacherID, len(r.Assignments))
	for _, a := range r.Assignments {
		assigned[a.Unit] = a.Teacher
	}

	// 1) 覆盖 + 候选资格
	for _, u := range p.Units {
		t, ok := assigned[u.ID]
		if !ok {
			out = append(out, Violation{Level: LevelHard, Kind: "cover", Unit: Ptr(u.ID),
				Message: fmt.Sprintf("%s 没有排老师", u.Key)})
			continue
		}
		if len(p.Candidates[u.ID]) > 0 && !containsTeacher(p.Candidates[u.ID], t) {
			out = append(out, Violation{Level: LevelHard, Kind: "candidate", Unit: Ptr(u.ID), Teacher: Ptr(t),
				Message: fmt.Sprintf("%s 排了 %d，但它不在候选集里", u.Key, t)})
		}
	}

	// 2) 硬约束
	for _, c := range p.Hard {
		switch ct := c.(type) {
		case PairFix:
			t, has := assigned[ct.Unit]
			switch ct.Mode {
			case ModeFix:
				if !has || t != ct.Teacher {
					out = append(out, Violation{Level: LevelHard, Kind: "pair_fix", Unit: Ptr(ct.Unit), Teacher: Ptr(ct.Teacher),
						Message: fmt.Sprintf("单元 %d 必须排老师 %d（%s）", ct.Unit, ct.Teacher, ct.Reason)})
				}
			case ModeBan:
				if has && t == ct.Teacher {
					out = append(out, Violation{Level: LevelHard, Kind: "pair_fix", Unit: Ptr(ct.Unit), Teacher: Ptr(ct.Teacher),
						Message: fmt.Sprintf("单元 %d 禁止排老师 %d（%s）", ct.Unit, ct.Teacher, ct.Reason)})
				}
			}
		case CountBound:
			n := 0
			for _, a := range r.Assignments {
				u, ok := unitByID[a.Unit]
				if !ok {
					continue
				}
				for _, cell := range u.Cells {
					if ct.Scope.MatchCell(a.Teacher, cell) {
						n++
						break
					}
				}
			}
			if (ct.Min >= 0 && n < ct.Min) || (ct.Max >= 0 && n > ct.Max) {
				out = append(out, Violation{Level: LevelHard, Kind: "count_bound",
					Message: fmt.Sprintf("计数约束被违反：实际 %d，要求 [%d, %d]（%s）", n, ct.Min, ct.Max, ct.Reason)})
			}
		case Cover:
			for _, id := range ct.Units {
				if _, ok := assigned[id]; !ok {
					out = append(out, Violation{Level: LevelHard, Kind: "cover", Unit: Ptr(id),
						Message: fmt.Sprintf("单元 %d 未被覆盖（%s）", id, ct.Reason)})
				}
			}
		}
	}

	// 3) 软代价 → 偏差
	for _, c := range p.Costs {
		switch ct := c.(type) {
		case Balance:
			counts := make(map[TeacherID]int, len(ct.Group))
			for _, t := range ct.Group {
				counts[t] = 0
			}
			for _, a := range r.Assignments {
				if _, ok := counts[a.Teacher]; ok {
					counts[a.Teacher]++
				}
			}
			min, max := -1, -1
			for _, t := range ct.Group {
				n := counts[t]
				if min == -1 || n < min {
					min = n
				}
				if n > max {
					max = n
				}
			}
			if min >= 0 && max-min > 0 {
				out = append(out, Violation{Level: LevelSoft, Kind: "balance",
					Message: fmt.Sprintf("次数不均：最多 %d 次、最少 %d 次，偏差 %d", max, min, max-min),
					Delta:   float64(max - min)})
			}
		case Spread, Prefer:
			// v1 不判这两个原语的软偏差：它们留给求解器消费。
		}
	}

	return out
}

func containsTeacher(list []TeacherID, t TeacherID) bool {
	for _, v := range list {
		if v == t {
			return true
		}
	}
	return false
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/plan/ -run 'TestCheck' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/plan/check.go internal/plan/check_test.go
git commit -m "feat(plan): 检查器——用声明式数据复核结果，三级冲突"
```

---

## Task 3: 规则接口与注册表

**Files:**
- Create: `internal/rules/rule.go`
- Test: `internal/rules/rule_test.go`

**Step 1: Write the failing test**

创建 `internal/rules/rule_test.go`：

```go
package rules

import (
	"encoding/json"
	"testing"

	"work-schedule/internal/plan"
)

type fakeRule struct{ weight float64 }

func (r *fakeRule) Meta() Meta {
	return Meta{Type: "fake", Label: "假规则", Mounts: []MountKind{MountGlobal}}
}
func (r *fakeRule) Costs(ctx *BuildCtx, out *CostSet) {
	out.Items = append(out.Items, plan.Prefer{Weight: r.weight, Sign: 1})
}

func TestRegistryRegisterLookup(t *testing.T) {
	Register((&fakeRule{}).Meta(), func(param json.RawMessage) (Rule, error) {
		return &fakeRule{weight: 1}, nil
	})

	f, ok := Lookup("fake")
	if !ok {
		t.Fatal("注册后应能查到 fake")
	}
	inst, err := f(nil)
	if err != nil {
		t.Fatalf("构造实例失败: %v", err)
	}
	if inst.Meta().Type != "fake" {
		t.Fatalf("Type 应为 fake，得到 %s", inst.Meta().Type)
	}
	if _, ok := Lookup("不存在"); ok {
		t.Fatal("未注册的类型不应查到")
	}

	found := false
	for _, m := range Registered() {
		if m.Type == "fake" {
			found = true
		}
	}
	if !found {
		t.Fatal("Registered() 应包含 fake")
	}
}

func TestCandidateSetRemove(t *testing.T) {
	cs := NewCandidateSet([]plan.UnitID{1}, []plan.TeacherID{10, 11})
	if !cs.Allow[1][10] {
		t.Fatal("初始应允许 10")
	}
	cs.Remove(1, 10, "不教主课")
	if cs.Allow[1][10] {
		t.Fatal("Remove 后不应再允许 10")
	}
	if cs.Reason[1][10] != "不教主课" {
		t.Fatalf("应记录原因，得到 %q", cs.Reason[1][10])
	}
	if !cs.Allow[1][11] {
		t.Fatal("11 不应受影响")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/rules/ -run 'TestRegistry|TestCandidateSet' -v`
Expected: FAIL —— `no Go files in .../internal/rules`

**Step 3: Write minimal implementation**

创建 `internal/rules/rule.go`：

```go
// Package rules 存放排班的原子规则：接口、注册表、实现与加载。
//
// 规则只做一件事：把自己的 (type, param) 翻译成 plan 里的声明式数据。
// 它不认识求解器，也不在求解过程中被反复调用——只在构建 Problem 时跑一次。
package rules

import (
	"encoding/json"
	"fmt"

	"work-schedule/internal/plan"
)

// MountKind 规则可挂载的目标类型。
type MountKind string

const (
	MountGlobal  MountKind = "global"
	MountTeacher MountKind = "teacher"
	MountClass   MountKind = "class"
	MountSubject MountKind = "subject"
	MountShift   MountKind = "shift"
	MountMerged  MountKind = "merged_class"
)

// ParamField 声明规则的一个参数，供前端自动生成表单、服务端校验。
type ParamField struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Type    string `json:"type"`              // int | float | string | bool | ref
	RefKind string `json:"refKind,omitempty"` // Type == ref 时的目标实体类型
	Default any    `json:"default,omitempty"`
}

// Meta 规则的元信息：类型名、展示名、可挂载目标、参数表。
type Meta struct {
	Type        string       `json:"type"`
	Label       string       `json:"label"`
	Mounts      []MountKind  `json:"mounts"`
	ParamSchema []ParamField `json:"paramSchema"`
}

// Rule 所有规则的最小接口。
type Rule interface {
	Meta() Meta
}

// CandidateFilter 产出 (a) 候选资格：收紧候选集。
type CandidateFilter interface {
	Filter(ctx *BuildCtx, cand *CandidateSet)
}

// ConstraintSource 产出 (b) 硬约束。
type ConstraintSource interface {
	Constraints(ctx *BuildCtx, out *ConstraintSet)
}

// CostSource 产出 (c) 软代价。
type CostSource interface {
	Costs(ctx *BuildCtx, out *CostSet)
}

// —— 规则实现能看到的输入 ——

// Shift 班次视图。
type Shift struct {
	ID         int64
	Name       string
	Start, End string
}

// Class 班级视图。
type Class struct {
	ID   int64
	Name string
}

// Teacher 老师视图，附带其任教的学科 / 班级。
type Teacher struct {
	ID         int64
	Name       string
	SubjectIDs []int64
	ClassIDs   []int64
}

// Binding 任课关系：老师 × 学科 × 班级。
type Binding struct {
	TeacherID, SubjectID, ClassID int64
}

// MergedClass 合班视图。
type MergedClass struct {
	ID       int64
	Name     string
	ClassIDs []int64
}

// BuildCtx 规则在构建 Problem 时能看到的全部输入。规则不连数据库，只认它。
type BuildCtx struct {
	ScopeID  int64
	Days     []string
	Shifts   []Shift
	Classes  []Class
	Teachers []Teacher
	Merged   []MergedClass
	Bindings []Binding
	Units    []plan.Unit
	Locks    map[plan.UnitID]plan.TeacherID
}

// —— 规则的产出集合 ——

// CandidateSet 候选集。构建时先用全量老师填满，各过滤规则做减法。
type CandidateSet struct {
	Allow  map[plan.UnitID]map[plan.TeacherID]bool
	Reason map[plan.UnitID]map[plan.TeacherID]string
}

// NewCandidateSet 用全量老师填满给定单元。
func NewCandidateSet(units []plan.UnitID, teachers []plan.TeacherID) *CandidateSet {
	cs := &CandidateSet{
		Allow:  make(map[plan.UnitID]map[plan.TeacherID]bool, len(units)),
		Reason: make(map[plan.UnitID]map[plan.TeacherID]string, len(units)),
	}
	for _, u := range units {
		cs.Allow[u] = make(map[plan.TeacherID]bool, len(teachers))
		cs.Reason[u] = make(map[plan.TeacherID]string)
		for _, t := range teachers {
			cs.Allow[u][t] = true
		}
	}
	return cs
}

// Remove 把某老师从某单元的候选里剔除，并记录原因。
func (c *CandidateSet) Remove(u plan.UnitID, t plan.TeacherID, reason string) {
	if _, ok := c.Allow[u]; !ok {
		return
	}
	if !c.Allow[u][t] {
		return
	}
	c.Allow[u][t] = false
	c.Reason[u][t] = reason
}

// ConstraintSet 硬约束集合。
type ConstraintSet struct {
	Items []plan.Constraint
}

// CostSet 软代价集合。
type CostSet struct {
	Items []plan.CostTerm
}

// —— 注册表 ——

// Factory 用已落库的 JSON 参数构造规则实例。
type Factory func(param json.RawMessage) (Rule, error)

type registration struct {
	Meta    Meta
	Factory Factory
}

var registry = map[string]registration{}

// Register 注册一种规则类型。重复注册直接 panic（属编码错误）。
// 约定在包的 init() 里调用。
func Register(meta Meta, f Factory) {
	if meta.Type == "" {
		panic("规则类型名不能为空")
	}
	if _, dup := registry[meta.Type]; dup {
		panic(fmt.Sprintf("规则类型重复注册: %s", meta.Type))
	}
	registry[meta.Type] = registration{Meta: meta, Factory: f}
}

// Lookup 按类型取工厂。
func Lookup(typ string) (Factory, bool) {
	r, ok := registry[typ]
	if !ok {
		return nil, false
	}
	return r.Factory, true
}

// Registered 返回所有已注册规则的元信息，供规则页展示。
func Registered() []Meta {
	out := make([]Meta, 0, len(registry))
	for _, r := range registry {
		out = append(out, r.Meta)
	}
	return out
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/rules/ -run 'TestRegistry|TestCandidateSet' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/rules/rule.go internal/rules/rule_test.go
git commit -m "feat(rules): 规则接口、注册表与 BuildCtx/产出集合"
```

---

## Task 4: schema——t_rule.param 列与幂等迁移

**Files:**
- Modify: `internal/store/schema.sql`（`t_rule` 建表处加 `param TEXT`，索引区加一条）
- Modify: `internal/store/store.go:51-55`（`migrate` 增加幂等补列）
- Test: `internal/store/store_test.go`

**Step 1: Write the failing test**

创建 `internal/store/store_test.go`：

```go
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
	if err := st.DB().Select(&cols, "PRAGMA table_info(t_rule)"); err != nil {
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestOpenAddsRuleParamColumn|TestEnsureColumnIsIdempotent' -v`
Expected: FAIL —— `undefined: st.ensureColumn`，且 `param` 列不存在

**Step 3: Write minimal implementation**

修改 `internal/store/schema.sql` 的 `t_rule` 建表：

```sql
-- 规则正文：type + param(JSON)；fair_count / fair_interval / max_per_day ... 等
CREATE TABLE IF NOT EXISTS t_rule (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_id   INTEGER NOT NULL,
    type       VARCHAR(64) NOT NULL,
    param      TEXT,
    col1       VARCHAR(255),
    col2       VARCHAR(255),
    col3       VARCHAR(255),
    col4       VARCHAR(255),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT,
    UNIQUE (id, scope_id),
    FOREIGN KEY (scope_id) REFERENCES t_scope(id) ON DELETE RESTRICT
);
```

在 schema.sql 末尾索引区追加：

```sql
-- 通用规则挂载（type='rule_mount'，from_id=目标实体，to_id=规则）
CREATE INDEX IF NOT EXISTS idx_mapping_rule_mount
    ON t_mapping(scope_id, to_id) WHERE deleted_at IS NULL AND type = 'rule_mount';
```

修改 `internal/store/store.go` 的 `migrate`：

```go
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
	if err := s.db.Select(&rows, "PRAGMA table_info("+table+")"); err != nil {
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestOpenAddsRuleParamColumn|TestEnsureColumnIsIdempotent' -v`
Expected: PASS

**Step 5: 确认整体仍能编译**

Run: `go build ./...`
Expected: 无输出

**Step 6: Commit**

```bash
git add internal/store/schema.sql internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): t_rule 增加 param 列 + 幂等补列迁移"
```

---

## Task 5: 两条公平规则（fair_count / fair_interval）

**Files:**
- Create: `internal/rules/fair.go`
- Test: `internal/rules/fair_test.go`

**Step 1: Write the failing test**

创建 `internal/rules/fair_test.go`：

```go
package rules

import (
	"encoding/json"
	"testing"

	"work-schedule/internal/plan"
)

func TestFairCountCosts(t *testing.T) {
	f, ok := Lookup("fair_count")
	if !ok {
		t.Fatal("fair_count 应在 init 里注册")
	}
	inst, err := f(json.RawMessage(`{"weight":2}`))
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	out := &CostSet{}
	inst.(*FairCount).Costs(&BuildCtx{Teachers: []Teacher{{ID: 10}, {ID: 11}, {ID: 12}}}, out)

	if len(out.Items) != 1 {
		t.Fatalf("应产出 1 条代价，得到 %d", len(out.Items))
	}
	b, ok := out.Items[0].(plan.Balance)
	if !ok {
		t.Fatalf("应为 Balance，得到 %T", out.Items[0])
	}
	if b.Weight != 2 {
		t.Fatalf("权重应来自 param（2），得到 %v", b.Weight)
	}
	if len(b.Group) != 3 || b.Group[0] != 10 || b.Group[2] != 12 {
		t.Fatalf("Group 应为全部老师，得到 %+v", b.Group)
	}
}

func TestFairIntervalCosts(t *testing.T) {
	f, ok := Lookup("fair_interval")
	if !ok {
		t.Fatal("fair_interval 应在 init 里注册")
	}
	inst, err := f(nil) // 无参数 → 用默认权重
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	out := &CostSet{}
	inst.(*FairInterval).Costs(&BuildCtx{}, out)
	if len(out.Items) != 1 {
		t.Fatalf("应产出 1 条代价，得到 %d", len(out.Items))
	}
	if _, ok := out.Items[0].(plan.Spread); !ok {
		t.Fatalf("应为 Spread，得到 %T", out.Items[0])
	}
}

func TestFairCountMeta(t *testing.T) {
	f, _ := Lookup("fair_count")
	inst, _ := f(nil)
	meta := inst.Meta()
	if meta.Type != "fair_count" || meta.Label == "" {
		t.Fatalf("Meta 不完整: %+v", meta)
	}
	found := false
	for _, m := range meta.Mounts {
		if m == MountGlobal {
			found = true
		}
	}
	if !found {
		t.Fatalf("fair_count 应可挂全局，得到 %+v", meta.Mounts)
	}
	if len(meta.ParamSchema) == 0 {
		t.Fatal("fair_count 应声明参数表，供前端自动生成表单")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/rules/ -run 'TestFair' -v`
Expected: FAIL —— `fair_count` 未注册 / `undefined: FairCount`

**Step 3: Write minimal implementation**

创建 `internal/rules/fair.go`：

```go
package rules

import (
	"encoding/json"

	"work-schedule/internal/plan"
)

// —— fair_count：组内每人次数尽量均等 ——

type fairCountParam struct {
	Weight float64 `json:"weight"`
}

// FairCount 次数均衡规则，产出 Balance。
type FairCount struct {
	weight float64
}

func (r *FairCount) Meta() Meta {
	return Meta{
		Type:   "fair_count",
		Label:  "次数均衡",
		Mounts: []MountKind{MountGlobal, MountTeacher},
		ParamSchema: []ParamField{
			{Name: "weight", Label: "权重", Type: "float", Default: 1.0},
		},
	}
}

func (r *FairCount) Costs(ctx *BuildCtx, out *CostSet) {
	group := make([]plan.TeacherID, 0, len(ctx.Teachers))
	for _, t := range ctx.Teachers {
		group = append(group, plan.TeacherID(t.ID))
	}
	out.Items = append(out.Items, plan.Balance{Group: group, Weight: r.weight})
}

func newFairCount(param json.RawMessage) (Rule, error) {
	p := fairCountParam{Weight: 1}
	if len(param) > 0 {
		if err := json.Unmarshal(param, &p); err != nil {
			return nil, err
		}
	}
	return &FairCount{weight: p.Weight}, nil
}

// —— fair_interval：同一老师的排班尽量在时间上散开 ——

type fairIntervalParam struct {
	Weight float64 `json:"weight"`
}

// FairInterval 间隔均衡规则，产出 Spread。
type FairInterval struct {
	weight float64
}

func (r *FairInterval) Meta() Meta {
	return Meta{
		Type:   "fair_interval",
		Label:  "间隔均衡",
		Mounts: []MountKind{MountGlobal, MountTeacher},
		ParamSchema: []ParamField{
			{Name: "weight", Label: "权重", Type: "float", Default: 1.0},
		},
	}
}

func (r *FairInterval) Costs(ctx *BuildCtx, out *CostSet) {
	out.Items = append(out.Items, plan.Spread{Scope: plan.SelectionScope{}, Weight: r.weight})
}

func newFairInterval(param json.RawMessage) (Rule, error) {
	p := fairIntervalParam{Weight: 1}
	if len(param) > 0 {
		if err := json.Unmarshal(param, &p); err != nil {
			return nil, err
		}
	}
	return &FairInterval{weight: p.Weight}, nil
}

func init() {
	Register((&FairCount{}).Meta(), newFairCount)
	Register((&FairInterval{}).Meta(), newFairInterval)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/rules/ -run 'TestFair' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/rules/fair.go internal/rules/fair_test.go
git commit -m "feat(rules): fair_count / fair_interval 两条公平规则"
```

---

## Task 6: 从库里加载挂载规则

**Files:**
- Create: `internal/rules/loader.go`
- Test: `internal/rules/loader_test.go`

**Step 1: Write the failing test**

创建 `internal/rules/loader_test.go`：

```go
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

func TestLoadGlobalAndPersonal(t *testing.T) {
	st := newTestStore(t)
	seedRule(t, st, 1, "fair_count", `{"weight":2}`, "global_rule", 0)
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/rules/ -run 'TestLoad' -v`
Expected: FAIL —— `undefined: Load`

**Step 3: Write minimal implementation**

创建 `internal/rules/loader.go`：

```go
package rules

import (
	"encoding/json"

	"work-schedule/internal/store"
)

// Mounted 一条已挂载的规则实例。
type Mounted struct {
	RuleID   int64
	Type     string
	Mount    MountKind
	TargetID int64
	Instance Rule
}

// Load 读出某排班下当前生效的规则。
//
// 兼容三种挂载连线：
//   - global_rule   → 全局（target = 0）
//   - personal_rule → 老师（target = t_mapping.from_id）
//   - rule_mount    → 通用挂载，目标种类由目标实体的 type 决定
//
// 认不出的规则类型直接跳过（规则被删、连线残留时不至于炸掉整个求解）。
func Load(st *store.Store, scopeID int64) ([]Mounted, error) {
	rows := []struct {
		RuleID   int64  `db:"rule_id"`
		Type     string `db:"type"`
		Param    string `db:"param"`
		Mount    string `db:"mount"`
		Target   int64  `db:"target"`
		TargetTy string `db:"target_type"`
	}{}
	const q = `
SELECT r.id AS rule_id, r.type AS type, COALESCE(r.param, '') AS param,
       COALESCE(m.type, 'global_rule') AS mount,
       COALESCE(m.from_id, 0) AS target,
       COALESCE(d.type, '') AS target_type
FROM t_rule r
LEFT JOIN t_mapping m ON m.scope_id = r.scope_id AND m.rule_id = r.id AND m.deleted_at IS NULL
LEFT JOIN t_dataset d ON d.scope_id = r.scope_id AND d.id = m.from_id AND d.deleted_at IS NULL
WHERE r.scope_id = ? AND r.deleted_at IS NULL
ORDER BY r.id`
	if err := st.DB().Select(&rows, q, scopeID); err != nil {
		return nil, err
	}

	out := make([]Mounted, 0, len(rows))
	for _, row := range rows {
		factory, ok := Lookup(row.Type)
		if !ok {
			continue // 未知类型跳过
		}
		inst, err := factory(json.RawMessage(row.Param))
		if err != nil {
			return nil, err
		}
		out = append(out, Mounted{
			RuleID:   row.RuleID,
			Type:     row.Type,
			Mount:    mountKind(row.Mount, row.TargetTy),
			TargetID: row.Target,
			Instance: inst,
		})
	}
	return out, nil
}

// mountKind 把库里的连线类型翻译成 MountKind。
func mountKind(mappingType, targetType string) MountKind {
	switch mappingType {
	case "global_rule":
		return MountGlobal
	case "personal_rule":
		return MountTeacher
	case "rule_mount":
		if targetType == "" {
			return MountGlobal // from_id 为空即全局
		}
		return MountKind(targetType) // 目标实体的 type 就是 MountKind
	}
	return MountGlobal
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/rules/ -v`
Expected: PASS（Task 3/5/6 的测试一起过）

**Step 5: Commit**

```bash
git add internal/rules/loader.go internal/rules/loader_test.go
git commit -m "feat(rules): 从 t_rule + 挂载连线加载当前生效规则"
```

---

## Task 7: 引擎——加载元数据图

**Files:**
- Create: `internal/engine/curriculum.go`
- Test: `internal/engine/curriculum_test.go`

**Step 1: Write the failing test**

创建 `internal/engine/curriculum_test.go`：

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run 'TestLoadCurriculum' -v`
Expected: FAIL —— `no Go files in .../internal/engine`

**Step 3: Write minimal implementation**

创建 `internal/engine/curriculum.go`：

```go
// Package engine 把库里的元数据 + 挂载规则组装成 plan.Problem。
//
// 它位于 plan 与 rules 之上：plan 提供契约，rules 把规则翻译成契约数据，
// engine 负责读库、组装 BuildCtx、跑规则、拼出 Problem。
package engine

import (
	"sort"

	"work-schedule/internal/rules"
	"work-schedule/internal/store"
)

// Curriculum 一个排班范围内的元数据图。
type Curriculum struct {
	ScopeID  int64
	Teachers []rules.Teacher
	Classes  []rules.Class
	Shifts   []rules.Shift
	Merged   []rules.MergedClass
	Bindings []rules.Binding
	Days     []string
}

// LoadCurriculum 读出一个范围内排班所需的全部元数据。
func LoadCurriculum(st *store.Store, scopeID int64) (*Curriculum, error) {
	c := &Curriculum{ScopeID: scopeID}

	entities := []struct {
		ID    int64  `db:"id"`
		Type  string `db:"type"`
		Name  string `db:"name"`
		Start string `db:"start_time"`
		End   string `db:"end_time"`
	}{}
	if err := st.DB().Select(&entities, `
		SELECT id, type, COALESCE(col1, '') AS name,
		       COALESCE(col2, '') AS start_time, COALESCE(col3, '') AS end_time
		FROM t_dataset
		WHERE scope_id = ? AND deleted_at IS NULL AND type IN ('teacher','class','shift','merged_class')
		ORDER BY id`, scopeID); err != nil {
		return nil, err
	}
	for _, e := range entities {
		switch e.Type {
		case "teacher":
			c.Teachers = append(c.Teachers, rules.Teacher{ID: e.ID, Name: e.Name})
		case "class":
			c.Classes = append(c.Classes, rules.Class{ID: e.ID, Name: e.Name})
		case "shift":
			c.Shifts = append(c.Shifts, rules.Shift{ID: e.ID, Name: e.Name, Start: e.Start, End: e.End})
		case "merged_class":
			c.Merged = append(c.Merged, rules.MergedClass{ID: e.ID, Name: e.Name})
		}
	}

	// 任课：老师 × 学科 × 班级 → 把学科 / 班级挂到老师上
	bindings := []struct {
		TeacherID int64 `db:"teacher_id"`
		SubjectID int64 `db:"subject_id"`
		ClassID   int64 `db:"class_id"`
	}{}
	if err := st.DB().Select(&bindings, `
		SELECT m.from_id AS teacher_id, bs.to_id AS subject_id, bc.to_id AS class_id
		FROM t_dataset b
		JOIN t_mapping m  ON m.scope_id = b.scope_id AND m.type = 'teacher_binding' AND m.to_id = b.id AND m.deleted_at IS NULL
		JOIN t_mapping bs ON bs.scope_id = b.scope_id AND bs.type = 'binding_subject' AND bs.from_id = b.id AND bs.deleted_at IS NULL
		JOIN t_mapping bc ON bc.scope_id = b.scope_id AND bc.type = 'binding_class' AND bc.from_id = b.id AND bc.deleted_at IS NULL
		WHERE b.scope_id = ? AND b.type = 'binding' AND b.deleted_at IS NULL
		ORDER BY b.id`, scopeID); err != nil {
		return nil, err
	}
	tIndex := make(map[int64]int, len(c.Teachers))
	for i, t := range c.Teachers {
		tIndex[t.ID] = i
	}
	for _, b := range bindings {
		c.Bindings = append(c.Bindings, rules.Binding{TeacherID: b.TeacherID, SubjectID: b.SubjectID, ClassID: b.ClassID})
		if i, ok := tIndex[b.TeacherID]; ok {
			c.Teachers[i].SubjectIDs = appendOnce(c.Teachers[i].SubjectIDs, b.SubjectID)
			c.Teachers[i].ClassIDs = appendOnce(c.Teachers[i].ClassIDs, b.ClassID)
		}
	}
	sort.Slice(c.Teachers, func(i, j int) bool { return c.Teachers[i].ID < c.Teachers[j].ID })

	// 合班成员
	members := []struct {
		MergedID int64 `db:"merged_id"`
		ClassID  int64 `db:"class_id"`
	}{}
	if err := st.DB().Select(&members, `
		SELECT m.from_id AS merged_id, m.to_id AS class_id
		FROM t_mapping m
		JOIN t_dataset c ON c.scope_id = m.scope_id AND c.id = m.to_id AND c.deleted_at IS NULL
		WHERE m.scope_id = ? AND m.type = 'merged_class_member' AND m.deleted_at IS NULL
		ORDER BY m.from_id, c.id`, scopeID); err != nil {
		return nil, err
	}
	mIndex := make(map[int64]int, len(c.Merged))
	for i, m := range c.Merged {
		mIndex[m.ID] = i
	}
	for _, m := range members {
		if i, ok := mIndex[m.MergedID]; ok {
			c.Merged[i].ClassIDs = append(c.Merged[i].ClassIDs, m.ClassID)
		}
	}

	// 排班日
	if err := st.DB().Select(&c.Days, `
		SELECT day FROM t_schedule_day WHERE scope_id = ? AND deleted_at IS NULL ORDER BY day`, scopeID); err != nil {
		return nil, err
	}

	return c, nil
}

func appendOnce(list []int64, v int64) []int64 {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run 'TestLoadCurriculum' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/engine/curriculum.go internal/engine/curriculum_test.go
git commit -m "feat(engine): 加载排班元数据图 Curriculum"
```

---

## Task 8: 引擎——组装 Problem

**Files:**
- Create: `internal/engine/build.go`
- Test: `internal/engine/build_test.go`

**Step 1: Write the failing test**

创建 `internal/engine/build_test.go`：

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run 'TestBuildProblem' -v`
Expected: FAIL —— `undefined: BuildProblem`

**Step 3: Write minimal implementation**

创建 `internal/engine/build.go`：

```go
package engine

import (
	"fmt"

	"work-schedule/internal/plan"
	"work-schedule/internal/rules"
	"work-schedule/internal/store"
)

// BuildProblem 读元数据 + 挂载规则，产出求解器与检查器共用的 Problem。
func BuildProblem(st *store.Store, scopeID int64) (*plan.Problem, error) {
	cur, err := LoadCurriculum(st, scopeID)
	if err != nil {
		return nil, err
	}

	// 1) 待排单元：排班日 × 班次 × 班级。合班尚未参与（每单元恒 1 格）。
	units := make([]plan.Unit, 0, len(cur.Days)*len(cur.Shifts)*len(cur.Classes))
	for _, day := range cur.Days {
		for _, sh := range cur.Shifts {
			for _, cl := range cur.Classes {
				units = append(units, plan.Unit{
					ID:    plan.UnitID(len(units) + 1),
					Key:   fmt.Sprintf("%s|%d|%d", day, sh.ID, cl.ID),
					Cells: []plan.Cell{{Day: day, ShiftID: sh.ID, ClassID: cl.ID}},
				})
			}
		}
	}

	unitIDs := make([]plan.UnitID, 0, len(units))
	teacherIDs := make([]plan.TeacherID, 0, len(cur.Teachers))
	for _, u := range units {
		unitIDs = append(unitIDs, u.ID)
	}
	for _, t := range cur.Teachers {
		teacherIDs = append(teacherIDs, plan.TeacherID(t.ID))
	}

	// 2) 人工锁定：t_assignment.locked = 1
	locks, err := loadLocks(st, scopeID, units)
	if err != nil {
		return nil, err
	}

	ctx := &rules.BuildCtx{
		ScopeID:  scopeID,
		Days:     cur.Days,
		Shifts:   cur.Shifts,
		Classes:  cur.Classes,
		Teachers: cur.Teachers,
		Merged:   cur.Merged,
		Bindings: cur.Bindings,
		Units:    units,
		Locks:    locks,
	}

	// 3) 跑规则：候选集默认全员，各规则做减法和追加。
	cand := rules.NewCandidateSet(unitIDs, teacherIDs)
	hard := &rules.ConstraintSet{}
	costs := &rules.CostSet{}

	mounted, err := rules.Load(st, scopeID)
	if err != nil {
		return nil, err
	}
	for _, m := range mounted {
		if f, ok := m.Instance.(rules.CandidateFilter); ok {
			f.Filter(ctx, cand)
		}
		if s, ok := m.Instance.(rules.ConstraintSource); ok {
			s.Constraints(ctx, hard)
		}
		if s, ok := m.Instance.(rules.CostSource); ok {
			s.Costs(ctx, costs)
		}
	}

	// 4) 值班指令 t_duty → CountBound
	dutyBounds, err := loadDutyBounds(st, scopeID)
	if err != nil {
		return nil, err
	}
	hard.Items = append(hard.Items, dutyBounds...)

	// 5) 锁定 → PairFix(Fix)
	for u, t := range locks {
		hard.Items = append(hard.Items, plan.PairFix{Unit: u, Teacher: t, Mode: plan.ModeFix, Reason: "人工锁定"})
	}

	// 6) 每格必须有老师
	hard.Items = append(hard.Items, plan.Cover{Units: unitIDs, Reason: "每格都要有人"})

	// 7) 候选集收窄为切片
	candidates := make(map[plan.UnitID][]plan.TeacherID, len(cand.Allow))
	reason := make(map[plan.UnitID]map[plan.TeacherID]string, len(cand.Allow))
	for u, m := range cand.Allow {
		kept := make([]plan.TeacherID, 0, len(m))
		for tid, ok := range m {
			if ok {
				kept = append(kept, tid)
			}
		}
		candidates[u] = kept
		if r, ok := cand.Reason[u]; ok && len(r) > 0 {
			reason[u] = r
		}
	}

	return &plan.Problem{
		Units:      units,
		Candidates: candidates,
		Hard:       hard.Items,
		Costs:      costs.Items,
		Locks:      locks,
		Reason:     reason,
	}, nil
}

// loadLocks 读人工锁定的格子，映射到单元。
func loadLocks(st *store.Store, scopeID int64, units []plan.Unit) (map[plan.UnitID]plan.TeacherID, error) {
	rows := []struct {
		Day       string `db:"day"`
		ShiftID   int64  `db:"shift_id"`
		ClassID   int64  `db:"class_id"`
		TeacherID int64  `db:"teacher_id"`
	}{}
	if err := st.DB().Select(&rows, `
		SELECT day, shift_id, class_id, teacher_id
		FROM t_assignment
		WHERE scope_id = ? AND version_id IS NULL AND deleted_at IS NULL
		  AND locked = 1 AND teacher_id IS NOT NULL`, scopeID); err != nil {
		return nil, err
	}
	byCell := make(map[plan.Cell]plan.UnitID, len(units))
	for _, u := range units {
		for _, c := range u.Cells {
			byCell[c] = u.ID
		}
	}
	locks := make(map[plan.UnitID]plan.TeacherID, len(rows))
	for _, r := range rows {
		if uid, ok := byCell[plan.Cell{Day: r.Day, ShiftID: r.ShiftID, ClassID: r.ClassID}]; ok {
			locks[uid] = plan.TeacherID(r.TeacherID)
		}
	}
	return locks, nil
}

// loadDutyBounds 把 t_duty 翻译成计数约束。值班指令不带班级，
// 所以语义是"该老师在该时段至少 / 最多被排 N 次"。
func loadDutyBounds(st *store.Store, scopeID int64) ([]plan.Constraint, error) {
	rows := []struct {
		Day       string `db:"day"`
		ShiftID   int64  `db:"shift_id"`
		TeacherID int64  `db:"teacher_id"`
		Required  int64  `db:"required"`
	}{}
	if err := st.DB().Select(&rows, `
		SELECT d.day AS day, d.shift_id AS shift_id, d.teacher_id AS teacher_id, d.required AS required
		FROM t_duty d
		JOIN t_dataset t ON t.scope_id = d.scope_id AND t.id = d.teacher_id AND t.deleted_at IS NULL
		WHERE d.scope_id = ? AND d.deleted_at IS NULL`, scopeID); err != nil {
		return nil, err
	}
	out := make([]plan.Constraint, 0, len(rows))
	for _, r := range rows {
		day := r.Day
		shiftID := r.ShiftID
		teacher := plan.TeacherID(r.TeacherID)
		scope := plan.SelectionScope{Teacher: &teacher, Day: &day, ShiftID: &shiftID}
		if r.Required == 1 {
			out = append(out, plan.CountBound{Scope: scope, Min: 1, Max: -1, Reason: "指定值班"})
		} else {
			out = append(out, plan.CountBound{Scope: scope, Min: -1, Max: 0, Reason: "指定不值班"})
		}
	}
	return out, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/engine/build.go internal/engine/build_test.go
git commit -m "feat(engine): 组装 Problem（单元/候选/锁定/值班/公平代价）"
```

---

## Task 9: ScheduleService 与接线

**Files:**
- Create: `internal/services/schedule.go`
- Test: `internal/services/schedule_test.go`
- Modify: `main.go:29-33`（注册新服务）

**Step 1: Write the failing test**

创建 `internal/services/schedule_test.go`：

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/services/ -run 'TestSetListClearAssignment|TestRuleTypesIncludesFairRules' -v`
Expected: FAIL —— `undefined: NewScheduleService`

**Step 3: Write minimal implementation**

创建 `internal/services/schedule.go`：

```go
package services

import (
	"database/sql"
	"errors"
	"sort"

	"work-schedule/internal/engine"
	"work-schedule/internal/plan"
	"work-schedule/internal/rules"
	"work-schedule/internal/store"
)

// ScheduleService 暴露排班核心能力：规则元信息、结果读写、结果复核。
type ScheduleService struct {
	store *store.Store
}

func NewScheduleService(st *store.Store) *ScheduleService {
	return &ScheduleService{store: st}
}

// RuleView 一种可用规则的元信息，供规则页生成列表与表单。
type RuleView struct {
	Type        string             `json:"type"`
	Label       string             `json:"label"`
	Mounts      []string           `json:"mounts"`
	ParamSchema []rules.ParamField `json:"paramSchema"`
}

// AssignmentView 一个课表格子。
type AssignmentView struct {
	Day       string `db:"day" json:"day"`
	ShiftID   int64  `db:"shift_id" json:"shiftId"`
	ClassID   int64  `db:"class_id" json:"classId"`
	TeacherID int64  `db:"teacher_id" json:"teacherId"`
	Locked    bool   `db:"locked" json:"locked"`
}

// RuleTypes 列出所有已注册的规则类型（按 type 排序，保证顺序稳定）。
func (s *ScheduleService) RuleTypes() []RuleView {
	metas := rules.Registered()
	out := make([]RuleView, 0, len(metas))
	for _, m := range metas {
		mounts := make([]string, 0, len(m.Mounts))
		for _, mk := range m.Mounts {
			mounts = append(mounts, string(mk))
		}
		out = append(out, RuleView{Type: m.Type, Label: m.Label, Mounts: mounts, ParamSchema: m.ParamSchema})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// ListAssignments 列出课表格子。versionID 为 0 表示当前结果，否则是某版本快照。
func (s *ScheduleService) ListAssignments(scopeID int64, versionID int64) ([]AssignmentView, error) {
	out := []AssignmentView{}
	var version any
	if versionID != 0 {
		version = versionID
	}
	err := s.store.DB().Select(&out, `
		SELECT day, shift_id, class_id, COALESCE(teacher_id, 0) AS teacher_id, locked
		FROM t_assignment
		WHERE scope_id = ? AND version_id IS ? AND deleted_at IS NULL
		ORDER BY day, shift_id, class_id`, scopeID, version)
	return out, err
}

// SetAssignment 写入/更新一个格子。teacherID 为 0 表示清空该格（保留行）。
// locked 为真时求解器不得改动它。
func (s *ScheduleService) SetAssignment(scopeID int64, day string, shiftID, classID, teacherID int64, locked bool) error {
	if day == "" || shiftID == 0 || classID == 0 {
		return errors.New("缺少日期 / 班次 / 班级")
	}
	now := nowMS()
	lockedInt := 0
	if locked {
		lockedInt = 1
	}
	var teacher any
	if teacherID != 0 {
		teacher = teacherID
	}

	var id int64
	err := s.store.DB().Get(&id, `
		SELECT id FROM t_assignment
		WHERE scope_id = ? AND version_id IS NULL AND day = ? AND shift_id = ? AND class_id = ? AND deleted_at IS NULL`,
		scopeID, day, shiftID, classID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = s.store.DB().Exec(`
			INSERT INTO t_assignment(scope_id, day, shift_id, class_id, teacher_id, locked, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, scopeID, day, shiftID, classID, teacher, lockedInt, now, now)
		return err
	case err != nil:
		return err
	default:
		_, err = s.store.DB().Exec(`
			UPDATE t_assignment SET teacher_id = ?, locked = ?, updated_at = ? WHERE id = ?`,
			teacher, lockedInt, now, id)
		return err
	}
}

// ClearAssignment 软删一个格子。
func (s *ScheduleService) ClearAssignment(scopeID int64, day string, shiftID, classID int64) error {
	now := nowMS()
	_, err := s.store.DB().Exec(`
		UPDATE t_assignment SET deleted_at = ?, updated_at = ?
		WHERE scope_id = ? AND version_id IS NULL AND day = ? AND shift_id = ? AND class_id = ? AND deleted_at IS NULL`,
		now, now, scopeID, day, shiftID, classID)
	return err
}

// Check 对当前结果做一次复核，返回冲突清单。只读，不重排。
func (s *ScheduleService) Check(scopeID int64) ([]plan.Violation, error) {
	p, err := engine.BuildProblem(s.store, scopeID)
	if err != nil {
		return nil, err
	}
	rows, err := s.ListAssignments(scopeID, 0)
	if err != nil {
		return nil, err
	}
	res := &plan.Result{}
	for _, a := range rows {
		if a.TeacherID == 0 {
			continue
		}
		u, ok := p.UnitForCell(plan.Cell{Day: a.Day, ShiftID: a.ShiftID, ClassID: a.ClassID})
		if !ok {
			continue // 该格子不在本次排班范围
		}
		res.Assignments = append(res.Assignments, plan.Assignment{
			Unit: u.ID, Teacher: plan.TeacherID(a.TeacherID), Locked: a.Locked,
		})
	}
	return plan.Check(p, res), nil
}
```

**Step 4: 在 `main.go` 注册新服务**

把 `Services` 列表改为：

```go
		Services: []application.Service{
			application.NewService(services.NewScopeService(st)),
			application.NewService(services.NewMetadataService(st)),
			application.NewService(services.NewCalendarService(st)),
			application.NewService(services.NewScheduleService(st)),
		},
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/services/ -run 'TestSetListClearAssignment|TestRuleTypesIncludesFairRules' -v`
Expected: PASS

**Step 6: 生成前端绑定**

Run: `wails3 generate bindings -ts -i`
Expected: `frontend/bindings/.../scheduleservice.ts` 等新文件生成。
（若报缺 `frontend/dist`，先跑 `npm run build --prefix frontend` 再重试；此步失败**不阻塞**前面所有后端测试。）

**Step 7: Commit**

```bash
git add internal/services/schedule.go internal/services/schedule_test.go main.go frontend/bindings
git commit -m "feat(services): ScheduleService——规则元信息/结果读写/实时复核"
```

---

## 收尾验证

Run: `go test ./...`
Expected: PASS（`internal/plan`、`internal/rules`、`internal/store`、`internal/engine`、`internal/services` 全绿）

Run: `go build ./...`
Expected: 无输出

## 本计划范围与后续

**本计划有意拆出去的（不是 spec 排除的）：**

- **贪心求解器**（`internal/solver` + `Solve` 落库）—— spec 实施顺序**第 5 步**，另出一份计划；
- **排班结果页 / 规则页前端** —— spec 第 4 步里的前端部分，另开会话（依赖本计划冻结的契约形状）。

**spec 明确列为范围外（业务未定 / 本次不做）：**

- **合班参与求解** —— 业务规则未定，`Unit.Cells` 已预留接口；
- **具体业务规则**（主课班、按学科轮换等）—— 等业务给规则，架构只保证"能快速加"；
- **`t_rule` 版本化 / 规则快照** —— 本次版本只针对结果；
- **or-tools / 第二条算法** —— 接口已留，需要时再接。

