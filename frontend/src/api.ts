import type {
  BindingView as BindingViewType,
  DatasetView as DatasetViewType,
} from '../bindings/work-schedule/internal/services/models'
import type { TablePaginationConfig } from 'antd'
import * as CalendarService from '../bindings/work-schedule/internal/services/calendarservice'
import * as MetadataService from '../bindings/work-schedule/internal/services/metadataservice'
import * as ScopeService from '../bindings/work-schedule/internal/services/scopeservice'

export { CalendarService, MetadataService, ScopeService }

export type {
  BatchResult,
  BindTextResult,
  BindingView,
  CalendarConfig,
  DatasetView,
  DutyView,
  MemberView,
  ScopeInfo,
  TagView,
} from '../bindings/work-schedule/internal/services/models'

/** 列表里用的实体：任课字段已经归一成非空数组。 */
export type DatasetRow = Omit<DatasetViewType, 'bindings'> & { bindings: BindingViewType[] }

/** 把后端返回的可空任课数组归一成空数组。 */
export function toDatasetRows(list: DatasetViewType[] | null): DatasetRow[] {
  return (list ?? []).map((row) => ({ ...row, bindings: row.bindings ?? [] }))
}

// 实体类型，对应 t_dataset.type
export const DATASET_TEACHER = 'teacher'
export const DATASET_CLASS = 'class'
export const DATASET_SUBJECT = 'subject'
export const DATASET_SHIFT = 'shift'
export const DATASET_SUBJECT_TAG = 'subject_tag'
export const DATASET_MERGED_CLASS = 'merged_class'

// 值班状态，对应 t_duty.required
export const DUTY_UNSPECIFIED = 'unspecified'
export const DUTY_REQUIRED = 'required'
export const DUTY_OFF = 'off'

// 星期标签，下标 0 是周一
export const WEEKDAY_LABELS = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']

// 表格每页条数
export const PAGE_SIZE = 20

/**
 * 表格分页配置：每页可选 10 / 20 / 50，默认 20。
 * 传 onChange 用于翻页或改每页条数时清空勾选，避免选中了看不见的行。
 */
export function tablePagination(onChange?: () => void): TablePaginationConfig {
  const config: TablePaginationConfig = {
    defaultPageSize: PAGE_SIZE,
    showSizeChanger: true,
    pageSizeOptions: [10, 20, 50],
  }
  if (onChange) config.onChange = onChange
  return config
}

/** 把后端返回的错误转成可直接展示的文案。 */
export function errorText(err: unknown): string {
  if (err instanceof Error && err.message) return err.message
  return String(err)
}
