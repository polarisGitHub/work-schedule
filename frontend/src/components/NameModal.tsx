import { useEffect } from 'react'
import { Form, Input, Modal } from 'antd'

type Props = {
  open: boolean
  /** 弹窗标题，如「添加老师」「修改班级」 */
  title: string
  /** 字段名，如「老师」 */
  label: string
  /** 初值，修改时传入 */
  value: string
  onCancel: () => void
  onSubmit: (name: string) => Promise<void>
}

/** 只有一个名称输入的弹窗，老师、班级、学科共用。 */
export default function NameModal({ open, title, label, value, onCancel, onSubmit }: Props) {
  const [form] = Form.useForm<{ name: string }>()

  useEffect(() => {
    if (open) form.setFieldsValue({ name: value })
  }, [open, value, form])

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
          await onSubmit(values.name)
          form.resetFields()
        }}
      >
        <Form.Item name="name" label={label} rules={[{ required: true, message: `请输入${label}` }]}>
          <Input autoFocus />
        </Form.Item>
      </Form>
    </Modal>
  )
}
