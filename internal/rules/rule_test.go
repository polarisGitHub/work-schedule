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
