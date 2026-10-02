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
