import type { ReactNode } from 'react'
import { Button, Input, Space } from 'antd'

type Props = {
  onAdd: () => void
  /** 批量删除，不传则不显示按钮 */
  onBatchDelete?: () => void
  /** 已选行数，为 0 时批量删除按钮禁用 */
  selectedCount?: number
  keyword: string
  onKeywordChange: (value: string) => void
  onSearch: () => void
  onReset: () => void
  /** 追加在左侧按钮区的额外操作，如学科页的「标签管理」 */
  extra?: ReactNode
}

/** 元数据页面统一的头部：左边添加 + 批量删除，右边按名称搜索。 */
export default function ListToolbar({
  onAdd,
  onBatchDelete,
  selectedCount = 0,
  keyword,
  onKeywordChange,
  onSearch,
  onReset,
  extra,
}: Props) {
  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        marginBottom: 16,
      }}
    >
      <Space>
        <Button type="primary" onClick={onAdd}>
          添加
        </Button>
        {onBatchDelete && (
          <Button danger disabled={selectedCount === 0} onClick={onBatchDelete}>
            批量删除
          </Button>
        )}
        {extra}
      </Space>
      <Space>
        <Input
          style={{ width: 220 }}
          placeholder="搜索名称"
          value={keyword}
          allowClear
          onChange={(e) => onKeywordChange(e.target.value)}
          onPressEnter={onSearch}
        />
        <Button type="primary" onClick={onSearch}>
          搜索
        </Button>
        <Button onClick={onReset}>重置</Button>
      </Space>
    </div>
  )
}
