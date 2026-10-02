import { useCallback, useEffect, useMemo, useState } from 'react'
import { App as AntdApp, Button, Checkbox, Modal, Popconfirm, Select, Space, Table } from 'antd'
import type { TableColumnsType, TablePaginationConfig } from 'antd'

import BatchBindModal from '../components/BatchBindModal'
import BatchNameModal from '../components/BatchNameModal'
import ListToolbar from '../components/ListToolbar'
import NameModal from '../components/NameModal'
import { reportBatchResult } from '../batchReport'
import { DATASET_CLASS, DATASET_SUBJECT, DATASET_TEACHER, MetadataService, PAGE_SIZE, errorText, toDatasetRows } from '../api'
import type { BindingView, DatasetRow } from '../api'

type Props = { scopeId: number }

type Pair = { subjectId?: number; classId?: number }

/** 表格行：老师的每一条任课占一行，没有任课的老师占一行，姓名/操作列按 rowSpan 合并。 */
type TeacherTableRow = DatasetRow & {
  binding: BindingView | null
  /** 该老师在本页首行是任课条数，后续行为 0（被合并掉）；没有任课时为 1。 */
  rowSpan: number
  /** 同一老师内第几条任课，用来拼唯一 rowKey。 */
  bindingIndex: number
}

/** 老师页：增删改老师，并给老师绑定「学科 + 班级」二元组。 */
export default function TeacherPage({ scopeId }: Props) {
  const { message, modal } = AntdApp.useApp()

  const [rows, setRows] = useState<DatasetRow[]>([])
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [search, setSearch] = useState('')
  /** 勾选的老师 id；表格按任课展开成多行，勾选仍以老师为单位 */
  const [selectedIds, setSelectedIds] = useState<number[]>([])

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<{ open: boolean; id: number; name: string }>({
    open: false,
    id: 0,
    name: '',
  })

  const [bindingFor, setBindingFor] = useState<DatasetRow | null>(null)
  const [pairs, setPairs] = useState<Pair[]>([])
  const [savingBindings, setSavingBindings] = useState(false)
  const [subjectOptions, setSubjectOptions] = useState<{ label: string; value: number }[]>([])
  const [classOptions, setClassOptions] = useState<{ label: string; value: number }[]>([])
  const [bindTextOpen, setBindTextOpen] = useState(false)
  // 分页按「老师」算：先切出本页老师，再展开成任课行，避免一个老师的多行被翻页切断
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(PAGE_SIZE)

  const maxPage = Math.max(1, Math.ceil(rows.length / pageSize))
  const currentPage = Math.min(page, maxPage)

  const tableRows = useMemo<TeacherTableRow[]>(() => {
    const start = (currentPage - 1) * pageSize
    return rows.slice(start, start + pageSize).flatMap<TeacherTableRow>((row) =>
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

  const pageTeacherIds = useMemo(
    () => rows.slice((currentPage - 1) * pageSize, currentPage * pageSize).map((row) => row.id),
    [rows, currentPage, pageSize],
  )
  const allPageSelected = pageTeacherIds.length > 0 && pageTeacherIds.every((id) => selectedIds.includes(id))
  const somePageSelected = pageTeacherIds.some((id) => selectedIds.includes(id))

  const toggleTeacher = (id: number, checked: boolean) => {
    setSelectedIds((prev) => (checked ? [...prev, id] : prev.filter((item) => item !== id)))
  }

  const togglePage = (checked: boolean) => {
    setSelectedIds((prev) =>
      checked
        ? [...new Set([...prev, ...pageTeacherIds])]
        : prev.filter((id) => !pageTeacherIds.includes(id)),
    )
  }

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const list = await MetadataService.ListDatasets(scopeId, DATASET_TEACHER, search)
      setRows(toDatasetRows(list))
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
      const result = await MetadataService.SaveDatasets(scopeId, DATASET_TEACHER, names)
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
      content: '关联的任课绑定、值班与课表格子会一起删除',
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
      await MetadataService.SaveDataset(scopeId, DATASET_TEACHER, editing.id, name)
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

  const clearBindings = async (row: DatasetRow) => {
    try {
      await MetadataService.ClearTeacherBindings(scopeId, [row.id])
      message.success('已清除绑定')
      await load()
    } catch (err) {
      message.error(errorText(err))
    }
  }

  const batchClearBindings = () => {
    const ids = selectedIds
    modal.confirm({
      title: `清除选中 ${ids.length} 位老师的绑定？`,
      content: '这些老师的全部任课绑定会被清除，老师本身保留',
      okText: '确定',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await MetadataService.ClearTeacherBindings(scopeId, ids)
          message.success('已清除绑定')
          await load()
        } catch (err) {
          message.error(errorText(err))
        }
      },
    })
  }

  const openBindings = async (row: DatasetRow) => {
    try {
      const [subjects, classes] = await Promise.all([
        MetadataService.ListDatasets(scopeId, DATASET_SUBJECT, ''),
        MetadataService.ListDatasets(scopeId, DATASET_CLASS, ''),
      ])
      setSubjectOptions((subjects ?? []).map((s) => ({ label: s.name, value: s.id })))
      setClassOptions((classes ?? []).map((c) => ({ label: c.name, value: c.id })))
      setPairs(row.bindings.map((b) => ({ subjectId: b.subjectId, classId: b.classId })))
      setBindingFor(row)
    } catch (err) {
      message.error(errorText(err))
    }
  }

  const saveBindings = async () => {
    if (!bindingFor) return
    const filled = pairs.filter((p) => p.subjectId && p.classId)
    if (filled.length !== pairs.length) {
      message.warning('请把每一行的学科和班级都选上')
      return
    }
    setSavingBindings(true)
    try {
      await MetadataService.SaveTeacherBindings(
        scopeId,
        bindingFor.id,
        filled.map((p) => ({ subjectId: p.subjectId as number, classId: p.classId as number })),
      )
      message.success('保存成功')
      setBindingFor(null)
      await load()
    } catch (err) {
      message.error(errorText(err))
    } finally {
      setSavingBindings(false)
    }
  }

  // 批量文本绑定：有校验错误就整批未写入，弹窗列出每行原因（保持弹窗开着便于修改）
  const submitBindText = async (text: string) => {
    try {
      const result = await MetadataService.BindByText(scopeId, text)
      const errors = result.errors ?? []
      if (errors.length > 0) {
        modal.warning({
          title: '未写入任何绑定，请修正以下问题',
          okText: '知道了',
          content: (
            <ul style={{ margin: '8px 0 0', paddingLeft: 20 }}>
              {errors.map((err) => (
                <li key={err}>{err}</li>
              ))}
            </ul>
          ),
        })
        return
      }
      setBindTextOpen(false)
      await load()
      const skipped = result.skipped > 0 ? `，跳过 ${result.skipped} 条已存在` : ''
      if (result.inserted > 0) {
        message.success(`已绑定 ${result.inserted} 条${skipped}`)
      } else {
        message.info(`没有新增绑定${skipped}`)
      }
    } catch (err) {
      message.error(errorText(err))
    }
  }

  const columns: TableColumnsType<TeacherTableRow> = [
    {
      // 自定义勾选列：和姓名/操作一样按老师合并单元格，否则同一个老师会出现多个复选框
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
          onChange={(e) => toggleTeacher(row.id, e.target.checked)}
        />
      ),
    },
    {
      title: '姓名',
      dataIndex: 'name',
      width: 220,
      onCell: (row) => ({ rowSpan: row.rowSpan }),
    },
    {
      title: '学科',
      key: 'subject',
      render: (_, row) =>
        row.binding ? row.binding.subject : <span style={{ color: '#999' }}>无</span>,
    },
    {
      title: '班级',
      key: 'class',
      render: (_, row) =>
        row.binding ? row.binding.class : <span style={{ color: '#999' }}>无</span>,
    },
    {
      title: '操作',
      width: 380,
      onCell: (row) => ({ rowSpan: row.rowSpan }),
      render: (_, row) => (
        <Space>
          <Button onClick={() => setEditing({ open: true, id: row.id, name: row.name })}>修改</Button>
          <Button onClick={() => void openBindings(row)}>任课绑定</Button>
          <Popconfirm
            title={`清除老师「${row.name}」的任课绑定？`}
            disabled={row.bindings.length === 0}
            onConfirm={() => void clearBindings(row)}
          >
            <Button disabled={row.bindings.length === 0}>清除绑定</Button>
          </Popconfirm>
          <Popconfirm title={`删除老师「${row.name}」？`} onConfirm={() => void remove(row)}>
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
        extra={
          <Space>
            <Button type="primary" onClick={() => setBindTextOpen(true)}>
              绑定
            </Button>
            <Button danger disabled={selectedIds.length === 0} onClick={batchClearBindings}>
              清除绑定
            </Button>
          </Space>
        }
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
        title="修改老师"
        label="老师"
        value={editing.name}
        onCancel={() => setEditing({ open: false, id: 0, name: '' })}
        onSubmit={saveName}
      />

      <BatchNameModal
        open={adding}
        title="添加老师"
        label="老师"
        onCancel={() => setAdding(false)}
        onSubmit={addNames}
      />

      <BatchBindModal
        open={bindTextOpen}
        onCancel={() => setBindTextOpen(false)}
        onSubmit={submitBindText}
      />

      <Modal
        open={bindingFor !== null}
        title={bindingFor ? `任课绑定：${bindingFor.name}` : ''}
        okText="确定"
        cancelText="取消"
        confirmLoading={savingBindings}
        onCancel={() => setBindingFor(null)}
        onOk={() => void saveBindings()}
        destroyOnHidden
      >
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          {pairs.map((pair, index) => (
            <Space key={index}>
              <Select
                style={{ width: 180 }}
                placeholder="学科"
                value={pair.subjectId}
                options={subjectOptions}
                onChange={(value) =>
                  setPairs(pairs.map((p, i) => (i === index ? { ...p, subjectId: value } : p)))
                }
              />
              <Select
                style={{ width: 180 }}
                placeholder="班级"
                value={pair.classId}
                options={classOptions}
                onChange={(value) =>
                  setPairs(pairs.map((p, i) => (i === index ? { ...p, classId: value } : p)))
                }
              />
              <Button onClick={() => setPairs(pairs.filter((_, i) => i !== index))}>删除</Button>
            </Space>
          ))}
          <Button onClick={() => setPairs([...pairs, {}])}>添加</Button>
        </Space>
      </Modal>
    </>
  )
}
