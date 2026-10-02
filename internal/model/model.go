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
)

// t_mapping.type：范围内的连线类型。
const (
	MappingTeacherBinding = "teacher_binding"
	MappingBindingSubject = "binding_subject"
	MappingBindingClass   = "binding_class"
	MappingGlobalRule     = "global_rule"
	MappingPersonalRule   = "personal_rule"
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
