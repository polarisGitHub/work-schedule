// Package engine 把库里的元数据 + 挂载规则组装成 plan.Problem。
//
// 它位于 plan 与 rules 之上：plan 提供契约，rules 把规则翻译成契约数据，
// engine 负责读库、组装 BuildCtx、跑规则、拼出 Problem。
package engine

import (
	"sort"

	"work-schedule/internal/rules"
	"work-schedule/internal/store"
)

// Curriculum 一个排班范围内的元数据图。
type Curriculum struct {
	ScopeID  int64
	Teachers []rules.Teacher
	Classes  []rules.Class
	Shifts   []rules.Shift
	Merged   []rules.MergedClass
	Bindings []rules.Binding
	Days     []string
}

// LoadCurriculum 读出一个范围内排班所需的全部元数据。
func LoadCurriculum(st *store.Store, scopeID int64) (*Curriculum, error) {
	c := &Curriculum{ScopeID: scopeID}

	entities := []struct {
		ID    int64  `db:"id"`
		Type  string `db:"type"`
		Name  string `db:"name"`
		Start string `db:"start_time"`
		End   string `db:"end_time"`
	}{}
	if err := st.DB().Select(&entities, `
		SELECT id, type, COALESCE(col1, '') AS name,
		       COALESCE(col2, '') AS start_time, COALESCE(col3, '') AS end_time
		FROM t_dataset
		WHERE scope_id = ? AND deleted_at IS NULL AND type IN ('teacher','class','shift','merged_class')
		ORDER BY id`, scopeID); err != nil {
		return nil, err
	}
	for _, e := range entities {
		switch e.Type {
		case "teacher":
			c.Teachers = append(c.Teachers, rules.Teacher{ID: e.ID, Name: e.Name})
		case "class":
			c.Classes = append(c.Classes, rules.Class{ID: e.ID, Name: e.Name})
		case "shift":
			c.Shifts = append(c.Shifts, rules.Shift{ID: e.ID, Name: e.Name, Start: e.Start, End: e.End})
		case "merged_class":
			c.Merged = append(c.Merged, rules.MergedClass{ID: e.ID, Name: e.Name})
		}
	}

	// 任课：老师 × 学科 × 班级 → 把学科 / 班级挂到老师上
	bindings := []struct {
		TeacherID int64 `db:"teacher_id"`
		SubjectID int64 `db:"subject_id"`
		ClassID   int64 `db:"class_id"`
	}{}
	if err := st.DB().Select(&bindings, `
		SELECT m.from_id AS teacher_id, bs.to_id AS subject_id, bc.to_id AS class_id
		FROM t_dataset b
		JOIN t_mapping m  ON m.scope_id = b.scope_id AND m.type = 'teacher_binding' AND m.to_id = b.id AND m.deleted_at IS NULL
		JOIN t_mapping bs ON bs.scope_id = b.scope_id AND bs.type = 'binding_subject' AND bs.from_id = b.id AND bs.deleted_at IS NULL
		JOIN t_mapping bc ON bc.scope_id = b.scope_id AND bc.type = 'binding_class' AND bc.from_id = b.id AND bc.deleted_at IS NULL
		WHERE b.scope_id = ? AND b.type = 'binding' AND b.deleted_at IS NULL
		ORDER BY b.id`, scopeID); err != nil {
		return nil, err
	}
	tIndex := make(map[int64]int, len(c.Teachers))
	for i, t := range c.Teachers {
		tIndex[t.ID] = i
	}
	for _, b := range bindings {
		c.Bindings = append(c.Bindings, rules.Binding{TeacherID: b.TeacherID, SubjectID: b.SubjectID, ClassID: b.ClassID})
		if i, ok := tIndex[b.TeacherID]; ok {
			c.Teachers[i].SubjectIDs = appendOnce(c.Teachers[i].SubjectIDs, b.SubjectID)
			c.Teachers[i].ClassIDs = appendOnce(c.Teachers[i].ClassIDs, b.ClassID)
		}
	}
	sort.Slice(c.Teachers, func(i, j int) bool { return c.Teachers[i].ID < c.Teachers[j].ID })

	// 合班成员
	members := []struct {
		MergedID int64 `db:"merged_id"`
		ClassID  int64 `db:"class_id"`
	}{}
	if err := st.DB().Select(&members, `
		SELECT m.from_id AS merged_id, m.to_id AS class_id
		FROM t_mapping m
		JOIN t_dataset c ON c.scope_id = m.scope_id AND c.id = m.to_id AND c.deleted_at IS NULL
		WHERE m.scope_id = ? AND m.type = 'merged_class_member' AND m.deleted_at IS NULL
		ORDER BY m.from_id, c.id`, scopeID); err != nil {
		return nil, err
	}
	mIndex := make(map[int64]int, len(c.Merged))
	for i, m := range c.Merged {
		mIndex[m.ID] = i
	}
	for _, m := range members {
		if i, ok := mIndex[m.MergedID]; ok {
			c.Merged[i].ClassIDs = append(c.Merged[i].ClassIDs, m.ClassID)
		}
	}

	// 排班日
	if err := st.DB().Select(&c.Days, `
		SELECT day FROM t_schedule_day WHERE scope_id = ? AND deleted_at IS NULL ORDER BY day`, scopeID); err != nil {
		return nil, err
	}

	return c, nil
}

func appendOnce(list []int64, v int64) []int64 {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
