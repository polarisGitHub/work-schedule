# 合班（逻辑班）交互设计

日期：2026-10-02

## 背景与目标

- 正常情况下一个老师守一个班；部分班级由一个老师同时守多个班，这几个班整体视为一个「合班」。
- 导入的班是**物理班**；合班是**逻辑班**，只能由物理班合成。
- 目标：在「班级」tab 下增加合班的管理入口，且**不改变现有任课绑定与排班数据结构**。

## 已确认的决策

1. 交互：班级 tab 内加子标签 `[正常班] [合班]`。
2. 一个物理班**可同时属于多个合班**（合班不是对物理班的划分）。
3. 任课绑定仍绑在**物理班**上，不绑到合班 → `TeacherPage` 不变。
4. 合班**不区分学科/班次**，合班表也**不展示老师/学科**。

## 数据模型

复用现有的实体 + 关系模型（`t_dataset` / `t_mapping`），**不新增表**：

- 新实体类型 `merged_class`：合班名存 `t_dataset.col1`。
- 新关系类型 `merged_class_member`：`from_id` = 合班，`to_id` = 物理班。多对多。

`internal/model/model.go` 新增两个常量：

```go
DatasetMergedClass        = "merged_class"
MappingMergedClassMember  = "merged_class_member"
```

## 后端接口（MetadataService）

- `ListDatasets(scope, "merged_class", kw)`：返回合班，附带成员。
  `DatasetView` 增加 `Members []MemberView`（`MemberView{ ID, Name }`）。
- `SaveMergedClass(scope, id, name, memberIDs)`：`id=0` 为新增。校验后**整表替换**成员关系（软删旧 link 再插新 link）。
- 删除合班：软删合班实体 + 其 `merged_class_member` 连线（`from_id` = 合班）。走现有 `deleteDatasetTx` 加分支。
- 删除物理班：`deleteDatasetTx` 额外软删以它为 `to_id` 的 `merged_class_member` 连线，即**自动从所有合班移除**。

校验规则：

- 合班名在同 scope 的 `merged_class` 内唯一。
- 成员必须存在且类型是 `class`。
- 成员数 ≥ 2。
- 不校验「物理班是否已在别的合班」——多对多，允许重叠。

## 前端

- `frontend/src/api.ts`：新增 `DATASET_MERGED_CLASS = 'merged_class'` 与成员类型。
- `frontend/src/pages/ClassPage.tsx`：改为 `Tabs` 容器，两个子标签。
  - **正常班**：现有表原样，新增一列「所属合班」（显示合班 tag，可能多个；独立班显示 `—`）。
  - **合班**：新表，列＝`名称 / 包含班级(tag) / 操作`；工具栏「新建合班」→ `MergedClassModal`（名称 input + 物理班多选）；行内「编辑成员 / 改名 / 解散」。
- `TeacherPage`：不变（绑定仍只选物理班）。

## 规则与边界

- 合班名在 `merged_class` 内唯一；不与物理班名做联动校验（不同实体类型）。
- 删除物理班自动从合班移除，不阻断删除；合班成员数可能因此 < 2，暂不自动处理，由用户手动调整。
- 删除合班不影响物理班与任课绑定。

## 不在本次范围

- 合班参与排班计算（solver 尚未实现）。
- 按学科/班次细分合班。
- 合班与指定老师的一对一关联。

## 假设（待校正）

- 合班成员最少 2 个。
- 正常班表的「所属合班」列默认显示（可关）。
