package services

import (
	"database/sql"
	"errors"
	"sort"

	"work-schedule/internal/engine"
	"work-schedule/internal/plan"
	"work-schedule/internal/rules"
	"work-schedule/internal/store"
)

// ScheduleService 暴露排班核心能力：规则元信息、结果读写、结果复核。
type ScheduleService struct {
	store *store.Store
}

func NewScheduleService(st *store.Store) *ScheduleService {
	return &ScheduleService{store: st}
}

// RuleView 一种可用规则的元信息，供规则页生成列表与表单。
type RuleView struct {
	Type        string             `json:"type"`
	Label       string             `json:"label"`
	Mounts      []string           `json:"mounts"`
	ParamSchema []rules.ParamField `json:"paramSchema"`
}

// AssignmentView 一个课表格子。
type AssignmentView struct {
	Day       string `db:"day" json:"day"`
	ShiftID   int64  `db:"shift_id" json:"shiftId"`
	ClassID   int64  `db:"class_id" json:"classId"`
	TeacherID int64  `db:"teacher_id" json:"teacherId"`
	Locked    bool   `db:"locked" json:"locked"`
}

// RuleTypes 列出所有已注册的规则类型（按 type 排序，保证顺序稳定）。
func (s *ScheduleService) RuleTypes() []RuleView {
	metas := rules.Registered()
	out := make([]RuleView, 0, len(metas))
	for _, m := range metas {
		mounts := make([]string, 0, len(m.Mounts))
		for _, mk := range m.Mounts {
			mounts = append(mounts, string(mk))
		}
		out = append(out, RuleView{Type: m.Type, Label: m.Label, Mounts: mounts, ParamSchema: m.ParamSchema})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// ListAssignments 列出课表格子。versionID 为 0 表示当前结果，否则是某版本快照。
func (s *ScheduleService) ListAssignments(scopeID int64, versionID int64) ([]AssignmentView, error) {
	out := []AssignmentView{}
	var version any
	if versionID != 0 {
		version = versionID
	}
	err := s.store.DB().Select(&out, `
		SELECT day, shift_id, class_id, COALESCE(teacher_id, 0) AS teacher_id, locked
		FROM t_assignment
		WHERE scope_id = ? AND version_id IS ? AND deleted_at IS NULL
		ORDER BY day, shift_id, class_id`, scopeID, version)
	return out, err
}

// SetAssignment 写入/更新一个格子。teacherID 为 0 表示清空该格（保留行）。
// locked 为真时求解器不得改动它。
func (s *ScheduleService) SetAssignment(scopeID int64, day string, shiftID, classID, teacherID int64, locked bool) error {
	if day == "" || shiftID == 0 || classID == 0 {
		return errors.New("缺少日期 / 班次 / 班级")
	}
	now := nowMS()
	lockedInt := 0
	if locked {
		lockedInt = 1
	}
	var teacher any
	if teacherID != 0 {
		teacher = teacherID
	}

	var id int64
	err := s.store.DB().Get(&id, `
		SELECT id FROM t_assignment
		WHERE scope_id = ? AND version_id IS NULL AND day = ? AND shift_id = ? AND class_id = ? AND deleted_at IS NULL`,
		scopeID, day, shiftID, classID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = s.store.DB().Exec(`
			INSERT INTO t_assignment(scope_id, day, shift_id, class_id, teacher_id, locked, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, scopeID, day, shiftID, classID, teacher, lockedInt, now, now)
		return err
	case err != nil:
		return err
	default:
		_, err = s.store.DB().Exec(`
			UPDATE t_assignment SET teacher_id = ?, locked = ?, updated_at = ? WHERE id = ?`,
			teacher, lockedInt, now, id)
		return err
	}
}

// ClearAssignment 软删一个格子。
func (s *ScheduleService) ClearAssignment(scopeID int64, day string, shiftID, classID int64) error {
	now := nowMS()
	_, err := s.store.DB().Exec(`
		UPDATE t_assignment SET deleted_at = ?, updated_at = ?
		WHERE scope_id = ? AND version_id IS NULL AND day = ? AND shift_id = ? AND class_id = ? AND deleted_at IS NULL`,
		now, now, scopeID, day, shiftID, classID)
	return err
}

// Check 对当前结果做一次复核，返回冲突清单。只读，不重排。
func (s *ScheduleService) Check(scopeID int64) ([]plan.Violation, error) {
	p, err := engine.BuildProblem(s.store, scopeID)
	if err != nil {
		return nil, err
	}
	rows, err := s.ListAssignments(scopeID, 0)
	if err != nil {
		return nil, err
	}
	res := &plan.Result{}
	for _, a := range rows {
		if a.TeacherID == 0 {
			continue
		}
		u, ok := p.UnitForCell(plan.Cell{Day: a.Day, ShiftID: a.ShiftID, ClassID: a.ClassID})
		if !ok {
			continue // 该格子不在本次排班范围
		}
		res.Assignments = append(res.Assignments, plan.Assignment{
			Unit: u.ID, Teacher: plan.TeacherID(a.TeacherID), Locked: a.Locked,
		})
	}
	return plan.Check(p, res), nil
}
