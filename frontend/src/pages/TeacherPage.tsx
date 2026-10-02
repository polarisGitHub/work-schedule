import { useCallback, useEffect, useState } from 'react'
import type { Key } from 'react'
import { App as AntdApp, Button, Modal, Popconfirm, Select, Space, Table } from 'antd'
import type { TableColumnsType } from 'antd'

import BatchNameModal from '../components/BatchNameModal'
import ListToolbar from '../components/ListToolbar'
import NameModal from '../components/NameModal'
import { reportBatchResult } from '../batchReport'
import { DATASET_CLASS, DATASET_SUBJECT, DATASET_TEACHER, MetadataService, errorText, tablePagination, toDatasetRows } from '../api'
import type { DatasetRow } from '../api'

type Props = { scopeId: number }

type Pair = { subjectId?: number; classId?: number }

/** 老师页：增删改老师，并给老师绑定「学科 + 班级」二元组。 */
export default function TeacherPage({ scopeId }: Props) {
  const { message, modal } = AntdApp.useApp()

  const [rows, setRows] = useState<DatasetRow[]>([])
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [search, setSearch] = useState('')
  const [selectedKeys, setSelectedKeys] = useState<Key[]>([])

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

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const list = await MetadataService.ListDatasets(scopeId, DATASET_TEACHER, search)
      setRows(toDatasetRows(list))
      setSelectedKeys([])
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
    const ids = selectedKeys.map(Number)
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

  const columns: TableColumnsType<DatasetRow> = [
    { title: '姓名', dataIndex: 'name', width: 220 },
    {
      title: '任课',
      dataIndex: 'bindings',
      render: (_, row) =>
        row.bindings.length === 0 ? (
          <span style={{ color: '#999' }}>无</span>
        ) : (
          row.bindings.map((b, i) => <div key={i}>{`${b.subject} / ${b.class}`}</div>)
        ),
    },
    {
      title: '操作',
      width: 280,
      render: (_, row) => (
        <Space>
          <Button onClick={() => setEditing({ open: true, id: row.id, name: row.name })}>修改</Button>
          <Button onClick={() => void openBindings(row)}>任课绑定</Button>
          <Popconfirm title={`删除老师「${row.name}」？`} onConfirm={() => void remove(row)}>
            <Button danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <>
      <ListToolbar
        onAdd={() => setAdding(true)}
        onBatchDelete={batchDelete}
        selectedCount={selectedKeys.length}
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
        pagination={tablePagination(() => setSelectedKeys([]))}
        rowSelection={{ selectedRowKeys: selectedKeys, onChange: setSelectedKeys }}
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
