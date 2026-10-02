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
