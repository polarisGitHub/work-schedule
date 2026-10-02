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
