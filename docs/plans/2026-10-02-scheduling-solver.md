# 排班求解器 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 加上可拔插的求解器（本期只做贪心 `greedy-swap`），让"一键排 + 手改 + 体检"闭环完整。

**Architecture:** 求解器只消费 `plan.Problem`、产出 `plan.Result`，是**纯函数**（不碰数据库、不写库）；因此任何算法都能拿同一份 `Problem` 单测、互相比对。本期先补一条缺失的结构约束原语 `ExclusiveSlot`（否则"一人同一时段只能一格"既没约束也查不出），再实现贪心 + 局部交换，最后由 `ScheduleService.Solve` 落库并复核。

**Tech Stack:** Go 1.27、标准库 `context` / `sort` / `testing`。

**依据 spec:** `docs/specs/2026-10-02-scheduling-architecture-spec.md`（实施顺序**第 5 步**）

**前置：** 后端核心计划已落地并合并（`internal/plan`、`internal/rules`、`internal/engine`、`internal/services/schedule.go`，测试全绿）。

---

## 对 spec 的一处修正（重要）

spec 的硬约束词汇只有 3 个（`CountBound` / `PairFix` / `Cover`），并把"一人同一时段只能一格"写成 `CountBound{teacher:T, slot:(D,S), Max:1}` 的例子——**但没有任何一层真正发射这条约束**，当前 `Problem.Hard` 里只有 duty 计数、锁定、Cover。结果是：求解器不知道这条规则，检查器也查不出来。

本期补一个**结构约束原语**：

```go
type ExclusiveSlot struct{ Reason string }   // 同一老师在同一 (天,班次) 下最多被排到一个单元
```

**为什么不按 spec 字面用 `CountBound` 逐条发射？** 那会生成 `老师数 × 天数 × 班次数` 条约束（几十老师 × 上百天 × 3 班次 ≈ 上万条），而现有检查器对每条 `CountBound` 都要扫一遍全部分配（O(约束 × 分配)）——"实时体检"会卡。`ExclusiveSlot` 一条搞定，检查器一次线性扫描即可。合班也不需要特例：一个单元覆盖多个格子，仍然只算"一个单元"。

因此 **spec 的硬约束词汇从 3 个变 4 个**（本计划会同步改 spec）。

---

## 约定

- 测试文件与被测文件同目录，命名 `xxx_test.go`。
- 跑测试：`go test ./internal/<pkg>/ -run <TestName> -v`；全量 `go test ./internal/...`。
- **不要用 `go build ./...`** —— 会撞到 `build/ios`（Wails 移动端构建目录里的 main 包，缺 `main` 函数，属既有问题）。用 `go build .` 或 `go test ./internal/...`。
- 每个任务结束都 commit。

---

## Task 1: 结构约束原语 `ExclusiveSlot`

**Files:**
- Modify: `internal/plan/problem.go`（新增 `ExclusiveSlot`）
- Modify: `internal/plan/check.go`（复核 `ExclusiveSlot`）
- Modify: `internal/engine/build.go:92-93`（发射该约束）
- Test: `internal/plan/check_test.go`、`internal/engine/build_test.go`（追加）

**Step 1: Write the failing tests**

在 `internal/plan/check_test.go` 末尾追加：

```go
func TestCheckExclusiveSlot(t *testing.T) {
	p := &Problem{
		Units: []Unit{
			{ID: 1, Key: "d1|1|c1", Cells: []Cell{{Day: "d1", ShiftID: 1, ClassID: 1}}},
			{ID: 2, Key: "d1|1|c2", Cells: []Cell{{Day: "d1", ShiftID: 1, ClassID: 2}}},
		},
		Candidates: map[UnitID][]TeacherID{1: {10, 11}, 2: {10, 11}},
		Hard:       []Constraint{ExclusiveSlot{Reason: "测试"}},
	}
	// 同一老师同一时段被排到两个单元 → 违规
	vs := Check(p, &Result{Assignments: []Assignment{{Unit: 1, Teacher: 10}, {Unit: 2, Teacher: 10}}})
	if !hasViolation(vs, "exclusive_slot") {
		t.Fatalf("期望 exclusive_slot 违规，得到 %+v", vs)
	}
	// 不同老师 → 不违规
	vs = Check(p, &Result{Assignments: []Assignment{{Unit: 1, Teacher: 10}, {Unit: 2, Teacher: 11}}})
	if hasViolation(vs, "exclusive_slot") {
		t.Fatalf("不同老师不应违规，得到 %+v", vs)
	}
	// 同一单元跨多个格子（合班）不算违规
	p2 := &Problem{
		Units: []Unit{{ID: 1, Key: "m", Cells: []Cell{
			{Day: "d1", ShiftID: 1, ClassID: 1},
			{Day: "d1", ShiftID: 1, ClassID: 2},
		}}},
		Candidates: map[UnitID][]TeacherID{1: {10}},
		Hard:       []Constraint{ExclusiveSlot{}},
	}
	vs = Check(p2, &Result{Assignments: []Assignment{{Unit: 1, Teacher: 10}}})
	if hasViolation(vs, "exclusive_slot") {
		t.Fatalf("同一单元多格子不应违规，得到 %+v", vs)
	}
}
```

在 `internal/engine/build_test.go` 末尾追加：

```go
func TestBuildProblemHasExclusiveSlot(t *testing.T) {
	st := newTestStore(t)
	_ = seedDataset(t, st, "teacher", "张老师")
	_ = seedDataset(t, st, "class", "高二3班")
	_ = seedDataset(t, st, "shift", "晚1")
	if _, err := st.DB().Exec(
		`INSERT INTO t_schedule_day(scope_id, day, created_at, updated_at) VALUES(1, '2026-10-06', 1, 1)`); err != nil {
		t.Fatalf("插入排班日失败: %v", err)
	}

	p, err := BuildProblem(st, 1)
	if err != nil {
		t.Fatalf("BuildProblem 失败: %v", err)
	}
	found := false
	for _, c := range p.Hard {
		if _, ok := c.(plan.ExclusiveSlot); ok {
			found = true
		}
	}
	if !found {
		t.Fatalf("Problem.Hard 应含 ExclusiveSlot，得到 %+v", p.Hard)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/plan/ -run TestCheckExclusiveSlot -v`
Expected: FAIL —— `undefined: ExclusiveSlot`

**Step 3: Write minimal implementation**

在 `internal/plan/problem.go` 的 `Cover` 之后新增：

```go
// ExclusiveSlot 结构约束：同一老师在同一 (天, 班次) 下最多被排到一个单元。
//
// 合班时一个单元覆盖多个格子，仍然只算"一个单元"，所以这里不需要特例。
// 单列成一个原语（而不是按 (老师,天,班次) 铺开成上万条 CountBound），
// 是为了让检查器一次线性扫描就能复核。
type ExclusiveSlot struct {
	Reason string `json:"reason,omitempty"`
}

func (ExclusiveSlot) isConstraint() {}
```

在 `internal/plan/check.go` 的硬约束 `switch ct := c.(type)` 里（`case Cover` 之后）新增：

```go
		case ExclusiveSlot:
			type slotKey struct {
				t     TeacherID
				day   string
				shift int64
			}
			occ := map[slotKey]UnitID{}
			reported := map[slotKey]bool{}
			for _, a := range r.Assignments {
				u, ok := unitByID[a.Unit]
				if !ok {
					continue
				}
				for _, cell := range u.Cells {
					k := slotKey{a.Teacher, cell.Day, cell.ShiftID}
					prev, dup := occ[k]
					if !dup {
						occ[k] = a.Unit
						continue
					}
					if prev != a.Unit && !reported[k] {
						reported[k] = true
						out = append(out, Violation{Level: LevelHard, Kind: "exclusive_slot", Teacher: Ptr(a.Teacher),
							Message: fmt.Sprintf("老师 %d 在 %s 的班次 %d 被排到了多个单元（%d、%d）", a.Teacher, cell.Day, cell.ShiftID, prev, a.Unit)})
					}
				}
			}
```

在 `internal/engine/build.go` 的第 6 步（`Cover`）之后新增：

```go
	// 6.5) 结构约束：同一老师同一时段只能守一个单元（合班由单元的多格子表达，无需特例）
	hard.Items = append(hard.Items, plan.ExclusiveSlot{Reason: "同一老师同一时段只能守一个单元"})
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/plan/ ./internal/engine/ -v`
Expected: PASS（含既有测试）

**Step 5: 同步修正 spec**

在 `docs/specs/2026-10-02-scheduling-architecture-spec.md` 的硬约束原语一节，把"三个原语"改为四个，并加入 `ExclusiveSlot`：

```markdown
type ExclusiveSlot struct{ Reason string }   // 同一老师在同一 (天,班次) 最多一个单元
```

同时把"一人同一时段只能一格 → `CountBound{...Max:1}`"那行改成"→ `ExclusiveSlot`"，并注明：这不是规则产出，而是 engine 每次都发射的**结构约束**。

**Step 6: Commit**

```bash
git add internal/plan/problem.go internal/plan/check.go internal/plan/check_test.go internal/engine/build.go internal/engine/build_test.go docs/specs/2026-10-02-scheduling-architecture-spec.md
git commit -m "feat(plan): 补结构约束原语 ExclusiveSlot（一人同一时段只能一格）"
```

---

## Task 2: 求解器接口与注册表

**Files:**
- Create: `internal/solver/solver.go`
- Test: `internal/solver/solver_test.go`

**Step 1: Write the failing test**

创建 `internal/solver/solver_test.go`：

```go
package solver

import (
	"context"
	"testing"

	"work-schedule/internal/plan"
)

type fakeSolver struct{ name string }

func (f fakeSolver) Name() string           { return f.name }
func (f fakeSolver) Label() string          { return "假算法" }
func (f fakeSolver) Capability() Capability { return Capability{} }
func (f fakeSolver) Deterministic() bool    { return true }
func (f fakeSolver) Solve(ctx context.Context, p *plan.Problem) (*plan.Result, error) {
	return &plan.Result{}, nil
}

func TestRegisterLookupAll(t *testing.T) {
	Register(fakeSolver{name: "b-solver"})
	Register(fakeSolver{name: "a-solver"})

	if _, ok := Lookup("a-solver"); !ok {
		t.Fatal("a-solver 注册后应能查到")
	}
	if _, ok := Lookup("nope"); ok {
		t.Fatal("未注册的名字不应查到")
	}

	all := All()
	// 不假定总数（同包其它文件也会注册算法），只断言：按名字升序 + 两个假算法都在
	for i := 1; i < len(all); i++ {
		if all[i-1].Name() > all[i].Name() {
			t.Fatalf("All 应按名字升序，得到 %+v", all)
		}
	}
	names := map[string]bool{}
	for _, s := range all {
		names[s.Name()] = true
	}
	if !names["a-solver"] || !names["b-solver"] {
		t.Fatalf("All 应包含两个假算法，得到 %+v", names)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("重复注册应 panic")
		}
	}()
	Register(fakeSolver{name: "dup"})
	Register(fakeSolver{name: "dup"})
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/solver/ -run 'TestRegister' -v`
Expected: FAIL —— `no Go files in .../internal/solver`

**Step 3: Write minimal implementation**

创建 `internal/solver/solver.go`：

```go
// Package solver 定义可拔插的求解器：接口、能力声明与按名字注册的注册表。
//
// 求解器只消费 plan.Problem、产出 plan.Result，是纯函数（不碰数据库、不写库），
// 所以任何算法都能拿同一份 Problem 单测、互相比较。
package solver

import (
	"context"
	"sort"

	"work-schedule/internal/plan"
)

// Capability 求解器能力声明：认哪些硬约束原语、哪些代价原语，是否承诺全局最优。
type Capability struct {
	Constraints []string `json:"constraints"`
	Costs       []string `json:"costs"`
	Optimal     bool     `json:"optimal"`
}

// Solver 所有算法都实现它。
type Solver interface {
	Name() string
	Label() string
	Capability() Capability
	Deterministic() bool
	Solve(ctx context.Context, p *plan.Problem) (*plan.Result, error)
}

var registry = map[string]Solver{}

// Register 注册一个算法；名字重复直接 panic（属编码错误）。约定在 init() 里调用。
func Register(s Solver) {
	if s.Name() == "" {
		panic("求解器名字不能为空")
	}
	if _, dup := registry[s.Name()]; dup {
		panic("求解器重复注册: " + s.Name())
	}
	registry[s.Name()] = s
}

// Lookup 按名字取算法。
func Lookup(name string) (Solver, bool) {
	s, ok := registry[name]
	return s, ok
}

// All 返回全部已注册算法，按名字升序（顺序稳定，供前端展示）。
func All() []Solver {
	out := make([]Solver, 0, len(registry))
	for _, s := range registry {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/solver/ -run 'TestRegister' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/solver/solver.go internal/solver/solver_test.go
git commit -m "feat(solver): 求解器接口、能力声明与注册表"
```

---

## Task 3: 贪心 + 局部交换求解器

**Files:**
- Create: `internal/solver/greedy.go`
- Test: `internal/solver/greedy_test.go`

**Step 1: Write the failing test**

创建 `internal/solver/greedy_test.go`：

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/solver/ -run 'TestGreedy' -v`
Expected: FAIL —— `undefined: GreedySwap`

**Step 3: Write minimal implementation**

创建 `internal/solver/greedy.go`：

```go
package solver

import (
	"context"
	"sort"

	"work-schedule/internal/plan"
)

// GreedySwap 贪心 + 局部交换。
//
// 先按候选数升序（MRV，最受限制的先排）逐个单元挑增量代价最小且可行的老师；
// 再对没排上的单元做一轮局部交换：把占住它候选老师的那个单元挪到别的候选上。
type GreedySwap struct{}

func (GreedySwap) Name() string         { return "greedy-swap" }
func (GreedySwap) Label() string        { return "贪心 + 局部交换" }
func (GreedySwap) Deterministic() bool  { return true }

func (GreedySwap) Capability() Capability {
	return Capability{
		Constraints: []string{"CountBound", "PairFix", "Cover", "ExclusiveSlot"},
		Costs:       []string{"Balance", "Spread", "Prefer"},
		Optimal:     false,
	}
}

func init() { Register(GreedySwap{}) }

// slotKey 老师 × 天 × 班次，用于 ExclusiveSlot。
type slotKey struct {
	teacher plan.TeacherID
	day     string
	shift   int64
}

// boundState 一条 CountBound 及其当前计数。
type boundState struct {
	bound plan.CountBound
	n     int
}

type solveState struct {
	p        *plan.Problem
	unitByID map[plan.UnitID]plan.Unit

	candidates map[plan.UnitID][]plan.TeacherID
	banned     map[plan.UnitID]map[plan.TeacherID]bool
	fixed      map[plan.UnitID]plan.TeacherID

	assigned  map[plan.UnitID]plan.TeacherID
	teacherN  map[plan.TeacherID]int
	dayN      map[plan.TeacherID]map[string]int
	occupancy map[slotKey]plan.UnitID
	bounds    []boundState
	exclusive bool

	reason map[plan.UnitID]string
}

func newSolveState(p *plan.Problem) *solveState {
	st := &solveState{
		p:          p,
		unitByID:   make(map[plan.UnitID]plan.Unit, len(p.Units)),
		candidates: make(map[plan.UnitID][]plan.TeacherID, len(p.Units)),
		banned:     make(map[plan.UnitID]map[plan.TeacherID]bool),
		fixed:      make(map[plan.UnitID]plan.TeacherID),
		assigned:   make(map[plan.UnitID]plan.TeacherID),
		teacherN:   make(map[plan.TeacherID]int),
		dayN:       make(map[plan.TeacherID]map[string]int),
		occupancy:  make(map[slotKey]plan.UnitID),
		reason:     make(map[plan.UnitID]string),
	}
	for _, un := range p.Units {
		st.unitByID[un.ID] = un
		// 候选排序，保证结果可复现（Problem.Candidates 是从 map 出来的，顺序不定）
		cs := append([]plan.TeacherID(nil), p.Candidates[un.ID]...)
		sort.Slice(cs, func(i, j int) bool { return cs[i] < cs[j] })
		st.candidates[un.ID] = cs
	}
	for _, c := range p.Hard {
		switch ct := c.(type) {
		case plan.PairFix:
			switch ct.Mode {
			case plan.ModeFix:
				st.fixed[ct.Unit] = ct.Teacher
			case plan.ModeBan:
				if st.banned[ct.Unit] == nil {
					st.banned[ct.Unit] = map[plan.TeacherID]bool{}
				}
				st.banned[ct.Unit][ct.Teacher] = true
			}
		case plan.CountBound:
			st.bounds = append(st.bounds, boundState{bound: ct})
		case plan.ExclusiveSlot:
			st.exclusive = true
		}
	}
	return st
}

// matchesBound 判断"老师 t 排到单元 u"是否计入这条计数约束。
func matchesBound(b plan.CountBound, t plan.TeacherID, u plan.Unit) bool {
	if b.Scope.Teacher != nil && *b.Scope.Teacher != t {
		return false
	}
	for _, cell := range u.Cells {
		if b.Scope.MatchCell(t, cell) {
			return true
		}
	}
	return false
}

func scopeMatchesUnit(s plan.SelectionScope, t plan.TeacherID, u plan.Unit) bool {
	if s.Teacher != nil && *s.Teacher != t {
		return false
	}
	for _, cell := range u.Cells {
		if s.MatchCell(t, cell) {
			return true
		}
	}
	return false
}

func containsTeacher(list []plan.TeacherID, t plan.TeacherID) bool {
	for _, v := range list {
		if v == t {
			return true
		}
	}
	return false
}

// feasible 增量判断把 t 排到 u 是否满足全部硬约束。
func (st *solveState) feasible(u plan.Unit, t plan.TeacherID) bool {
	if st.banned[u.ID][t] {
		return false
	}
	if st.exclusive {
		for _, cell := range u.Cells {
			if cur, ok := st.occupancy[slotKey{t, cell.Day, cell.ShiftID}]; ok && cur != u.ID {
				return false
			}
		}
	}
	for i := range st.bounds {
		b := &st.bounds[i]
		if b.bound.Max < 0 {
			continue
		}
		delta := 0
		if matchesBound(b.bound, t, u) {
			delta = 1
		}
		if b.n+delta > b.bound.Max {
			return false
		}
	}
	return true
}

// assign 落一条排定并更新计数。
func (st *solveState) assign(u plan.Unit, t plan.TeacherID) {
	st.assigned[u.ID] = t
	st.teacherN[t]++
	if st.dayN[t] == nil {
		st.dayN[t] = map[string]int{}
	}
	if st.exclusive {
		for _, cell := range u.Cells {
			st.occupancy[slotKey{t, cell.Day, cell.ShiftID}] = u.ID
		}
	}
	for i := range st.bounds {
		if matchesBound(st.bounds[i].bound, t, u) {
			st.bounds[i].n++
		}
	}
	for _, cell := range u.Cells {
		st.dayN[t][cell.Day]++
	}
}

// unassign 撤销一条排定。
func (st *solveState) unassign(u plan.Unit) {
	t, ok := st.assigned[u.ID]
	if !ok {
		return
	}
	delete(st.assigned, u.ID)
	st.teacherN[t]--
	if st.exclusive {
		for _, cell := range u.Cells {
			k := slotKey{t, cell.Day, cell.ShiftID}
			if cur, ok := st.occupancy[k]; ok && cur == u.ID {
				delete(st.occupancy, k)
			}
		}
	}
	for i := range st.bounds {
		if matchesBound(st.bounds[i].bound, t, u) {
			st.bounds[i].n--
		}
	}
	for _, cell := range u.Cells {
		st.dayN[t][cell.Day]--
	}
}

// cost 把 t 排到 u 的增量代价（越小越好）。
func (st *solveState) cost(u plan.Unit, t plan.TeacherID) float64 {
	var c float64
	for _, ct := range st.p.Costs {
		switch v := ct.(type) {
		case plan.Balance:
			if containsTeacher(v.Group, t) {
				c += v.Weight * float64(st.teacherN[t])
			}
		case plan.Spread:
			if scopeMatchesUnit(v.Scope, t, u) {
				c += v.Weight * float64(st.sameDayCount(t, u))
			}
		case plan.Prefer:
			if scopeMatchesUnit(v.Scope, t, u) {
				if v.Sign > 0 {
					c -= v.Weight
				} else {
					c += v.Weight
				}
			}
		}
	}
	return c
}

func (st *solveState) sameDayCount(t plan.TeacherID, u plan.Unit) int {
	n := 0
	for _, cell := range u.Cells {
		n += st.dayN[t][cell.Day]
	}
	return n
}

// tryAssignBest 给单元挑一个代价最小且可行的老师。avoid 非 0 时跳过该老师。
func (st *solveState) tryAssignBest(u plan.Unit, avoid plan.TeacherID) bool {
	if _, ok := st.assigned[u.ID]; ok {
		return true
	}
	best, bestCost, found := plan.TeacherID(0), 0.0, false
	for _, t := range st.candidates[u.ID] {
		if avoid != 0 && t == avoid {
			continue
		}
		if !st.feasible(u, t) {
			continue
		}
		c := st.cost(u, t)
		if !found || c < bestCost || (c == bestCost && t < best) {
			best, bestCost, found = t, c, true
		}
	}
	if !found {
		if st.reason[u.ID] == "" {
			if len(st.candidates[u.ID]) == 0 {
				st.reason[u.ID] = "没有候选老师"
			} else {
				st.reason[u.ID] = "候选老师都被同时段占用或超出上限"
			}
		}
		return false
	}
	st.assign(u, best)
	return true
}

// trySwapIn 尝试给未排上的单元腾位置：把它候选老师在同时段占用的那个单元挪到别的候选上。
func (st *solveState) trySwapIn(u plan.Unit) bool {
	for _, t := range st.candidates[u.ID] {
		if st.banned[u.ID][t] {
			continue
		}
		var blockers []plan.UnitID
		for _, cell := range u.Cells {
			if cur, ok := st.occupancy[slotKey{t, cell.Day, cell.ShiftID}]; ok && cur != u.ID {
				blockers = append(blockers, cur)
			}
		}
		for _, bid := range blockers {
			if _, isFixed := st.fixed[bid]; isFixed {
				continue // 锁定的不动
			}
			bu, ok := st.unitByID[bid]
			if !ok {
				continue
			}
			old := st.assigned[bid]
			st.unassign(bu)
			if st.tryAssignBest(bu, t) && st.feasible(u, t) {
				st.assign(u, t)
				return true
			}
			// 挪不动 / 挪了也没用 → 还原
			if _, still := st.assigned[bid]; still {
				st.unassign(bu)
			}
			st.assign(bu, old)
		}
	}
	return false
}

// repairMinBounds 补齐"至少 N 次"的计数约束（如指定值班）。补不上就留给检查器报。
func (st *solveState) repairMinBounds() {
	for i := range st.bounds {
		b := &st.bounds[i]
		if b.bound.Min < 0 || b.bound.Scope.Teacher == nil {
			continue
		}
		t := *b.bound.Scope.Teacher
		for b.n < b.bound.Min {
			if !st.forceAssignForBound(b, t) {
				break
			}
		}
	}
}

func (st *solveState) forceAssignForBound(b *boundState, t plan.TeacherID) bool {
	for _, u := range st.p.Units {
		if _, ok := st.assigned[u.ID]; ok {
			continue
		}
		if !matchesBound(b.bound, t, u) {
			continue
		}
		if !containsTeacher(st.candidates[u.ID], t) {
			continue
		}
		if !st.feasible(u, t) {
			continue
		}
		st.assign(u, t)
		return true
	}
	return false
}

// Solve 实现 Solver。
func (g GreedySwap) Solve(ctx context.Context, p *plan.Problem) (*plan.Result, error) {
	st := newSolveState(p)

	// 1) 固定对（人工锁定 / 指定 Fix）先落。落不进去也照留，交给检查器报。
	for _, un := range p.Units {
		if t, ok := st.fixed[un.ID]; ok {
			st.assign(un, t)
		}
	}

	// 2) MRV 顺序：候选少的先排；同序时按 Key，保证确定性。
	order := append([]plan.Unit(nil), p.Units...)
	sort.SliceStable(order, func(i, j int) bool {
		li, lj := len(st.candidates[order[i].ID]), len(st.candidates[order[j].ID])
		if li != lj {
			return li < lj
		}
		return order[i].Key < order[j].Key
	})

	for idx, un := range order {
		if idx%64 == 0 && ctx.Err() != nil {
			st.reason[un.ID] = "求解超时，未处理"
			continue
		}
		st.tryAssignBest(un, 0)
	}

	// 3) 局部交换：救回没排上的单元
	for _, un := range order {
		if _, ok := st.assigned[un.ID]; ok {
			continue
		}
		st.trySwapIn(un)
	}

	// 4) 补齐 Min 类计数约束
	st.repairMinBounds()

	// 5) 组织结果
	res := &plan.Result{Optimal: false}
	for _, un := range p.Units {
		t, ok := st.assigned[un.ID]
		if !ok {
			res.Unassigned = append(res.Unassigned, plan.Unassigned{Unit: un.ID, Reason: st.reason[un.ID]})
			continue
		}
		_, locked := st.fixed[un.ID]
		res.Assignments = append(res.Assignments, plan.Assignment{Unit: un.ID, Teacher: t, Locked: locked})
	}
	res.Score = st.totalCost()
	return res, nil
}

// totalCost 按 CostTerm 聚合目标值，便于横向比较不同算法。
// 求和顺序保持确定（Group 是切片、单元按原顺序、老师 key 先排序），避免浮点顺序差。
func (st *solveState) totalCost() float64 {
	var c float64
	for _, ct := range st.p.Costs {
		switch v := ct.(type) {
		case plan.Balance:
			min, max := -1, -1
			for _, t := range v.Group {
				n := st.teacherN[t]
				if min == -1 || n < min {
					min = n
				}
				if n > max {
					max = n
				}
			}
			if min >= 0 {
				c += v.Weight * float64(max-min)
			}
		case plan.Spread:
			for _, t := range sortedTeacherKeys(st.dayN) {
				if v.Scope.Teacher != nil && *v.Scope.Teacher != t {
					continue
				}
				mx := 0
				for _, n := range st.dayN[t] {
					if n > mx {
						mx = n
					}
				}
				if mx > 1 {
					c += v.Weight * float64(mx-1)
				}
			}
		case plan.Prefer:
			for _, un := range st.p.Units {
				t, ok := st.assigned[un.ID]
				if !ok {
					continue
				}
				if scopeMatchesUnit(v.Scope, t, un) {
					if v.Sign > 0 {
						c -= v.Weight
					} else {
						c += v.Weight
					}
				}
			}
		}
	}
	return c
}

func sortedTeacherKeys(m map[plan.TeacherID]map[string]int) []plan.TeacherID {
	out := make([]plan.TeacherID, 0, len(m))
	for t := range m {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/solver/ -v`
Expected: PASS（Task 2 / 3 的所有测试）

**Step 5: Commit**

```bash
git add internal/solver/greedy.go internal/solver/greedy_test.go
git commit -m "feat(solver): greedy-swap——贪心 + 局部交换"
```

---

## Task 4: `ScheduleService.Solve` 与落库

**Files:**
- Modify: `internal/services/schedule.go`（新增 `Solvers` / `Solve` / `persistResult`）
- Test: `internal/services/schedule_test.go`（追加）

> `main.go` 已经注册了 `ScheduleService`（`main.go:33`），本任务**不用改** `main.go`。

**Step 1: Write the failing test**

在 `internal/services/schedule_test.go` 末尾追加：

```go
func TestSolversListsGreedy(t *testing.T) {
	st := newScheduleStore(t)
	svc := NewScheduleService(st)
	found := false
	for _, s := range svc.Solvers() {
		if s.Name == "greedy-swap" {
			found = true
			if len(s.Capability.Constraints) == 0 {
				t.Fatal("求解器应声明能力")
			}
		}
	}
	if !found {
		t.Fatal("Solvers 应列出 greedy-swap")
	}
}

func TestSolveUnknownSolver(t *testing.T) {
	st := newScheduleStore(t)
	svc := NewScheduleService(st)
	if _, err := svc.Solve(1, "nope"); err == nil {
		t.Fatal("未知求解器应报错")
	}
}

func TestSolveWritesAndPreservesLock(t *testing.T) {
	st := newScheduleStore(t)
	svc := NewScheduleService(st)
	_ = seedDatasetSvc(t, st, "teacher", "张老师")
	teacher2 := seedDatasetSvc(t, st, "teacher", "李老师")
	class1 := seedDatasetSvc(t, st, "class", "高二3班")
	_ = seedDatasetSvc(t, st, "class", "高二4班")
	shift := seedDatasetSvc(t, st, "shift", "晚1")

	if _, err := st.DB().Exec(
		`INSERT INTO t_schedule_day(scope_id, day, created_at, updated_at) VALUES(1, '2026-10-06', 1, 1)`); err != nil {
		t.Fatalf("插入排班日失败: %v", err)
	}
	// 先把「高二3班」锁定给李老师
	if err := svc.SetAssignment(1, "2026-10-06", shift, class1, teacher2, true); err != nil {
		t.Fatalf("锁定失败: %v", err)
	}

	report, err := svc.Solve(1, "") // 空名字 → 用默认 greedy-swap
	if err != nil {
		t.Fatalf("Solve 失败: %v", err)
	}
	if report.Assigned != 2 {
		t.Fatalf("同一天一个班次两个班都应排上，得到 %+v", report)
	}
	for _, v := range report.Violations {
		if v.Level == plan.LevelHard {
			t.Fatalf("可行问题上不该有硬冲突: %+v", v)
		}
	}

	// 锁定格保持不变
	var locked struct {
		TeacherID int64 `db:"teacher_id"`
		Locked    bool  `db:"locked"`
	}
	if err := st.DB().Get(&locked, `
		SELECT teacher_id, locked FROM t_assignment
		WHERE scope_id = 1 AND day = '2026-10-06' AND shift_id = ? AND class_id = ? AND deleted_at IS NULL`,
		shift, class1); err != nil {
		t.Fatalf("查锁定格失败: %v", err)
	}
	if locked.TeacherID != teacher2 || !locked.Locked {
		t.Fatalf("锁定格应保持 teacher2 且 locked，得到 %+v", locked)
	}

	// 同一时段两个班必须由两个不同老师守（ExclusiveSlot）
	var distinct int
	if err := st.DB().Get(&distinct, `
		SELECT COUNT(DISTINCT teacher_id) FROM t_assignment
		WHERE scope_id = 1 AND day = '2026-10-06' AND shift_id = ? AND deleted_at IS NULL`, shift); err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if distinct != 2 {
		t.Fatalf("同时段两格应由两位老师分守，得到 distinct=%d", distinct)
	}
}
```

> 需要在 `schedule_test.go` 的 import 里加上 `work-schedule/internal/plan`。

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/ -run 'TestSolvers|TestSolve' -v`
Expected: FAIL —— `svc.Solvers undefined` / `svc.Solve undefined`

**Step 3: Write minimal implementation**

在 `internal/services/schedule.go` 顶部 import 加入：

```go
	"context"
	"fmt"
	"strings"

	"work-schedule/internal/solver"
```

在文件末尾追加：

```go
// SolverInfo 供前端列出可选算法。
type SolverInfo struct {
	Name          string            `json:"name"`
	Label         string            `json:"label"`
	Capability    solver.Capability `json:"capability"`
	Deterministic bool              `json:"deterministic"`
}

// Solvers 列出所有可用算法。
func (s *ScheduleService) Solvers() []SolverInfo {
	all := solver.All()
	out := make([]SolverInfo, 0, len(all))
	for _, sv := range all {
		out = append(out, SolverInfo{
			Name:          sv.Name(),
			Label:         sv.Label(),
			Capability:    sv.Capability(),
			Deterministic: sv.Deterministic(),
		})
	}
	return out
}

// SolveReport 一次求解的汇总。
type SolveReport struct {
	Assigned   int              `json:"assigned"`
	Unassigned int              `json:"unassigned"`
	Optimal    bool             `json:"optimal"`
	Score      float64          `json:"score"`
	Violations []plan.Violation `json:"violations"`
}

// Solve 求解并落库。solverName 为空时用日历里配置的算法，再空则用默认。
// 锁定格一律不动；无解不报错，未排上的单元进 Unassigned、冲突进 Violations。
func (s *ScheduleService) Solve(scopeID int64, solverName string) (SolveReport, error) {
	name, err := s.resolveSolverName(scopeID, solverName)
	if err != nil {
		return SolveReport{}, err
	}
	sv, ok := solver.Lookup(name)
	if !ok {
		return SolveReport{}, fmt.Errorf("未知求解器: %s", name)
	}

	p, err := engine.BuildProblem(s.store, scopeID)
	if err != nil {
		return SolveReport{}, err
	}
	res, err := sv.Solve(context.Background(), p)
	if err != nil {
		return SolveReport{}, err
	}
	if err := s.persistResult(scopeID, p, res); err != nil {
		return SolveReport{}, err
	}

	return SolveReport{
		Assigned:   len(res.Assignments),
		Unassigned: len(res.Unassigned),
		Optimal:    res.Optimal,
		Score:      res.Score,
		Violations: plan.Check(p, res),
	}, nil
}

// resolveSolverName 依次取：调用方指定 → 日历配置 → 默认。
func (s *ScheduleService) resolveSolverName(scopeID int64, solverName string) (string, error) {
	if name := strings.TrimSpace(solverName); name != "" {
		return name, nil
	}
	var cfg string
	err := s.store.DB().Get(&cfg, `
		SELECT solver FROM t_calendar WHERE scope_id = ? AND deleted_at IS NULL LIMIT 1`, scopeID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return defaultSolver, nil
	case err != nil:
		return "", err
	}
	if strings.TrimSpace(cfg) == "" {
		return defaultSolver, nil
	}
	return cfg, nil
}

// persistResult 把结果写回当前版本的 t_assignment。
// 规则：不在新结果里的格子软删；锁定行一律跳过（不动、不删）。
func (s *ScheduleService) persistResult(scopeID int64, p *plan.Problem, res *plan.Result) error {
	unitByID := make(map[plan.UnitID]plan.Unit, len(p.Units))
	for _, u := range p.Units {
		unitByID[u.ID] = u
	}
	want := make(map[plan.Cell]plan.TeacherID)
	for _, a := range res.Assignments {
		u, ok := unitByID[a.Unit]
		if !ok {
			continue
		}
		for _, c := range u.Cells {
			want[c] = a.Teacher
		}
	}

	now := nowMS()
	tx, err := s.store.DB().Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1) 软删不在新结果里的当前行（锁定行不动）
	current := []struct {
		ID      int64  `db:"id"`
		Day     string `db:"day"`
		ShiftID int64  `db:"shift_id"`
		ClassID int64  `db:"class_id"`
		Locked  bool   `db:"locked"`
	}{}
	if err := tx.Select(&current, `
		SELECT id, day, shift_id, class_id, locked
		FROM t_assignment
		WHERE scope_id = ? AND version_id IS NULL AND deleted_at IS NULL`, scopeID); err != nil {
		return err
	}
	for _, r := range current {
		if r.Locked {
			continue
		}
		if _, keep := want[plan.Cell{Day: r.Day, ShiftID: r.ShiftID, ClassID: r.ClassID}]; keep {
			continue
		}
		if _, err := tx.Exec(`UPDATE t_assignment SET deleted_at = ?, updated_at = ? WHERE id = ?`, now, now, r.ID); err != nil {
			return err
		}
	}

	// 2) 写入新结果（已存在则更新；锁定行不动）
	for cell, t := range want {
		var row struct {
			ID     int64 `db:"id"`
			Locked bool  `db:"locked"`
		}
		err := tx.Get(&row, `
			SELECT id, locked FROM t_assignment
			WHERE scope_id = ? AND version_id IS NULL AND day = ? AND shift_id = ? AND class_id = ? AND deleted_at IS NULL`,
			scopeID, cell.Day, cell.ShiftID, cell.ClassID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := tx.Exec(`
				INSERT INTO t_assignment(scope_id, day, shift_id, class_id, teacher_id, locked, created_at, updated_at)
				VALUES(?, ?, ?, ?, ?, 0, ?, ?)`,
				scopeID, cell.Day, cell.ShiftID, cell.ClassID, t, now, now); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if row.Locked {
				continue // 锁定格里的人由用户说了算
			}
			if _, err := tx.Exec(`UPDATE t_assignment SET teacher_id = ?, updated_at = ? WHERE id = ?`,
				t, now, row.ID); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/ -v`
Expected: PASS

**Step 5: 全量验证**

Run: `go test ./internal/...`
Expected: 全部 ok

Run: `go build .`
Expected: 无输出

**Step 6: 重新生成前端绑定**

Run: `wails3 generate bindings -ts -i`
Expected: `frontend/bindings/.../scheduleservice.ts` 里多出 `Solvers` / `Solve` / `SolveReport`。
（若报缺 `frontend/dist`，先 `npm run build --prefix frontend` 再重试；此步失败不阻塞后端测试。）

**Step 7: Commit**

```bash
git add internal/services/schedule.go internal/services/schedule_test.go frontend/bindings
git commit -m "feat(services): Solve 求解落库 + Solvers 列出算法"
```

---

## 收尾

Run: `go test ./internal/... && go build .`
Expected: 全绿

## 不在本计划范围

- **规则页 / 结果页前端**（`Solvers`/`Solve`/`Check` 的界面）—— 另开会话；
- **第二条算法**（DP / or-tools sidecar）—— 接口已留，注册一个 `Solver` 实现即可；
- **版本快照**（`SaveVersion` / `RestoreVersion`）—— 本次只写当前结果；
- **合班参与求解**—— 业务规则未定，`Unit.Cells` 已预留。

