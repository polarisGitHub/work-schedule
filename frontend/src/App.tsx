import { useCallback, useEffect, useState } from 'react'
import { ProLayout } from '@ant-design/pro-components'
import type { ProLayoutProps } from '@ant-design/pro-components'
import {
  App as AntdApp,
  Button,
  Divider,
  Empty,
  Input,
  Popconfirm,
  Select,
  Space,
} from 'antd'
import {
  BookOutlined,
  CalendarOutlined,
  ClockCircleOutlined,
  DeleteOutlined,
  EditOutlined,
  HistoryOutlined,
  ProfileOutlined,
  TableOutlined,
  TeamOutlined,
  UserOutlined,
} from '@ant-design/icons'

import { ScopeService, errorText } from './api'
import type { ScopeInfo } from './api'
import NameModal from './components/NameModal'
import TeacherPage from './pages/TeacherPage'
import ClassPage from './pages/ClassPage'
import SubjectPage from './pages/SubjectPage'
import ShiftPage from './pages/ShiftPage'
import CalendarPage from './pages/CalendarPage'

const route = {
  path: '/',
  routes: [
    { path: '/teacher', name: '老师', icon: <UserOutlined /> },
    { path: '/class', name: '班级', icon: <TeamOutlined /> },
    { path: '/subject', name: '学科', icon: <BookOutlined /> },
    { path: '/shift', name: '班次', icon: <ClockCircleOutlined /> },
    { path: '/calendar', name: '日历', icon: <CalendarOutlined /> },
    { path: '/rule', name: '规则', icon: <ProfileOutlined /> },
    { path: '/result', name: '排班结果', icon: <TableOutlined /> },
    { path: '/version', name: '版本', icon: <HistoryOutlined /> },
  ],
} satisfies ProLayoutProps['route']

// antd 默认的深蓝导航：深蓝底 + 主色蓝选中项。内容区仍走浅色（不需要 navTheme="realDark"，
// 那个会把 darkAlgorithm 应用到整个子树，连内容一起变黑）。
const NAV_BG = '#001529'
const NAV_TOKENS = {
  sider: {
    colorMenuBackground: NAV_BG,
    colorTextMenu: 'rgba(255, 255, 255, 0.65)',
    colorTextMenuSecondary: 'rgba(255, 255, 255, 0.45)',
    colorTextMenuItemHover: '#ffffff',
    colorTextMenuSelected: '#ffffff',
    colorTextMenuActive: '#ffffff',
    colorTextMenuTitle: '#ffffff',
    colorBgMenuItemSelected: '#1677ff',
    colorBgMenuItemHover: 'rgba(255, 255, 255, 0.08)',
  },
  header: {
    colorBgHeader: NAV_BG,
    colorBgScrollHeader: NAV_BG,
    colorHeaderTitle: '#ffffff',
    colorTextMenu: 'rgba(255, 255, 255, 0.65)',
    colorTextMenuSecondary: 'rgba(255, 255, 255, 0.45)',
    colorTextMenuSelected: '#ffffff',
    colorTextMenuActive: '#ffffff',
    colorTextRightActionsItem: 'rgba(255, 255, 255, 0.65)',
    colorBgRightActionsItemHover: 'rgba(255, 255, 255, 0.08)',
    colorBgMenuItemSelected: '#1677ff',
    colorBgMenuItemHover: 'rgba(255, 255, 255, 0.08)',
    colorBgMenuElevated: NAV_BG,
  },
} satisfies NonNullable<ProLayoutProps['token']>

/** 页面主体：按菜单路径选择页面，切换排班时靠 key 重新挂载。 */
function PageBody({ pathname, scopeID }: { pathname: string; scopeID: number }) {
  switch (pathname) {
    case '/teacher':
      return <TeacherPage scopeId={scopeID} />
    case '/class':
      return <ClassPage scopeId={scopeID} />
    case '/subject':
      return <SubjectPage scopeId={scopeID} />
    case '/shift':
      return <ShiftPage scopeId={scopeID} />
    case '/calendar':
      return <CalendarPage scopeId={scopeID} />
    default:
      return <Empty style={{ marginTop: 120 }} description="页面待实现" />
  }
}

export default function App() {
  const { message, modal } = AntdApp.useApp()
  const [pathname, setPathname] = useState('/teacher')
  const [scopes, setScopes] = useState<ScopeInfo[]>([])
  const [scopeID, setScopeID] = useState<number>()
  const [scopeOpen, setScopeOpen] = useState(false)
  const [version, setVersion] = useState(0)
  const [newName, setNewName] = useState('')
  const [creating, setCreating] = useState(false)
  const [renaming, setRenaming] = useState<{ open: boolean; name: string }>({ open: false, name: '' })

  const loadScopes = useCallback(async () => {
    try {
      const list = (await ScopeService.ListScopes()) ?? []
      setScopes(list)
      setScopeID((current) =>
        current !== undefined && list.some((scope) => scope.id === current) ? current : list[0]?.id,
      )
    } catch (err) {
      message.error(errorText(err))
    }
  }, [message])

  useEffect(() => {
    void loadScopes()
  }, [loadScopes])

  const createScope = async () => {
    const name = newName.trim()
    if (!name) {
      message.warning('请输入排班名称')
      return
    }
    setCreating(true)
    try {
      const created = await ScopeService.CreateScope(name)
      setNewName('')
      await loadScopes()
      setScopeID(created.id)
      message.success('已新建排班')
    } catch (err) {
      message.error(errorText(err))
    } finally {
      setCreating(false)
    }
  }

  const renameScope = async (name: string) => {
    if (scopeID === undefined) return
    try {
      await ScopeService.RenameScope(scopeID, name)
      setRenaming({ open: false, name: '' })
      await loadScopes()
      message.success('保存成功')
    } catch (err) {
      message.error(errorText(err))
    }
  }

  const purgeDeleted = async () => {
    try {
      const count = await ScopeService.PurgeDeleted()
      setVersion((v) => v + 1)
      message.success(count > 0 ? `已清理 ${count} 条已删除数据` : '没有需要清理的数据')
    } catch (err) {
      message.error(errorText(err))
    }
  }

  const deleteScope = async (id: number) => {
    try {
      await ScopeService.DeleteScope(id)
      await loadScopes()
      message.success('已删除排班')
    } catch (err) {
      message.error(errorText(err))
    }
  }

  const openRename = (scope: ScopeInfo) => {
    setScopeOpen(false)
    setRenaming({ open: true, name: scope.name })
  }

  const confirmDeleteScope = (scope: ScopeInfo) => {
    setScopeOpen(false)
    modal.confirm({
      title: `删除排班「${scope.name}」？`,
      content: '该排班下的老师、班级、学科、班次、日历与排班结果会一起删除',
      okText: '确定',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: () => deleteScope(scope.id),
    })
  }

  return (
    <ProLayout
      title="排班工具"
      logo={false}
      layout="mix"
      splitMenus={false}
      navTheme="light"
      token={NAV_TOKENS}
      fixSiderbar
      fixedHeader
      route={route}
      location={{ pathname }}
      menuItemRender={(item, dom) => (
        <a
          onClick={(e) => {
            e.preventDefault()
            if (item.path) setPathname(item.path)
          }}
        >
          {dom}
        </a>
      )}
      actionsRender={() => [
        <div className="scope-bar" key="scope">
          <span>当前排班</span>
          <Select
            style={{ width: 260 }}
            placeholder="请选择排班"
            value={scopeID}
            open={scopeOpen}
            onOpenChange={setScopeOpen}
            onChange={setScopeID}
            options={scopes.map((scope) => ({ label: scope.name, value: scope.id }))}
            optionRender={(option) => {
              const scope = scopes.find((item) => item.id === option.value)
              if (!scope) return option.label
              return (
                <div className="scope-option">
                  <span className="scope-option-name">{scope.name}</span>
                  <span className="scope-option-actions">
                    <Button
                      type="text"
                      size="small"
                      title="重命名"
                      icon={<EditOutlined />}
                      onMouseDown={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                      }}
                      onClick={(e) => {
                        e.stopPropagation()
                        openRename(scope)
                      }}
                    />
                    <Button
                      type="text"
                      size="small"
                      danger
                      title="删除"
                      icon={<DeleteOutlined />}
                      onMouseDown={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                      }}
                      onClick={(e) => {
                        e.stopPropagation()
                        confirmDeleteScope(scope)
                      }}
                    />
                  </span>
                </div>
              )
            }}
            popupRender={(menu) => (
              <>
                {menu}
                <Divider style={{ margin: '4px 0' }} />
                <Space.Compact style={{ padding: 4, width: '100%' }}>
                  <Input
                    placeholder="新排班名称"
                    value={newName}
                    onChange={(e) => setNewName(e.target.value)}
                    onPressEnter={() => void createScope()}
                  />
                  <Button type="primary" loading={creating} onClick={() => void createScope()}>
                    新建
                  </Button>
                </Space.Compact>
              </>
            )}
          />
        </div>,
        <Popconfirm
          key="clean"
          title="清理已删除的数据？"
          description="把已逻辑删除的记录做物理删除（含已删除的排班），删除后无法恢复"
          onConfirm={() => void purgeDeleted()}
        >
          <Button danger>清理</Button>
        </Popconfirm>,
      ]}
      contentStyle={{ background: '#f5f5f5' }}
    >
      <div className="page-panel">
        {scopeID === undefined ? (
          <Empty style={{ marginTop: 120 }} description="还没有排班，点右上角「当前排班」新建一个" />
        ) : (
          <PageBody key={`${scopeID}-${version}`} pathname={pathname} scopeID={scopeID} />
        )}
      </div>

      <NameModal
        open={renaming.open}
        title="重命名排班"
        label="排班名称"
        value={renaming.name}
        onCancel={() => setRenaming({ open: false, name: '' })}
        onSubmit={renameScope}
      />
    </ProLayout>
  )
}
