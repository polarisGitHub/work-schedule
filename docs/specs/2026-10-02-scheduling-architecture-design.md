# 排班架构设计（规则 / 求解器 / 检查器）

日期：2026-10-02

## 背景与目标

现状：

- 元数据已就绪：老师 / 班级 / 学科 / 班次 / 学科标签 / 合班 / 任课绑定（`t_dataset` + `t_mapping`）。
- 时间网格已就绪：日历 → 排班日（天）× 班次（`t_calendar` / `t_schedule_day`）。
- 值班指令已就绪：`t_duty`（某天某班次某老师必须 / 禁止值班）。
- 结果载体已就绪：`t_assignment`（天 + 班次 + 班级 → 老师，含 `locked`），版本快照走 `version_id`。
- 规则只有占位的两条：`fair_count` / `fair_interval`（`t_rule` + `t_mapping` 挂载）。
- `t_calendar.solver` 已存算法名（默认 `greedy-swap`），但求解器尚未实现。

目标：搭一套**可拔插**的排班架构——规则能快速添加并组合，算法能替换，结果与规则冲突时不阻断求解、由独立检查器报出。

## 已确认的决策

1. **排班基本单元**＝课表格子 `(天, 班次, 班级)`。`Unit.Cells` 预留合班扩展，现在恒为 1 格。
2. **候选资格全部由规则表达**，系统不预设任何推导（"不写规则＝全员候选"）。
3. **规则硬 / 软两分**；违反硬规则也**不阻断**求解（留空 + 上报）。
4. **规则 ＝ 元数据（`type` + `param`）＋ Go 实现 ＋ 挂载**；规则只在"构建 Problem"时运行，产出**声明式数据**。
5. 规则与求解器之间的边界是**声明式 `Problem`**，**不是回调**。
   - 理由：DP / ILP 无法消费"任意回调"还保证全局最优，回调等于把所有算法降级成贪心。
6. **版本只针对排班结果**，不快照规则（后续再议）。
7. 求解器**先只做一个简单版本**（greedy + 局部 swap），接口保持可拔插；or-tools 后续作为**可选算法**接入。

## 架构总览

```
元数据 (老师/班级/学科/班次/合班/任课/标签)
        ＋
挂载的规则 (t_rule{type,param} × t_mapping{global|personal|…})
        │
        ▼
┌─────────────────────────────────────────────┐
│ 规则层 RuleRegistry                          │
│   type → RuleImpl(type, param)               │
│   每条规则只产出一件事（或几件）：            │
│     (a) 候选过滤    unit → 允许/禁止 teacher  │
│     (b) 硬约束      声明式 Constraint         │
│     (c) 代价项      声明式 CostTerm           │
└─────────────────────────────────────────────┘
        │
        ▼
┌─────────────────────────────────────────────┐
│ 问题构造 ProblemBuilder → Problem            │
│   Units      待排单元（空格子）              │
│   Candidates unit → []teacher                │
│   Hard       []Constraint                    │
│   Cost       []CostTerm                      │
│   Locks      人工锁定 / 值班指令             │
└─────────────────────────────────────────────┘
        │
        ▼
┌─────────────────────────────────────────────┐
│ 求解器 SolverRegistry                        │
│   name → Solve(Problem) → Result             │
│   Result{ Assignments, Unassigned[+原因] }   │
└─────────────────────────────────────────────┘
        │
        ├──▶ 落库 t_assignment（locked 保留）
        │
        ▼
┌─────────────────────────────────────────────┐
│ 检查器 Checker（不重写逻辑）                 │
│   用同一批规则 + Candidates 复核 Result      │
│   → Violations[{级别, 位置, 说明}]           │
└─────────────────────────────────────────────┘
```

### 三条边界纪律

1. **规则层不认识求解器，求解器不认识规则类型**——两边只认 `Problem`。
2. 规则实现只干一件事：把自己的 `(type, param)` **翻译**成 `Problem` 的三类产出之一。它不含算法逻辑。
3. 检查器**不重写判定**：候选集 / 硬约束 / 代价项都能对"某个具体结果"求值，所以同一批规则既能喂求解器、又能复核结果——这就是"冲突不影响求解，但能被检查出来"的落点。

## 规则模型

### 数据形状

```sql
-- t_rule 增加一列 param（JSON），type 标识规则类型
ALTER TABLE t_rule ADD COLUMN param TEXT;

-- 挂载：这条规则作用在哪（复用 t_mapping，不新增表）
t_mapping{ type='rule_mount',
           from_id = 目标实体 id,   -- 老师/班级/学科/班次/合班；NULL = 全局
           to_id   = rule_id }
```

现有 `global_rule` / `personal_rule` 是它的两个特例（`from_id IS NULL` 与 `from_id = 老师`），后续可平滑归并。

选用 JSON 列而不是复用 `col1..col4`：规则参数是变长的，JSON 表达更自由；`col1..col4` 留给"可用于查询过滤"的少量字段。

### 规则实现：按需实现三个小接口

```go
type Rule interface{ Meta() Meta }   // 名字、label、可挂目标、参数表单 schema

type CandidateFilter interface {     // (a) 局部资格
    Apply(ctx *BuildCtx, cand *CandidateSet)
}
type ConstraintProvider interface {  // (b) 硬约束
    Provide(ctx *BuildCtx, out *ConstraintSet)
}
type CostProvider interface {        // (c) 软目标
    Provide(ctx *BuildCtx, out *CostSet)
}
```

关键：**这些只在"构建 Problem"时跑一次**，产出的是声明式数据（候选集 / 约束 / 代价项），不是求解器运行时反复调用的回调。

### BuildCtx：规则能看到的全部输入

```go
type BuildCtx struct {
    Scope      int64
    Curriculum *Curriculum   // 已加载元数据图：老师/班级/学科/班次/合班/任课/标签
    Days       []string
    Shifts     []Shift
    Units      []Unit        // 本次要排的空格子
    Locks      []PairFix     // 人工锁定 + 值班指令
}
```

规则**不连数据库**，只认 `ctx` → 规则实现是纯函数，好测。

### 加一条规则要写多少

以 `fair_count`（次数均衡）为例：

1. `type FairCountParam struct{ ... }`（JSON tag）
2. `func (r *FairCount) Meta() Meta{...}`——type 名、label、可挂目标、参数字段
3. `func (r *FairCount) Provide(ctx, out *CostSet){...}`——按需实现的一个方法
4. 注册表加一行 `Register("fair_count", ...)`
5. **前端 0 代码**：参数表单由 `Meta().ParamSchema` 自动生成

一个规则最多写 3 个方法，且**永远不碰求解器、不碰前端表单**。现有 `fair_count` / `fair_interval` 改造成两条规则实现，作为第一条样例。

## Problem 契约

`Problem` 是规则层和求解器之间唯一的契约。

```go
type Problem struct {
    Units      []Unit
    Candidates map[UnitID][]TeacherID
    Hard       []Constraint
    Costs      []CostTerm
    Locks      map[UnitID]TeacherID   // 人工锁定 / 值班指令
    Reason     map[UnitID]map[TeacherID]string  // 候选被排除的原因（解释用）
}

type Unit struct {
    ID    UnitID
    Key   string   // 如 "2026-10-06|晚1|高二3班"，用于展示与落库
    Cells []Cell   // 本单元覆盖的物理格子；现在恒为 1 个
}
type Cell struct { Day string; ShiftID, ClassID int64 }
```

`Cells` 恒为 1 格是**给合班预留的接口**：等合班业务定下来，只让某些 Unit 的 `Cells` 变成多个，上层（规则 / 求解器 / 检查器）都不用改。

### 硬约束：三个原语

```go
type Constraint interface{ isConstraint() }

type CountBound struct {            // 对某个"选择子集"的计数上下界
    Scope    SelectionScope         // {teacher:T, day:D} / {teacher:T, slot:(D,S)} / {teacher:T, days:[..]}
    Min, Max int                    // -1 = 不限
    Reason   string                 // 谁产生的，用于解释
}
type PairFix struct { Unit UnitID; Teacher TeacherID; Mode Fix|Ban; Reason string }
type Cover   struct { Units []UnitID; Reason string }
```

覆盖能力：

- 一人同一时段只能一格 → `CountBound{teacher:T, slot:(D,S), Max:1}`
- 每人每天 / 每周上限 → `CountBound{teacher:T, day:D, Max:n}`
- 间隔至少 n 天 → 对每个长度 n 的滑窗生成 `Max:1`
- 某天某班次必须 / 禁止某老师（`t_duty`）→ `CountBound{teacher:T, slot:(D,S), Min:1}` / `Max:0`
- 人工锁定 → `PairFix{Fix}`
- 每格必须有老师 → `Cover`

### 软代价：三个原语

```go
type CostTerm interface{ isCostTerm() }
type Balance struct { Group []TeacherID; Weight float64 }               // 组内次数尽量均等（fair_count）
type Spread  struct { Scope SelectionScope; Weight float64 }            // 同一老师的点尽量散开（fair_interval）
type Prefer  struct { Scope SelectionScope; Weight float64; Sign int }  // 线性偏好 / 惩罚
```

代价原语是唯一"要动底层"的地方，所以刻意只放 3 个、按需再长。

### 两条纪律

1. **人工锁定 / 值班优先于规则**：`Locks` 无条件落地，且它占用的资源（老师、时段、当日 / 周配额）立刻对其它单元生效，等价于先把这些约束扣掉。锁定**一律不动**；由此产生的硬冲突**不由求解器偷偷修**，留给检查器报出。
2. **求解器必须声明能力**：

```go
type Capability struct {
    Constraints []string   // 支持哪些 Constraint
    Costs       []string   // 支持哪些 CostTerm
    Optimal     bool       // 是否承诺全局最优
}
```

前端列算法时带上能力；若某算法不认某条已挂载规则产出的约束，**直接提示"该算法不支持这条规则"**，而不是静默忽略。

## 求解器

```go
type Solver interface {
    Name()          string
    Capability()    Capability
    Deterministic() bool
    Solve(ctx context.Context, p *Problem) (*Result, error)
}

type Result struct {
    Assignments []Assignment    // 已排：Unit → Teacher（标注是否来自锁定）
    Unassigned  []Unassigned    // 没排上的 Unit + 原因
    Score       float64         // 目标值，用于横向比较不同算法
    Optimal     bool            // 是否已证最优（贪心给 false）
}

type Assignment struct { Unit UnitID; Teacher TeacherID; Locked bool }
type Unassigned struct {
    Unit      UnitID
    Reason    string    // "无候选老师" / "候选都被同时段占用" / "与锁定互斥"
    BlockedBy []string  // 相关规则的 label
}
```

纪律：

1. **`Solve` 是纯函数**：`Problem` 进、`Result` 出，**不碰数据库、不写库**。任何算法都能拿同一份 `Problem` 单测、互相 PK。落库由服务层统一做。
2. **注册表按名字挂**：`solver.Register(greedy.New())`；`t_calendar.solver` 存的 name 就是 key（默认 `greedy-swap`）。查不到直接报错，不静默回退。
3. **求解器不做合规判断**：合规由检查器独立复核，算法无法靠"忽略规则"蒙混过关。
4. **可复现**：`Deterministic()` 为 false 的算法用固定 seed（或把 seed 存进结果），保证同样输入同样输出。
5. **跑不完也不崩**：收 `ctx`，超时 / 规模太大时返回**当前最优 + `Optimal=false`**，而不是报错。

### 本期实现范围

先做**一个简单版本**：`greedy`（按受限程度排序逐个格子选代价最小的候选）+ **局部 swap 提升**。注册名 `greedy-swap`，与 `t_calendar.solver` 的默认值对齐。

### 后续可选：or-tools

or-tools 是 C++ 写的，官方只提供 Python / C# / Java wrapper；Go 的 `github.com/google/or-tools` 必须 CGO + 原生库，**与当前 `CGO_ENABLED=0 GOOS=windows` 的一键交叉编译直接冲突**。

若后续要接，走**外部进程求解器**（sidecar）：主程序把 `Problem` 序列化成 JSON 经 stdin 交给独立 exe，stdout 收回 `Result`；内嵌算法与外部进程在 `Solver` 接口下**一视同仁**。这依赖 `Problem` / `Result` 有稳定的 JSON schema 与版本号。**因为边界是声明式 `Problem`，这个口子随时能接，不是技术债。**

## 检查器

**核心纪律：不重写任何规则逻辑。** 只消费 `Problem`（声明式）+ 具体的 `Result`，做一次求值：

```go
func Check(p *Problem, r *Result) []Violation

type Violation struct {
    Level     Level     // Hard | Soft | Info
    Kind      string
    Unit      *UnitID
    Teacher   *TeacherID
    RuleLabel string
    Message   string    // 人话
    Delta     float64   // 软偏差量
}
```

| `Problem` 里的东西 | 检查器怎么判 | 级别 |
|---|---|---|
| `Candidates` | `结果[Unit] ∉ Candidates[Unit]` → 违规 | Hard |
| `PairFix{Fix}` | 该对没被排上 → 违规 | Hard |
| `PairFix{Ban}` | 该对被排上了 → 违规 | Hard |
| `CountBound` | 按 `Scope` 对结果计数，越界 → 违规 | Hard |
| `Cover` | 该单元没人 → 违规（即"留空"） | Hard |
| `Balance/Spread/Prefer` | 对结果求值，算偏差 `Delta` | Soft |

要点：

1. **"冲突不阻断"的落点**：对任何结果都能跑——求解器留了空、用户手改乱锁、算法超时给的次优解——只报不改，**不阻断保存**。留空即 `Cover` 未满足，同时附上 `Unassigned.Reason`（同一套解释文本）。
2. **人工改动闭环**：每次改一格就**全量重查**（规模几千格，毫秒级，不做增量）。
3. **新规则不写检查代码**：声明式 `Problem` 的最大红利——规则产出 `CountBound` / `Balance`…，检查器天生就认这些原语。
4. **三级展示**：Hard（红）/ Soft（黄，带偏差量）/ Info（灰，如"规则挂了但当前无单元受影响"）。前端按 `Unit` 分组高亮格子。
5. `Check` 是纯函数，不碰数据库、可单测，**不需要注册表**。

## 落库与版本

- 当前结果 = `t_assignment.version_id IS NULL`；**lock 的行原样保留**。
- 重排 = 按 `(scope, NULL, day, shift, class)` upsert `teacher_id`；新结果里消失的格子软删。
- **交互约定：手改想留住就必须锁**，否则下次求解会被冲掉。
- **版本只针对排班结果**：`SaveVersion` 把当前结果复制成 `version_id=X` 的行；`RestoreVersion` 用快照覆盖当前。两步都是纯数据拷贝，不碰求解器，不做规则快照。

## 服务接口

```go
// internal/services/schedule.go
Solvers() []SolverInfo                     // name/label/capability/deterministic
Rules(scopeID) []RuleView                  // 已挂载规则 + 可用规则类型（带 ParamSchema）
SaveRule / MountRule / UnmountRule
Solve(scopeID, solver) SolveResult         // buildProblem → 求解 → 落库 → 检查
Check(scopeID) []Violation                 // 只读复核，不重排
SetAssignment(scope, day, shift, class, teacherID, locked)
ListAssignments(scope, versionID?)
SaveVersion / ListVersions / RestoreVersion
```

`buildProblem` 先做成**纯函数、按需重建**；真觉得卡再按 `scope` 加缓存，失效键取元数据 / 规则 / 日历 `updated_at` 的最大值。**不提前缓存。**

## 目录结构

```
internal/
  rules/    rule.go registry.go fair_count.go fair_interval.go …
  solver/   solver.go registry.go greedy.go
  plan/     problem.go build.go check.go
  services/ schedule.go
```

## 实施顺序

1. **契约先行**：`plan/problem.go`（Unit / Constraint / CostTerm / Problem / Result / Violation，纯类型，零依赖）
2. **规则骨架**：`rules` 注册表 + `BuildCtx`，把现有 `fair_count` / `fair_interval` 改造成两条规则实现
3. **Problem 构建**：`plan/build.go`（跑出候选集 + 代价项）
4. **检查器 + 结果读写**：`plan/check.go` + `SetAssignment` + 排班结果页 → **此时"纯人工排 + 实时体检"已可用**
5. **贪心求解器**：`solver/greedy.go` + `Solve` 落库 → "一键排 + 手改 + 体检"闭环完整
6. 规则页、锁定交互、版本快照

注意顺序：**第 4 步就已经有产品价值**（人工排班 + 即时报冲突），求解器在第 5 步才进来。不用一次做完就能验证架构。

## 本次范围外

- **合班参与求解**：业务规则未定；`Unit.Cells` 已预留接口。
- **具体业务规则**（主课班、按学科轮换等）：等业务给规则，架构只保证"能快速加"。
- **`t_rule` 版本化 / 规则快照**：本次版本只针对结果，后续再议。
- **or-tools / 第二条算法**：接口已留，需要时再接。

## 后续

- 玩法验证后，评估"规则数量 / 公平性要求"是否到了必须上 CP-SAT 的程度。
- 需要时接外部进程求解器（sidecar），或纯 Go 的 `gophersat`（SAT / 伪布尔 / MAXSAT）作为无 CGO 的最优性方案。
