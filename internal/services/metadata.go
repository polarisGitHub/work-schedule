package services

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"

	"work-schedule/internal/model"
	"work-schedule/internal/store"
)

// BindingView 一条任课关系：老师 + 学科 + 班级。一条任课在库里是三条 mapping。
type BindingView struct {
	TeacherID int64  `json:"teacherId"`
	Teacher   string `json:"teacher"`
	SubjectID int64  `json:"subjectId"`
	Subject   string `json:"subject"`
	ClassID   int64  `json:"classId"`
	Class     string `json:"class"`
}

// BindingInput 任课绑定的输入项。
type BindingInput struct {
	SubjectID int64 `json:"subjectId"`
	ClassID   int64 `json:"classId"`
}

// MemberView 合班包含的一个物理班。
type MemberView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// DatasetView 元数据实体；老师与班级附带任课信息，班次附带起止时间，学科附带标签，合班附带成员。
type DatasetView struct {
	ID       int64         `json:"id"`
	Name     string        `json:"name"`
	Start    string        `json:"start"`
	End      string        `json:"end"`
	Bindings []BindingView `json:"bindings"`
	TagIDs   []int64       `json:"tagIds"`
	Members  []MemberView  `json:"members"`
}

// TagView 一个学科标签、被多少学科挂载，以及是否为内置标签（内置的不允许删除）。
type TagView struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Count   int64  `json:"count"`
	Builtin bool   `json:"builtin"`
}

// t_dataset.col2 对学科标签存内置标记：内置标签（主课 / 专业课）不允许删除。
const subjectTagBuiltinFlag = "1"

// MetadataService 管理范围内的实体：老师、班级、学科、学科标签、班次，以及任课绑定。
type MetadataService struct {
	store *store.Store
}

func NewMetadataService(st *store.Store) *MetadataService {
	return &MetadataService{store: st}
}

// bindingQuery 取范围内全部有效任课，一次查完再按老师/班级挂到列表上。
const bindingQuery = `
SELECT m.from_id AS teacher_id, COALESCE(t.col1, '') AS teacher,
       bs.to_id  AS subject_id, COALESCE(s.col1, '') AS subject,
       bc.to_id  AS class_id,   COALESCE(c.col1, '') AS class
FROM t_dataset b
JOIN t_mapping m  ON m.scope_id = b.scope_id AND m.type = 'teacher_binding' AND m.to_id = b.id AND m.deleted_at IS NULL
JOIN t_dataset t  ON t.scope_id = b.scope_id AND t.id = m.from_id AND t.deleted_at IS NULL
JOIN t_mapping bs ON bs.scope_id = b.scope_id AND bs.type = 'binding_subject' AND bs.from_id = b.id AND bs.deleted_at IS NULL
JOIN t_dataset s  ON s.scope_id = b.scope_id AND s.id = bs.to_id AND s.deleted_at IS NULL
JOIN t_mapping bc ON bc.scope_id = b.scope_id AND bc.type = 'binding_class' AND bc.from_id = b.id AND bc.deleted_at IS NULL
JOIN t_dataset c  ON c.scope_id = b.scope_id AND c.id = bc.to_id AND c.deleted_at IS NULL
WHERE b.scope_id = ? AND b.type = 'binding' AND b.deleted_at IS NULL
ORDER BY b.id`

// ListDatasets 列出某类型的实体；老师与班级会带上任课，供列表展示。
func (s *MetadataService) ListDatasets(scopeID int64, typ string, keyword string) ([]DatasetView, error) {
	query := `SELECT id, COALESCE(col1, '') AS name, COALESCE(col2, '') AS start_time, COALESCE(col3, '') AS end_time
	          FROM t_dataset WHERE scope_id = ? AND type = ? AND deleted_at IS NULL`
	args := []any{scopeID, typ}
	if kw := strings.TrimSpace(keyword); kw != "" {
		query += ` AND col1 LIKE ?`
		args = append(args, "%"+kw+"%")
	}
	query += ` ORDER BY id`

	rows := []struct {
		ID    int64  `db:"id"`
		Name  string `db:"name"`
		Start string `db:"start_time"`
		End   string `db:"end_time"`
	}{}
	if err := s.store.DB().Select(&rows, query, args...); err != nil {
		return nil, err
	}

	out := make([]DatasetView, 0, len(rows))
	for _, r := range rows {
		out = append(out, DatasetView{
			ID: r.ID, Name: r.Name, Start: r.Start, End: r.End,
			Bindings: []BindingView{}, TagIDs: []int64{}, Members: []MemberView{},
		})
	}
	if typ == model.DatasetTeacher || typ == model.DatasetClass {
		if err := s.attachBindings(scopeID, typ, out); err != nil {
			return nil, err
		}
	}
	if typ == model.DatasetSubject {
		if err := s.attachSubjectTags(scopeID, out); err != nil {
			return nil, err
		}
	}
	if typ == model.DatasetMergedClass {
		if err := s.attachMembers(scopeID, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// mergedMemberQuery 取范围内全部有效的合班-物理班成员关系，一次查完再挂到合班上。
const mergedMemberQuery = `
SELECT m.from_id AS merged_id, c.id AS class_id, COALESCE(c.col1, '') AS class_name
FROM t_mapping m
JOIN t_dataset c ON c.scope_id = m.scope_id AND c.id = m.to_id AND c.deleted_at IS NULL
WHERE m.scope_id = ? AND m.type = 'merged_class_member' AND m.deleted_at IS NULL
ORDER BY m.from_id, c.id`

// attachMembers 把成员物理班挂到对应合班上。
func (s *MetadataService) attachMembers(scopeID int64, list []DatasetView) error {
	rows := []struct {
		MergedID  int64  `db:"merged_id"`
		ClassID   int64  `db:"class_id"`
		ClassName string `db:"class_name"`
	}{}
	if err := s.store.DB().Select(&rows, mergedMemberQuery, scopeID); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	index := make(map[int64]int, len(list))
	for i := range list {
		index[list[i].ID] = i
	}
	for _, r := range rows {
		if i, ok := index[r.MergedID]; ok {
			list[i].Members = append(list[i].Members, MemberView{ID: r.ClassID, Name: r.ClassName})
		}
	}
	return nil
}

// subjectTagQuery 取范围内全部有效的学科-标签挂载，一次查完再挂到学科列表上。
const subjectTagQuery = `
SELECT m.from_id AS subject_id, m.to_id AS tag_id
FROM t_mapping m
JOIN t_dataset t ON t.scope_id = m.scope_id AND t.id = m.to_id AND t.deleted_at IS NULL
WHERE m.scope_id = ? AND m.type = 'subject_tag' AND m.deleted_at IS NULL
ORDER BY m.from_id, m.to_id`

// attachSubjectTags 把标签 id 挂到对应学科上。
func (s *MetadataService) attachSubjectTags(scopeID int64, list []DatasetView) error {
	rows := []struct {
		SubjectID int64 `db:"subject_id"`
		TagID     int64 `db:"tag_id"`
	}{}
	if err := s.store.DB().Select(&rows, subjectTagQuery, scopeID); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	index := make(map[int64]int, len(list))
	for i := range list {
		index[list[i].ID] = i
	}
	for _, r := range rows {
		if i, ok := index[r.SubjectID]; ok {
			list[i].TagIDs = append(list[i].TagIDs, r.TagID)
		}
	}
	return nil
}

// ListSubjectTags 列出范围内全部学科标签，并统计各标签被多少学科挂载。
func (s *MetadataService) ListSubjectTags(scopeID int64) ([]TagView, error) {
	out := []TagView{}
	if err := s.store.DB().Select(&out, `
		SELECT t.id AS id, COALESCE(t.col1, '') AS name,
		       (SELECT COUNT(*) FROM t_mapping m
		        JOIN t_dataset s ON s.scope_id = m.scope_id AND s.id = m.from_id
		                            AND s.type = 'subject' AND s.deleted_at IS NULL
		        WHERE m.scope_id = t.scope_id AND m.type = 'subject_tag'
		          AND m.to_id = t.id AND m.deleted_at IS NULL) AS count,
		       CASE WHEN COALESCE(t.col2, '') = ? THEN 1 ELSE 0 END AS builtin
		FROM t_dataset t
		WHERE t.scope_id = ? AND t.type = 'subject_tag' AND t.deleted_at IS NULL
		ORDER BY t.id`, subjectTagBuiltinFlag, scopeID); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveSubjectTags 整表替换一个学科的标签挂载。
func (s *MetadataService) SaveSubjectTags(scopeID int64, subjectID int64, tagIDs []int64) error {
	tx, err := s.store.DB().Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.Get(&exists, `SELECT COUNT(*) FROM t_dataset WHERE id = ? AND scope_id = ? AND type = ? AND deleted_at IS NULL`,
		subjectID, scopeID, model.DatasetSubject); err != nil {
		return err
	}
	if exists == 0 {
		return errors.New("学科不存在")
	}

	now := nowMS()
	if _, err := tx.Exec(`
		UPDATE t_mapping SET deleted_at = ?, updated_at = ?
		WHERE scope_id = ? AND type = ? AND from_id = ? AND deleted_at IS NULL`,
		now, now, scopeID, model.MappingSubjectTag, subjectID); err != nil {
		return err
	}

	seen := map[int64]bool{}
	for _, tagID := range tagIDs {
		if tagID == 0 || seen[tagID] {
			continue
		}
		seen[tagID] = true
		var n int
		if err := tx.Get(&n, `SELECT COUNT(*) FROM t_dataset WHERE id = ? AND scope_id = ? AND type = ? AND deleted_at IS NULL`,
			tagID, scopeID, model.DatasetSubjectTag); err != nil {
			return err
		}
		if n == 0 {
			return errors.New("标签不存在")
		}
		if _, err := tx.Exec(`
			INSERT INTO t_mapping(scope_id, type, from_id, to_id, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`,
			scopeID, model.MappingSubjectTag, subjectID, tagID, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// bindingRow bindingQuery 的结果行。
type bindingRow struct {
	TeacherID int64  `db:"teacher_id"`
	Teacher   string `db:"teacher"`
	SubjectID int64  `db:"subject_id"`
	Subject   string `db:"subject"`
	ClassID   int64  `db:"class_id"`
	Class     string `db:"class"`
}

// attachBindings 把任课挂到老师（按老师）或班级（按班级）上。
func (s *MetadataService) attachBindings(scopeID int64, typ string, list []DatasetView) error {
	rows := []bindingRow{}
	if err := s.store.DB().Select(&rows, bindingQuery, scopeID); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	index := make(map[int64]int, len(list))
	for i := range list {
		index[list[i].ID] = i
	}
	for _, r := range rows {
		key := r.TeacherID
		if typ == model.DatasetClass {
			key = r.ClassID
		}
		i, ok := index[key]
		if !ok {
			continue
		}
		list[i].Bindings = append(list[i].Bindings, BindingView{
			TeacherID: r.TeacherID, Teacher: r.Teacher,
			SubjectID: r.SubjectID, Subject: r.Subject,
			ClassID: r.ClassID, Class: r.Class,
		})
	}
	return nil
}

// BatchResult 批量添加的结果。
type BatchResult struct {
	Inserted   []string `json:"inserted"`
	Duplicates []string `json:"duplicates"`
}

// SaveDatasets 批量添加实体：先对入参去重（粘贴的文本本身可能重复），
// 再和库里已有的名称对比，重复的不写入。支持老师、班级、学科、学科标签，班次还要填时间段。
func (s *MetadataService) SaveDatasets(scopeID int64, typ string, names []string) (BatchResult, error) {
	result := BatchResult{Inserted: []string{}, Duplicates: []string{}}
	switch typ {
	case model.DatasetTeacher, model.DatasetClass, model.DatasetSubject, model.DatasetSubjectTag:
	default:
		return result, errors.New("只支持批量添加老师、班级、学科、学科标签")
	}

	// 先去重：去空行、去重，保留第一次出现的顺序
	candidates := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		candidates = append(candidates, name)
	}
	if len(candidates) == 0 {
		return result, nil
	}

	tx, err := s.store.DB().Beginx()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	now := nowMS()
	for _, name := range candidates {
		var exists int
		if err := tx.Get(&exists, `
			SELECT COUNT(*) FROM t_dataset
			WHERE scope_id = ? AND type = ? AND col1 = ? AND deleted_at IS NULL`,
			scopeID, typ, name); err != nil {
			return result, err
		}
		if exists > 0 {
			result.Duplicates = append(result.Duplicates, name)
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO t_dataset(scope_id, type, col1, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
			scopeID, typ, name, now, now); err != nil {
			return result, err
		}
		result.Inserted = append(result.Inserted, name)
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

// SaveDataset 新增或改名。id 为 0 表示新增。
func (s *MetadataService) SaveDataset(scopeID int64, typ string, id int64, name string) (DatasetView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return DatasetView{}, errors.New("名称不能为空")
	}
	if err := s.checkNameUnique(scopeID, typ, id, name); err != nil {
		return DatasetView{}, err
	}

	now := nowMS()
	if id == 0 {
		res, err := s.store.DB().Exec(
			`INSERT INTO t_dataset(scope_id, type, col1, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
			scopeID, typ, name, now, now)
		if err != nil {
			return DatasetView{}, err
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return DatasetView{}, err
		}
		return DatasetView{ID: newID, Name: name, Bindings: []BindingView{}}, nil
	}

	res, err := s.store.DB().Exec(
		`UPDATE t_dataset SET col1 = ?, updated_at = ? WHERE id = ? AND scope_id = ? AND type = ? AND deleted_at IS NULL`,
		name, now, id, scopeID, typ)
	if err != nil {
		return DatasetView{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return DatasetView{}, errors.New("记录不存在")
	}
	return DatasetView{ID: id, Name: name, Bindings: []BindingView{}}, nil
}

// SaveShift 新增或修改班次，时间段不能与已有班次重叠。区间按左闭右开比较。
func (s *MetadataService) SaveShift(scopeID int64, id int64, name string, start string, end string) (DatasetView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return DatasetView{}, errors.New("班次名称不能为空")
	}
	startAt, err := normalizeClock(start)
	if err != nil {
		return DatasetView{}, err
	}
	endAt, err := normalizeClock(end)
	if err != nil {
		return DatasetView{}, err
	}
	if startAt >= endAt {
		return DatasetView{}, errors.New("结束时间必须晚于开始时间")
	}
	if err := s.checkNameUnique(scopeID, model.DatasetShift, id, name); err != nil {
		return DatasetView{}, err
	}

	var overlap int
	if err := s.store.DB().Get(&overlap, `
		SELECT COUNT(*) FROM t_dataset
		WHERE scope_id = ? AND type = ? AND deleted_at IS NULL AND id <> ?
		  AND col2 < ? AND col3 > ?`,
		scopeID, model.DatasetShift, id, endAt, startAt); err != nil {
		return DatasetView{}, err
	}
	if overlap > 0 {
		return DatasetView{}, errors.New("时间段与已有班次重叠")
	}

	now := nowMS()
	if id == 0 {
		res, err := s.store.DB().Exec(
			`INSERT INTO t_dataset(scope_id, type, col1, col2, col3, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
			scopeID, model.DatasetShift, name, startAt, endAt, now, now)
		if err != nil {
			return DatasetView{}, err
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return DatasetView{}, err
		}
		return DatasetView{ID: newID, Name: name, Start: startAt, End: endAt, Bindings: []BindingView{}}, nil
	}

	res, err := s.store.DB().Exec(`
		UPDATE t_dataset SET col1 = ?, col2 = ?, col3 = ?, updated_at = ?
		WHERE id = ? AND scope_id = ? AND type = ? AND deleted_at IS NULL`,
		name, startAt, endAt, now, id, scopeID, model.DatasetShift)
	if err != nil {
		return DatasetView{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return DatasetView{}, errors.New("记录不存在")
	}
	return DatasetView{ID: id, Name: name, Start: startAt, End: endAt, Bindings: []BindingView{}}, nil
}

// SaveMergedClass 新增或修改合班，整表替换成员物理班。id 为 0 表示新增。
// 合班名在 merged_class 内唯一；成员只能是物理班且至少 2 个；同一物理班可属于多个合班。
func (s *MetadataService) SaveMergedClass(scopeID int64, id int64, name string, memberIDs []int64) (DatasetView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return DatasetView{}, errors.New("名称不能为空")
	}
	if err := s.checkNameUnique(scopeID, model.DatasetMergedClass, id, name); err != nil {
		return DatasetView{}, err
	}

	members := make([]int64, 0, len(memberIDs))
	seen := map[int64]bool{}
	for _, mid := range memberIDs {
		if mid == 0 || seen[mid] {
			continue
		}
		seen[mid] = true
		members = append(members, mid)
	}
	if len(members) < 2 {
		return DatasetView{}, errors.New("合班至少需要选择 2 个班级")
	}

	tx, err := s.store.DB().Beginx()
	if err != nil {
		return DatasetView{}, err
	}
	defer tx.Rollback()

	for _, mid := range members {
		var n int
		if err := tx.Get(&n, `SELECT COUNT(*) FROM t_dataset WHERE id = ? AND scope_id = ? AND type = ? AND deleted_at IS NULL`,
			mid, scopeID, model.DatasetClass); err != nil {
			return DatasetView{}, err
		}
		if n == 0 {
			return DatasetView{}, errors.New("班级不存在")
		}
	}

	now := nowMS()
	mergedID := id
	if id == 0 {
		res, err := tx.Exec(`INSERT INTO t_dataset(scope_id, type, col1, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
			scopeID, model.DatasetMergedClass, name, now, now)
		if err != nil {
			return DatasetView{}, err
		}
		if mergedID, err = res.LastInsertId(); err != nil {
			return DatasetView{}, err
		}
	} else {
		res, err := tx.Exec(`UPDATE t_dataset SET col1 = ?, updated_at = ? WHERE id = ? AND scope_id = ? AND type = ? AND deleted_at IS NULL`,
			name, now, id, scopeID, model.DatasetMergedClass)
		if err != nil {
			return DatasetView{}, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return DatasetView{}, errors.New("记录不存在")
		}
		if _, err := tx.Exec(`UPDATE t_mapping SET deleted_at = ?, updated_at = ? WHERE scope_id = ? AND type = ? AND from_id = ? AND deleted_at IS NULL`,
			now, now, scopeID, model.MappingMergedClassMember, id); err != nil {
			return DatasetView{}, err
		}
	}

	for _, mid := range members {
		if _, err := tx.Exec(`INSERT INTO t_mapping(scope_id, type, from_id, to_id, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`,
			scopeID, model.MappingMergedClassMember, mergedID, mid, now, now); err != nil {
			return DatasetView{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return DatasetView{}, err
	}
	return s.loadMergedClass(scopeID, mergedID, name)
}

// loadMergedClass 读回合班及其成员，用于保存后返回。
func (s *MetadataService) loadMergedClass(scopeID int64, id int64, name string) (DatasetView, error) {
	view := DatasetView{ID: id, Name: name, Bindings: []BindingView{}, TagIDs: []int64{}, Members: []MemberView{}}
	rows := []struct {
		ID   int64  `db:"class_id"`
		Name string `db:"class_name"`
	}{}
	if err := s.store.DB().Select(&rows, `
		SELECT c.id AS class_id, COALESCE(c.col1, '') AS class_name
		FROM t_mapping m
		JOIN t_dataset c ON c.scope_id = m.scope_id AND c.id = m.to_id AND c.deleted_at IS NULL
		WHERE m.scope_id = ? AND m.type = ? AND m.from_id = ? AND m.deleted_at IS NULL
		ORDER BY c.id`, scopeID, model.MappingMergedClassMember, id); err != nil {
		return DatasetView{}, err
	}
	for _, r := range rows {
		view.Members = append(view.Members, MemberView{ID: r.ID, Name: r.Name})
	}
	return view, nil
}

// DeleteDataset 软删实体：老师 / 班级 / 学科连带删相关任课，并连带软删引用它的值班与课表格子；删标签只解除挂载，内置标签不允许删除。
func (s *MetadataService) DeleteDataset(scopeID int64, id int64) error {
	tx, err := s.store.DB().Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := deleteDatasetTx(tx, scopeID, id, nowMS()); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteDatasets 批量软删实体，已经不存在（或被删过）的直接跳过。老师 / 班级 / 学科 / 班次 / 学科标签都走这里。
func (s *MetadataService) DeleteDatasets(scopeID int64, ids []int64) error {
	tx, err := s.store.DB().Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := nowMS()
	for _, id := range ids {
		if err := deleteDatasetTx(tx, scopeID, id, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// deleteDatasetTx 在事务里软删一个实体及其从属数据。
func deleteDatasetTx(tx *sqlx.Tx, scopeID int64, id int64, now int64) error {
	var typ string
	if err := tx.Get(&typ, `SELECT type FROM t_dataset WHERE id = ? AND scope_id = ? AND deleted_at IS NULL`, id, scopeID); err != nil {
		// 批量删除时记录可能已经不在，跳过即可
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	// 值班与课表格子引用了这个实体，一并软删，否则物理清理时会撞上外键 RESTRICT。
	detachRefs := []string{
		`UPDATE t_duty SET deleted_at = ? WHERE scope_id = ? AND shift_id = ? AND deleted_at IS NULL`,
		`UPDATE t_duty SET deleted_at = ? WHERE scope_id = ? AND teacher_id = ? AND deleted_at IS NULL`,
		`UPDATE t_assignment SET deleted_at = ? WHERE scope_id = ? AND shift_id = ? AND deleted_at IS NULL`,
		`UPDATE t_assignment SET deleted_at = ? WHERE scope_id = ? AND teacher_id = ? AND deleted_at IS NULL`,
		`UPDATE t_assignment SET deleted_at = ? WHERE scope_id = ? AND class_id = ? AND deleted_at IS NULL`,
	}
	for _, stmt := range detachRefs {
		if _, err := tx.Exec(stmt, now, scopeID, id); err != nil {
			return err
		}
	}

	// 学科-标签的挂载随两端任意一端删除而解除：删学科清 from_id，删标签清 to_id。
	if typ == model.DatasetSubject {
		if _, err := tx.Exec(`
			UPDATE t_mapping SET deleted_at = ?, updated_at = ?
			WHERE scope_id = ? AND type = ? AND from_id = ? AND deleted_at IS NULL`,
			now, now, scopeID, model.MappingSubjectTag, id); err != nil {
			return err
		}
	}
	if typ == model.DatasetSubjectTag {
		var builtin int
		if err := tx.Get(&builtin, `
			SELECT CASE WHEN COALESCE(col2, '') = ? THEN 1 ELSE 0 END
			FROM t_dataset WHERE id = ? AND scope_id = ?`,
			subjectTagBuiltinFlag, id, scopeID); err != nil {
			return err
		}
		if builtin == 1 {
			return errors.New("内置标签不能删除")
		}
		if _, err := tx.Exec(`
			UPDATE t_mapping SET deleted_at = ?, updated_at = ?
			WHERE scope_id = ? AND type = ? AND to_id = ? AND deleted_at IS NULL`,
			now, now, scopeID, model.MappingSubjectTag, id); err != nil {
			return err
		}
	}

	// 合班的成员关系随合班删除而解除；物理班删除时也从它所属的合班里移除该成员。
	if typ == model.DatasetMergedClass {
		if _, err := tx.Exec(`
			UPDATE t_mapping SET deleted_at = ?, updated_at = ?
			WHERE scope_id = ? AND type = ? AND from_id = ? AND deleted_at IS NULL`,
			now, now, scopeID, model.MappingMergedClassMember, id); err != nil {
			return err
		}
	}
	if typ == model.DatasetClass {
		if _, err := tx.Exec(`
			UPDATE t_mapping SET deleted_at = ?, updated_at = ?
			WHERE scope_id = ? AND type = ? AND to_id = ? AND deleted_at IS NULL`,
			now, now, scopeID, model.MappingMergedClassMember, id); err != nil {
			return err
		}
	}

	var bindingIDs []int64
	var err error
	switch typ {
	case model.DatasetTeacher:
		err = selectTeacherBindingIDs(tx, scopeID, id, &bindingIDs)
	case model.DatasetClass:
		err = selectBindingIDsByTarget(tx, model.MappingBindingClass, scopeID, id, &bindingIDs)
	case model.DatasetSubject:
		err = selectBindingIDsByTarget(tx, model.MappingBindingSubject, scopeID, id, &bindingIDs)
	}
	if err != nil {
		return err
	}
	for _, bindingID := range bindingIDs {
		if err := softDeleteBinding(tx, scopeID, bindingID, now); err != nil {
			return err
		}
	}

	_, err = tx.Exec(`UPDATE t_dataset SET deleted_at = ?, updated_at = ? WHERE id = ? AND scope_id = ?`, now, now, id, scopeID)
	return err
}

// SaveTeacherBindings 整表替换一个老师的任课关系。
func (s *MetadataService) SaveTeacherBindings(scopeID int64, teacherID int64, pairs []BindingInput) error {
	tx, err := s.store.DB().Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.Get(&exists, `SELECT COUNT(*) FROM t_dataset WHERE id = ? AND scope_id = ? AND type = ? AND deleted_at IS NULL`,
		teacherID, scopeID, model.DatasetTeacher); err != nil {
		return err
	}
	if exists == 0 {
		return errors.New("老师不存在")
	}

	now := nowMS()
	var oldIDs []int64
	if err := selectTeacherBindingIDs(tx, scopeID, teacherID, &oldIDs); err != nil {
		return err
	}
	for _, bindingID := range oldIDs {
		if err := softDeleteBinding(tx, scopeID, bindingID, now); err != nil {
			return err
		}
	}

	seen := map[[2]int64]bool{}
	for _, pair := range pairs {
		if pair.SubjectID == 0 || pair.ClassID == 0 {
			return errors.New("请选择学科和班级")
		}
		key := [2]int64{pair.SubjectID, pair.ClassID}
		if seen[key] {
			continue
		}
		seen[key] = true

		for _, ref := range []struct {
			typ    string
			id     int64
			errMsg string
		}{
			{model.DatasetSubject, pair.SubjectID, "学科不存在"},
			{model.DatasetClass, pair.ClassID, "班级不存在"},
		} {
			var n int
			if err := tx.Get(&n, `SELECT COUNT(*) FROM t_dataset WHERE id = ? AND scope_id = ? AND type = ? AND deleted_at IS NULL`,
				ref.id, scopeID, ref.typ); err != nil {
				return err
			}
			if n == 0 {
				return errors.New(ref.errMsg)
			}
		}

		res, err := tx.Exec(`INSERT INTO t_dataset(scope_id, type, created_at, updated_at) VALUES(?, ?, ?, ?)`,
			scopeID, model.DatasetBinding, now, now)
		if err != nil {
			return err
		}
		bindingID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		links := []struct {
			typ      string
			from, to int64
		}{
			{model.MappingTeacherBinding, teacherID, bindingID},
			{model.MappingBindingSubject, bindingID, pair.SubjectID},
			{model.MappingBindingClass, bindingID, pair.ClassID},
		}
		for _, link := range links {
			if _, err := tx.Exec(
				`INSERT INTO t_mapping(scope_id, type, from_id, to_id, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`,
				scopeID, link.typ, link.from, link.to, now, now); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// BindTextResult 文本批量绑定的结果。errors 非空表示整批都没有写入。
type BindTextResult struct {
	Inserted int      `json:"inserted"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors"`
}

// BindByText 按「课程 班级 老师」的文本批量建立任课绑定。
// 每行一条，空白分隔三列，顺序固定为 课程 / 班级 / 老师；三者在当前排班里都必须已存在。
// 只要有一行不合法（列数不对、实体不存在、同名无法确定）整批就不写入，逐行原因放进 errors；
// 这样一次粘贴要么全部成功、要么不改动任何数据，用户改完重贴即可。
// 已经存在的绑定不算错误，跳过并计入 skipped。
func (s *MetadataService) BindByText(scopeID int64, text string) (BindTextResult, error) {
	result := BindTextResult{Errors: []string{}}

	// 实体名 → id；同类型下重名用 -1 标记，避免绑错。
	metas := []struct {
		ID   int64  `db:"id"`
		Type string `db:"type"`
		Name string `db:"name"`
	}{}
	if err := s.store.DB().Select(&metas, `
		SELECT id, type, COALESCE(col1, '') AS name FROM t_dataset
		WHERE scope_id = ? AND deleted_at IS NULL AND type IN (?, ?, ?)`,
		scopeID, model.DatasetSubject, model.DatasetClass, model.DatasetTeacher); err != nil {
		return result, err
	}
	subjectIDs := map[string]int64{}
	classIDs := map[string]int64{}
	teacherIDs := map[string]int64{}
	addName := func(m map[string]int64, name string, id int64) {
		if _, ok := m[name]; ok {
			m[name] = -1
			return
		}
		m[name] = id
	}
	for _, m := range metas {
		switch m.Type {
		case model.DatasetSubject:
			addName(subjectIDs, m.Name, m.ID)
		case model.DatasetClass:
			addName(classIDs, m.Name, m.ID)
		case model.DatasetTeacher:
			addName(teacherIDs, m.Name, m.ID)
		}
	}

	// 已有任课，用于判重；重复的不再新建绑定节点。
	existing := map[[3]int64]bool{}
	rows := []bindingRow{}
	if err := s.store.DB().Select(&rows, bindingQuery, scopeID); err != nil {
		return result, err
	}
	for _, r := range rows {
		existing[[3]int64{r.TeacherID, r.SubjectID, r.ClassID}] = true
	}

	resolve := func(m map[string]int64, label, name string) (int64, string) {
		id, ok := m[name]
		if !ok {
			return 0, fmt.Sprintf("%s「%s」不存在", label, name)
		}
		if id < 0 {
			return 0, fmt.Sprintf("%s「%s」存在多个同名项，无法确定", label, name)
		}
		return id, ""
	}

	// 先只解析校验、收集待写入项，确认全部合法后再落库。
	type pendingBinding struct{ subjectID, classID, teacherID int64 }
	pending := []pendingBinding{}
	seen := map[[3]int64]bool{}
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行「%s」：格式不正确，应为「课程 班级 老师」三列，空格分隔", i+1, line))
			continue
		}
		subjectID, subjectErr := resolve(subjectIDs, "课程", fields[0])
		classID, classErr := resolve(classIDs, "班级", fields[1])
		teacherID, teacherErr := resolve(teacherIDs, "老师", fields[2])
		msgs := []string{}
		for _, m := range []string{subjectErr, classErr, teacherErr} {
			if m != "" {
				msgs = append(msgs, m)
			}
		}
		if len(msgs) > 0 {
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行「%s」：%s", i+1, line, strings.Join(msgs, "；")))
			continue
		}
		key := [3]int64{teacherID, subjectID, classID}
		if existing[key] || seen[key] {
			result.Skipped++
			continue
		}
		seen[key] = true
		pending = append(pending, pendingBinding{subjectID: subjectID, classID: classID, teacherID: teacherID})
	}

	if len(result.Errors) > 0 || len(pending) == 0 {
		return result, nil
	}

	tx, err := s.store.DB().Beginx()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	now := nowMS()
	for _, b := range pending {
		res, err := tx.Exec(`INSERT INTO t_dataset(scope_id, type, created_at, updated_at) VALUES(?, ?, ?, ?)`,
			scopeID, model.DatasetBinding, now, now)
		if err != nil {
			return result, err
		}
		bindingID, err := res.LastInsertId()
		if err != nil {
			return result, err
		}
		links := []struct {
			typ      string
			from, to int64
		}{
			{model.MappingTeacherBinding, b.teacherID, bindingID},
			{model.MappingBindingSubject, bindingID, b.subjectID},
			{model.MappingBindingClass, bindingID, b.classID},
		}
		for _, link := range links {
			if _, err := tx.Exec(
				`INSERT INTO t_mapping(scope_id, type, from_id, to_id, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`,
				scopeID, link.typ, link.from, link.to, now, now); err != nil {
				return result, err
			}
		}
		result.Inserted++
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

// ClearTeacherBindings 清除若干老师名下的全部任课绑定，老师本身保留。
// 老师不存在或本来就没有绑定都直接跳过，不报错；整体在一个事务里完成。
func (s *MetadataService) ClearTeacherBindings(scopeID int64, teacherIDs []int64) error {
	tx, err := s.store.DB().Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := nowMS()
	for _, teacherID := range teacherIDs {
		var bindingIDs []int64
		if err := selectTeacherBindingIDs(tx, scopeID, teacherID, &bindingIDs); err != nil {
			return err
		}
		for _, bindingID := range bindingIDs {
			if err := softDeleteBinding(tx, scopeID, bindingID, now); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// selectTeacherBindingIDs 找出某个老师名下的全部任课节点 id。
func selectTeacherBindingIDs(tx *sqlx.Tx, scopeID int64, teacherID int64, out *[]int64) error {
	return tx.Select(out, `
		SELECT b.id FROM t_dataset b
		JOIN t_mapping m ON m.scope_id = b.scope_id AND m.type = ? AND m.to_id = b.id AND m.deleted_at IS NULL
		WHERE b.scope_id = ? AND b.type = ? AND b.deleted_at IS NULL AND m.from_id = ?`,
		model.MappingTeacherBinding, scopeID, model.DatasetBinding, teacherID)
}

// checkNameUnique 同一范围内同类型名称不重复。
func (s *MetadataService) checkNameUnique(scopeID int64, typ string, id int64, name string) error {
	var dup int
	if err := s.store.DB().Get(&dup, `
		SELECT COUNT(*) FROM t_dataset
		WHERE scope_id = ? AND type = ? AND col1 = ? AND deleted_at IS NULL AND id <> ?`,
		scopeID, typ, name, id); err != nil {
		return err
	}
	if dup > 0 {
		return errors.New("名称已存在")
	}
	return nil
}

// selectBindingIDsByTarget 找出引用了某个班级/学科的任课节点。
func selectBindingIDsByTarget(tx *sqlx.Tx, mappingType string, scopeID int64, targetID int64, out *[]int64) error {
	return tx.Select(out, `
		SELECT b.id FROM t_dataset b
		JOIN t_mapping m ON m.scope_id = b.scope_id AND m.type = ? AND m.from_id = b.id AND m.deleted_at IS NULL
		WHERE b.scope_id = ? AND b.type = ? AND b.deleted_at IS NULL AND m.to_id = ?`,
		mappingType, scopeID, model.DatasetBinding, targetID)
}

// softDeleteBinding 删掉一条任课：三条连线加中间的绑定节点。
func softDeleteBinding(tx *sqlx.Tx, scopeID int64, bindingID int64, now int64) error {
	softDeleteLinks := []string{
		`UPDATE t_mapping SET deleted_at = ?, updated_at = ? WHERE scope_id = ? AND from_id = ? AND deleted_at IS NULL`,
		`UPDATE t_mapping SET deleted_at = ?, updated_at = ? WHERE scope_id = ? AND to_id = ? AND deleted_at IS NULL`,
	}
	for _, stmt := range softDeleteLinks {
		if _, err := tx.Exec(stmt, now, now, scopeID, bindingID); err != nil {
			return err
		}
	}
	_, err := tx.Exec(
		`UPDATE t_dataset SET deleted_at = ?, updated_at = ? WHERE scope_id = ? AND id = ? AND deleted_at IS NULL`,
		now, now, scopeID, bindingID)
	return err
}

var clockRe = regexp.MustCompile(`^([0-9]{2}):([0-9]{2})(?::([0-9]{2}))?$`)

// normalizeClock 把 HH:mm 或 HH:mm:ss 统一成 HH:mm:ss。
func normalizeClock(v string) (string, error) {
	m := clockRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return "", errors.New("时间格式不正确")
	}
	hour, _ := strconv.Atoi(m[1])
	minute, _ := strconv.Atoi(m[2])
	second := 0
	if m[3] != "" {
		second, _ = strconv.Atoi(m[3])
	}
	if hour > 23 || minute > 59 || second > 59 {
		return "", errors.New("时间格式不正确")
	}
	return m[1] + ":" + m[2] + ":" + fmt.Sprintf("%02d", second), nil
}
