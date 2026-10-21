# DocHarbor 页面重设计实施文档

整理日期：2026-10-21

本文是前端重设计的实施规格，面向在本仓库内直接改代码的实施者。范围是信息架构、视觉和组件拆分三件事一起做；后端 API、数据逻辑和 URL 兼容性保持不变。

## 0. 参考材料

| 材料 | 位置 | 说明 |
| --- | --- | --- |
| 可点击原型 | `doc/ui-redesign/prototype.html` | 浏览器直接打开，URL hash 切页（`#docs`、`#history`、`#scans`、`#repo-settings`、`#ai`、`#ai-providers`、`#ai-diagnostics`、`#settings`），左下角可切亮/暗主题 |
| 原型截图 | `doc/ui-redesign/*.png` | 文件名为 `页面-宽度-主题.png` |
| 现有实现 | `src/App.vue`、`src/styles.css` | 3000 行单组件 + 1900 行全局 CSS |

`doc/ui-redesign/` 只存在于本地工作区，已加入 `.git/info/exclude`，**不要提交**。原型里有内部仓库的真实数据，而本仓库是公开仓库。

原型只是视觉和交互参考：数据是写死的，图标是内联 SVG，部分行为是演示用的简化版。和本文冲突时以本文为准。第 11 节列出了原型中不要照搬的地方。

## 1. 目标与非目标

### 目标

1. **信息架构**：重排导航层级，把运维信息从阅读界面挪走，仓库配置从弹窗改为独立页面。
2. **视觉**：中性工具风格（参考 GitHub / Linear 的灰白界面，品牌绿只用于主要操作和选中态）。建立颜色、字号、圆角 token，支持暗色模式。去掉“卡片套卡片”，面板之间用 1px 边框分隔。
3. **工程**：拆分 `App.vue`，每个静态页面只加载自己需要的视图代码。

### 非目标

- 不改 Go 后端，不新增或修改 API。
- 不改已有 URL 和 query 参数语义（`src/lib/navigation.ts` 的 `pageQueryKeys`），旧 `?tab=` 链接继续重定向。
- 不引入新的运行时依赖。图标继续用 `lucide-vue-next`，不加载外部字体，不引入 UI 组件库或 CSS 框架。
- 不改 Markdown、Mermaid、HTML 预览的渲染逻辑和按需加载策略，只改外观。

## 2. 硬约束

1. **按需加载**：`mermaid`、`marked`、`dompurify`、`html-preview` 不能出现在任何页面的首屏模块里。`npm run check:bundle` 会检查这一点，也检查每页的 gzip 预算（文档页 130 KiB，其余 100 KiB）。新增的 `repo-settings` 页面要加进 `scripts/check-bundle-budget.mjs`，预算 100 KiB。
2. **无 JS 可读**：Astro 在构建期渲染 Vue 组件。禁用 JS 时，页面仍要显示品牌标题和页面标题（见 e2e `static history page remains meaningful without JavaScript`）。
3. **URL 状态**：`repo`、`view`、`branch`、`dir`、`version`、`commit`、`session`、`scope`、`question` 等 query 参数的读写行为保持不变，包括深链接恢复（见 e2e `history page restores a commit deep link`）。
4. **文件列表行结构**：标题和来源分支**必须分两行**，长分支名在第二行用省略号截断，标题宽度不受影响。这是之前专门修过的问题，由 e2e `document titles survive long backup branches` 锁定。保留 `.file-row`、`.file-row-title`、`.file-row-branch` 这三个类名和 `title` 属性。
5. **可访问名称**：提交历史的每一行仍是 `<button>`（不要改成 `<tr @click>`）；目录路径导航保留 `aria-label="目录路径"`；“上一级”按钮的可访问名称保持为“上一级”（只放图标时用 `aria-label`）。
6. **HTML 独立预览页** `/html-preview/` 不加载主应用（`HTMLPreviewPage.vue` 及其 `.html-viewer-*` 样式保留）。
7. **字体**：系统字体栈，见第 4.2 节。

## 3. 信息架构

### 3.1 页面与路由

| 页面 | 路径 | AppTab | 页面 `<title>` | 归属 |
| --- | --- | --- | --- | --- |
| 文档 | `/` | `docs` | `DocHarbor` | 仓库 |
| 提交历史 | `/history/` | `history` | `提交历史 · DocHarbor` | 仓库 |
| 扫描记录 | `/scans/` | `runs` | `扫描记录 · DocHarbor` | 仓库 |
| 仓库设置（新增） | `/repo-settings/` | `repo-settings` | `仓库设置 · DocHarbor` | 仓库 |
| AI 问答 | `/ai/` | `ai` | `AI 问答 · DocHarbor` | 全局 |
| AI 供应商 | `/ai/config/`（路径不变） | `ai-config` | `AI 供应商 · DocHarbor` | 管理 |
| AI 诊断 | `/ai/diagnostics/` | `ai-diagnostics` | `AI 诊断 · DocHarbor` | 管理 |
| 系统设置 | `/settings/` | `settings` | `系统设置 · DocHarbor` | 管理 |

新增路由要做的事：

- `navigation.ts`：`AppTab` 加 `'repo-settings'`；`pagePaths['repo-settings'] = '/repo-settings/'`；`pageQueryKeys['repo-settings'] = ['repo']`；`legacyTabs` 加 `'repo-settings': 'repo-settings'`。
- 新增 `src/pages/repo-settings/index.astro`。
- Go 静态托管已经支持目录 `index.html`，不用改后端。

“Git 历史”统一改称“提交历史”，“AI 配置”统一改称“AI 供应商”。旧名称在页面文案和测试中都要替换。

### 3.2 侧栏（全局，232px）

自上而下：

1. **品牌**：图标 + “DocHarbor”。保持为 `<h1>`，视觉上按 15px 粗体显示。
2. **AI 问答**：单独一项，放在最上面，因为它是跨仓库的主要入口。
3. **仓库**分组：
   - 组标题右侧是“+”图标按钮（`aria-label="新增仓库"`），点击打开新增仓库弹窗。
   - 每个仓库一行：状态点 + 名称（省略号截断）+ 右侧文件数。
   - 状态点颜色：`latest_scan.status` 为 `success` 显示绿色，`partial_success` 显示黄色，`failed` 显示红色，没有扫描记录显示灰色。
   - 文件数取 `latest_scan.file_count`，用等宽数字。
   - 当前仓库页（文档 / 提交历史 / 扫描记录 / 仓库设置）中，所选仓库高亮。
   - 在全局页点击仓库进入该仓库的文档页；在仓库页点击则停留在当前标签页，只切换仓库。
4. **管理**分组：AI 供应商、AI 诊断、系统设置。
5. **底部**：主题切换（跟随系统 / 浅色 / 深色），见第 9 节。

语义结构：AI 问答和管理分组放在 `<nav aria-label="全局功能">` 里，仓库列表放在 `<nav aria-label="仓库">` 里。导航项是真实的 `<a href>`，页面切换是真实的页面跳转（保持 MPA）。

### 3.3 仓库页头

仓库下的四个页面共用一个页头：

- 第一行：仓库名（`<h2>`，18px）+ 扫描状态胶囊（如“部分成功 · 2 个错误”，错误数为 0 时不显示后半段）。右侧只有一个“立即扫描”按钮。
- 第二行（次要文字）：Git URL（等宽、浅色）· “最近扫描 10-21 11:40 · 6 个分支”（时间取 `latest_scan.started_at`，分支数取 `latest_scan.branch_count`）。
- 下划线标签：文档（带文件数）/ 提交历史 / 扫描记录 / 仓库设置。

以下内容从页头移走：Webhook URL 和 Secret 按钮（迁到仓库设置和系统设置）、单独的“刷新列表”图标按钮（不再需要，扫描结束后自动刷新）、“配置”按钮（由“仓库设置”标签替代）。

没有仓库时，主区显示空状态：“还没有仓库”，说明文字“添加 Git 仓库后，DocHarbor 会同步并扫描文档目录”，以及一个“新增仓库”主按钮。

### 3.4 全局页头

AI 问答和三个管理页使用简单页头：标题 `<h2>` + 一行说明，右侧放页面级主操作（例如 AI 供应商页的“新增供应商”）。

## 4. 设计 token

所有颜色只能在 `src/styles/tokens.css` 中定义，组件样式只能引用 token。验收时用 `grep -E '#[0-9a-fA-F]{3,6}\b' src --include=*.vue --include=*.css` 检查，除 `tokens.css` 外不应出现十六进制颜色。git 图的车道色也走 token。

### 4.1 颜色

| Token | 浅色 | 深色 | 用途 |
| --- | --- | --- | --- |
| `--bg` | `#f6f7f6` | `#0f1211` | 侧栏、页面底色 |
| `--surface` | `#ffffff` | `#151918` | 主区、面板、输入框 |
| `--surface-2` | `#f1f3f1` | `#1b201e` | 悬停、表头、行内代码、分段控件底 |
| `--surface-3` | `#e8ebe8` | `#232926` | 导航选中项 |
| `--border` | `#e1e4e1` | `#252b28` | 分隔线 |
| `--border-strong` | `#cdd2cd` | `#343b37` | 输入框、按钮描边 |
| `--fg` | `#191c1a` | `#e3e7e4` | 正文 |
| `--fg-muted` | `#5a615c` | `#9aa39d` | 次要文字 |
| `--fg-subtle` | `#858c87` | `#6d7670` | 弱提示、元信息 |
| `--accent` | `#1c7a4e` | `#36a46d` | 主按钮、选中下划线、焦点环 |
| `--accent-hover` | `#166640` | `#42b67b` | 主按钮悬停 |
| `--accent-fg` | `#ffffff` | `#06120b` | 主按钮文字 |
| `--accent-subtle` | `#e5f1ea` | `#14281e` | 选中文件行、分支标签底、焦点光晕 |
| `--accent-text` | `#176640` | `#5cc58f` | 链接、分支标签文字 |
| `--ok` / `--ok-bg` | `#1a7f37` / `#e7f4eb` | `#4bbf65` / `#13281a` | 成功 |
| `--warn` / `--warn-bg` | `#8a5a00` / `#fdf3d7` | `#d6a23a` / `#2a2210` | 部分成功、警告 |
| `--danger` / `--danger-bg` | `#c4282d` / `#fcebeb` | `#f2655f` / `#2e1617` | 失败、删除 |
| `--info` / `--info-bg` | `#0b62c4` / `#e8f0fb` | `#5ea8ff` / `#10223a` | 信息提示、git ref 标签 |
| `--lane-1..4` | `#1c7a4e` `#0b62c4` `#b0561a` `#8547c6` | `#4bbf7f` `#5ea8ff` `#e08a4c` `#b38cf0` | git 图车道（超过 4 条循环使用） |
| `--brand-lime` / `--brand-ink` | `#d8f36b` / `#20382c` | 同左 | 只用于品牌图标 |
| `--scrim` | `rgb(18 22 20 / .32)` | `rgb(0 0 0 / .55)` | 抽屉、弹窗遮罩 |
| `--shadow` | 见原型 | 见原型 | 只用于浮层（菜单、抽屉、弹窗） |

暗色写法：bare `:root` 定义浅色值；`@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) {…} }` 和 `:root[data-theme="dark"] {…}` 两处重定义深色值，并设置 `color-scheme: dark`。

### 4.2 字体与字号

```css
--font-ui: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", "Noto Sans SC", sans-serif;
--font-mono: ui-monospace, "SF Mono", "JetBrains Mono", "Cascadia Mono", Menlo, Consolas, monospace;
--fs-xs: 11.5px; --fs-sm: 12.5px; --fs-md: 13.5px; --fs-lg: 15px; --fs-xl: 18px; --fs-2xl: 24px;
```

- 界面基准字号为 13.5px，行高 1.5。
- 文档正文和 AI 回答用 15px，行高约 1.75。
- 路径、SHA、分支名、模型名、URL、glob 一律用等宽字体。
- 数字列和时间用 `font-variant-numeric: tabular-nums`。

### 4.3 尺寸

- 圆角：4px（标签、小元素）/ 6px（按钮、输入框、导航项）/ 8px（表格外框、汇总条）/ 12px（弹窗、AI 输入框）。
- 按钮高 30px，小按钮 26px；输入框高 32px；导航项最小高度 30px；表格行高 34–38px。
- 只有浮层带阴影。表格外框、汇总条这类块只用 1px `--border` 描边，不加阴影。

### 4.4 公共样式与组件

放在 `src/styles/components.css` 或 `src/components/ui/`：

| 名称 | 规格 |
| --- | --- |
| 按钮 `.btn` | 变体：默认（描边）、`.primary`（实心 accent）、`.ghost`（无边框）、`.danger`（红色文字），尺寸 `.sm`，纯图标 `.icon`（必须带 `aria-label` 或 `title`） |
| `StatusPill` | 圆角胶囊 + 前置小圆点，变体 ok / warn / danger / info / neutral，统一负责状态码到中文的映射（见第 5 节） |
| `BranchTag` | 等宽小字，accent-subtle 底；`neutral` 变体为灰底；超长省略并带 `title` |
| 分段控件 `.seg` | 灰底，选中项为白底加细阴影 |
| `Switch` | 32×18 开关，`role="switch"` 和 `aria-checked` |
| `DrawerPanel` | 右侧抽屉，宽 `min(480px, 100vw)`，包含头部、可滚动主体和底部操作栏，有遮罩；Esc 和点遮罩关闭；打开时焦点移到第一个输入框，关闭后焦点回到触发按钮 |
| `ModalDialog` | 居中弹窗，宽 `min(560px, 100%)`，其余行为同抽屉 |
| `DropdownMenu` | 点击“⋯”弹出，点击外部或按 Esc 关闭；危险项放最下面，前面加分隔线 |
| `InlineConfirm` | 危险操作的页内二次确认（例如按钮变为“确认删除？[删除] [取消]”）。不要用 `window.confirm` |
| `CopyField` | 只读输入框 + 复制按钮；`navigator.clipboard` 失败时退回选中文字；复制成功后按钮短暂显示“已复制” |
| `Notice` | 行内提示条，变体 info / warn / danger，前置图标 |
| `EmptyState` | 居中图标 + 一句说明 + 可选操作按钮 |

## 5. 逐页规格

状态码到中文的映射集中写在一个模块里（例如 `src/lib/labels.ts`），原始英文值放在 `title` 属性中便于排查：

- 扫描状态：`success` 成功、`partial_success` 部分成功、`failed` 失败、`running` 扫描中，其他值原样显示。
- 触发方式：`startup` 服务启动、`scheduled` 定时、`manual` 手动、`webhook` Webhook，其他值原样显示。
- 诊断状态：`succeeded` 通过、`completed_with_gaps` 有缺口、`insufficient_evidence` 证据不足、`failed` 失败。
- 候选服务置信度：`high` 高、`medium` 中、`low` 低。

### 5.1 文档 `/`

截图：`docs-1440-light.png`、`docs-1440-dark.png`、`docs-390-light.png`。

布局为三栏：文件栏 300px | 阅读区 自适应 | 右栏 232px。宽度 ≤1280px 时隐藏右栏，右栏内容移到正文下方。

**文件栏**

- 顶部工具区：
  - 分段控件 [智能最新 | 按分支]。选“按分支”时，右侧出现分支下拉框（小尺寸）。不在按分支模式时**不渲染**下拉框，而不是置灰显示。
  - 搜索框（回车搜索，行为同现在）。
- 路径行：“上一级”图标按钮 + 面包屑（根目录 / doc / …）。搜索时末尾显示“搜索结果”。
- 文件列表：目录在前（单行）。文件行按第 2 节约束 4 分两行：第一行是标题，第二行是来源分支（`.file-row-branch`，用 BranchTag 样式）。
  - 新规则：**只有来源分支不等于仓库默认分支时才渲染第二行**；按分支模式下所有文件都不渲染第二行。这样功能分支来的文档能一眼看出来。
  - 选中行用 `--accent-subtle` 底色加标题加粗。
- 底部状态行：“7 个目录 · 17 个文件”。

**阅读区头部**

- 文件路径（等宽、浅色）。
- 标题（`<h3>`，24px，用 `text-wrap: balance`）。
- 右侧操作：
  - “基于本文提问”：描边按钮，带文字。
  - “下载”：图标按钮。
  - “⋯”菜单：文件历史（滚动到“路径事件”区）、独立窗口打开（仅 HTML 文件）、复制文件路径。
- 元信息行：分支标签 · 短 SHA · 最后修改时间（含年份）· 文件大小。
- 智能最新模式下，如果 `fileContent.branch !== repo.default_branch`，元信息行末尾追加浅色说明：“智能最新选中了 {branch} 上的版本”。

**正文**

- Markdown 最大宽度 780px，左对齐（不居中），左右内边距 32px。
- 样式：h2 19px；表格放在横向滚动容器里，带 1px 外框，表头用 `--surface-2`；行内代码用 `--surface-2` 底；链接用 `--accent-text` 加浅色下划线。
- HTML 预览 iframe 占满阅读区宽度，背景保持白色（文档自带样式）。
- 加载中、预览不可用、不支持预览三种状态用 EmptyState 样式，文案沿用现有。
- 未选文件时显示 EmptyState：“从左侧选择文档”。

**右栏**

- **本页目录**：从渲染后的 Markdown 收集 h2，必要时加 h3。点击滚动到对应标题。滚动时高亮当前标题（IntersectionObserver）。少于 2 个标题时不显示。HTML 文件不显示。
- **分支版本**：列出 `fileContent.versions`，每项为分支标签 + 状态胶囊 + 短 SHA 和时间，当前版本标“当前”。
- **路径事件**：列出 `docEvents`。打开文件时自动调用 `docHistory` 加载，不再需要手动点“文件历史”按钮。没有事件时显示“没有删除、移动或重命名记录”。

**窄屏（≤860px）**：文件栏和阅读区二选一显示。打开文件后显示阅读区，头部出现“← 文件列表”返回按钮。

### 5.2 提交历史 `/history/`

截图：`history-1440-light.png`。

布局：左侧提交列表（自适应）| 右侧详情面板 380px。≤1080px 时详情移到列表下方。

- 工具栏：分支下拉框（“全部分支” + 各分支）。原型里的搜索框**不实现**，因为过滤会破坏车道连续性。
- 列表：表头为“提交说明 / 作者 / 时间 / SHA”。每行是 `<button>`，用 CSS grid 排出各列，行高 34px。
  - 图形列复用现有车道算法，只把颜色换成 `--lane-*`，线宽 2，节点是空心圆（`--surface` 填充、车道色描边）。
  - ref 装饰用 `--info` 色标签放在说明前面，HEAD 等 ref 的样式保持现有区分。
  - 合并提交的说明用 `--fg-muted`。
  - 选中行用 `--accent-subtle` 底。
- 详情面板：
  - 提交说明第一行作为 `<h3>`，其余行作为正文（保留换行，用 `--fg-muted`）。
  - 键值列表：SHA（短 SHA + 复制完整 SHA 按钮）、作者（`title` 为邮箱）、时间、父提交（短 SHA）、引用。
  - 变更文件：取状态首字母放进 18px 方块，A 绿、M 黄、D 红、R 蓝，其他灰；路径用等宽字体；重命名显示 `旧 → 新`。
  - 未选中提交时显示 EmptyState“选择提交查看详情”。
- 窄屏：隐藏作者和 SHA 列。

### 5.3 扫描记录 `/scans/`

截图：`scans-1440-light.png`。

页面带内边距，内容如下：

1. **汇总条**：四格，带 1px 外框，窄屏变成 2×2。
   - 最近一次：状态胶囊。
   - 自动拉取：`sync_interval_seconds === -1` 时显示“已关闭”，否则显示“每 N 分钟”（不足 1 分钟按秒）。
   - 追踪分支：`latest_scan.branch_count` 个。
   - 扫描目录：`scan_paths` 的 path 以逗号连接，等宽字体。
2. **错误提示**：最近一次的 `error_count > 0` 时显示 warn 提示：“最近一次扫描有 N 个错误。M 个文件已更新索引。展开该行查看错误详情。”
3. **表格**：列为 状态 / 触发方式 / 分支 / 文件 / 跳过 / 错误 / 开始时间 / 耗时。
   - 数字列右对齐；值为 0 时用 `--fg-subtle`；错误数大于 0 时用 `--danger` 加粗。
   - 耗时 = `finished_at - started_at`，`finished_at` 为空时显示“进行中”。
   - `error_count > 0` 或 `error_message` 非空的行可以展开，在下方显示 `error_message` 和格式化后的 `detail_json`（`<pre>`，横向滚动）。

### 5.4 仓库设置 `/repo-settings/`（新页面）

截图：`repo-settings-1440-light.png`。

内容最大宽度 880px。分节布局：左侧 240px 放节标题和说明，右侧放字段；≤1080px 时上下排列。字段和保存逻辑复用现有 `form`、`saveRepo`、`splitList` 等。

| 节 | 字段 | 说明文字 |
| --- | --- | --- |
| 基本信息 | 名称、Slug（等宽，留空按名称生成）、Git URL（等宽） | 显示名称和 Git 地址。Slug 用于链接，留空时按名称生成。Git URL 下方提示：私有仓库的凭据放在服务端 credentials/ 目录，不写入数据库。 |
| 同步 | 自动拉取 Switch（关闭即 `sync_interval_seconds = -1`，打开默认 3600）、默认分支、拉取间隔秒（仅开启时显示，最小 60）、追踪分支 | DocHarbor 维护一份 bare mirror，按周期拉取并扫描追踪分支。开关说明：关闭后不在启动时和按周期拉取，仍可手动扫描或由 Webhook 触发。 |
| 智能最新 | 参与分支（include）、排除分支（exclude）、分支优先级 | 每篇文档在参与分支中取最近修改的有效版本。同一时间有多个版本时，按分支优先级决定。 |
| 扫描目录 | textarea，每行一个 | 只索引这些目录下的文件，每行一个，相对仓库根目录。 |
| GitHub Webhook | Payload URL（CopyField，取现有 `githubWebhookURL`） | 在 GitHub 仓库的 Settings → Webhooks 中填入此地址，push 后立即触发扫描。下面放 info 提示：“Secret 由所有仓库共用，在 系统设置 → GitHub Webhook 中查看或轮换”，并链接到 `/settings/`。 |

- 列表类字段（分支、glob）用等宽输入框，下方提示“逗号分隔，支持通配符”。
- 有改动时，底部出现吸底保存栏：“有未保存的修改 [放弃] [保存]”。保存成功后保存栏消失，并刷新仓库列表。
- 有未保存修改时离开页面，用 `beforeunload` 拦截。
- 校验错误显示在对应字段下方。接口错误显示在保存栏左侧，替换“有未保存的修改”。

### 5.5 新增仓库弹窗

在任意页面点侧栏“+”打开 ModalDialog，标题“新增仓库”：

- 必填区：Git URL、名称、默认分支（默认 main）、扫描目录（默认“doc, docs”。提交时按逗号或换行拆分）。
- “更多选项”折叠区：其余字段同仓库设置，默认值和现有新增表单一致。
- 主按钮“添加并开始扫描”。创建成功后跳转到新仓库的文档页 `/?repo={id}`。

### 5.6 AI 问答 `/ai/`

> **过渡期规格**：本节照常实施，它是旧引擎（`AI_ENGINE=legacy`）期间的界面。AI 问答之后会改为嵌入 pi，pi 引擎会话的界面以 [DocHarbor AI问答重构实施文档](DocHarbor%20AI问答重构实施文档.md) 第 9.2 节为准，在那份文档的步骤 3 中实现。旧链路删除时，本节的证据栏、候选服务、检索过程等组件一并删除。

截图：`ai-1440-light.png`、`ai-1440-dark.png`、`ai-390-light.png`。

页头说明行：状态胶囊（已启用 / 本地检索模式）· 供应商 · 模型 · “在全部仓库的智能最新文档和代码中检索，回答附带引用”。未启用时追加链接“前往 AI 供应商”。

布局：会话 240px | 对话 自适应 | 证据 320px。≤1280px 时两侧分别缩到 220 / 280px；≤1080px 时隐藏会话栏，在对话区顶部放“会话”按钮，点击用抽屉打开会话列表；≤860px 时证据栏移到对话下方。

**会话栏**

- 头部：“会话”+“新会话”小按钮。“刷新”放进同一行的图标按钮。
- 列表按日期分组（组标题如 2026-06-28），每项为标题（单行省略）+ 时间（HH:mm）。

**对话区**

- 消息列最大宽度 720px，居中。
- 提问：右对齐气泡，`--surface-2` 底，圆角 12/12/4/12。
- 回答：不加边框。头部为小头像（Bot 图标）+“供应商 · 模型”。下面依次是正文（15px）、通知条（现有 `messageNotice`，用 Notice 组件）、状态行（现有 `messageStatusLabel`，流式生成中显示“生成中”和停止按钮，如果现有逻辑支持中止）。
- 回答下方的操作：复制、查看诊断（跳转 `/ai/diagnostics/`）。
- 空状态：“从全部仓库提问，回答会附带文档和代码引用”。
- **引用标记**：把正文中的 `[C12]` 渲染成小号等宽标签。是否做成可点击要先确认映射：
  - 后端在 `internal/app/ai.go`（约 7457–7468 行）和 `ai_answer_composer.go:314` 中，按证据顺序给出 `[C{i+1}]` 编号。
  - 先核实保存的 `AIMessageCitation` 能否按 `message_id` 和顺序可靠地对应回编号。
  - 能对应：点击标签时，右栏切到“引用”并高亮滚动到对应项，引用项上也显示同样的编号。
  - 不能对应：标签只做样式，不可点击，也不要为此改后端。
- **输入框**：圆角 12px 的整体边框，聚焦时 accent 描边加光晕。内含：
  - textarea（2 行起，自动增高，最多 8 行）。
  - 底行：范围下拉（全部仓库 / 当前仓库：{名称} / 当前文件，禁用逻辑沿用现有）、“包含功能分支”复选框、快捷键提示、发送图标按钮。
  - **Enter 发送，Shift+Enter 换行**。输入法组合中（`event.isComposing` 或 `keyCode === 229`）按 Enter 不能发送。
  - 范围是“当前文件”时，在输入框上方显示文件路径小标签。

**证据栏**

用下划线标签切换：

- **引用**（计数）：每条是可点击卡片，点击后用现有 `openAICitation` 打开对应文档。
  - 第一行：编号（如能对应）· 仓库名 · 来源范围（`smart_latest` 显示“智能最新”，`branch_candidate` 显示“功能分支候选”）。
  - 第二行：文件路径（等宽）。
  - 第三行：分支标签（neutral）· 短 SHA ·“第 a-b 行”。
- **候选服务**（计数）：服务名 + 置信度胶囊 + 原因。
- **检索过程**：现有 `aiEvidenceChain` 渲染成步骤列表，每步前面是圆形状态标记（完成用绿色对勾，进行中用转圈，失败用红色）。
- 生成过程中，“检索过程”标签旁显示进行中指示。

### 5.7 AI 供应商 `/ai/config/`

> **过渡期规格**：本节照常实施。AI 问答重构的步骤 3 会按 [AI问答重构实施文档](DocHarbor%20AI问答重构实施文档.md) 第 9.3 节删除路由顺序、RPM、成本等级等内容，并调整状态条。

截图：`ai-providers-1440-light.png`。

页头右侧是主按钮“新增供应商”。

1. **状态条**（1px 外框，单行可换行）：
   - 启用状态胶囊。
   - 路由顺序：`#1 名称 模型 → #2 名称 模型`，来自 `aiActiveRouteProviders`。
   - 密钥存储状态（`encryption_ready`）和最近测试结果。
   - 右侧：已启用时显示“停用”（danger 小按钮，用 InlineConfirm 二次确认）。未启用且配置完整时显示“启用”。
2. **待应用提示**：`has_unapplied_changes` 时，在状态条下显示 warn 提示，内容为待应用的路由顺序和“应用配置”主按钮（现有 `applyAISettingsChanges`）。
3. **供应商表格**：列为 供应商（名称 + “默认”标签，第二行是等宽 base_url）/ 模型（等宽）/ API Key（“尾号 xxxx”或 warn 胶囊“未配置”）/ 路由（#n 或 —）/ 连接（最近测试结果，正常显示绿色对勾）/ 操作。
   - 操作列：“编辑”小按钮，加“⋯”菜单：测试连接、设为默认、删除（危险项）。禁用条件沿用现有 `aiProviderBusy` 等。
   - 删除需要二次确认。删除默认供应商需要选替代供应商时，沿用现有逻辑，在确认区域里给出选择。
4. **编辑 / 新增抽屉**：字段同现有表单（预设、名称、Base URL、模型、API Key、“高级设置”折叠区里的超时 / RPM / 成本等级 / 优先级），字段错误显示在字段下方。
   - API Key 的 placeholder：已配置时为“已配置，留空则不修改”。说明文字：“加密保存在服务端数据目录，页面不会再显示完整 Key。”
   - 底部：左侧“测试连接”，右侧“取消”“保存”。AI 未启用时，“保存”换成“保存并启用”。
   - 测试结果在抽屉底部上方显示一行（成功为绿色对勾 + 耗时 / 模型，失败为红色错误信息）。

原来页面上常驻的大表单去掉，所有编辑都走抽屉。

### 5.8 AI 诊断 `/ai/diagnostics/`

> **过渡期规格**：本节照常实施，对应旧引擎的诊断数据。AI 问答重构的步骤 3 会按 [AI问答重构实施文档](DocHarbor%20AI问答重构实施文档.md) 第 9.4 节加入 pi 运行记录；旧链路删除时，移除本节的旧诊断区块。

截图：`ai-diagnostics-1440-light.png`。

- **工具栏**：
  - Token 密码输入框（前置钥匙图标）。
  - 状态下拉（中文选项，值不变）。
  - 关键词搜索框。
  - “读取”按钮。
  - 右侧浅色提示：“需要 ai.diagnostics.read 能力的 Token，在系统设置中签发”，链接到 `/settings/`。
- **主体**：Run 列表 320px | 详情。
  - 列表项：状态胶囊、`#id`、时间 · 耗时（秒，一位小数）；第二行是问题（单行省略）。
  - 详情头：`Run #id` + 问题 + 状态胶囊。
  - 四格阶段汇总（带外框，窄屏 2×2）：任务框架 / 证据契约 / 证据包 / 回答校验，每格为标签 + 等宽主值 + 次要说明，取值同现有。
  - **检索轮次**：表格，列为 轮次 / 原因 / 查询 / 新证据。缺失键和 delta 作为每轮下方的次行。
  - **契约覆盖**：三列行，依次为 状态胶囊（已覆盖 / 部分 / 缺失）、等宽 key、原因。
  - **步骤**：每步一个 `<details>`。summary 为 状态胶囊 + agent 名 + 等宽 step_type + 右侧耗时；展开后是 JSON `<pre>`。
  - **数据源**：沿用现有内容，用同样的行样式。
- 空状态：未输入 Token 时显示“输入诊断 Token 后读取 Run”；没有 Run 时显示“没有符合条件的 Run”。

### 5.9 系统设置 `/settings/`

> **后续补充**：本节照常实施。AI 问答重构的步骤 3 会按 [AI问答重构实施文档](DocHarbor%20AI问答重构实施文档.md) 第 9.4 节新增“MCP 接入”一节，并给访问令牌增加 `docs.read` 能力。

截图：`settings-1440-light.png`。

分节布局同仓库设置。

1. **访问令牌**：
   - 说明：“签发短期 Token 给脚本或排查人员调用受保护的接口。Token 只显示一次。”
   - 有效期：下拉框，选项为 1 小时 / 8 小时 / 24 小时 / 自定义；选“自定义”时出现秒数输入，范围 300–86400。
   - Viewer Key 范围：等宽输入框，placeholder“留空表示不限制”。
   - 能力：两个复选框（AI 历史读取 `ai.history.read`、AI 问答排查 `ai.diagnostics.read`），替换现在的多选框。
   - “签发 Token”主按钮。
   - 签发后显示 warn 框：粗体“复制并保存这个 Token，关闭页面后无法再次查看。”、等宽 Token（可换行）、复制按钮、过期时间和 viewer 范围。
2. **GitHub Webhook**（从页头迁来）：
   - 说明：“所有仓库共用一个 Secret。轮换后要同步更新每个 GitHub 仓库的 Webhook 配置，否则推送会验签失败。”
   - Secret 用 CopyField，默认遮罩，旁边是显示 / 隐藏按钮。未配置时显示“未配置”和“生成 Secret”按钮。
   - “轮换 Secret”是 danger 按钮，需要 InlineConfirm 二次确认。
   - 底部浅色说明：“各仓库的 Payload URL 在对应仓库的「仓库设置」中。”
   - 复用现有的 `toggleWebhookSecret`、`refreshWebhookSecret`、`copyWebhookSecret`。
3. 操作结果（原 `settingsBanner`）在对应节内用 Notice 显示，不放页面顶部。

## 6. 组件拆分

### 6.1 目录结构

```text
src/
  entries/                 # 每个静态页面一个入口岛，只引用自己的视图
    DocsPage.vue           # <AppShell tab="docs"><DocsView /></AppShell>
    HistoryPage.vue
    ScansPage.vue
    RepoSettingsPage.vue
    AIChatPage.vue
    AIProvidersPage.vue
    AIDiagnosticsPage.vue
    SettingsPage.vue
  components/
    shell/                 # AppShell、AppSidebar、MobileBar、RepoHeader、PageHeader、ThemeSwitch、NewRepoDialog
    ui/                    # 第 4.4 节的公共组件
    HTMLPreviewPage.vue    # 保持不变
  views/                   # 每页主体（不要放进 src/pages/，那是 Astro 路由目录）
    DocsView.vue  HistoryView.vue  ScansView.vue  RepoSettingsView.vue
    AIChatView.vue  AIProvidersView.vue  AIDiagnosticsView.vue  SettingsView.vue
    docs/                  # DocsView 子组件：FileBrowser、DocReader、DocRail、MarkdownBody
    ai/                    # MessageItem、Composer、EvidencePanel 等
  composables/
    useRepos.ts            # repos、selectedRepo、branches、loadAll、selectRepo；模块级单例，Shell 和视图共享
    useURLState.ts         # 现有 readURLState / applyURLState / updateURL，按页面拆
    useDocs.ts             # 文件列表、搜索、打开文件、版本与路径事件
    useMarkdownPreview.ts  # rebuildMarkdownPreview、Mermaid、图片重试、HTML 预览加载
    useHistory.ts  useScanRuns.ts  useRepoForm.ts  useWebhookSecret.ts
    useAIChat.ts           # 会话、消息、流式事件处理、证据
    useAISettings.ts  useDiagnostics.ts  useAccessToken.ts  useTheme.ts
  lib/
    git-graph.ts           # buildGraphRows、车道计算等纯函数（从 App.vue 原样迁出）
    markdown-links.ts      # resolveRepoRelativePath、resolveMarkdownResourceURL 等纯函数
    format.ts              # formatTime、formatHistoryTime、formatBytes、shortSha、耗时
    labels.ts              # 第 5 节的状态 / 触发方式映射
    navigation.ts  content-type.ts  html-preview-resource.ts（保留）
  styles/
    tokens.css  base.css  components.css   # 全局；视图专属样式写在各 SFC 的 <style scoped>
```

### 6.2 要求

- 每个 `src/pages/**/index.astro` 改为挂载对应的 `entries/*Page.vue`（`client:load`）。这样各页只打包自己的视图，`App.vue` 最后删除。不要在 `.astro` 模板里把 Vue 视图当作 Vue 岛的 slot 子节点传入，那样子节点不会被水合。
- 多个页面共用的状态（仓库列表、所选仓库）放在 composable 的模块级 `ref` 里，或者在 AppShell 中 `provide`。不要通过 props 一层层往下传。
- 纯函数迁到 `lib/` 时**不改逻辑**，并补单元测试：git-graph 至少覆盖线性、分叉、合并三种；markdown-links 覆盖相对路径、`../`、外链、锚点。
- 迁移时保留现有中文注释中说明“为什么”的部分。
- `src/styles.css` 最终删除。`.html-viewer-*` 样式迁到 `HTMLPreviewPage.vue` 的 `<style>` 里，或者放进 `base.css`。

## 7. 响应式

| 断点 | 变化 |
| --- | --- |
| ≤1280px | 文档页右栏内容移到正文下方；AI 两侧栏收窄 |
| ≤1080px | AI 会话栏改为抽屉；提交详情移到列表下方；设置页分节改为上下排列 |
| ≤860px | 侧栏变成左侧抽屉（宽 264px，带遮罩，Esc 关闭）。顶部出现移动端标题栏（菜单按钮 + 当前仓库名或页面名）。页头按钮只留图标；文档页文件栏和阅读区二选一；AI 证据栏移到对话下方，输入框吸底；表格隐藏次要列（`.hide-sm`）；汇总条和阶段格变 2×2 |

任何宽度下，页面本身都不能横向滚动。只有表格、代码块和 git 图可以在各自容器内横向滚动。所有贴边元素都要处理 `env(safe-area-inset-*)`。

## 8. 测试改动

需要修改的 e2e（`e2e/static-mpa.spec.ts`）：

| 用例 | 改动 |
| --- | --- |
| `static history page remains meaningful without JavaScript` | 标题改为 `提交历史 · DocHarbor`；页面标题 heading 改为“提交历史”；`AI 问答` 链接断言保留 |
| `global navigation performs a real page navigation` | 不变 |
| `history first load does not request document rendering dependencies` | heading 改为“提交历史” |
| `mobile AI navigation exposes every global page…` | 先点击移动端标题栏的“菜单”按钮，再断言 `全局功能` 导航里的 AI 问答、AI 供应商、AI 诊断、系统设置可见；横向溢出断言保留 |
| `document titles survive long backup branches` | 原则上不改。README（来源为 main）按新规则不渲染分支行，现有断言没有涉及它，应该仍能通过 |

需要新增的 e2e：

- `/repo-settings/?repo=7`：显示表单，修改后出现保存栏，保存时发出 PATCH `/api/repos/7`；页面能看到 Webhook Payload URL，但看不到 Secret。
- 侧栏“+”打开新增仓库弹窗，Esc 关闭。
- `/settings/`：Webhook Secret 默认遮罩；轮换需要二次确认。
- AI 输入框：Enter 发送；Shift+Enter 不发送；输入法组合中的 Enter 不发送（mock 掉流式接口）。
- 暗色：`colorScheme: 'dark'` 下打开文档页，`body` 背景色等于 `--bg` 的深色值。
- 390px 下每个页面都没有横向溢出（循环检查全部 8 个页面）。

单元测试：第 6.2 节要求的 `git-graph`、`markdown-links`、`labels`、`format`；`navigation.test.ts` 补上 `repo-settings` 路由。

## 9. 主题切换与本页目录

这两项是原有功能之外新增的，放在最后一个阶段做，各自独立提交，方便单独回退。

- **主题切换**：
  - 侧栏底部三段控件：跟随系统 / 浅色 / 深色。选择保存在 `localStorage['docharbor-theme']`。
  - 在 `AppLayout.astro` 的 `<head>` 里用一小段内联脚本，在首屏绘制前设置 `<html data-theme>`，避免闪烁。读写 `localStorage` 要用 try/catch 包住。
  - Mermaid 按当前主题选 `default` 或 `dark`，主题切换后重新渲染当前文档的图。
  - `/html-preview/` 不受主题影响。
- **本页目录**：见第 5.1 节右栏。

## 10. 实施步骤

每一步结束时 `npm test`、`npm run build`、`npm run check:bundle`、`npm run test:e2e` 都要通过，然后单独提交。

1. **抽出纯函数**：抽出 `lib/git-graph.ts`、`lib/markdown-links.ts`、`lib/format.ts` 并补单测。界面不变。
2. **结构拆分**：建立 composables、shell、views 和 entries，Astro 页面改挂 entries，删除 `App.vue`。这一步沿用旧类名和旧 `styles.css`，界面基本不变，现有 e2e 全部通过。
3. **token 和公共组件**：加入 `tokens.css`、`base.css`、`components.css` 和 `components/ui/*`。
4. **新外观**：逐页套用新布局和样式，顺序为 文档 → 提交历史 → 扫描记录 → AI 问答 → AI 供应商 → AI 诊断 → 系统设置。每做完一页，截 1440 和 390 两种宽度的图，和 `doc/ui-redesign/` 对照。
5. **信息架构调整**：侧栏分组、`/repo-settings/` 页、Webhook 迁移、新增仓库弹窗、导航改名，同时更新和新增 e2e，加上 bundle 预算项。
6. **暗色模式**：暗色 token 生效后，逐页检查暗色截图，重点看 Markdown 表格、代码、Mermaid、git 图和状态胶囊。
7. **主题切换和本页目录**（第 9 节）。
8. **收尾**：删除 `src/styles.css` 中已无引用的部分，执行第 11 节的 grep 检查，更新 `README.md` 的页面列表和 `doc/DocHarbor使用手册.md` 里涉及界面位置的描述（截图可以留到下次再更新，但要在手册里注明）。

## 11. 验收清单

- [ ] 第 8 节的测试全部通过；`npm run check:bundle` 通过，各页 gzip 体积不超过预算。
- [ ] 8 个页面在 1440 和 390 宽度、亮色和暗色下截图都没有错位、截断或重叠，也没有横向溢出。
- [ ] 除 `tokens.css` 外，`src` 中没有十六进制颜色字面量。
- [ ] 键盘可以操作：Tab 顺序合理，焦点环可见；Esc 能关闭抽屉、弹窗、菜单和移动端侧栏；抽屉关闭后焦点回到触发元素。
- [ ] 旧链接 `/?tab=history&repo=7&branch=x` 仍能重定向；`/?repo=1&version=…`、`/history/?repo=1&commit=…`、`/ai/?session=…` 这类深链接都能恢复状态。
- [ ] 没有调用 `window.confirm`、`alert` 或 `prompt`。
- [ ] 后端代码没有改动（`git diff --stat -- internal cmd` 为空）。

## 12. 原型中不要照搬的地方

- 原型的文件行是单行，分支标签放在右侧。实现时按第 2 节约束 4 **分两行**，只是在默认分支时不渲染第二行。
- 原型的提交列表是可点击的 `<tr>`，实现时用 `<button>` 行。
- 原型里的提交搜索框、诊断 Token 的示例值、Token 和 Secret 的示例值、“内网 Qwen”供应商、提交的变更文件列表、诊断详情，都是示例数据或演示功能，不要实现成固定内容。
- 原型的图标是手写 SVG，实现时用 `lucide-vue-next` 中对应的图标（Bot、SlidersHorizontal、Activity、Settings、BookOpen、GitBranch、ListChecks、Folder、FileText、Search、Download、History、MoreHorizontal、ExternalLink、ChevronUp、ChevronLeft、Copy、Send、X、Menu、Info、TriangleAlert、Check、ShieldCheck、Eye、EyeOff、RotateCw、Plus、RefreshCw、KeyRound）。
- 原型宽度 ≤1280px 时直接隐藏文档右栏，实现时要把内容移到正文下方，不能丢掉分支版本和路径事件。
- 原型的状态都在一个全局对象里，用 innerHTML 渲染，这只是原型写法，和实现结构无关。
