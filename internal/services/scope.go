package services

import (
	"errors"
	"strings"
	"time"

	"work-schedule/internal/store"
)

// nowMS 当前时间的 Unix 毫秒，业务表的时间列统一用它。
func nowMS() int64 { return time.Now().UnixMilli() }

// ScopeInfo 排班范围（一个排班）。
type ScopeInfo struct {
	ID   int64  `db:"id" json:"id"`
	Name string `db:"name" json:"name"`
}

// ScopeService 管理排班范围：切换的前提是有一个排班列表。
type ScopeService struct {
	store *store.Store
}

func NewScopeService(st *store.Store) *ScopeService {
	return &ScopeService{store: st}
}

// ListScopes 返回未删除的排班，按创建顺序。
func (s *ScopeService) ListScopes() ([]ScopeInfo, error) {
	out := []ScopeInfo{}
	err := s.store.DB().Select(&out, `
		SELECT id, name FROM t_scope WHERE deleted_at IS NULL ORDER BY id`)
	return out, err
}

// CreateScope 新建排班，名称不能与现有排班重复。
func (s *ScopeService) CreateScope(name string) (ScopeInfo, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ScopeInfo{}, errors.New("排班名称不能为空")
	}
	var exists int
	if err := s.store.DB().Get(&exists, `SELECT COUNT(*) FROM t_scope WHERE name = ? AND deleted_at IS NULL`, name); err != nil {
		return ScopeInfo{}, err
	}
	if exists > 0 {
		return ScopeInfo{}, errors.New("已存在同名排班")
	}
	now := nowMS()
	res, err := s.store.DB().Exec(`INSERT INTO t_scope(name, created_at, updated_at) VALUES(?, ?, ?)`, name, now, now)
	if err != nil {
		return ScopeInfo{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ScopeInfo{}, err
	}
	return ScopeInfo{ID: id, Name: name}, nil
}

// RenameScope 修改排班名称。
func (s *ScopeService) RenameScope(scopeID int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("排班名称不能为空")
	}
	var dup int
	if err := s.store.DB().Get(&dup, `
		SELECT COUNT(*) FROM t_scope WHERE name = ? AND deleted_at IS NULL AND id <> ?`, name, scopeID); err != nil {
		return err
	}
	if dup > 0 {
		return errors.New("已存在同名排班")
	}
	res, err := s.store.DB().Exec(`
		UPDATE t_scope SET name = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		name, nowMS(), scopeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("排班不存在")
	}
	return nil
}

// softDeleteScopeSQL 逻辑删除排班时连带软删的下属表。
// 全部写成静态 SQL，不拼表名，表名清单本身就是白名单。
var softDeleteScopeSQL = []string{
	`UPDATE t_assignment SET deleted_at = ? WHERE scope_id = ? AND deleted_at IS NULL`,
	`UPDATE t_duty SET deleted_at = ? WHERE scope_id = ? AND deleted_at IS NULL`,
	`UPDATE t_schedule_day SET deleted_at = ? WHERE scope_id = ? AND deleted_at IS NULL`,
	`UPDATE t_day_off SET deleted_at = ? WHERE scope_id = ? AND deleted_at IS NULL`,
	`UPDATE t_calendar SET deleted_at = ? WHERE scope_id = ? AND deleted_at IS NULL`,
	`UPDATE t_mapping SET deleted_at = ? WHERE scope_id = ? AND deleted_at IS NULL`,
	`UPDATE t_rule SET deleted_at = ? WHERE scope_id = ? AND deleted_at IS NULL`,
	`UPDATE t_version SET deleted_at = ? WHERE scope_id = ? AND deleted_at IS NULL`,
	`UPDATE t_dataset SET deleted_at = ? WHERE scope_id = ? AND deleted_at IS NULL`,
}

// DeleteScope 逻辑删除一个排班，它下面的全部数据一起软删。
func (s *ScopeService) DeleteScope(scopeID int64) error {
	tx, err := s.store.DB().Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := nowMS()
	for _, stmt := range softDeleteScopeSQL {
		if _, err := tx.Exec(stmt, now, scopeID); err != nil {
			return err
		}
	}
	res, err := tx.Exec(`UPDATE t_scope SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, now, now, scopeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("排班不存在")
	}
	return tx.Commit()
}

// purgeDeletedSQL 物理删除逻辑删除数据的 SQL，按「子表在前」排序，
// 避免外键 RESTRICT 拦住父表。静态 SQL，不拼表名。
var purgeDeletedSQL = []string{
	`DELETE FROM t_assignment WHERE deleted_at IS NOT NULL`, // → t_version, t_dataset
	`DELETE FROM t_duty WHERE deleted_at IS NOT NULL`,       // → t_dataset
	`DELETE FROM t_mapping WHERE deleted_at IS NOT NULL`,    // → t_dataset, t_rule
	`DELETE FROM t_schedule_day WHERE deleted_at IS NOT NULL`,
	`DELETE FROM t_day_off WHERE deleted_at IS NOT NULL`,
	`DELETE FROM t_calendar WHERE deleted_at IS NOT NULL`,
	`DELETE FROM t_rule WHERE deleted_at IS NOT NULL`,    // 引用方 t_mapping 已清
	`DELETE FROM t_version WHERE deleted_at IS NOT NULL`, // 引用方 t_assignment 已清
	`DELETE FROM t_dataset WHERE deleted_at IS NOT NULL`, // 引用方上面的子表已清
	`DELETE FROM t_scope WHERE deleted_at IS NOT NULL`,   // 排班本身最后删
}

// PurgeDeleted 把全库里逻辑删除的数据做物理删除，返回删除的行数。
func (s *ScopeService) PurgeDeleted() (int64, error) {
	tx, err := s.store.DB().Beginx()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var total int64
	for _, stmt := range purgeDeletedSQL {
		res, err := tx.Exec(stmt)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err == nil {
			total += n
		}
	}
	return total, tx.Commit()
}
