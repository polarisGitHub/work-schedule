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

// ExclusiveSlot 结构约束：同一老师在同一 (天, 班次) 下最多被排到一个单元。
//
// 合班时一个单元覆盖多个格子，仍然只算"一个单元"，所以这里不需要特例。
// 单列成一个原语（而不是按 (老师,天,班次) 铺开成上万条 CountBound），
// 是为了让检查器一次线性扫描就能复核。
type ExclusiveSlot struct {
	Reason string `json:"reason,omitempty"`
}

func (ExclusiveSlot) isConstraint() {}

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
