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
