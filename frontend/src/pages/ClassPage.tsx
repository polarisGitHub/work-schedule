import { useCallback, useEffect, useMemo, useState } from 'react'
import { App as AntdApp, Button, Checkbox, Popconfirm, Space, Table, Tabs, Tag } from 'antd'
import type { TableColumnsType, TablePaginationConfig } from 'antd'

import BatchNameModal from '../components/BatchNameModal'
import ListToolbar from '../components/ListToolbar'
import MergedClassModal from '../components/MergedClassModal'
import NameModal from '../components/NameModal'
import { reportBatchResult } from '../batchReport'
import {
  DATASET_CLASS,
  DATASET_MERGED_CLASS,
  MetadataService,
  PAGE_SIZE,
  errorText,
  tablePagination,
  toDatasetRows,
} from '../api'
import type { BindingView, DatasetRow, DatasetView } from '../api'

type Props = { scopeId: number }

/** 班级页：正常班（物理班）与合班（逻辑班）两个子标签。切换时销毁另一个，回来即重新加载。 */
export default function ClassPage({ scopeId }: Props) {
  return (
    <Tabs
      destroyOnHidden
      items={[
        { key: 'normal', label: '正常班', children: <NormalClassTab scopeId={scopeId} /> },
        { key: 'merged', label: '合班', children: <MergedClassTab scopeId={scopeId} /> },
      ]}
    />
  )
}

/** 表格行：班级的每一条任课占一行，没有任课的班级占一行，名称/操作列按 rowSpan 合并。 */
type ClassTableRow = DatasetRow & {
  binding: BindingView | null
  /** 该班级在本页首行是任课条数，后续行为 0（被合并掉）；没有任课时为 1。 */
  rowSpan: number
  /** 同一班级内第几条任课，用来拼唯一 rowKey。 */
  bindingIndex: number
}

/** 正常班：增删改物理班，任课信息只读；「所属合班」列来自合班标签。 */
function NormalClassTab({ scopeId }: { scopeId: number }) {
  const { message, modal } = AntdApp.useApp()

  const [rows, setRows] = useState<DatasetRow[]>([])
  const [merged, setMerged] = useState<DatasetView[]>([])
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [search, setSearch] = useState('')
  /** 勾选的班级 id；表格按任课展开成多行，勾选仍以班级为单位 */
  const [selectedIds, setSelectedIds] = useState<number[]>([])

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<{ open: boolean; id: number; name: string }>({
    open: false,
    id: 0,
    name: '',
  })

  // 分页按「班级」算：先切出本页班级，再展开成任课行，避免一个班级的多行被翻页切断
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(PAGE_SIZE)

  const maxPage = Math.max(1, Math.ceil(rows.length / pageSize))
  const currentPage = Math.min(page, maxPage)

  const tableRows = useMemo<ClassTableRow[]>(() => {
    const start = (currentPage - 1) * pageSize
    return rows.slice(start, start + pageSize).flatMap<ClassTableRow>((row) =>
      row.bindings.length === 0
        ? [{ ...row, binding: null, rowSpan: 1, bindingIndex: 0 }]
        : row.bindings.map((binding, i) => ({
            ...row,
            binding,
            rowSpan: i === 0 ? row.bindings.length : 0,
            bindingIndex: i,
          })),
    )
  }, [rows, currentPage, pageSize])

  // 物理班 id → 它所属的合班名（可能多个）
  const mergedNamesByClass = useMemo(() => {
    const map = new Map<number, string[]>()
    for (const item of merged) {
      for (const member of item.members ?? []) {
        const names = map.get(member.id) ?? []
        names.push(item.name)
        map.set(member.id, names)
      }
    }
    return map
  }, [merged])

  const pageClassIds = useMemo(
    () => rows.slice((currentPage - 1) * pageSize, currentPage * pageSize).map((row) => row.id),
    [rows, currentPage, pageSize],
  )
  const allPageSelected = pageClassIds.length > 0 && pageClassIds.every((id) => selectedIds.includes(id))
  const somePageSelected = pageClassIds.some((id) => selectedIds.includes(id))

  const toggleClass = (id: number, checked: boolean) => {
    setSelectedIds((prev) => (checked ? [...prev, id] : prev.filter((item) => item !== id)))
  }

  const togglePage = (checked: boolean) => {
    setSelectedIds((prev) =>
      checked
        ? [...new Set([...prev, ...pageClassIds])]
        : prev.filter((id) => !pageClassIds.includes(id)),
    )
  }

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [list, mergedList] = await Promise.all([
        MetadataService.ListDatasets(scopeId, DATASET_CLASS, search),
        MetadataService.ListDatasets(scopeId, DATASET_MERGED_CLASS, ''),
      ])
      setRows(toDatasetRows(list))
      setMerged(mergedList ?? [])
      setSelectedIds([])
    } catch (err) {
      message.error(errorText(err))
    } finally {
      setLoading(false)
    }
  }, [scopeId, search, message])

  useEffect(() => {
    void load()
  }, [load])

  const addNames = async (names: string[]) => {
    try {
      const result = await MetadataService.SaveDatasets(scopeId, DATASET_CLASS, names)
      setAdding(false)
      await load()
      reportBatchResult(result, { message, modal })
    } catch (err) {
      message.error(errorText(err))
    }
  }

  const batchDelete = () => {
    const ids = selectedIds
    modal.confirm({
      title: `删除选中的 ${ids.length} 项？`,
      content: '关联的任课绑定、值班与课表格子会一起删除；同时会从所属合班中移除',
      okText: '确定',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await MetadataService.DeleteDatasets(scopeId, ids)
          message.success('删除成功')
          await load()
        } catch (err) {
          message.error(errorText(err))
        }
      },
    })
  }

  const saveName = async (name: string) => {
    try {
      await MetadataService.SaveDataset(scopeId, DATASET_CLASS, editing.id, name)
      setEditing({ open: false, id: 0, name: '' })
      message.success('保存成功')
      await load()
    } catch (err) {
      message.error(errorText(err))
    }
  }

  const remove = async (row: DatasetRow) => {
    try {
      await MetadataService.DeleteDataset(scopeId, row.id)
      message.success('删除成功')
      await load()
    } catch (err) {
      message.error(errorText(err))
    }
  }

  const columns: TableColumnsType<ClassTableRow> = [
    {
      // 自定义勾选列：和名称/操作一样按班级合并单元格，否则同一个班级会出现多个复选框
      title: (
        <Checkbox
          checked={allPageSelected}
          indeterminate={somePageSelected && !allPageSelected}
          onChange={(e) => togglePage(e.target.checked)}
        />
      ),
      key: 'select',
      width: 48,
      align: 'center',
      onCell: (row) => ({ rowSpan: row.rowSpan }),
      render: (_, row) => (
        <Checkbox
          checked={selectedIds.includes(row.id)}
          onChange={(e) => toggleClass(row.id, e.target.checked)}
        />
      ),
    },
    {
      title: '名称',
      dataIndex: 'name',
      width: 220,
      onCell: (row) => ({ rowSpan: row.rowSpan }),
    },
    {
      title: '老师',
      key: 'teacher',
      render: (_, row) =>
        row.binding ? row.binding.teacher : <span style={{ color: '#999' }}>无</span>,
    },
    {
      title: '学科',
      key: 'subject',
      render: (_, row) =>
        row.binding ? row.binding.subject : <span style={{ color: '#999' }}>无</span>,
    },
    {
      title: '所属合班',
      key: 'merged',
      onCell: (row) => ({ rowSpan: row.rowSpan }),
      render: (_, row) => {
        const names = mergedNamesByClass.get(row.id) ?? []
        if (names.length === 0) return <span style={{ color: '#999' }}>—</span>
        return (
          <Space size={4} wrap>
            {names.map((name) => (
              <Tag key={name} color="blue" style={{ marginInlineEnd: 0 }}>
                {name}
              </Tag>
            ))}
          </Space>
        )
      },
    },
    {
      title: '操作',
      width: 200,
      onCell: (row) => ({ rowSpan: row.rowSpan }),
      render: (_, row) => (
        <Space>
          <Button onClick={() => setEditing({ open: true, id: row.id, name: row.name })}>修改</Button>
          <Popconfirm
            title={`删除班级「${row.name}」？`}
            description="相关任课绑定会一起删除，并从所属合班移除"
            onConfirm={() => void remove(row)}
          >
            <Button danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const pagination: TablePaginationConfig = {
    current: currentPage,
    pageSize,
    total: rows.length,
    showSizeChanger: true,
    pageSizeOptions: [10, 20, 50],
    onChange: (nextPage, nextSize) => {
      // 改每页条数时回到第 1 页；翻页或改条数都清空勾选，避免选了看不见的行
      setPage(nextSize !== pageSize ? 1 : nextPage)
      setPageSize(nextSize)
      setSelectedIds([])
    },
  }

  return (
    <>
      <ListToolbar
        onAdd={() => setAdding(true)}
        onBatchDelete={batchDelete}
        selectedCount={selectedIds.length}
        keyword={keyword}
        onKeywordChange={setKeyword}
        onSearch={() => {
          setPage(1)
          setSearch(keyword)
        }}
        onReset={() => {
          setPage(1)
          setKeyword('')
          setSearch('')
        }}
      />
      <Table
        rowKey={(row) => `${row.id}-${row.bindingIndex}`}
        columns={columns}
        dataSource={tableRows}
        loading={loading}
        pagination={pagination}
        rowClassName={(row) => (selectedIds.includes(row.id) ? 'merged-row-selected' : '')}
      />

      <NameModal
        open={editing.open}
        title="修改班级"
        label="班级"
        value={editing.name}
        onCancel={() => setEditing({ open: false, id: 0, name: '' })}
        onSubmit={saveName}
      />

      <BatchNameModal
        open={adding}
        title="添加班级"
        label="班级"
        onCancel={() => setAdding(false)}
        onSubmit={addNames}
      />
    </>
  )
}

/** 合班：管理由物理班合成的逻辑班，不涉及老师/学科的绑定。 */
function MergedClassTab({ scopeId }: { scopeId: number }) {
  const { message, modal } = AntdApp.useApp()

  const [rows, setRows] = useState<DatasetView[]>([])
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [search, setSearch] = useState('')
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [classOptions, setClassOptions] = useState<{ label: string; value: number }[]>([])
  const [editing, setEditing] = useState<{ open: boolean; id: number; name: string; memberIds: number[] }>(
    { open: false, id: 0, name: '', memberIds: [] },
  )

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const list = await MetadataService.ListDatasets(scopeId, DATASET_MERGED_CLASS, search)
      setRows(list ?? [])
      setSelectedIds([])
    } catch (err) {
      message.error(errorText(err))
    } finally {
      setLoading(false)
    }
  }, [scopeId, search, message])

  useEffect(() => {
    void load()
  }, [load])

  // 打开弹窗时拉取物理班选项；合班只能由物理班合成
  const openModal = async (row?: DatasetView) => {
    try {
      const classes = await MetadataService.ListDatasets(scopeId, DATASET_CLASS, '')
      setClassOptions((classes ?? []).map((c) => ({ label: c.name, value: c.id })))
      setEditing(
        row
          ? { open: true, id: row.id, name: row.name, memberIds: (row.members ?? []).map((m) => m.id) }
          : { open: true, id: 0, name: '', memberIds: [] },
      )
    } catch (err) {
      message.error(errorText(err))
    }
  }

  const save = async (name: string, memberIds: number[]) => {
    try {
      await MetadataService.SaveMergedClass(scopeId, editing.id, name, memberIds)
      setEditing({ open: false, id: 0, name: '', memberIds: [] })
      message.success('保存成功')
      await load()
    } catch (err) {
      message.error(errorText(err))
    }
  }

  const remove = async (row: DatasetView) => {
    try {
      await MetadataService.DeleteDataset(scopeId, row.id)
      message.success('删除成功')
      await load()
    } catch (err) {
      message.error(errorText(err))
    }
  }

  const batchDelete = () => {
    const ids = selectedIds
    modal.confirm({
      title: `删除选中的 ${ids.length} 个合班？`,
      content: '只解散合班，物理班与任课绑定不受影响',
      okText: '确定',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await MetadataService.DeleteDatasets(scopeId, ids)
          message.success('删除成功')
          await load()
        } catch (err) {
          message.error(errorText(err))
        }
      },
    })
  }

  const columns: TableColumnsType<DatasetView> = [
    {
      title: '名称',
      dataIndex: 'name',
      width: 260,
    },
    {
      title: '包含班级',
      key: 'members',
      render: (_, row) => {
        const members = row.members ?? []
        if (members.length === 0) return <span style={{ color: '#999' }}>无</span>
        return (
          <Space size={4} wrap>
            {members.map((member) => (
              <Tag key={member.id} style={{ marginInlineEnd: 0 }}>
                {member.name}
              </Tag>
            ))}
          </Space>
        )
      },
    },
    {
      title: '操作',
      width: 180,
      render: (_, row) => (
        <Space>
          <Button onClick={() => void openModal(row)}>编辑</Button>
          <Popconfirm
            title={`删除合班「${row.name}」？`}
            description="只解散合班，物理班与任课绑定不受影响"
            onConfirm={() => void remove(row)}
          >
            <Button danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <>
      <ListToolbar
        onAdd={() => void openModal()}
        onBatchDelete={batchDelete}
        selectedCount={selectedIds.length}
        keyword={keyword}
        onKeywordChange={setKeyword}
        onSearch={() => setSearch(keyword)}
        onReset={() => {
          setKeyword('')
          setSearch('')
        }}
      />
      <Table
        rowKey="id"
        columns={columns}
        dataSource={rows}
        loading={loading}
        pagination={tablePagination(() => setSelectedIds([]))}
        rowSelection={{
          selectedRowKeys: selectedIds,
          onChange: (keys) => setSelectedIds(keys.map(Number)),
        }}
        rowClassName={(row) => (selectedIds.includes(row.id) ? 'merged-row-selected' : '')}
      />

      <MergedClassModal
        open={editing.open}
        title={editing.id === 0 ? '新建合班' : '编辑合班'}
        name={editing.name}
        memberIds={editing.memberIds}
        classOptions={classOptions}
        onCancel={() => setEditing({ open: false, id: 0, name: '', memberIds: [] })}
        onSubmit={save}
      />
    </>
  )
}
