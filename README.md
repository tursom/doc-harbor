# DocHarbor

DocHarbor 是一个独立的 Git 文档浏览服务。系统按仓库配置 clone bare mirror，扫描指定目录下的文档文件，提供智能最新视图、按分支浏览、Markdown/Mermaid 预览、单文件下载、提交历史和扫描记录。

需求来源见 [DocHarbor 产品需求与设计文档](doc/DocHarbor产品需求与设计文档.md)。完整操作说明见 [DocHarbor 使用手册](doc/DocHarbor使用手册.md)。AI 问答扩展设计见 [DocHarbor AI 问答功能设计文档](doc/DocHarbor%20AI问答功能设计文档.md)，Agent 重构方案见 [DocHarbor AI Agent 架构重构设计文档](doc/DocHarbor%20AI%20Agent架构重构设计文档.md)。

## 功能范围

- 仓库配置 CRUD：仓库 URL、默认分支、追踪分支、智能最新规则、扫描目录和扫描周期。
- Git mirror 同步：首次 `git clone --mirror`，后续 `git remote update --prune`。
- 多分支扫描：按配置目录执行 `git ls-tree`，SQLite 只保存索引和元数据。
- 智能最新：按文档维度选择最新有效版本，展示来源分支、commit 和最近修改时间。
- 分支浏览：切换分支查看该分支 HEAD 的扫描结果。
- 文档预览：前端渲染 Markdown，支持 Mermaid；HTML 支持嵌入和独立窗口预览，其他类型可下载。
- 单文件下载：基于索引中的 blob sha 读取 Git 对象。
- 提交历史：仓库提交图、commit 详情、变更文件列表。
- 文件历史：展示分支状态和扫描识别的删除、移动、重命名事件。

## 本地运行

前置依赖：

- Go 1.25+（Docker 构建使用 1.26）
- Node.js >=22.19.0（运行 pi 必需；运行镜像使用 Node 24）
- pi `@earendil-works/pi-coding-agent@1.0.0`（使用 pi 引擎时，`npm install -g --omit=dev @earendil-works/pi-coding-agent@1.0.0`）
- Git
- SQLite CGO 环境

启动后端：

```bash
go mod download
go run ./cmd/doc-harbor
```

启动 Astro 静态多页面前端开发服务：

```bash
npm install
npm run dev
```

前端默认代理 `/api` 到 `http://127.0.0.1:8080`。生产构建会生成八个独立的主页面：

| 页面 | 路径 | 入口 |
| --- | --- | --- |
| 文档 | `/` | 仓库页签 |
| 提交历史 | `/history/` | 仓库页签 |
| 扫描记录 | `/scans/` | 仓库页签 |
| 仓库设置 | `/repo-settings/` | 仓库页签 |
| AI 问答 | `/ai/` | 侧栏顶部 |
| AI 供应商 | `/ai/config/` | 侧栏「管理」 |
| AI 诊断 | `/ai/diagnostics/` | 侧栏「管理」 |
| 系统设置 | `/settings/` | 侧栏「管理」 |

`/html-preview/` 是额外的独立 HTML 预览页，不加载主应用。窄屏通过顶部「菜单」打开侧栏；侧栏底部可选择跟随系统、浅色或深色主题。旧 `?tab=` 链接继续重定向，并保留对应页面的查询参数。

仓库编辑从原来的「配置」弹窗迁到「仓库设置」页，修改后在底部保存栏保存或放弃；新增仓库仍通过侧栏仓库分组的「+」打开弹窗。Webhook Payload URL 位于「仓库设置 → GitHub Webhook」，共享 Secret 的显示、复制和轮换迁到「系统设置 → GitHub Webhook」。原「Git 历史」改称「提交历史」，原「AI 配置」改称「AI 供应商」，路径不变。

页面框架在构建阶段生成 HTML，运行时数据继续通过同源 `/api` 从 Go 服务加载。每个 Astro 页面挂载自己的 `src/entries/*Page.vue` 入口岛，由入口在 Vue 内组合 `AppShell` 与视图 slots；不要从 Astro 模板把 Vue 视图作为 island 的 slot 传入，否则子视图不会水合。Markdown、DOMPurify、Mermaid 和 HTML 预览实现按需加载。

也可以构建前端后由 Go 服务直接托管：

```bash
npm run build
WEB_DIR=./dist go run ./cmd/doc-harbor
```

前端与静态页面验证：

```bash
npm test
npm run build
npm run check:bundle
npm run test:e2e
```

`check:bundle` 会检查每个静态页面的首屏 JavaScript gzip 预算。Playwright 测试使用 Go 静态托管器验证目录页面、真实多页导航、旧链接兼容和依赖延迟加载。

重设计测试位于 `e2e/redesign.spec.ts`，使用 `e2e/fixtures/redesign.ts` 中的虚构数据拦截 API，覆盖保存、弹窗焦点、Secret 轮换确认、AI 输入法、深链接和八页窄屏布局。构建完成后可单独运行：

```bash
npx playwright test e2e/static-mpa.spec.ts e2e/redesign.spec.ts
DOCHARBOR_SCREENSHOTS=1 npx playwright test e2e/redesign.spec.ts --grep 'redesign screenshots' --output=test-results/redesign
```

第二条命令生成八页 × 1440/390 宽度 × 浅色/深色共 32 张截图，包含选中文档、提交详情、展开的扫描错误、AI 对话与诊断、已签发的虚构 Token 等内容。图片位于已忽略的 `test-results/redesign/` 各用例子目录，供人工检查错位、重叠和截断；生成截图不等于已通过视觉验收。不要复制或提交含真实内部数据的 `doc/ui-redesign/` 原型。使用手册中的旧版截图尚未随重设计更新，以新版文字和页面为准。

## 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `DATA_DIR` | `./data` | 数据目录，存放 SQLite 和 bare mirror |
| `HTTP_ADDR` | `:8080` | HTTP 监听地址 |
| `ALLOWED_CLIENT_CIDRS` | 空 | HTTP 来源网段白名单，逗号分隔 IPv4/IPv6 CIDR；空表示不限制，非法配置阻止启动。按 TCP 对端匹配，所有页面/API/Webhook 均生效，不信任转发头 |
| `DB_DSN` | `${DATA_DIR}/doc-harbor.db` | SQLite DSN |
| `GIT_BIN` | `git` | Git 命令 |
| `WEB_DIR` | `./web/dist` | 静态前端目录 |
| `DEFAULT_SCAN_INTERVAL` | `3600` | 默认扫描间隔秒数 |
| `MAX_PREVIEW_FILE_SIZE` | `2097152` | 默认 Markdown 预览大小限制 |
| `ALLOWED_GIT_HOSTS` | 空 | 逗号分隔 Git host 白名单，空表示不限制 |
| `ALLOW_LOCAL_GIT` | `0` | 是否允许本地路径或 `file://` 仓库 |
| `GITHUB_WEBHOOK_SECRET` | 空 | GitHub Webhook 共享 secret，空时 webhook 入口不可用 |
| `AI_ENGINE` | `legacy` | 新会话引擎，`legacy` / `pi`。前端按会话 engine 选择界面；默认切换与旧链路删除仍按质量验收和稳定期条件执行 |
| `PI_BIN` | `pi` | pi 可执行文件；离线后端测试可指向假进程 |
| `AI_PI_POOL_SIZE` | `3` | pi 最大进程数，按需启动；每个 run 独占进程，超出后排队 |
| `AI_PI_MAX_RUNS_PER_PROCESS` | `100` | 每进程运行次数上限，空闲时回收 |
| `AI_RUN_TIMEOUT_SECONDS` | `300` | 单次 pi run 超时秒数，超时发送 abort |
| `AI_MAX_TOOL_CALLS` | `40` | 单次 pi run 工具调用上限 |
| `MCP_ENABLED` | `true` | 是否开放外部 `/api/mcp`；关闭时外部访问返回 404，内部 pi 令牌仍可使用 |

私有仓库凭据不写入数据库。数据库中只保存凭据引用字段，实际密钥通过容器挂载或主机环境提供。

AI provider API key 通过前端 AI 供应商页的新增或编辑抽屉录入。DocHarbor 会在 `DATA_DIR/secrets/ai-master.key` 自动生成本机加密主密钥，并随数据目录持久化；不需要额外配置环境变量。

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

在「仓库设置 → GitHub Webhook」复制当前仓库的 Payload URL；在「系统设置 → GitHub Webhook」查看或复制共享 Secret，默认遮罩显示。未配置时可生成 Secret，已有 Secret 的轮换需要页内二次确认。数据库中保存的 Secret 优先于 `GITHUB_WEBHOOK_SECRET`；轮换后立即生效，需要同步更新所有 GitHub 仓库的配置。DocHarbor 本身不维护用户权限，这个入口需要继续依赖 Pangolin 或其他外层访问控制保护。

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

管理员可以在系统设置页的“访问令牌”区域签发临时 Token。有效期可选 1 小时、8 小时、24 小时或自定义，自定义通常为 300 到 86400 秒；仅含 `docs.read` 的令牌上限为 90 天；Token 使用 `DATA_DIR/secrets/access-token.key` 中的 HMAC-SHA256 密钥签名，密钥丢失或轮换后已签发 Token 会失效。

当前支持签发 `ai.history.read`、`ai.diagnostics.read` 和 `docs.read` 能力，分别用于远程只读访问 AI 对话历史、AI 问答排查数据和 MCP 文档/代码工具。系统设置中单独选择 `docs.read` 可签发 7/30/90 天令牌（最长 `ttl_seconds=7776000`）；混合其他能力仍最长 24 小时。混合能力与已选长期限冲突时，页面提示并阻止签发，不会静默缩短期限。`scope.viewer_key` 留空时不限制 viewer，填写后服务端按该值收窄 AI 历史及诊断；MCP 仓库读取不使用 viewer scope。

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

运行镜像包含 Node 24 和锁定的 pi 1.0.0。`pi-runtime` 阶段单独安装并执行无模型 `get_state` 检查，可以独立验证 Alpine/musl；不会构建前端、发送 prompt 或挂载本机配置：

2026-10-21 本地构建与断网容器复验通过，Node 24.22.0、pi 1.0.0，按实施规格保留 Alpine；完整记录见 [步骤 2 周边交付验证](doc/AI步骤2周边交付验证.md)。

```bash
docker build --target pi-runtime -t docharbor-pi-runtime:local .
docker run --rm --network none --entrypoint node \
  docharbor-pi-runtime:local /usr/local/lib/docharbor/pi-rpc-smoke.mjs
```

脚本要求 Node >=22.19.0、pi 恰为 1.0.0，使用临时 `HOME` 和 `PI_CODING_AGENT_DIR`、环境白名单及 `PI_OFFLINE=1`；只发送一次 `get_state`，检查成功响应后再关闭 stdin。宿主机安装 pi 后也可执行 `node scripts/pi-rpc-smoke.mjs`。它仅验证 RPC 启动与协议，不代表 MCP 工具循环或模型回答质量已验收。若 Alpine 因原生模块/musl 无法通过，按实施文档 §11 将运行阶段改为 `node:24-bookworm-slim`，Go 阶段同步改为 `golang:1.26-bookworm`，安装 glibc 环境下的 CGO/SQLite 构建依赖和 git、openssh-client、tini、ca-certificates、tzdata；更换基础镜像后须重跑同一检查。

## MCP 接入

`/api/mcp` 使用 streamable HTTP，提供启用仓库中文档和代码的只读工具：`list_repos`、`list_branches`、`list_docs`、`search_docs`、`search_code`、`read`、`list_dir`。`git_log`、`diff_files` 属于后续 P1。先通过受保护的 `POST /api/tokens` 签发只包含 `docs.read` 的令牌，最长 90 天；将返回的 token 存入客户端环境变量 `DOCHARBOR_TOKEN`，不要把实际值写进配置示例或版本库。

签发请求体示例（仅展示请求，不展示返回的 token）：

```json
{"ttl_seconds":2592000,"capabilities":["docs.read"],"scope":{"viewer_key":""}}
```

pi 1.0.0：

```bash
pi mcp add docharbor --url https://docs.example.com/api/mcp \
  --bearer-token-env-var DOCHARBOR_TOKEN --exposure direct
pi mcp list
```

也可合并到用户 `mcp.json`（默认 `~/.pi/agent/mcp.json`，以实际 `PI_CODING_AGENT_DIR` 为准）：

```json
{
  "mcpServers": {
    "docharbor": {
      "url": "https://docs.example.com/api/mcp",
      "headers": {"Authorization":"Bearer ${DOCHARBOR_TOKEN}"},
      "exposure": "direct"
    }
  },
  "autoEnableCodemode": false
}
```

在 `/mcp` 中核对暴露方式为 `direct`。`--no-tools` 下默认 codemode 暴露无法使用，不要加 `--no-extensions`，否则内置 MCP 也被禁用。若存在代理，本机 MCP 地址必须加入 `NO_PROXY` 和 `no_proxy`（`127.0.0.1,localhost`，保留原值）。服务端内部 pi 配置位于 `$DATA_DIR/ai/pi-agent/`，由后端管理，不要编辑个人 pi 配置来替代服务端配置；内部 MCP 令牌和供应商 Key 由后端通过子进程环境传入。

Codex 的 `~/.codex/config.toml` 可使用环境变量令牌引用：

```toml
[mcp_servers.docharbor]
url = "https://docs.example.com/api/mcp"
bearer_token_env_var = "DOCHARBOR_TOKEN"
```

Claude Code 支持同类 HTTP 配置，可合并到对应 MCP 配置文件，保持 token 为环境变量占位符：

```json
{"mcpServers":{"docharbor":{"type":"http","url":"https://docs.example.com/api/mcp","headers":{"Authorization":"Bearer ${DOCHARBOR_TOKEN}"}}}}
```

上述 pi 参数按 1.0.0 随包 CLI/MCP 文档核对；其他客户端按当前安装版本检查环境变量展开能力。无令牌返回 401、缺少 `docs.read` 返回 403，来源网段仍受 `ALLOWED_CLIENT_CIDRS` 限制。网关需要另行放行 `/api/mcp`，不要开放管理用的 `/api/tokens`。

## AI 问答使用

前端按会话 `engine` 展示。pi 会话支持开发/产品/测试角色、范围提示、工具过程和可核对的引用；运行中按 Enter 发送补充说明，点击停止按钮终止运行。离开页面会断开接收，后台已开始的问题继续执行，回到会话后自动读取保存的结果。回答下方可复制、反馈和打开指定 run 的诊断记录；诊断令牌默认不保存，可选择只在当前标签页会话内记住。

右栏按文件合并本轮引用，另有用量标签。未核实的行范围会显示提示，点击引用在新标签页阅读原文。旧会话带「旧版」标签，服务配置为 `pi` 后只读；默认 `legacy` 的过渡环境仍可在旧会话提问。AI 未启用时暂停新提问，可查看历史并前往供应商页配置。

2026-10-21 用户明确决定「跳过评估阶段，实现前端」，据此推进步骤 3。默认供应商此前返回 401，真实模型回答质量仍未验收；本次决定不等于切换默认引擎、部署或删除旧链路。

前端交付、自动化验证和桌面/窄屏截图见 [AI 前端重构验收记录](doc/AI前端重构验收记录.md)。记录包含停止、补充说明、刷新恢复、反馈和诊断的验证范围。

## AI 评估与人工评分

实施规格见 [AI 问答重构实施文档](doc/DocHarbor%20AI问答重构实施文档.md)。`cmd/ai-eval` 通过 HTTP 逐题新建会话，保存模型最终回答、SSE 过程与 API 用量；不自动判断答案正确率。

题集 [questions.jsonl](testdata/ai-eval/questions.jsonl) 包含开发、测试、产品各 10 题。现有 `internal/app/ai_agent_eval_test.go` 只有 6 个核心原题，使用 widget_metric、widget orders 和 QuantumDelivery 的虚构 Git fixture；本题集保留原题，并从相同 fixture 扩写角色问题，`source` 明确标注原题/改写。它不是 30 道生产真实问题，重复场景也不是独立业务覆盖。没有这些 fixture 的实例不能据此测业务准确率：先准备等价的已扫描测试仓库，或由维护者补入真实导出问题并重写对应 `expect`，再对两引擎使用同一份题集及仓库快照。

业务示例题集 [questions-real.jsonl](testdata/ai-eval/questions-real.jsonl) 根据实施规格的 SGPS、通用商户、token、秒杀库存、CDK、礼物订单等场景整理，开发/测试/产品各 10 题，包含追问和判定要点。这是规格示例与其扩展，尚未包含生产导出的真实问答；用于当前已扫描业务仓库的评估时，将下面命令的 `-file` 替换为此文件。当前阶段的测试、MCP 连通验证和真实调用阻塞见 [阶段验收记录](doc/AI问答重构阶段验收记录.md)。

JSONL 每行必填 `{id, role, question, expect}`；可选 `source`、`scope` 和 `context`（前置用户问题数组，顺序发送到同一会话）。`id` 仅含英文字母、数字、`-`、`_` 且唯一，`role` 为开发/产品/测试。`expect` 是人工判定要点，不是正则或自动判分程序。追问“参数是什么？”的前置提问会实际调用 API，并单独保存过程与用量。

以下命令会产生真实模型调用，由维护者在准备好模型与仓库后执行。`-engine` 选择客户端协议并核对返回的会话引擎；新建会话的引擎仍由服务端 `AI_ENGINE` 决定。对比时指向两个分别配置为 legacy/pi 的本地实例（使用不同监听端口及数据目录），或由维护者明确配置后依次运行；工具不会修改默认引擎，匹配失败会在提问前退出：

```bash
# 指向 AI_ENGINE=legacy 的本地实例；原版响应没有 engine 时按 legacy 处理。
go run ./cmd/ai-eval -engine legacy -file testdata/ai-eval/questions.jsonl \
  -url http://127.0.0.1:14220

# 指向维护者准备的 AI_ENGINE=pi 评估实例。
go run ./cmd/ai-eval -engine pi -file testdata/ai-eval/questions.jsonl \
  -url http://127.0.0.1:14221
```

默认 `-timeout 6m`（每轮，含排队）、`-out out/ai-eval`。通过 `-scope '{"repo_mode":"selected","repo_ids":[1],"source_mode":"smart_latest_with_branch_candidates"}'` 可以覆盖全部题目的范围；ID 必须来自当前实例，两个数据库中的仓库 ID 可能不同。若 metric_value/metric_cents 的 fixture 分属不同仓库，应在自用题集逐题填写 `scope`，避免把两个版本混在一起。

输出在 `out/ai-eval/<时间>-<引擎>-<唯一后缀>/`，包含固化输入 `questions.json`、汇总 `results.json`、人工评分表 `scores.md`，以及逐题目录的 `answer.md`、`result.json` 和 `events.jsonl`。有前置轮时另存 `context-01.jsonl` 等文件，包含该轮最终回答与用量。pi SSE 工具预览受服务端 2000 字符限制；需完整工具结果时另用带 `ai.diagnostics.read` 的诊断 API。legacy 使用原有 `/messages/stream`，以 `message_done.message.content` 为准覆盖草稿，保留最终回答的 `prompt_tokens/completion_tokens`；它不包含整个旧规划链路的总成本。pi 使用 `/runs`，保留原始 `usage`、引用及工具事件，并额外保存 `transcript.json`，用当前 run 的持久化最终答案覆盖工具循环中的中间说明或重试草稿。API 没返回用量时写 `null`，不伪造零费用。

采集失败、流中断、超时或非成功 run 仍保留已有材料，批次继续并最终返回非零。Ctrl+C 后保留已采集题目；SSE 断开不会自动停止已开始的 pi run，应根据 `session_id` 检查历史或调用 abort。`scores.md` 每题留正确性（1/0.5/0）、引用核实、角色适配和备注栏，按 ID 对照两次产物，并核对范围及前置轮一致；耗时 P90 使用 nearest-rank，仅统计采集成功题的主轮请求（含排队，不含会话创建、前置轮与 transcript 获取）；`result.json` 的 `run_elapsed_ms` 是该主轮耗时，`elapsed_ms` 是整题总耗时。失败题须单独评审。实施规格原定的前端检查点已按用户明确决定跳过；默认引擎切换与旧链路删除仍按步骤 4/5 的质量验收和稳定期条件执行。

离线验证命令（仅 HTTP/SSE mock，不调用模型）：

```bash
go test ./cmd/ai-eval
```

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
