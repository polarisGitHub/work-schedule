import { useCallback, useEffect, useState } from 'react'
import type { Key } from 'react'
import { App as AntdApp, Button, Form, Input, Modal, Popconfirm, Space, Table, TimePicker } from 'antd'
import type { TableColumnsType } from 'antd'
import dayjs from 'dayjs'
import type { Dayjs } from 'dayjs'

import ListToolbar from '../components/ListToolbar'
import { DATASET_SHIFT, MetadataService, errorText, tablePagination } from '../api'
import type { DatasetView } from '../api'

type Props = { scopeId: number }

type FormValues = { name: string; start: Dayjs; end: Dayjs }

/** 把 HH:mm:ss 转成 dayjs，避免依赖 dayjs 的自定义解析插件。 */
function toDayjs(clock: string): Dayjs | undefined {
  if (!clock) return undefined
  const [hour, minute, second] = clock.split(':').map(Number)
  return dayjs().hour(hour).minute(minute).second(second)
}

/** 班次页：按时间段定义班次，时间段之间不能重叠。 */
export default function ShiftPage({ scopeId }: Props) {
  const { message, modal } = AntdApp.useApp()
  const [form] = Form.useForm<FormValues>()

  const [rows, setRows] = useState<DatasetView[]>([])
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [search, setSearch] = useState('')
  const [selectedKeys, setSelectedKeys] = useState<Key[]>([])
  const [editing, setEditing] = useState<{ open: boolean; id: number; name: string; start: string; end: string }>({
    open: false,
    id: 0,
    name: '',
    start: '',
    end: '',
  })

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const list = await MetadataService.ListDatasets(scopeId, DATASET_SHIFT, search)
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

  useEffect(() => {
    if (!editing.open) return
    form.setFieldsValue({
      name: editing.name,
      start: toDayjs(editing.start) as Dayjs,
      end: toDayjs(editing.end) as Dayjs,
    })
  }, [editing, form])

  const closeModal = () => setEditing({ open: false, id: 0, name: '', start: '', end: '' })

  const batchDelete = () => {
    const ids = selectedKeys.map(Number)
    modal.confirm({
      title: `删除选中的 ${ids.length} 项？`,
      content: '引用这些班次的值班与课表格子会一起删除',
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

  const submit = async (values: FormValues) => {
    try {
      await MetadataService.SaveShift(
        scopeId,
        editing.id,
        values.name,
        values.start.format('HH:mm:ss'),
        values.end.format('HH:mm:ss'),
      )
      closeModal()
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
    { title: '开始', dataIndex: 'start', width: 200 },
    { title: '结束', dataIndex: 'end', width: 200 },
    {
      title: '操作',
      width: 200,
      render: (_, row) => (
        <Space>
          <Button
            onClick={() => setEditing({ open: true, id: row.id, name: row.name, start: row.start, end: row.end })}
          >
            修改
          </Button>
          <Popconfirm title={`删除班次「${row.name}」？`} onConfirm={() => void remove(row)}>
            <Button danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <>
      <ListToolbar
        onAdd={() => setEditing({ open: true, id: 0, name: '', start: '', end: '' })}
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

      <Modal
        open={editing.open}
        title={editing.id ? '修改班次' : '添加班次'}
        okText="确定"
        cancelText="取消"
        onCancel={closeModal}
        onOk={() => form.submit()}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" onFinish={submit}>
          <Form.Item name="name" label="班次" rules={[{ required: true, message: '请输入班次' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="start" label="开始" rules={[{ required: true, message: '请选择开始时间' }]}>
            <TimePicker format="HH:mm:ss" style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="end" label="结束" rules={[{ required: true, message: '请选择结束时间' }]}>
            <TimePicker format="HH:mm:ss" style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
