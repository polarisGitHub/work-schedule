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
