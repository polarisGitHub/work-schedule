import { Form, Input, Modal } from 'antd'

type Props = {
  open: boolean
  onCancel: () => void
  onSubmit: (text: string) => Promise<void>
}

const PLACEHOLDER = `语文 一年级一班 张三
数学 一年级二班 李四`

/** 批量绑定弹窗：每行一条「课程 班级 老师」，空格分隔，可粘贴多行。 */
export default function BatchBindModal({ open, onCancel, onSubmit }: Props) {
  const [form] = Form.useForm<{ text: string }>()

  return (
    <Modal
      open={open}
      title="批量绑定"
      destroyOnHidden
      width={520}
      okText="确定"
      cancelText="取消"
      onCancel={onCancel}
      onOk={() => form.submit()}
    >
      <Form
        form={form}
        layout="vertical"
        onFinish={async (values) => {
          const text = values.text.trim()
          if (!text) return
          await onSubmit(text)
        }}
      >
        <Form.Item
          name="text"
          label="绑定关系"
          extra="每行一条，三列空格分隔：课程 班级 老师；三者需已存在，任一行有误则整批不写入"
          rules={[{ required: true, whitespace: true, message: '请输入绑定关系' }]}
        >
          <Input.TextArea rows={10} placeholder={PLACEHOLDER} />
        </Form.Item>
      </Form>
    </Modal>
  )
}
