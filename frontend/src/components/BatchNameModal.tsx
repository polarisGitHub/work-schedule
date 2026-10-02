import { Form, Input, Modal } from 'antd'

type Props = {
  open: boolean
  /** 弹窗标题，如「添加老师」 */
  title: string
  /** 字段名，如「老师」 */
  label: string
  onCancel: () => void
  onSubmit: (names: string[]) => Promise<void>
}

/** 批量添加弹窗：一行一个名称，粘贴多行就是批量添加。 */
export default function BatchNameModal({ open, title, label, onCancel, onSubmit }: Props) {
  const [form] = Form.useForm<{ names: string }>()

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
          const trimmed = values.names
            .split('\n')
            .map((name) => name.trim())
            .filter(Boolean)
          // 粘贴的文本本身可能重复，先按输入去重再写入
          const names = [...new Set(trimmed)]
          if (names.length === 0) return
          await onSubmit(names)
        }}
      >
        <Form.Item
          name="names"
          label={label}
          extra={`每行一个，可粘贴多行批量添加`}
          rules={[{ required: true, whitespace: true, message: `请输入${label}名称` }]}
        >
          <Input.TextArea rows={8} placeholder={`每行一个${label}名称`} />
        </Form.Item>
      </Form>
    </Modal>
  )
}
