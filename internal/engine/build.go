package engine

import (
	"fmt"

	"work-schedule/internal/plan"
	"work-schedule/internal/rules"
	"work-schedule/internal/store"
)

// BuildProblem 读元数据 + 挂载规则，产出求解器与检查器共用的 Problem。
func BuildProblem(st *store.Store, scopeID int64) (*plan.Problem, error) {
	cur, err := LoadCurriculum(st, scopeID)
	if err != nil {
		return nil, err
	}

	// 1) 待排单元：排班日 × 班次 × 班级。合班尚未参与（每单元恒 1 格）。
	units := make([]plan.Unit, 0, len(cur.Days)*len(cur.Shifts)*len(cur.Classes))
	for _, day := range cur.Days {
		for _, sh := range cur.Shifts {
			for _, cl := range cur.Classes {
				units = append(units, plan.Unit{
					ID:    plan.UnitID(len(units) + 1),
					Key:   fmt.Sprintf("%s|%d|%d", day, sh.ID, cl.ID),
					Cells: []plan.Cell{{Day: day, ShiftID: sh.ID, ClassID: cl.ID}},
				})
			}
		}
	}

	unitIDs := make([]plan.UnitID, 0, len(units))
	teacherIDs := make([]plan.TeacherID, 0, len(cur.Teachers))
	for _, u := range units {
		unitIDs = append(unitIDs, u.ID)
	}
	for _, t := range cur.Teachers {
		teacherIDs = append(teacherIDs, plan.TeacherID(t.ID))
	}

	// 2) 人工锁定：t_assignment.locked = 1
	locks, err := loadLocks(st, scopeID, units)
	if err != nil {
		return nil, err
	}

	ctx := &rules.BuildCtx{
		ScopeID:  scopeID,
		Days:     cur.Days,
		Shifts:   cur.Shifts,
		Classes:  cur.Classes,
		Teachers: cur.Teachers,
		Merged:   cur.Merged,
		Bindings: cur.Bindings,
		Units:    units,
		Locks:    locks,
	}

	// 3) 跑规则：候选集默认全员，各规则做减法和追加。
	cand := rules.NewCandidateSet(unitIDs, teacherIDs)
	hard := &rules.ConstraintSet{}
	costs := &rules.CostSet{}

	mounted, err := rules.Load(st, scopeID)
	if err != nil {
		return nil, err
	}
	for _, m := range mounted {
		if f, ok := m.Instance.(rules.CandidateFilter); ok {
			f.Filter(ctx, cand)
		}
		if s, ok := m.Instance.(rules.ConstraintSource); ok {
			s.Constraints(ctx, hard)
		}
		if s, ok := m.Instance.(rules.CostSource); ok {
			s.Costs(ctx, costs)
		}
	}

	// 4) 值班指令 t_duty → CountBound
	dutyBounds, err := loadDutyBounds(st, scopeID)
	if err != nil {
		return nil, err
	}
	hard.Items = append(hard.Items, dutyBounds...)

	// 5) 锁定 → PairFix(Fix)
	for u, t := range locks {
		hard.Items = append(hard.Items, plan.PairFix{Unit: u, Teacher: t, Mode: plan.ModeFix, Reason: "人工锁定"})
	}

	// 6) 每格必须有老师
	hard.Items = append(hard.Items, plan.Cover{Units: unitIDs, Reason: "每格都要有人"})

	// 7) 候选集收窄为切片
	candidates := make(map[plan.UnitID][]plan.TeacherID, len(cand.Allow))
	reason := make(map[plan.UnitID]map[plan.TeacherID]string, len(cand.Allow))
	for u, m := range cand.Allow {
		kept := make([]plan.TeacherID, 0, len(m))
		for tid, ok := range m {
			if ok {
				kept = append(kept, tid)
			}
		}
		candidates[u] = kept
		if r, ok := cand.Reason[u]; ok && len(r) > 0 {
			reason[u] = r
		}
	}

	return &plan.Problem{
		Units:      units,
		Candidates: candidates,
		Hard:       hard.Items,
		Costs:      costs.Items,
		Locks:      locks,
		Reason:     reason,
	}, nil
}

// loadLocks 读人工锁定的格子，映射到单元。
func loadLocks(st *store.Store, scopeID int64, units []plan.Unit) (map[plan.UnitID]plan.TeacherID, error) {
	rows := []struct {
		Day       string `db:"day"`
		ShiftID   int64  `db:"shift_id"`
		ClassID   int64  `db:"class_id"`
		TeacherID int64  `db:"teacher_id"`
	}{}
	if err := st.DB().Select(&rows, `
		SELECT day, shift_id, class_id, teacher_id
		FROM t_assignment
		WHERE scope_id = ? AND version_id IS NULL AND deleted_at IS NULL
		  AND locked = 1 AND teacher_id IS NOT NULL`, scopeID); err != nil {
		return nil, err
	}
	byCell := make(map[plan.Cell]plan.UnitID, len(units))
	for _, u := range units {
		for _, c := range u.Cells {
			byCell[c] = u.ID
		}
	}
	locks := make(map[plan.UnitID]plan.TeacherID, len(rows))
	for _, r := range rows {
		if uid, ok := byCell[plan.Cell{Day: r.Day, ShiftID: r.ShiftID, ClassID: r.ClassID}]; ok {
			locks[uid] = plan.TeacherID(r.TeacherID)
		}
	}
	return locks, nil
}

// loadDutyBounds 把 t_duty 翻译成计数约束。值班指令不带班级，
// 所以语义是"该老师在该时段至少 / 最多被排 N 次"。
func loadDutyBounds(st *store.Store, scopeID int64) ([]plan.Constraint, error) {
	rows := []struct {
		Day       string `db:"day"`
		ShiftID   int64  `db:"shift_id"`
		TeacherID int64  `db:"teacher_id"`
		Required  int64  `db:"required"`
	}{}
	if err := st.DB().Select(&rows, `
		SELECT d.day AS day, d.shift_id AS shift_id, d.teacher_id AS teacher_id, d.required AS required
		FROM t_duty d
		JOIN t_dataset t ON t.scope_id = d.scope_id AND t.id = d.teacher_id AND t.deleted_at IS NULL
		WHERE d.scope_id = ? AND d.deleted_at IS NULL`, scopeID); err != nil {
		return nil, err
	}
	out := make([]plan.Constraint, 0, len(rows))
	for _, r := range rows {
		day := r.Day
		shiftID := r.ShiftID
		teacher := plan.TeacherID(r.TeacherID)
		scope := plan.SelectionScope{Teacher: &teacher, Day: &day, ShiftID: &shiftID}
		if r.Required == 1 {
			out = append(out, plan.CountBound{Scope: scope, Min: 1, Max: -1, Reason: "指定值班"})
		} else {
			out = append(out, plan.CountBound{Scope: scope, Min: -1, Max: 0, Reason: "指定不值班"})
		}
	}
	return out, nil
}
