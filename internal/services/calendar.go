package services

import (
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"

	"work-schedule/internal/model"
	"work-schedule/internal/store"
)

const dateLayout = "2006-01-02"

// defaultSolver 日历默认求解器。
const defaultSolver = "greedy-swap"

// CalendarConfig 日历配置：起止日期 + 哪些星期上课。
type CalendarConfig struct {
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Weekdays  []bool `json:"weekdays"`
	Solver    string `json:"solver"`
}

// DutyView 一条值班约束：某天某班次某老师必须值班或必须休息。
type DutyView struct {
	Day       string `db:"day" json:"day"`
	ShiftID   int64  `db:"shift_id" json:"shiftId"`
	Shift     string `db:"shift" json:"shift"`
	TeacherID int64  `db:"teacher_id" json:"teacherId"`
	Teacher   string `db:"teacher" json:"teacher"`
	Required  bool   `db:"required" json:"required"`
}

// CalendarService 管理日历、排班日与值班约束。
type CalendarService struct {
	store *store.Store
}

func NewCalendarService(st *store.Store) *CalendarService {
	return &CalendarService{store: st}
}

// GetCalendar 取该排班的日历配置，没有则返回 nil。
func (s *CalendarService) GetCalendar(scopeID int64) (*CalendarConfig, error) {
	row := struct {
		Start  string `db:"start_date"`
		End    string `db:"end_date"`
		Monday int64  `db:"monday"`
		Tue    int64  `db:"tuesday"`
		Wed    int64  `db:"wednesday"`
		Thu    int64  `db:"thursday"`
		Fri    int64  `db:"friday"`
		Sat    int64  `db:"saturday"`
		Sun    int64  `db:"sunday"`
		Solver string `db:"solver"`
	}{}
	err := s.store.DB().Get(&row, `
		SELECT start_date, end_date, monday, tuesday, wednesday, thursday, friday, saturday, sunday, solver
		FROM t_calendar WHERE scope_id = ? AND deleted_at IS NULL LIMIT 1`, scopeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	flags := []int64{row.Monday, row.Tue, row.Wed, row.Thu, row.Fri, row.Sat, row.Sun}
	weekdays := make([]bool, 7)
	for i, f := range flags {
		weekdays[i] = f != 0
	}
	return &CalendarConfig{
		StartDate: row.Start,
		EndDate:   row.End,
		Weekdays:  weekdays,
		Solver:    row.Solver,
	}, nil
}

// SaveCalendar 保存日历并按日期与星期重算排班日。
// 首次保存时会插入两条全局公平规则（fair_count、fair_interval）。
func (s *CalendarService) SaveCalendar(scopeID int64, cfg CalendarConfig) error {
	start, err := time.Parse(dateLayout, cfg.StartDate)
	if err != nil {
		return errors.New("开始日期格式不正确")
	}
	end, err := time.Parse(dateLayout, cfg.EndDate)
	if err != nil {
		return errors.New("结束日期格式不正确")
	}
	if end.Before(start) {
		return errors.New("结束日期不能早于开始日期")
	}
	if len(cfg.Weekdays) != 7 {
		return errors.New("请选择上课的星期")
	}
	picked := false
	for _, day := range cfg.Weekdays {
		picked = picked || day
	}
	if !picked {
		return errors.New("请至少选择一个星期")
	}
	solver := cfg.Solver
	if solver == "" {
		solver = defaultSolver
	}

	tx, err := s.store.DB().Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := nowMS()
	weekdayFlags := make([]any, 7)
	for i, day := range cfg.Weekdays {
		if day {
			weekdayFlags[i] = 1
		} else {
			weekdayFlags[i] = 0
		}
	}

	var calendarID int64
	err = tx.Get(&calendarID, `SELECT id FROM t_calendar WHERE scope_id = ? AND deleted_at IS NULL`, scopeID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		args := append([]any{scopeID, cfg.StartDate, cfg.EndDate}, weekdayFlags...)
		args = append(args, solver, now, now)
		if _, err := tx.Exec(`
			INSERT INTO t_calendar(scope_id, start_date, end_date, monday, tuesday, wednesday, thursday, friday, saturday, sunday, solver, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		args := append([]any{cfg.StartDate, cfg.EndDate}, weekdayFlags...)
		args = append(args, solver, now, calendarID)
		if _, err := tx.Exec(`
			UPDATE t_calendar
			SET start_date = ?, end_date = ?, monday = ?, tuesday = ?, wednesday = ?, thursday = ?, friday = ?, saturday = ?, sunday = ?, solver = ?, updated_at = ?
			WHERE id = ?`, args...); err != nil {
			return err
		}
	}

	if err := ensureFairRules(tx, scopeID, now); err != nil {
		return err
	}
	if err := syncScheduleDays(tx, scopeID, start, end, cfg.Weekdays, now); err != nil {
		return err
	}
	return tx.Commit()
}

// ListScheduleDays 列出该排班全部要排的日期。
func (s *CalendarService) ListScheduleDays(scopeID int64) ([]string, error) {
	out := []string{}
	err := s.store.DB().Select(&out, `
		SELECT day FROM t_schedule_day WHERE scope_id = ? AND deleted_at IS NULL ORDER BY day`, scopeID)
	return out, err
}

// ToggleScheduleDay 把某天加入或排除排班日历，返回操作后是否在排班日内。
// 排除时同时软删当天的值班；取消排除时把这一天加回排班日。
func (s *CalendarService) ToggleScheduleDay(scopeID int64, day string) (bool, error) {
	if _, err := time.Parse(dateLayout, day); err != nil {
		return false, errors.New("日期格式不正确")
	}
	tx, err := s.store.DB().Beginx()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	now := nowMS()
	var scheduled int
	if err := tx.Get(&scheduled, `
		SELECT COUNT(*) FROM t_schedule_day WHERE scope_id = ? AND day = ? AND deleted_at IS NULL`, scopeID, day); err != nil {
		return false, err
	}

	if scheduled > 0 {
		if err := removeScheduleDay(tx, scopeID, day, now); err != nil {
			return false, err
		}
		var hasOff int
		if err := tx.Get(&hasOff, `
			SELECT COUNT(*) FROM t_day_off WHERE scope_id = ? AND day = ? AND deleted_at IS NULL`, scopeID, day); err != nil {
			return false, err
		}
		if hasOff == 0 {
			if _, err := tx.Exec(`INSERT INTO t_day_off(scope_id, day, created_at, updated_at) VALUES(?, ?, ?, ?)`,
				scopeID, day, now, now); err != nil {
				return false, err
			}
		}
		return false, tx.Commit()
	}

	if _, err := tx.Exec(`
		UPDATE t_day_off SET deleted_at = ?, updated_at = ? WHERE scope_id = ? AND day = ? AND deleted_at IS NULL`,
		now, now, scopeID, day); err != nil {
		return false, err
	}
	var exists int
	if err := tx.Get(&exists, `SELECT COUNT(*) FROM t_schedule_day WHERE scope_id = ? AND day = ?`, scopeID, day); err != nil {
		return false, err
	}
	if exists > 0 {
		if _, err := tx.Exec(`
			UPDATE t_schedule_day SET deleted_at = NULL, updated_at = ? WHERE scope_id = ? AND day = ?`,
			now, scopeID, day); err != nil {
			return false, err
		}
	} else {
		if _, err := tx.Exec(`INSERT INTO t_schedule_day(scope_id, day, created_at, updated_at) VALUES(?, ?, ?, ?)`,
			scopeID, day, now, now); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// ListDuties 列出该排班全部值班约束。
func (s *CalendarService) ListDuties(scopeID int64) ([]DutyView, error) {
	out := []DutyView{}
	err := s.store.DB().Select(&out, `
		SELECT d.day AS day,
		       d.shift_id AS shift_id, COALESCE(s.col1, '') AS shift,
		       d.teacher_id AS teacher_id, COALESCE(t.col1, '') AS teacher,
		       d.required AS required
		FROM t_duty d
		JOIN t_dataset s ON s.scope_id = d.scope_id AND s.id = d.shift_id AND s.deleted_at IS NULL
		JOIN t_dataset t ON t.scope_id = d.scope_id AND t.id = d.teacher_id AND t.deleted_at IS NULL
		WHERE d.scope_id = ? AND d.deleted_at IS NULL
		ORDER BY d.day, d.shift_id, d.teacher_id`, scopeID)
	return out, err
}

// SetDuty 设置某天某班次某老师的值班状态：required 值班、off 不值班、unspecified 取消指定。
func (s *CalendarService) SetDuty(scopeID int64, day string, shiftID int64, teacherID int64, status string) error {
	if status != model.DutyRequired && status != model.DutyOff && status != model.DutyUnspecified {
		return errors.New("无效的值班状态")
	}
	now := nowMS()
	if status == model.DutyUnspecified {
		_, err := s.store.DB().Exec(`
			UPDATE t_duty SET deleted_at = ?, updated_at = ?
			WHERE scope_id = ? AND day = ? AND shift_id = ? AND teacher_id = ? AND deleted_at IS NULL`,
			now, now, scopeID, day, shiftID, teacherID)
		return err
	}

	var scheduled int
	if err := s.store.DB().Get(&scheduled, `
		SELECT COUNT(*) FROM t_schedule_day WHERE scope_id = ? AND day = ? AND deleted_at IS NULL`, scopeID, day); err != nil {
		return err
	}
	if scheduled == 0 {
		return errors.New("该日期不在排班日内")
	}
	for _, ref := range []struct {
		typ    string
		id     int64
		errMsg string
	}{
		{model.DatasetShift, shiftID, "班次不存在"},
		{model.DatasetTeacher, teacherID, "老师不存在"},
	} {
		var n int
		if err := s.store.DB().Get(&n, `
			SELECT COUNT(*) FROM t_dataset WHERE id = ? AND scope_id = ? AND type = ? AND deleted_at IS NULL`,
			ref.id, scopeID, ref.typ); err != nil {
			return err
		}
		if n == 0 {
			return errors.New(ref.errMsg)
		}
	}

	required := 0
	if status == model.DutyRequired {
		required = 1
	}
	var dutyID int64
	err := s.store.DB().Get(&dutyID, `
		SELECT id FROM t_duty WHERE scope_id = ? AND day = ? AND shift_id = ? AND teacher_id = ? AND deleted_at IS NULL`,
		scopeID, day, shiftID, teacherID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = s.store.DB().Exec(`
			INSERT INTO t_duty(scope_id, day, shift_id, teacher_id, required, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?)`, scopeID, day, shiftID, teacherID, required, now, now)
		return err
	case err != nil:
		return err
	default:
		_, err = s.store.DB().Exec(`UPDATE t_duty SET required = ?, updated_at = ? WHERE id = ?`, required, now, dutyID)
		return err
	}
}

// removeScheduleDay 把某天移出排班日，并软删当天值班。
func removeScheduleDay(tx *sqlx.Tx, scopeID int64, day string, now int64) error {
	if _, err := tx.Exec(`
		UPDATE t_schedule_day SET deleted_at = ?, updated_at = ? WHERE scope_id = ? AND day = ? AND deleted_at IS NULL`,
		now, now, scopeID, day); err != nil {
		return err
	}
	_, err := tx.Exec(`
		UPDATE t_duty SET deleted_at = ?, updated_at = ? WHERE scope_id = ? AND day = ? AND deleted_at IS NULL`,
		now, now, scopeID, day)
	return err
}

// syncScheduleDays 按起止日期与星期重算排班日，休息日排除在外。
func syncScheduleDays(tx *sqlx.Tx, scopeID int64, start, end time.Time, weekdays []bool, now int64) error {
	want := map[string]bool{}
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		// Go 的 Weekday 周日为 0，这里下标 0 是周一。
		if weekdays[(int(day.Weekday())+6)%7] {
			want[day.Format(dateLayout)] = true
		}
	}

	offs := []string{}
	if err := tx.Select(&offs, `SELECT day FROM t_day_off WHERE scope_id = ? AND deleted_at IS NULL`, scopeID); err != nil {
		return err
	}
	for _, day := range offs {
		delete(want, day)
	}

	have := []string{}
	if err := tx.Select(&have, `SELECT day FROM t_schedule_day WHERE scope_id = ? AND deleted_at IS NULL`, scopeID); err != nil {
		return err
	}
	haveSet := make(map[string]bool, len(have))
	for _, day := range have {
		haveSet[day] = true
	}

	for day := range want {
		if haveSet[day] {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO t_schedule_day(scope_id, day, created_at, updated_at) VALUES(?, ?, ?, ?)`,
			scopeID, day, now, now); err != nil {
			return err
		}
	}
	for _, day := range have {
		if want[day] {
			continue
		}
		if err := removeScheduleDay(tx, scopeID, day, now); err != nil {
			return err
		}
	}
	return nil
}

// ensureFairRules 首次保存日历时补两条全局公平规则。
func ensureFairRules(tx *sqlx.Tx, scopeID int64, now int64) error {
	var count int
	if err := tx.Get(&count, `SELECT COUNT(*) FROM t_mapping WHERE scope_id = ? AND type = ? AND deleted_at IS NULL`,
		scopeID, model.MappingGlobalRule); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, ruleType := range []string{model.RuleFairCount, model.RuleFairInterval} {
		res, err := tx.Exec(`INSERT INTO t_rule(scope_id, type, created_at, updated_at) VALUES(?, ?, ?, ?)`,
			scopeID, ruleType, now, now)
		if err != nil {
			return err
		}
		ruleID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO t_mapping(scope_id, type, rule_id, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
			scopeID, model.MappingGlobalRule, ruleID, now, now); err != nil {
			return err
		}
	}
	return nil
}
