# 排班工具（work-schedule）

基于 Wails v3 的桌面排班应用：教师排班系统，后端 Go + SQLite，前端 React + Ant Design。

## 技术栈

| 层级 | 技术 |
|------|------|
| 桌面框架 | Wails v3.0.0-beta.27 |
| 后端 | Go 1.27 |
| 数据库 | SQLite（modernc.org/sqlite，纯 Go 驱动，免 CGO） |
| 前端 | React 19 + TypeScript + Vite 8 |
| UI | antd 6 + @ant-design/pro-components v3 |

## 工程结构

```
work-schedule/
├── main.go                     # 应用入口：窗口创建、服务注册、资源嵌入
├── internal/
│   ├── model/                  # 枚举常量（dataset / mapping / rule / duty 类型）
│   ├── services/               # 绑定到前端的服务（Wails Service）
│   │   ├── scope.go            # 排班范围：列表、新建、清理
│   │   ├── metadata.go         # 老师 / 班级 / 学科 / 班次 + 任课绑定
│   │   └── calendar.go         # 日历、排班日、值班约束
│   └── store/                  # SQLite 连接管理 + schema.sql（启动时自动建表）
├── frontend/                   # 前端工程（Vite）
│   ├── src/
│   │   ├── main.tsx            # 入口：antd ConfigProvider + 中文 locale
│   │   ├── api.ts              # 绑定封装、类型、常量
│   │   ├── App.tsx             # ProLayout 主框架（侧边菜单 + 顶栏排班切换）
│   │   ├── components/         # ListToolbar、NameModal
│   │   └── pages/              # 老师 / 班级 / 学科 / 班次 / 日历
│   └── bindings/               # wails3 generate bindings 自动生成，勿手改
├── build/bin/                  # 构建产物输出目录
└── package.json                # 根入口脚本（dev / build）
```

## 环境要求

- Go 1.27+
- Node.js 26+ / npm 12+
- wails3 CLI：

  ```bash
  CGO_ENABLED=0 go install github.com/wailsapp/wails/v3/cmd/wails3@latest
  ```

> 国内网络建议：`go env -w GOPROXY=https://goproxy.cn,direct`，npm 使用 npmmirror 镜像。

## 开发与构建

```bash
# 开发（默认）：server 模式，会重新构建前端 + Go 再启动，浏览器打开 http://localhost:8080
# 没有热更新：改完代码要重新执行一次
npm run dev

# 带热更新：Go 后端 8080 + Vite dev server 5173，浏览器打开 http://localhost:5173
# 前端改动即时热更新；改 Go 代码要重启这个命令
npm run dev:hot

# 其他方式
npm run dev:window   # 弹出原生 Wails 窗口（WSLg，需中文字体）
npm run dev:ui       # 只起 Vite（无后端，绑定调用不可用）

# 打包：前端构建 + 交叉编译 Windows amd64 可执行文件
npm run build
# 产物：build/bin/work-schedule.exe
```

交叉编译无需 mingw / CGO（`CGO_ENABLED=0 GOOS=windows GOARCH=amd64`，已固化在根 `package.json` 的 build 脚本中）。

### 服务绑定

新增或修改 `internal/services` 中的 Service 方法后，重新生成绑定：

```bash
wails3 generate bindings -ts -i
```

前端通过 `frontend/bindings/` 下的生成代码调用后端方法。

`npm run dev:hot` 的原理：`@wailsio/runtime` 按 `window.location.origin + /wails/...` 发请求，所以页面由 Vite（5173）提供时，在 `frontend/vite.config.ts` 里把 `/wails` 转发到 8080 的 Go 进程即可。

## 功能

顶栏右侧是「当前排班」下拉 + 「清理」。切换排班、重命名、删除排班都在下拉列表里：每行排班右侧有重命名/删除按钮（鼠标悬停或选中该行时出现），底部是「新排班名称 + 新建」。所有数据按排班（`t_scope`）隔离。

批量添加（老师 / 班级 / 学科）：输入先按行 trim + 去重（粘贴的文本本身可能重复，这部分静默去重不报），再和库里「同排班 + 同类型 + 同名且未删除」的记录对比，库中已存在的跳过不写并返回名单，前端弹窗列出这些重复名称。软删过的同名可以重新加。

批量删除（老师 / 班级 / 学科 / 班次）：表格左侧复选框勾选，有选中时「批量删除」按钮才可用，点击后 `modal.confirm` 二次确认再删；已经不在的记录会被跳过。

所有表格分页，每页可选 10 / 20 / 50，默认 20（`api.ts` 里的 `PAGE_SIZE` 和 `tablePagination()`）。翻页或改每页条数会清空已勾选的行，避免选中看不见的记录。

删除一律是逻辑删除（写 `deleted_at`）：删实体是软删，删排班会把该排班下全部数据一起软删。下拉里的删除用 `modal.confirm` 二次确认（不能用 Popconfirm：下拉一关，锚在下拉里的确认框会跟着卸载）。「清理」把全库已逻辑删除的记录物理删除（含已删除的排班），表按子表在前清理，避免外键 RESTRICT，弹窗会给出删除行数。

界面：导航（顶栏 + 侧边栏）用 antd 默认深蓝 `#001529`、选中项用默认主色蓝 `#1677ff`，内容区浅色。注意不要用 `navTheme="realDark"` —— 那会把 darkAlgorithm 应用到整个子树，连内容一起变黑；导航配色走 ProLayout 的 `token`。

日历单元格配色：排班浅蓝 · 不排班浅红 · 有人值班浅绿 · 有人不值班浅橙 · 两者都有浅紫。

| 页面 | 能力 |
|------|------|
| 老师 | 增删改老师；批量添加（弹窗里一行一个，粘贴多行即批量插入）；任课绑定（老师 → 「学科 + 班级」二元组，整表替换） |
| 班级 | 增删改班级；批量添加；任课信息只读 |
| 学科 | 增删改学科；批量添加 |
| 班次 | 按时间段增删改班次；时间段不能重叠（左闭右开，首尾相接算不重叠） |
| 日历 | 选起止日期 + 勾选星期生成排班日历；月历只显示本月，左右箭头切月；点日期可反选排班日或配置「某天某班次某老师」值班/不值班；日历下方只汇总配置过值班/不值班的老师，一个都没有时整张表不显示 |

规则、排班结果、版本三个页面待实现。

## 注意事项

- 运行数据（SQLite）位于 `data/work-schedule.db`，已加入 `.gitignore`。
- 建表在 `internal/store/schema.sql`，应用启动时执行（`CREATE TABLE/INDEX IF NOT EXISTS`，可重复执行）。改表结构后直接改这个文件。
- 在 Linux/WSL 本机运行 GUI 窗口需要安装 webkit2gtk 系统依赖；日常开发在浏览器中进行，完整绑定链路需在打包后于 Windows 上验证。
- 删除是软删（写 `deleted_at`）。删老师/班级/学科会连带软删相关任课，删任何实体都会连带软删引用它的值班与课表格子；「清理」负责把这些软删记录物理删掉。
- 首次保存日历时会自动插入两条全局公平规则（`fair_count`、`fair_interval`），供后续求解使用。

## 待办

- [ ] 规则页：全局规则与老师个人规则
- [ ] 排班结果页：课表格子（`t_assignment`）、锁定、求解（greedy-swap）
- [ ] 版本页：课表快照与恢复
- [ ] 保存日历时同步生成课表空格子（排班日 × 班次 × 班级）
- [ ] 应用图标与安装包配置
