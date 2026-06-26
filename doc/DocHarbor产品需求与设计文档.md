# DocHarbor 产品需求与设计文档

## 1. 文档目标

本文用于沉淀一个新的独立项目：DocHarbor。系统配置好 Git 仓库和扫描目录后，自动 clone 和同步仓库，扫描指定目录下的文档文件，提供给前端查看、预览和单文件下载。

本系统不落在现有 `dev-manager`、`go-gva-admin` 或其他业务服务内，按一个新的独立项目设计和交付。

## 2. 已确认产品约束

### 2.1 项目形态

- 新建独立项目，不复用 `dev-manager` 作为承载服务。
- 系统自己负责 clone 远程 Git 仓库，并定期 fetch 最新提交。
- 系统以 Git 仓库为文档源，不在前端上传文档，不在系统内编辑文档。

### 2.2 鉴权边界

- 当前不做用户系统。
- 当前不做系统内角色、权限、登录态和用户维度审计。
- 访问控制交给 Pangolin 代理鉴权。
- 如果后续需要区分“只读访问”和“配置管理”，优先通过 Pangolin 路由或访问策略拆分，而不是在首期引入用户系统。

### 2.3 下载边界

- 当前只支持单文件下载。
- 暂不支持目录打包 zip 下载。
- 后续如果有需求，再增加目录打包下载和批量下载任务。

### 2.4 分支与历史

- 默认入口提供“智能最新”视图：扫描所有纳入管理的分支后，按文档维度选择最新有效版本。
- 保留“按分支浏览”视图，用户可以指定分支查看该分支 HEAD 内容。
- 需要提供图形化 Git 历史查看能力，包括提交图、提交详情、文件变更列表、文件删除、移动和重命名记录。
- 删除、移动和重命名不能简单表现为文档消失，需要在文档历史和版本候选中可追踪。

### 2.5 文档范围

- 只扫描配置指定的目录。
- 跳过 `.git`、隐藏系统目录、临时目录和超出大小限制的文件。
- 首期只做 Markdown 预览，Markdown 内需要支持 Mermaid 图表；其他文件先只支持下载。

## 3. 产品目标

### 3.1 核心目标

1. 降低内部文档散落在多个 Git 仓库中的查找成本。
2. 让非研发用户可以通过浏览器查看 Git 中的最新文档。
3. 保持 Git 作为唯一可信文档源，避免系统内出现独立副本和版本分叉。
4. 支持按分支浏览，让测试、预发、线上等不同分支的文档可以被直接查看。
5. 支持图形化查看 Git 历史，让用户能理解文档近期变更。

### 3.2 用户场景

| 场景 | 描述 |
| ---- | ---- |
| 查看最新文档 | 用户进入系统，默认查看所有有效分支中智能计算出的最新文档 |
| 指定分支查看 | 用户切换到指定分支，查看该分支下的文档树和文件内容 |
| 下载单文件 | 用户在文档详情页下载当前文件 |
| 查看变更历史 | 用户查看仓库或文件的 Git 提交历史，定位最近修改 |
| 配置文档源 | 管理人员配置仓库地址、分支、扫描目录和扫描周期 |
| 手动刷新 | 管理人员在需要时手动触发仓库同步和扫描 |

## 4. 首期范围

### 4.1 本期范围

- 新项目服务端和前端。
- 仓库配置管理。
- Git clone、fetch、branch 列表同步。
- 指定目录扫描。
- 智能最新文档树浏览。
- 指定分支文档树浏览。
- Markdown 文档预览。
- Mermaid 图表渲染。
- 单文件下载。
- 仓库级图形化 Git 历史。
- 文件级历史入口。
- 文件删除、移动、重命名的历史追踪。
- 扫描记录和错误信息查看。

### 4.2 明确不做

- 不做用户系统。
- 不做系统内权限模型。
- 不做文档在线编辑。
- 不做 Git commit、push、merge request 或审批流程。
- 不做目录打包下载。
- 不做评论、点赞、收藏等协作功能。
- 不做复杂全文搜索引擎接入。
- 不把文档内容写回业务仓库。

## 5. 项目命名与技术栈建议

### 5.1 项目命名

产品名确定为 `DocHarbor`，项目目录和后续仓库名使用 `doc-harbor`。

命名含义：

- `Doc`：核心对象是文档。
- `Harbor`：多个 Git 仓库文档汇聚、停靠和分发的港口。
- 整体语义是内部文档的集中入口，兼顾浏览、下载和 Git 历史追踪。


### 5.2 技术栈建议

首期推荐单体应用：

| 层级 | 建议 |
| ---- | ---- |
| 后端 | Go + Gin 或 Go 标准库 HTTP |
| 前端 | Vue 3 + TypeScript |
| 数据库 | SQLite 起步，后续可迁移 MySQL |
| Git 操作 | 系统 `git` 命令，使用参数数组调用 |
| 部署 | Docker 镜像 + 持久化数据卷 |
| 鉴权 | Pangolin 反向代理 |

选择理由：

- 没有用户系统和复杂权限，SQLite 足够承载配置、索引和扫描记录。
- Git 仓库文件内容不进入数据库，数据库只保存索引和元信息。
- 单体应用方便部署到 Pangolin 后面。
- Go 单二进制适合做 Git 命令编排、文件流下载和后台定时任务。

如果后续需要多实例部署，再引入 MySQL、Redis 锁和共享对象存储。

## 6. 总体架构

### 6.1 组件划分

```
doc-harbor
├── Web Frontend
│   ├── 仓库列表
│   ├── 文档浏览
│   ├── 文件预览
│   ├── Git 历史图
│   └── 配置与扫描记录
├── API Server
│   ├── 仓库配置 API
│   ├── 文档树 API
│   ├── 文件预览和下载 API
│   ├── Git 历史 API
│   └── 扫描任务 API
├── Scheduler / Worker
│   ├── 定时同步仓库
│   ├── 定时扫描目录
│   └── 手动扫描任务
├── Git Storage
│   └── bare mirror 仓库
└── Metadata DB
    ├── 仓库配置
    ├── 分支快照
    ├── 文档实体
    ├── 分支版本索引
    ├── 智能最新索引
    └── 扫描记录
```

### 6.2 数据流

1. 管理人员配置仓库 URL、默认分支、追踪分支、扫描目录和扫描周期。
2. 系统首次扫描时执行 `git clone --mirror`，将仓库存为本地 bare mirror。
3. 定时任务或手动任务执行 `git remote update --prune`。
4. 系统读取仓库分支列表，定位每个追踪分支的 HEAD commit。
5. 对每个分支的指定目录执行 `git ls-tree -r`。
6. 按 include、exclude 和文件大小限制过滤。
7. 将文档身份、路径、类型、大小、blob sha、commit sha、文件状态等元信息写入版本索引。
8. 系统基于所有有效分支候选版本计算智能最新视图。
9. 前端浏览时读取智能最新或指定分支索引构建目录树。
10. 前端预览或下载文件时，后端通过 `git cat-file` 或 `git show <commit>:<path>` 从 Git 对象中读取内容并返回。

## 7. 分支智能管理与 Git 存储设计

### 7.1 智能最新视图设计

#### 7.1.1 视图目标

“智能最新”不是简单选择 HEAD 时间最新的分支，而是按文档维度在所有有效分支中选择最新有效版本。

这样可以避免某个 feature 分支刚提交了一行无关代码，就把整个分支误认为最新文档来源。

#### 7.1.2 文档身份

首期文档身份按以下优先级识别：

1. Markdown frontmatter 中的稳定 `doc_id`，如果后续引入该字段。
2. Git rename/move 历史识别出的同一文件链路。
3. 仓库 + 扫描目录 + 相对路径。

首期可以先以“仓库 + 扫描目录 + 相对路径”为主，同时保留 Git rename/move 事件表，为后续自动归并文档身份留出空间。

#### 7.1.3 最新版本选择

同一个文档在多个分支存在候选版本时，按以下规则选择智能最新版本：

1. 只在启用仓库、启用扫描目录和有效分支中选择。
2. 候选版本必须是当前分支 HEAD 上存在的文件。
3. 优先按该文件自己的 `last_commit_time` 倒序排序。
4. 时间相同时按分支优先级排序，例如 `main/master`、`release/*`、`develop`、`feature/*`。
5. 再相同时按分支名和 commit sha 做稳定排序。

前端展示智能最新文档时，必须展示来源分支、来源 commit 和最近修改时间。

#### 7.1.4 分支有效性

分支是否参与智能最新由配置决定：

- `tracked_branches`：纳入扫描和浏览的分支。
- `latest_include_branches`：参与智能最新的分支。
- `latest_exclude_branches`：从智能最新中排除的分支。
- `stale_branch_days`：超过指定天数没有更新的分支默认不参与智能最新，但仍可按分支手动浏览。
- `branch_priority`：智能最新平局时使用的分支优先级。

默认建议：

- 扫描默认分支和配置指定分支。
- 如果配置为扫描所有分支，智能最新仍优先排除 `archive/*`、`tmp/*`、`dependabot/*` 等低价值分支。

#### 7.1.5 删除、移动和重命名

删除、移动和重命名都不能只表现为索引行消失：

- 删除：当前分支 HEAD 不再存在该文档时，该分支上的候选版本标记为 `deleted`，并记录删除 commit。
- 移动：路径变化但内容链路可通过 Git 历史识别时，记录 old path 和 new path。
- 重命名：文件名变化但文档身份保持一致时，记录 rename 事件。
- 智能最新只从 `active` 状态候选版本中选择；如果所有有效分支都删除了该文档，文档在默认列表中隐藏，但可在历史或已删除视图中查看。

文档详情页需要展示该文档在各分支的状态：

- active：该分支当前存在。
- deleted：该分支当前已删除。
- renamed：该分支当前路径已变化。
- moved：该分支当前目录已变化。

### 7.2 仓库 clone 方式

推荐使用 bare mirror：

```bash
git clone --mirror <repo_url> <data_dir>/repos/<repo_id>.git
```

后续更新：

```bash
git -C <data_dir>/repos/<repo_id>.git remote update --prune
```

选择 bare mirror 的原因：

- 不需要维护工作区。
- 可以直接读取任意分支、tag、commit 的树和 blob。
- 支持历史查看，不受当前 checkout 状态影响。
- 多分支浏览更稳定。

### 7.3 引用读取

- 分支列表：读取 `refs/heads/*`。
- 默认分支：优先使用配置值，例如 `main`、`master`、`release`。
- 指定分支：前端传分支名，后端校验该分支存在后解析到该分支 HEAD commit sha。
- 历史 commit：前端传 commit sha，后端校验 commit 属于该仓库。

### 7.4 文件读取

文件内容从 Git 对象读取，不从临时 checkout 目录读取。

读取方式：

```bash
git -C <repo.git> cat-file -p <blob_sha>
```

或：

```bash
git -C <repo.git> show <commit_sha>:<file_path>
```

下载接口优先使用索引中的 `blob_sha`，避免分支 HEAD 在请求过程中变化导致内容漂移。

## 8. 数据模型设计

### 8.1 repositories

仓库配置表。

| 字段 | 类型 | 说明 |
| ---- | ---- | ---- |
| id | integer | 主键 |
| name | string | 仓库展示名 |
| slug | string | URL 和接口中使用的稳定标识 |
| repo_url | string | Git 仓库地址 |
| default_branch | string | 默认查看分支 |
| tracked_branches | string | 追踪分支配置，JSON 数组，支持 `["main"]` 或 `["*"]` |
| latest_include_branches | string | 参与智能最新的分支 include 规则，JSON 数组 |
| latest_exclude_branches | string | 不参与智能最新的分支 exclude 规则，JSON 数组 |
| stale_branch_days | integer | 分支超过多少天无更新后不参与智能最新 |
| branch_priority | string | 分支优先级规则，JSON 数组 |
| credential_ref | string | 凭据引用，不直接暴露密钥内容 |
| enabled | bool | 是否启用扫描 |
| sync_interval_seconds | integer | 同步间隔 |
| max_file_size_bytes | integer | 单文件预览和索引大小限制 |
| created_at | datetime | 创建时间 |
| updated_at | datetime | 更新时间 |

### 8.2 repo_scan_paths

扫描目录配置表。

| 字段 | 类型 | 说明 |
| ---- | ---- | ---- |
| id | integer | 主键 |
| repo_id | integer | 仓库 ID |
| path | string | 仓库内扫描目录 |
| include_globs | string | 包含规则，JSON 数组 |
| exclude_globs | string | 排除规则，JSON 数组 |
| enabled | bool | 是否启用 |
| created_at | datetime | 创建时间 |
| updated_at | datetime | 更新时间 |

默认排除规则：

- `.git/**`
- `node_modules/**`
- `vendor/**`
- `dist/**`
- `build/**`
- `.DS_Store`
- 临时文件和隐藏系统文件

### 8.3 repo_refs

仓库分支快照表。

| 字段 | 类型 | 说明 |
| ---- | ---- | ---- |
| id | integer | 主键 |
| repo_id | integer | 仓库 ID |
| ref_type | string | `branch` 或 `tag` |
| ref_name | string | 分支名或 tag 名 |
| commit_sha | string | 当前指向的 commit |
| commit_time | datetime | commit 时间 |
| last_scanned_at | datetime | 最近扫描时间 |
| created_at | datetime | 创建时间 |
| updated_at | datetime | 更新时间 |

首期主要使用 `branch`。

### 8.4 documents

文档实体表，用来表达“同一份文档”的稳定身份。它不等同于某个分支上的某个路径。

| 字段 | 类型 | 说明 |
| ---- | ---- | ---- |
| id | integer | 主键 |
| repo_id | integer | 仓库 ID |
| scan_path | string | 初始命中的扫描目录 |
| doc_key | string | 文档稳定标识，首期可用规范化路径，后续可接入 frontmatter `doc_id` |
| current_title | string | 当前展示标题 |
| current_path | string | 当前智能最新版本路径 |
| status | string | `active`、`deleted` |
| created_from_branch | string | 首次发现分支 |
| created_from_commit | string | 首次发现 commit |
| latest_version_id | integer | 当前智能最新版本 ID |
| created_at | datetime | 创建时间 |
| updated_at | datetime | 更新时间 |

建议唯一索引：

- `(repo_id, doc_key)`

### 8.5 doc_versions

文档版本表，表示某份文档在某个分支 HEAD 上的当前候选状态。

| 字段 | 类型 | 说明 |
| ---- | ---- | ---- |
| id | integer | 主键 |
| repo_id | integer | 仓库 ID |
| document_id | integer | 文档实体 ID |
| branch | string | 分支名 |
| head_commit_sha | string | 扫描时分支 HEAD |
| scan_path | string | 命中的扫描目录 |
| file_path | string | 仓库内完整路径 |
| previous_path | string | 移动或重命名前的路径 |
| dir_path | string | 父目录 |
| file_name | string | 文件名 |
| extension | string | 扩展名 |
| mime_type | string | MIME 类型 |
| file_size | integer | 文件大小 |
| blob_sha | string | Git blob sha |
| status | string | `active`、`deleted`、`renamed`、`moved` |
| title | string | 文档标题，Markdown 可取一级标题 |
| previewable | bool | 是否支持预览 |
| download_enabled | bool | 是否支持下载 |
| last_commit_sha | string | 该文件最近一次修改 commit |
| last_commit_time | datetime | 该文件最近一次修改时间 |
| delete_commit_sha | string | 删除 commit，未删除时为空 |
| delete_commit_time | datetime | 删除时间，未删除时为空 |
| rename_score | integer | Git rename 相似度，非 rename/move 时为空 |
| participates_latest | bool | 是否参与智能最新计算 |
| created_at | datetime | 创建时间 |
| updated_at | datetime | 更新时间 |

建议唯一索引：

- `(repo_id, branch, document_id)`

建议查询索引：

- `(repo_id, branch, dir_path)`
- `(repo_id, branch, extension)`
- `(repo_id, branch, last_commit_time)`
- `(repo_id, document_id, branch)`

### 8.6 doc_latest

智能最新物化表，用来给默认文档树快速查询。

| 字段 | 类型 | 说明 |
| ---- | ---- | ---- |
| id | integer | 主键 |
| repo_id | integer | 仓库 ID |
| document_id | integer | 文档实体 ID |
| version_id | integer | 被选中的最新版本 ID |
| source_branch | string | 来源分支 |
| source_commit_sha | string | 来源 commit |
| file_path | string | 展示路径 |
| dir_path | string | 展示父目录 |
| file_name | string | 展示文件名 |
| last_commit_time | datetime | 最新修改时间 |
| selection_reason | string | 选择原因，例如 `latest_file_commit`、`branch_priority_tiebreak` |
| created_at | datetime | 创建时间 |
| updated_at | datetime | 更新时间 |

建议唯一索引：

- `(repo_id, document_id)`

建议查询索引：

- `(repo_id, dir_path)`
- `(repo_id, last_commit_time)`

### 8.7 doc_path_events

文档路径事件表，用来记录删除、移动、重命名等文件生命周期变化。

| 字段 | 类型 | 说明 |
| ---- | ---- | ---- |
| id | integer | 主键 |
| repo_id | integer | 仓库 ID |
| document_id | integer | 文档实体 ID |
| branch | string | 分支名 |
| event_type | string | `created`、`deleted`、`renamed`、`moved` |
| old_path | string | 旧路径 |
| new_path | string | 新路径 |
| commit_sha | string | 事件发生 commit |
| commit_time | datetime | 事件发生时间 |
| rename_score | integer | Git rename 相似度 |
| created_at | datetime | 创建时间 |

建议查询索引：

- `(repo_id, document_id, branch, commit_time)`
- `(repo_id, branch, commit_sha)`

### 8.8 scan_runs

扫描任务记录表。

| 字段 | 类型 | 说明 |
| ---- | ---- | ---- |
| id | integer | 主键 |
| repo_id | integer | 仓库 ID |
| trigger_type | string | `scheduled`、`manual`、`startup` |
| status | string | `running`、`success`、`partial_success`、`failed` |
| branch_count | integer | 分支数量 |
| file_count | integer | 成功索引文件数 |
| skipped_count | integer | 跳过文件数 |
| error_count | integer | 文件级错误数 |
| started_at | datetime | 开始时间 |
| finished_at | datetime | 完成时间 |
| error_message | text | 任务级错误 |
| detail_json | text | 分支和目录级统计 |

### 8.9 credential_refs

凭据引用表或配置文件。

| 字段 | 类型 | 说明 |
| ---- | ---- | ---- |
| id | integer | 主键 |
| name | string | 凭据名称 |
| type | string | `ssh_key`、`https_token` |
| secret_path | string | 密钥文件路径或环境变量名称 |
| created_at | datetime | 创建时间 |
| updated_at | datetime | 更新时间 |

首期推荐不在数据库中保存明文密钥。密钥通过容器挂载文件或环境变量注入，数据库只保存引用。

## 9. 扫描流程设计

### 9.1 触发方式

- 启动后自动扫描启用的仓库。
- 定时扫描。
- 前端手动触发扫描。

### 9.2 任务流程

1. 校验仓库配置是否启用。
2. 获取仓库级任务锁，防止同一仓库并发扫描。
3. 如果本地 mirror 不存在，执行 clone。
4. 如果本地 mirror 已存在，执行 fetch/prune。
5. 读取分支列表，应用 `tracked_branches` 过滤。
6. 对每个分支解析 HEAD commit。
7. 如果该分支 HEAD 和上次扫描一致，可以跳过文件扫描。
8. 对每个扫描目录执行递归 `ls-tree`，得到该分支 HEAD 下的当前文件集合。
9. 按规则过滤文件。
10. 读取文件元信息和最近一次修改 commit。
11. 将当前文件集合与上一轮该分支的 `doc_versions` 对比，识别新增、更新、删除。
12. 通过 Git diff/rename 检测补充移动和重命名事件。
13. 在事务中更新 `documents`、`doc_versions`、`doc_path_events`。
14. 基于有效分支候选版本重算 `doc_latest`。
15. 写入 `repo_refs` 和 `scan_runs`。
16. 释放任务锁。

### 9.3 失败处理

- clone、fetch、仓库损坏、Git 命令不可用属于任务级失败。
- 单个文件元信息读取失败属于文件级失败，记录错误并继续扫描其他文件。
- 删除、移动、重命名识别失败属于文件生命周期识别失败，不应阻断当前分支基础索引；可以先按路径变化记录为删除和新增，并在详情中标记“未归并”。
- 单个分支扫描失败不影响其他分支，最终任务状态为 `partial_success`。
- 如果任务级失败，保留上一次成功索引，不清空前端可见文档。

### 9.4 增量策略

首期采用分支级增量：

- 分支 HEAD 未变化：跳过扫描。
- 分支 HEAD 变化：重新扫描该分支配置目录，更新该分支 `doc_versions`，并重算受影响文档的 `doc_latest`。

后续如果仓库很大，再优化为基于 diff 的增量索引。

### 9.5 删除、移动和重命名识别

首期推荐把文件生命周期识别分成两层：

1. 当前态对比：用本轮 `ls-tree` 结果和上一轮该分支版本集合对比，识别当前存在、新增和删除。
2. Git 历史辅助：对 HEAD 变化范围执行 diff，识别 rename/move。

可使用的 Git 命令：

```bash
git -C <repo.git> diff --name-status --find-renames --find-copies <old_head> <new_head> -- <scan_path>
```

处理规则：

- `D`：记录删除事件，并将该分支对应 `doc_versions.status` 标记为 `deleted`。
- `Rxxx`：记录重命名或移动事件，将旧路径和新路径关联到同一个 `document_id`。
- `A`：如果无法和历史路径关联，则创建新的 `documents`。
- `M`：更新当前分支上的 `doc_versions`。

移动和重命名的区分：

- 仅文件名变化：`renamed`。
- 目录变化但文件名不变：`moved`。
- 目录和文件名都变化：按 `moved` 记录，同时保留 old path/new path。

智能最新计算只选择 `active` 状态版本。已删除版本保留在 `doc_versions` 和 `doc_path_events` 中，用于历史查看和跨分支状态展示。

## 10. 文件类型与预览设计

### 10.1 首期支持

| 类型 | 扩展名 | 行为 |
| ---- | ---- | ---- |
| Markdown | `.md`, `.markdown` | 前端渲染预览，支持 Mermaid，支持下载 |
| 文本 | `.txt`, `.log`, `.json`, `.yaml`, `.yml`, `.toml`, `.sql` | 首期不预览，仅支持下载 |
| 图片 | `.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`, `.svg` | 首期不预览，仅支持下载 |
| PDF | `.pdf` | 首期不预览，仅支持下载 |
| Office | `.doc`, `.docx`, `.xls`, `.xlsx`, `.ppt`, `.pptx` | 不解析，支持下载 |
| 压缩包 | `.zip`, `.tar`, `.gz`, `.7z`, `.rar` | 不解析，支持下载 |

### 10.2 Markdown 预览

- 后端返回 Markdown 原文、文件元信息和当前 commit。
- 前端渲染 Markdown。
- HTML 内容需要进行 sanitize，避免脚本执行。
- 相对图片和相对链接按当前文件所在目录解析。
- 指向仓库内文件的相对链接尽量转换为系统内文档浏览链接。
- 指向外部站点的链接保留为外链。

### 10.3 Mermaid 支持

Markdown 预览必须支持 Mermaid：

- 识别 fenced code block 中语言标记为 `mermaid` 的代码块。
- 前端使用 Mermaid 渲染流程图、时序图、状态图、类图等 Mermaid 支持的图表。
- Mermaid 渲染失败时，不影响整篇 Markdown 展示；失败图表降级展示原始 Mermaid 代码和错误提示。
- Mermaid 代码块不在后端执行，后端只返回原文。
- Mermaid 渲染结果需要和 Markdown HTML 一起纳入前端安全边界，禁止脚本执行和不受控 HTML 注入。
- Mermaid 图表需要适配浅色主题，后续可按前端主题扩展深色模式。

### 10.4 预览大小限制

- 单文件预览默认限制 2 MB。
- 超过限制的文件只展示元信息和下载按钮。
- 下载不受预览大小限制，但仍受服务端配置的最大响应限制保护。

## 11. Git 历史查看设计

### 11.1 仓库历史

前端提供仓库历史页面：

- 分支选择器。
- 提交图。
- commit 列表。
- commit message。
- author。
- commit time。
- branch/tag 标记。
- parent commits。
- changed files 统计。

后端可以基于以下命令获取提交图数据：

```bash
git -C <repo.git> log --topo-order --date=iso-strict --decorate --parents --max-count=<limit> <branch>
```

后端返回结构化 commit 数据，由前端渲染图形化提交线。

### 11.2 Commit 详情

点击 commit 后展示：

- commit sha。
- author 和 email。
- commit time。
- commit message。
- parent commit。
- 文件变更列表。
- 每个文件的状态：新增、修改、删除、重命名。

后端可以通过以下命令获取文件变更：

```bash
git -C <repo.git> diff-tree --no-commit-id --name-status --find-renames -r <commit_sha>
```

### 11.3 文件历史

文档详情页提供“文件历史”入口：

- 按 `document_id` 查询文档历史，而不是只按当前路径查询。
- 展示该文档在各分支上的当前状态、当前路径、最后修改 commit 和删除 commit。
- 展示 `doc_path_events` 中记录的创建、删除、移动、重命名事件。
- 支持点击某个 commit 查看该 commit 时的文件内容。
- 如果某个历史版本已经删除，展示删除信息和删除前最后一个可查看版本。

后端可以用索引数据作为主路径，再用 Git 命令补充单路径历史：

```bash
git -C <repo.git> log --follow --date=iso-strict -- <file_path>
```

对于扫描过程中已经识别出的 rename/move，文件历史必须把 old path 和 new path 串到同一个文档时间线上。未能自动归并的路径变化，先以“疑似删除/新增”展示，并允许后续通过文档身份规则优化。

### 11.4 历史版本查看

文件预览接口支持传入 `commit_sha`：

- 传 `branch` 且不传 `commit_sha`：查看该分支最新扫描结果。
- 传 `commit_sha`：查看指定 commit 中的文件内容。

历史版本查看不写入当前 `doc_versions` 或 `doc_latest` 索引，直接从 Git 对象读取。

## 12. API 设计

接口路径示例统一使用 `/api` 前缀。

### 12.1 仓库配置

| 方法 | 路径 | 说明 |
| ---- | ---- | ---- |
| GET | `/api/repos` | 仓库列表 |
| POST | `/api/repos` | 新增仓库 |
| GET | `/api/repos/:repo_id` | 仓库详情 |
| PATCH | `/api/repos/:repo_id` | 修改仓库配置 |
| DELETE | `/api/repos/:repo_id` | 停用或删除仓库 |
| POST | `/api/repos/:repo_id/scan` | 手动触发扫描 |
| GET | `/api/repos/:repo_id/scan-runs` | 扫描记录 |

### 12.2 分支与文档浏览

| 方法 | 路径 | 说明 |
| ---- | ---- | ---- |
| GET | `/api/repos/:repo_id/branches` | 分支列表 |
| GET | `/api/repos/:repo_id/tree` | 文档目录树，参数 `view=latest|branch`、`branch`、`path` |
| GET | `/api/repos/:repo_id/files` | 文件列表，参数 `view=latest|branch`、`branch`、`dir` |
| GET | `/api/repos/:repo_id/documents/:document_id` | 文档实体详情 |
| GET | `/api/repos/:repo_id/documents/:document_id/versions` | 文档在各分支的候选版本和状态 |
| GET | `/api/repos/:repo_id/versions/:version_id/content` | 指定文档版本预览内容 |
| GET | `/api/repos/:repo_id/versions/:version_id/download` | 指定文档版本单文件下载 |

### 12.3 历史查看

| 方法 | 路径 | 说明 |
| ---- | ---- | ---- |
| GET | `/api/repos/:repo_id/history` | 仓库提交图，参数 `branch`、`limit` |
| GET | `/api/repos/:repo_id/commits/:sha` | commit 详情 |
| GET | `/api/repos/:repo_id/documents/:document_id/history` | 文档历史，包含路径事件和各分支版本 |
| GET | `/api/repos/:repo_id/path-events` | 路径事件列表，参数 `document_id`、`branch`、`event_type` |
| GET | `/api/repos/:repo_id/blob` | 指定 commit 和 path 的文件内容 |
| GET | `/api/repos/:repo_id/blob/download` | 指定 commit 和 path 的文件下载 |

### 12.4 安全校验

- 所有接口只接受已配置仓库 ID。
- 文件路径必须来自索引或通过仓库内路径规范化校验。
- 禁止 `../`、绝对路径、空字节等路径输入。
- Git 命令必须通过参数数组执行，不拼接 shell 字符串。
- Git 命令读取路径时使用 `--` 分隔 pathspec。
- commit sha 必须符合 Git 对象格式，并校验属于当前仓库。

## 13. 前端页面设计

### 13.1 仓库列表页

展示：

- 仓库名称。
- 默认分支和智能最新策略状态。
- 最新扫描状态。
- 最新扫描时间。
- 文件数量。
- 错误提示。

操作：

- 进入文档浏览。
- 进入 Git 历史。
- 手动扫描。
- 进入配置。

### 13.2 文档浏览页

布局：

- 顶部：仓库选择、视图切换、分支选择、扫描状态。
- 左侧：目录树。
- 中间：文件列表或文件预览。
- 右侧或顶部信息区：文件元信息、来源分支、来源 commit、下载按钮、历史入口。

能力：

- 默认展示智能最新文档树。
- 切换分支后重新加载目录树。
- 点击目录查看子文件。
- 点击文件进入预览。
- 下载当前文件。
- 查看当前文档在所有分支的版本和状态。
- 查看当前文档的删除、移动、重命名历史。

### 13.3 Git 历史页

布局：

- 顶部：仓库、分支、提交数量限制。
- 左侧或主区域：提交图。
- 右侧：commit 详情。

能力：

- 渲染 commit 节点和父子关系。
- 展示 commit message、作者、时间。
- 点击 commit 查看变更文件。
- 点击变更文件查看该 commit 下的文件内容。
- 对删除、移动、重命名文件展示明确状态和 old path/new path。

### 13.4 配置页

能力：

- 新增仓库配置。
- 修改仓库 URL、默认分支、追踪分支、扫描目录、include/exclude。
- 配置扫描周期。
- 触发手动扫描。
- 查看扫描记录。

由于首期不做用户系统，配置页访问控制依赖 Pangolin。如果需要更强保护，可以让 Pangolin 对 `/settings` 或 `/api/repos` 写接口使用更严格的访问策略。

## 14. 部署设计

### 14.1 运行形态

推荐 Docker 部署：

```text
doc-harbor
├── /app
└── /data
    ├── doc-harbor.db
    ├── repos/
    ├── credentials/
    └── logs/
```

### 14.2 必要配置

| 配置 | 说明 |
| ---- | ---- |
| `DATA_DIR` | 数据目录 |
| `HTTP_ADDR` | HTTP 监听地址 |
| `DB_DSN` | SQLite 文件路径或 MySQL DSN |
| `GIT_BIN` | Git 命令路径 |
| `DEFAULT_SCAN_INTERVAL` | 默认扫描间隔 |
| `MAX_PREVIEW_FILE_SIZE` | 最大预览文件大小 |
| `ALLOWED_GIT_HOSTS` | 允许 clone 的 Git host 白名单 |

### 14.3 Pangolin 接入

- Pangolin 对外暴露系统域名。
- Pangolin 负责用户登录和访问控制。
- 应用服务只接收 Pangolin 转发后的请求。
- 应用首期不依赖 `X-Forwarded-User` 等用户头。
- 后续如果需要审计，可读取 Pangolin 透传的用户头作为操作人来源。

## 15. 安全与边界

### 15.1 Git clone 安全

- 配置 Git URL 时建议校验 host 白名单。
- 不允许 `file://`、本机任意路径 clone，除非显式打开本地仓库模式。
- 凭据不落明文数据库。
- clone 和 fetch 设置超时。
- 单仓库设置最大磁盘占用告警。

### 15.2 文件访问安全

- 下载接口不接受本机绝对路径。
- 下载接口通过 repo、document、version、branch、commit、blob 或已索引路径定位文件。
- 所有路径都必须是仓库内相对路径。
- Markdown 渲染需要清洗 HTML。

### 15.3 服务稳定性

- 每个仓库同一时间只允许一个扫描任务。
- 扫描任务失败不清空旧索引。
- Git 命令设置超时和输出大小限制。
- 大文件不进入预览。
- 前端展示任务失败原因，方便运维处理。

## 16. 分阶段实施计划

### 16.1 阶段一：项目骨架与仓库同步

目标：

- 新建独立项目。
- 完成基础 HTTP 服务和前端骨架。
- 完成仓库配置 CRUD。
- 完成 Git clone/fetch。
- 完成分支列表读取。

验收：

- 可以新增一个仓库配置。
- 系统可以 clone 远程仓库。
- 前端可以看到仓库和分支列表。

### 16.2 阶段二：扫描与文档浏览

目标：

- 完成扫描目录配置。
- 完成多分支 HEAD 内容扫描。
- 完成 `documents`、`doc_versions`、`doc_latest` 索引写入。
- 完成智能最新目录树和文件列表。
- 完成指定分支目录树和文件列表。

验收：

- 配置指定目录后，可以在前端看到智能最新文档树。
- 切换分支后，可以看到该分支对应的文档树。
- 智能最新文件能展示来源分支和来源 commit。
- 扫描失败不会影响上一次成功结果。

### 16.3 阶段三：预览与下载

目标：

- 完成 Markdown 预览。
- 完成 Mermaid 图表渲染。
- 完成单文件下载。
- 完成相对图片和相对链接处理。

验收：

- Markdown 文档可以正常阅读。
- Mermaid 代码块可以渲染为图表。
- Mermaid 渲染失败时不影响 Markdown 正文展示。
- 文件可以单独下载。
- 非 Markdown 文件首期不预览但可下载。
- 超大 Markdown 文件不预览但可下载。

### 16.4 阶段四：Git 历史图

目标：

- 完成仓库提交图 API。
- 完成前端提交图渲染。
- 完成 commit 详情和文件变更列表。
- 完成文件历史入口。
- 完成删除、移动、重命名路径事件识别和展示。
- 完成文档在各分支的版本状态展示。
- 支持点击历史 commit 查看当时文件内容。

验收：

- 可以在前端看到指定分支的图形化提交历史。
- 可以点击 commit 查看变更文件。
- 可以从文档详情页查看该文档历史。
- 可以看到文档在不同分支上的 active/deleted/renamed/moved 状态。
- 可以看到移动和重命名的 old path/new path。

### 16.5 阶段五：收口与运维能力

目标：

- 增加扫描记录页面。
- 增加错误展示。
- 增加基础搜索。
- 增加磁盘占用和仓库状态展示。
- 补齐部署文档。

验收：

- 运维可以判断仓库 clone、fetch、扫描是否正常。
- 用户可以按名称或路径搜索文档。
- 系统可以通过 Pangolin 代理稳定访问。

## 17. 验收标准

首期整体验收标准：

1. 可以配置至少一个远程 Git 仓库。
2. 系统可以自动 clone 该仓库。
3. 系统可以扫描配置的一个或多个目录。
4. 前端可以查看智能最新文档树。
5. 前端可以切换指定分支查看文档树。
6. Markdown 文件可以预览。
7. Markdown 中的 Mermaid 代码块可以渲染为图表。
8. 文件可以单独下载。
9. 可以查看仓库图形化 Git 历史。
10. 可以查看 commit 详情和文件变更列表。
11. 可以从文档详情查看该文档历史和各分支版本状态。
12. 删除、移动、重命名在文档历史中可追踪。
13. 系统不要求登录，不维护用户表。
14. 访问控制通过 Pangolin 完成。

## 18. 待确认问题

1. 首期数据库是否接受 SQLite，还是需要直接使用 MySQL。
2. 私有仓库凭据首期优先支持 SSH deploy key，还是 HTTPS token。
3. 默认纳入智能最新的分支规则如何设置，是只包含默认分支和 release 分支，还是包含所有非 stale 分支。
4. stale 分支默认阈值是否使用 180 天。
5. Markdown 内部相对链接是否需要首期全部转换为系统内跳转。
6. 配置页是否需要由 Pangolin 单独配置更高权限的访问策略。
