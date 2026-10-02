import { useCallback, useEffect, useState } from 'react'
import type { Key } from 'react'
import { App as AntdApp, Button, Popconfirm, Space, Table } from 'antd'
import type { TableColumnsType } from 'antd'

import BatchNameModal from '../components/BatchNameModal'
import ListToolbar from '../components/ListToolbar'
import NameModal from '../components/NameModal'
import { reportBatchResult } from '../batchReport'
import { DATASET_SUBJECT, MetadataService, errorText, tablePagination } from '../api'
import type { DatasetView } from '../api'

type Props = { scopeId: number }

/** 学科页：增删改学科。 */
export default function SubjectPage({ scopeId }: Props) {
  const { message, modal } = AntdApp.useApp()

  const [rows, setRows] = useState<DatasetView[]>([])
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

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const list = await MetadataService.ListDatasets(scopeId, DATASET_SUBJECT, search)
      setRows(list ?? [])
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
      const result = await MetadataService.SaveDatasets(scopeId, DATASET_SUBJECT, names)
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
      await MetadataService.SaveDataset(scopeId, DATASET_SUBJECT, editing.id, name)
      setEditing({ open: false, id: 0, name: '' })
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

  const columns: TableColumnsType<DatasetView> = [
    { title: '名称', dataIndex: 'name' },
    {
      title: '操作',
      width: 200,
      render: (_, row) => (
        <Space>
          <Button onClick={() => setEditing({ open: true, id: row.id, name: row.name })}>修改</Button>
          <Popconfirm
            title={`删除学科「${row.name}」？`}
            description="相关任课绑定会一起删除"
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
        title="修改学科"
        label="学科"
        value={editing.name}
        onCancel={() => setEditing({ open: false, id: 0, name: '' })}
        onSubmit={saveName}
      />

      <BatchNameModal
        open={adding}
        title="添加学科"
        label="学科"
        onCancel={() => setAdding(false)}
        onSubmit={addNames}
      />
    </>
  )
}
