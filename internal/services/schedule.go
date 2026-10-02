package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"work-schedule/internal/engine"
	"work-schedule/internal/plan"
	"work-schedule/internal/rules"
	"work-schedule/internal/solver"
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

// SolverInfo 供前端列出可选算法。
type SolverInfo struct {
	Name          string            `json:"name"`
	Label         string            `json:"label"`
	Capability    solver.Capability `json:"capability"`
	Deterministic bool              `json:"deterministic"`
}

// Solvers 列出所有可用算法。
func (s *ScheduleService) Solvers() []SolverInfo {
	all := solver.All()
	out := make([]SolverInfo, 0, len(all))
	for _, sv := range all {
		out = append(out, SolverInfo{
			Name:          sv.Name(),
			Label:         sv.Label(),
			Capability:    sv.Capability(),
			Deterministic: sv.Deterministic(),
		})
	}
	return out
}

// SolveReport 一次求解的汇总。
type SolveReport struct {
	Assigned   int              `json:"assigned"`
	Unassigned int              `json:"unassigned"`
	Optimal    bool             `json:"optimal"`
	Score      float64          `json:"score"`
	Violations []plan.Violation `json:"violations"`
}

// Solve 求解并落库。solverName 为空时用日历里配置的算法，再空则用默认。
// 锁定格一律不动；无解不报错，未排上的单元进 Unassigned、冲突进 Violations。
func (s *ScheduleService) Solve(scopeID int64, solverName string) (SolveReport, error) {
	name, err := s.resolveSolverName(scopeID, solverName)
	if err != nil {
		return SolveReport{}, err
	}
	sv, ok := solver.Lookup(name)
	if !ok {
		return SolveReport{}, fmt.Errorf("未知求解器: %s", name)
	}

	p, err := engine.BuildProblem(s.store, scopeID)
	if err != nil {
		return SolveReport{}, err
	}
	res, err := sv.Solve(context.Background(), p)
	if err != nil {
		return SolveReport{}, err
	}
	if err := s.persistResult(scopeID, p, res); err != nil {
		return SolveReport{}, err
	}

	return SolveReport{
		Assigned:   len(res.Assignments),
		Unassigned: len(res.Unassigned),
		Optimal:    res.Optimal,
		Score:      res.Score,
		Violations: plan.Check(p, res),
	}, nil
}

// resolveSolverName 依次取：调用方指定 → 日历配置 → 默认。
func (s *ScheduleService) resolveSolverName(scopeID int64, solverName string) (string, error) {
	if name := strings.TrimSpace(solverName); name != "" {
		return name, nil
	}
	var cfg string
	err := s.store.DB().Get(&cfg, `
		SELECT solver FROM t_calendar WHERE scope_id = ? AND deleted_at IS NULL LIMIT 1`, scopeID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return defaultSolver, nil
	case err != nil:
		return "", err
	}
	if strings.TrimSpace(cfg) == "" {
		return defaultSolver, nil
	}
	return cfg, nil
}

// persistResult 把结果写回当前版本的 t_assignment。
// 规则：不在新结果里的格子软删；锁定行一律跳过（不动、不删）。
func (s *ScheduleService) persistResult(scopeID int64, p *plan.Problem, res *plan.Result) error {
	unitByID := make(map[plan.UnitID]plan.Unit, len(p.Units))
	for _, u := range p.Units {
		unitByID[u.ID] = u
	}
	want := make(map[plan.Cell]plan.TeacherID)
	for _, a := range res.Assignments {
		u, ok := unitByID[a.Unit]
		if !ok {
			continue
		}
		for _, c := range u.Cells {
			want[c] = a.Teacher
		}
	}

	now := nowMS()
	tx, err := s.store.DB().Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1) 软删不在新结果里的当前行（锁定行不动）
	current := []struct {
		ID      int64  `db:"id"`
		Day     string `db:"day"`
		ShiftID int64  `db:"shift_id"`
		ClassID int64  `db:"class_id"`
		Locked  bool   `db:"locked"`
	}{}
	if err := tx.Select(&current, `
		SELECT id, day, shift_id, class_id, locked
		FROM t_assignment
		WHERE scope_id = ? AND version_id IS NULL AND deleted_at IS NULL`, scopeID); err != nil {
		return err
	}
	for _, r := range current {
		if r.Locked {
			continue
		}
		if _, keep := want[plan.Cell{Day: r.Day, ShiftID: r.ShiftID, ClassID: r.ClassID}]; keep {
			continue
		}
		if _, err := tx.Exec(`UPDATE t_assignment SET deleted_at = ?, updated_at = ? WHERE id = ?`, now, now, r.ID); err != nil {
			return err
		}
	}

	// 2) 写入新结果（已存在则更新；锁定行不动）
	for cell, t := range want {
		var row struct {
			ID     int64 `db:"id"`
			Locked bool  `db:"locked"`
		}
		err := tx.Get(&row, `
			SELECT id, locked FROM t_assignment
			WHERE scope_id = ? AND version_id IS NULL AND day = ? AND shift_id = ? AND class_id = ? AND deleted_at IS NULL`,
			scopeID, cell.Day, cell.ShiftID, cell.ClassID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := tx.Exec(`
				INSERT INTO t_assignment(scope_id, day, shift_id, class_id, teacher_id, locked, created_at, updated_at)
				VALUES(?, ?, ?, ?, ?, 0, ?, ?)`,
				scopeID, cell.Day, cell.ShiftID, cell.ClassID, t, now, now); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if row.Locked {
				continue // 锁定格里的人由用户说了算
			}
			if _, err := tx.Exec(`UPDATE t_assignment SET teacher_id = ?, updated_at = ? WHERE id = ?`,
				t, now, row.ID); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}
