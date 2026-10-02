import { useCallback, useEffect, useState } from 'react'
import { CloseOutlined } from '@ant-design/icons'
import { App as AntdApp, Drawer, Input, Popconfirm, Tag, Tooltip } from 'antd'

import { DATASET_SUBJECT_TAG, MetadataService, errorText } from '../api'
import type { TagView } from '../api'

type Props = {
  scopeId: number
  open: boolean
  onClose: () => void
  /** 标签增删改后通知父级刷新学科列表（标签列依赖它） */
  onChanged: () => void
}

/** 学科标签管理抽屉：就地增删改标签，不新增导航 tab，也不用弹窗。 */
export default function SubjectTagDrawer({ scopeId, open, onClose, onChanged }: Props) {
  const { message } = AntdApp.useApp()

  const [tags, setTags] = useState<TagView[]>([])
  const [loading, setLoading] = useState(false)
  const [newName, setNewName] = useState('')
  const [editing, setEditing] = useState<{ id: number; name: string }>({ id: 0, name: '' })

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const list = await MetadataService.ListSubjectTags(scopeId)
      setTags(list ?? [])
    } catch (err) {
      message.error(errorText(err))
    } finally {
      setLoading(false)
    }
  }, [scopeId, message])

  useEffect(() => {
    if (open) {
      setNewName('')
      setEditing({ id: 0, name: '' })
      void load()
    }
  }, [open, load])

  const add = async () => {
    const name = newName.trim()
    if (!name) return
    try {
      const result = await MetadataService.SaveDatasets(scopeId, DATASET_SUBJECT_TAG, [name])
      if ((result.inserted ?? []).length === 0) {
        message.warning(`「${name}」已存在`)
        return
      }
      setNewName('')
      message.success('已添加')
      await load()
      onChanged()
    } catch (err) {
      message.error(errorText(err))
    }
  }

  const remove = async (tag: TagView) => {
    try {
      await MetadataService.DeleteDataset(scopeId, tag.id)
      message.success('删除成功')
      await load()
      onChanged()
    } catch (err) {
      message.error(errorText(err))
    }
  }

  // 改名：先退出编辑态，再提交；名称没变就直接放弃
  const saveRename = async (id: number, original: string) => {
    const name = editing.name.trim()
    setEditing({ id: 0, name: '' })
    if (!name || name === original) return
    try {
      await MetadataService.SaveDataset(scopeId, DATASET_SUBJECT_TAG, id, name)
      await load()
      onChanged()
    } catch (err) {
      message.error(errorText(err))
    }
  }

  return (
    <Drawer open={open} title="学科标签" width={420} onClose={onClose}>
      <Input.Search
        value={newName}
        placeholder="输入标签名称，回车添加"
        enterButton="添加"
        allowClear
        onChange={(e) => setNewName(e.target.value)}
        onSearch={() => void add()}
      />
      <div style={{ marginTop: 16, display: 'flex', flexWrap: 'wrap', gap: 8 }}>
        {tags.map((tag) =>
          editing.id === tag.id ? (
            <Input
              key={tag.id}
              size="small"
              autoFocus
              style={{ width: 120 }}
              value={editing.name}
              onChange={(e) => setEditing((prev) => ({ ...prev, name: e.target.value }))}
              onPressEnter={() => void saveRename(tag.id, tag.name)}
              onBlur={() => void saveRename(tag.id, tag.name)}
            />
          ) : (
            <Tooltip
              key={tag.id}
              title={
                tag.builtin
                  ? `内置标签，不可删除；已用于 ${tag.count} 个学科`
                  : `已用于 ${tag.count} 个学科；点文字改名，点 × 删除`
              }
            >
              <Tag style={{ fontSize: 14, padding: '2px 8px', marginInlineEnd: 0 }}>
                <span style={{ cursor: 'text' }} onClick={() => setEditing({ id: tag.id, name: tag.name })}>
                  {tag.name}
                </span>
                {!tag.builtin && (
                  <Popconfirm
                    title={`删除标签「${tag.name}」？`}
                    description={tag.count > 0 ? `会从 ${tag.count} 个学科上解除该标签` : undefined}
                    okText="删除"
                    cancelText="取消"
                    okButtonProps={{ danger: true }}
                    onConfirm={() => void remove(tag)}
                  >
                    <span
                      role="button"
                      aria-label="删除标签"
                      style={{
                        marginInlineStart: 6,
                        display: 'inline-flex',
                        alignItems: 'center',
                        cursor: 'pointer',
                        color: 'rgba(0, 0, 0, 0.45)',
                      }}
                    >
                      <CloseOutlined style={{ fontSize: 10 }} />
                    </span>
                  </Popconfirm>
                )}
              </Tag>
            </Tooltip>
          ),
        )}
        {!loading && tags.length === 0 && <span style={{ color: '#999' }}>还没有标签</span>}
      </div>
    </Drawer>
  )
}
