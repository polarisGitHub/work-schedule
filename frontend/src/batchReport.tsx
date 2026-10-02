import type { ReactNode } from 'react'

import type { BatchResult } from './api'

// 只用到这两个方法，用结构化类型避免依赖 antd 内部的类型路径
type Notifier = {
  message: { success: (content: string) => void }
  modal: { warning: (config: { title: string; content: ReactNode; okText?: string }) => void }
}

/** 汇报批量添加的结果：全成功就轻提示，有重复就弹窗列出名单。 */
export function reportBatchResult(result: BatchResult, notifier: Notifier) {
  const inserted = result.inserted ?? []
  const duplicates = result.duplicates ?? []

  if (duplicates.length === 0) {
    notifier.message.success(`已添加 ${inserted.length} 个`)
    return
  }

  notifier.modal.warning({
    title: '部分名称已存在，未写入',
    okText: '知道了',
    content: (
      <div>
        <div>{`成功添加 ${inserted.length} 个；以下 ${duplicates.length} 个已存在，未写入：`}</div>
        <ul style={{ margin: '8px 0 0', paddingLeft: 20 }}>
          {duplicates.map((name) => (
            <li key={name}>{name}</li>
          ))}
        </ul>
      </div>
    ),
  })
}
