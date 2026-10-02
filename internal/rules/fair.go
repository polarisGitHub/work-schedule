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
