// Package model 定义元数据层的枚举常量，取值与 schema.sql 中的 type 字段对应。
package model

// t_dataset.type：范围内的实体类型。
const (
	DatasetTeacher = "teacher"
	DatasetClass   = "class"
	DatasetSubject = "subject"
	DatasetShift   = "shift"
	// DatasetBinding 是一条任课关系的中间节点，本身没有名称。
	DatasetBinding = "binding"
	// DatasetSubjectTag 是学科标签，名称存 col1；一个学科可挂多个标签。
	DatasetSubjectTag = "subject_tag"
	// DatasetMergedClass 是合班（逻辑班），名称存 col1；成员由 merged_class_member 连线记录。
	DatasetMergedClass = "merged_class"
)

// t_mapping.type：范围内的连线类型。
const (
	MappingTeacherBinding = "teacher_binding"
	MappingBindingSubject = "binding_subject"
	MappingBindingClass   = "binding_class"
	MappingGlobalRule     = "global_rule"
	MappingPersonalRule   = "personal_rule"
	// MappingSubjectTag 是学科到标签的挂载：from_id 是学科，to_id 是标签。
	MappingSubjectTag = "subject_tag"
	// MappingMergedClassMember 是合班到物理班的成员关系：from_id 是合班，to_id 是物理班。
	MappingMergedClassMember = "merged_class_member"
)

// t_rule.type：规则类型。
const (
	RuleFairCount    = "fair_count"
	RuleFairInterval = "fair_interval"
)

// t_duty.required 的三种状态。
const (
	DutyUnspecified = "unspecified"
	DutyRequired    = "required"
	DutyOff         = "off"
)
