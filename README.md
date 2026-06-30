# DocHarbor

DocHarbor 是一个独立的 Git 文档浏览服务。系统按仓库配置 clone bare mirror，扫描指定目录下的文档文件，提供智能最新视图、按分支浏览、Markdown/Mermaid 预览、单文件下载、Git 历史和扫描记录。

需求来源见 [DocHarbor 产品需求与设计文档](doc/DocHarbor产品需求与设计文档.md)。完整操作说明见 [DocHarbor 使用手册](doc/DocHarbor使用手册.md)。AI 问答扩展设计见 [DocHarbor AI 问答功能设计文档](doc/DocHarbor%20AI问答功能设计文档.md)，Agent 重构方案见 [DocHarbor AI Agent 架构重构设计文档](doc/DocHarbor%20AI%20Agent架构重构设计文档.md)。

## 功能范围

- 仓库配置 CRUD：仓库 URL、默认分支、追踪分支、智能最新规则、扫描目录和扫描周期。
- Git mirror 同步：首次 `git clone --mirror`，后续 `git remote update --prune`。
- 多分支扫描：按配置目录执行 `git ls-tree`，SQLite 只保存索引和元数据。
- 智能最新：按文档维度选择最新有效版本，展示来源分支、commit 和最近修改时间。
- 分支浏览：切换分支查看该分支 HEAD 的扫描结果。
- Markdown 预览：前端渲染 Markdown，支持 Mermaid，非 Markdown 首期只下载。
- 单文件下载：基于索引中的 blob sha 读取 Git 对象。
- Git 历史：仓库提交图、commit 详情、变更文件列表。
- 文件历史：展示分支状态和扫描识别的删除、移动、重命名事件。

## 本地运行

前置依赖：

- Go 1.24+
- Node.js 22+
- Git
- SQLite CGO 环境

启动后端：

```bash
go mod download
go run ./cmd/doc-harbor
```

启动前端开发服务：

```bash
npm install
npm run dev
```

前端默认代理 `/api` 到 `http://127.0.0.1:8080`。

也可以构建前端后由 Go 服务直接托管：

```bash
npm run build
WEB_DIR=./dist go run ./cmd/doc-harbor
```

## 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `DATA_DIR` | `./data` | 数据目录，存放 SQLite 和 bare mirror |
| `HTTP_ADDR` | `:8080` | HTTP 监听地址 |
| `DB_DSN` | `${DATA_DIR}/doc-harbor.db` | SQLite DSN |
| `GIT_BIN` | `git` | Git 命令 |
| `WEB_DIR` | `./web/dist` | 静态前端目录 |
| `DEFAULT_SCAN_INTERVAL` | `3600` | 默认扫描间隔秒数 |
| `MAX_PREVIEW_FILE_SIZE` | `2097152` | 默认 Markdown 预览大小限制 |
| `ALLOWED_GIT_HOSTS` | 空 | 逗号分隔 Git host 白名单，空表示不限制 |
| `ALLOW_LOCAL_GIT` | `0` | 是否允许本地路径或 `file://` 仓库 |
| `GITHUB_WEBHOOK_SECRET` | 空 | GitHub Webhook 共享 secret，空时 webhook 入口不可用 |

私有仓库凭据不写入数据库。数据库中只保存凭据引用字段，实际密钥通过容器挂载或主机环境提供。

AI provider API key 通过前端 AI 配置页录入。DocHarbor 会在 `DATA_DIR/secrets/ai-master.key` 自动生成本机加密主密钥，并随数据目录持久化；不需要额外配置环境变量。

通用访问 Token 使用前端页面签发。DocHarbor 会在 `DATA_DIR/secrets/access-token.key` 自动生成 HMAC 签名密钥；密钥丢失或轮换后，已签发但未过期的 Token 会失效。

SSH 仓库可以使用默认挂载的 `~/.ssh`，也可以把项目专用 deploy key 放到 `credentials/ssh/` 后自行在 `credentials/.gitconfig` 中配置 `core.sshCommand`。

HTTP(S) 仓库可以把凭据放在 `credentials/`：

`.netrc` 示例：

```text
machine git.example.com
login your-user
password your-token
```

`.git-credentials` 示例：

```text
https://your-user:your-token@git.example.com
```

可选 `.gitconfig` 示例：

```ini
[credential]
	helper = store --file /credentials/.git-credentials
```

`credentials/` 默认忽略真实凭据文件，只保留说明文件。

## GitHub Webhook

DocHarbor 提供固定前缀的 GitHub Webhook 入口：

```text
POST /api/webhooks/github/{repoID}
```

`{repoID}` 是 DocHarbor 仓库 ID，每个仓库使用独立 URL。例如：

```text
https://docs.example.com/api/webhooks/github/1
```

配置步骤：

1. 为服务设置共享 secret：

```bash
GITHUB_WEBHOOK_SECRET=your-random-secret docker compose up --build -d
```

1. 在 GitHub 仓库 `Settings -> Webhooks` 新增 webhook：
   - Payload URL：`https://<你的域名>/api/webhooks/github/<repoID>`
   - Content type：`application/json`
   - Secret：与 `GITHUB_WEBHOOK_SECRET` 一致
   - Events：选择 `Just the push event`

1. 如果服务放在 Pangolin 保护域名后，建议为 webhook 放行白名单路径：

```text
/api/webhooks/github/*
```

Webhook 会校验 `X-Hub-Signature-256` 的 HMAC-SHA256 签名。`ping` 事件用于 GitHub 测试；`push` 事件会异步触发扫描并立即返回 `202 Accepted`，扫描记录的触发类型为 `github_webhook`；其他事件会返回 ignored，不触发扫描。

仓库页面的 GitHub Webhook 行提供“显示 Secret”按钮，会明文显示当前部署环境中的共享 `GITHUB_WEBHOOK_SECRET`，用于复制到 GitHub Webhook 配置。DocHarbor 本身不维护用户权限，这个入口需要继续依赖 Pangolin 或其他外层访问控制保护。

本地可以用下面的方式构造签名验证：

```bash
body='{"zen":"Keep it logically awesome."}'
secret='your-random-secret'
sig="sha256=$(printf '%s' "$body" | openssl dgst -sha256 -hmac "$secret" -hex | awk '{print $2}')"
curl -i \
  -H "X-GitHub-Event: ping" \
  -H "X-Hub-Signature-256: $sig" \
  -H "Content-Type: application/json" \
  --data "$body" \
  http://127.0.0.1:14220/api/webhooks/github/1
```

## 通用访问 Token

管理员可以在系统设置页的“访问 Token”区域签发临时 Token。默认有效期为 3600 秒，允许范围为 300 到 86400 秒；Token 使用 `DATA_DIR/secrets/access-token.key` 中的 HMAC-SHA256 密钥签名，密钥丢失或轮换后已签发 Token 会失效。

当前支持签发 `ai.history.read` 和 `ai.diagnostics.read` 能力，分别用于远程只读访问 AI 对话历史和 AI 问答排查数据。`scope.viewer_key` 留空时不限制 viewer，填写后服务端会强制按该 `viewer_key` 收窄支持该 scope 的接口。

签发接口：

```text
POST /api/tokens
```

请求示例：

```json
{
  "ttl_seconds": 3600,
  "capabilities": ["ai.history.read", "ai.diagnostics.read"],
  "scope": {
    "viewer_key": ""
  }
}
```

响应包含 `token`、`expires_at`、`capabilities` 和 `scope`，其中 `token` 只显示一次。需要临时 Token 鉴权的远程接口统一放在 `/api/access/*` 前缀下，方便网关按前缀放行。远程服务使用带 `ai.history.read` 能力的 Token 调用只读历史 API：

```bash
curl -H "Authorization: Bearer $ACCESS_TOKEN" \
  "http://127.0.0.1:14220/api/access/ai/history/sessions?archived=all&limit=50"
```

列表接口支持分页和过滤：

```text
GET /api/access/ai/history/sessions?limit=50&cursor=...&q=...&archived=0|1|all&updated_after=2026-06-29T00:00:00Z&updated_before=2026-06-30T00:00:00Z
```

详情接口返回单个会话、消息、候选服务和引用：

```bash
curl -H "Authorization: Bearer $ACCESS_TOKEN" \
  "http://127.0.0.1:14220/api/access/ai/history/sessions/1"
```

如果 Token 带 `scope.viewer_key` 范围，服务端会强制按该 `viewer_key` 收窄列表和详情；不属于该范围的会话按 `404` 处理。Token 无效或过期返回 `401`，Token 有效但缺少 `ai.history.read` 能力返回 `403`。

AI 问答排查接口使用独立的 `ai.diagnostics.read` 能力，用于远程查看 run 级链路数据。列表接口支持分页和过滤：

```text
GET /api/access/ai/diagnostics/runs?limit=50&cursor=...&session_id=1&status=failed&q=...&started_after=2026-06-29T00:00:00Z&started_before=2026-06-30T00:00:00Z
```

数据源接口返回当前 AI 可访问的启用仓库、启用扫描路径、默认分支/候选分支、最新扫描摘要和索引限制摘要：

```bash
curl -H "Authorization: Bearer $ACCESS_TOKEN" \
  "http://127.0.0.1:14220/api/access/ai/diagnostics/data-sources"
```

详情接口返回单次问答的会话、用户消息、助手消息、run、脱敏 steps、本次 scope 解析后的数据源、候选服务和引用：

```bash
curl -H "Authorization: Bearer $ACCESS_TOKEN" \
  "http://127.0.0.1:14220/api/access/ai/diagnostics/runs/1"
```

排查接口不会返回 step 的 `input_json`、`output_json`、仓库 `repo_url` / `credential_ref`、provider secret、API key、AI secret id 或 access-token 签名密钥。Token 有效但缺少 `ai.diagnostics.read` 能力时返回 `403`。

## Docker

```bash
docker compose up --build
```

服务启动后访问 `http://127.0.0.1:14220`。生产环境建议放在 Pangolin 之后，由 Pangolin 负责登录、访问控制和配置页访问策略。

DocHarbor 容器会通过 `tini` / Compose `init: true` 回收 Git 命令产生的孤儿子进程。生产部署不要移除镜像入口中的 init 进程；如果平台覆盖了 `ENTRYPOINT`，需要提供等价的 init/reaper。已经存在的 `git <defunct>` 僵尸进程不能单独 kill 清理，需要重启当前容器。

## 安全边界

- API 不维护用户和权限，鉴权交给 Pangolin。
- 访问 Token 由已受保护的系统设置页签发；签发 API `/api/tokens` 也需要继续依赖 Pangolin 或其他外层访问控制保护，网关只应放行远程 Token 鉴权前缀 `/api/access/*`。
- Git 命令均通过参数数组调用。
- 文件下载只通过已索引版本或仓库内规范化路径读取。
- 默认禁止本地 Git URL，避免任意本机路径 clone。
- Markdown HTML 在前端 sanitize 后渲染，Mermaid 渲染失败会降级为原始代码块。

## 当前限制

- 首期不做目录 zip 下载、在线编辑、评论、审批或全文搜索引擎。
- 智能最新的文档身份首期主要使用仓库内规范化路径；rename/move 事件已入库，用于后续更强归并。
- 超大文件可下载，Markdown 超过预览限制时不预览。
