import { useEffect } from 'react'
import { Form, Input, Modal, Select } from 'antd'

type Props = {
  open: boolean
  /** 弹窗标题，如「新建合班」「编辑合班」 */
  title: string
  /** 初值：合班名 */
  name: string
  /** 初值：成员物理班 id */
  memberIds: number[]
  /** 可选的物理班（合班只能由物理班合成） */
  classOptions: { label: string; value: number }[]
  onCancel: () => void
  onSubmit: (name: string, memberIds: number[]) => Promise<void>
}

/** 新建/编辑合班弹窗：一个合班名 + 多选物理班。 */
export default function MergedClassModal({
  open,
  title,
  name,
  memberIds,
  classOptions,
  onCancel,
  onSubmit,
}: Props) {
  const [form] = Form.useForm<{ name: string; memberIds: number[] }>()

  useEffect(() => {
    if (open) form.setFieldsValue({ name, memberIds })
  }, [open, name, memberIds, form])

  return (
    <Modal
      open={open}
      title={title}
      destroyOnHidden
      okText="确定"
      cancelText="取消"
      onCancel={onCancel}
      onOk={() => form.submit()}
    >
      <Form
        form={form}
        layout="vertical"
        onFinish={async (values) => {
          await onSubmit(values.name, values.memberIds ?? [])
          form.resetFields()
        }}
      >
        <Form.Item name="name" label="合班名称" rules={[{ required: true, message: '请输入合班名称' }]}>
          <Input autoFocus placeholder="如「101+102 晚自习合班」" />
        </Form.Item>
        <Form.Item
          name="memberIds"
          label="包含班级"
          rules={[
            {
              validator: (_, value: number[] | undefined) =>
                value && value.length >= 2
                  ? Promise.resolve()
                  : Promise.reject(new Error('请至少选择 2 个物理班')),
            },
          ]}
        >
          <Select
            mode="multiple"
            placeholder="选择至少 2 个物理班"
            options={classOptions}
            optionFilterProp="label"
            showSearch
          />
        </Form.Item>
      </Form>
    </Modal>
  )
}
