# DocHarbor AI 问答重构实施文档

整理日期：2026-10-21

本文是 AI 问答重构的实施规格，面向在本仓库内直接改代码的实施者。现有的 AI 问答是 DocHarbor 自建的固定检索管线，本次改为“DocHarbor 提供只读 MCP 工具 + 嵌入 pi 编码代理做问答”。

与 [DocHarbor 页面重设计实施文档](DocHarbor页面重设计实施文档.md) 的关系：那份文档先完成，其中 §5.6–§5.8 的 AI 页面是旧引擎过渡期的界面。本文步骤 3 在它的基础上加入 pi 引擎的界面，前端按会话的 `engine` 字段选择渲染方式。旧链路删除时（步骤 5），再移除旧界面组件。

## 0. 背景与已定决策

### 0.1 为什么重构

现有链路是：问题 → 关键词拆分 → `git grep` 计数打分 → 每个文件截一段不超过 700 字的片段 → 模型一次性作答。真实问答记录里有这些问题：

- 中文被切成单字或二字词（“你好”命中“你”“好”；候选理由里出现“户服”“是什”）。同义词和别称搜不到：用户问“通用商户 API”，回答“未找到”，但这套 API 是存在的。
- 模型只能看到片段，不能自己打开文件或顺着调用链读。回答里大量出现“证据未展示具体字段”。
- 追问靠意图分类和拼接，容易答非所问。
- 任务框架、证据契约、意图打分等规则不断叠加，`internal/app/ai*.go` 已经有 1.85 万行（含测试），校验“通过”也不代表答对。

### 0.2 已定决策

| 决策 | 结论 |
| --- | --- |
| 问答引擎 | 嵌入 pi（`@earendil-works/pi-coding-agent`，锁定 1.0.0），以 RPC 子进程方式运行，不在 Go 里自己写 agent 循环 |
| 工具层 | DocHarbor 用 Go 实现只读 MCP server。pi 只连这一个 MCP；开发者自己的 pi、pi-web、Codex、Claude Code 也可以接这个 MCP |
| 进程模型 | 复用进程，用进程池加 `switch_session` / `new_session` 切换会话 |
| 用户 | 开发、产品、测试三类角色。典型场景是前端对接接口、测试排错、产品梳理流程和规则 |
| 旧链路 | 先放在开关后面并存，评估达标后删除 |

## 1. 目标与非目标

### 目标

1. 回答准确率明显高于旧链路（见第 14 节评估方法）。
2. 用户能看到助手“搜了什么、读了什么”，能中途停止，也能补充说明。
3. 每个关键结论都带可点击、可核实的引用。
4. 同一套 MCP 工具同时服务网页问答和开发者自己的 agent 客户端。
5. 删除旧的检索管线、任务框架、证据契约、校验器等代码。

### 非目标

- 不让模型执行 shell、写文件、访问数据库或外网。pi 的内置工具全部关闭。
- 不做向量索引。检索靠模型多次调用 `git grep` 和读取文件完成。
- 不迁移旧会话内容。旧会话只读展示。
- 不做多供应商自动切换（failover）。见第 10 节。

## 2. 总体架构

```text
浏览器 ──SSE──> DocHarbor (Go)
                 ├─ AI 会话 API（/api/ai/...）
                 ├─ pi 进程池 ──stdin/stdout JSONL──> pi --mode rpc（N 个子进程）
                 │                                     │
                 │                                     └─HTTP──> /api/mcp（同一个 DocHarbor 进程）
                 ├─ MCP server（/api/mcp，只读工具）<──HTTP── 开发者自己的 pi / Codex / Claude Code
                 └─ bare mirrors + SQLite（现有）
```

- pi 只做 agent 循环：调用模型、执行 MCP 工具、记录会话。
- DocHarbor 负责工具、权限、会话元数据、运行记录、引用核实和前端。
- pi 访问 MCP 走本机回环地址，使用进程内的内部令牌。外部客户端使用访问令牌（`docs.read` 能力）。

## 3. 已验证的事实

以下结论在 2026-10-21 用本机 pi 1.0.0 实测过，实施时直接按此做：

| 事项 | 结论 |
| --- | --- |
| 启动参数 | `pi --mode rpc --no-tools --no-context-files --no-skills --session-dir <dir> --provider <p> --model <m>` 可以正常启动 |
| 启动开销 | 冷启动约 0.2–0.3 秒，常驻内存约 110–126 MB/进程；`new_session` 约 10 ms |
| `--no-extensions` | **不能用**。它会把内置的 MCP 扩展也关掉。改用隔离配置目录 `PI_CODING_AGENT_DIR` 来避免加载个人扩展 |
| MCP 暴露方式 | 默认的 `codemode` 暴露在 `--no-tools` 下无法调用（pi 会提示“neither is active; they cannot be called”）。**必须设置 `"exposure": "direct"`** 和 `"autoEnableCodemode": false` |
| 代理 | 宿主机或容器里有 `HTTP_PROXY` / `HTTPS_PROXY` 时，pi 连本机 MCP 会报 `fetch failed`。启动 pi 时必须设置 `NO_PROXY` / `no_proxy`，包含 `127.0.0.1,localhost` |
| 令牌传递 | `mcp.json` 的 headers 支持 `${ENV}` 插值，`Authorization: Bearer ${DH_MCP_TOKEN}` 可用；`models.json` 的 `apiKey` 支持 `$ENV`。密钥都通过环境变量传入，不写到磁盘 |
| 切换会话 | 每次 `new_session` 或 `switch_session` 都会重新连一次 MCP（initialize + tools/list），本机回环开销可以忽略 |
| 会话文件 | 只在有消息后才写入 JSONL 文件；格式见 pi 的 `docs/session-format.md`（树状 entry，带 `id` / `parentId`） |
| 原生模块 | pi 依赖的 `pi-tui` 带 glibc 预编译的 `.node` 文件（X11 剪贴板）。RPC 模式预计不会加载它，但需要在 Alpine 镜像中实测（见第 11 节） |
| Node 版本 | 要求 `>=22.19.0` |
| Go MCP SDK | `github.com/modelcontextprotocol/go-sdk` v1.8.0 可用，支持 streamable HTTP |

尚未验证：pi 配合 DeepSeek（或 yym 网关模型）走工具循环时的回答质量和耗时。这是第 15 节步骤 2 结束时的检查点。

## 4. MCP server

### 4.1 接口与鉴权

- 路径：`/api/mcp`，传输方式为 streamable HTTP。用官方 `github.com/modelcontextprotocol/go-sdk` 实现，代码放在 `internal/app/mcp*.go`，或者新建 `internal/mcp` 包。
- 鉴权：`Authorization: Bearer <token>`，接受两种令牌：
  1. 现有访问令牌（`internal/app/access_token.go`），需要新增能力 `docs.read`。只含 `docs.read` 的令牌最长有效期放宽到 90 天，其他能力仍是 24 小时。
  2. 内部令牌：DocHarbor 启动时随机生成 32 字节，只存在内存里，通过环境变量交给 pi 子进程，用常量时间比较。
- `ALLOWED_CLIENT_CIDRS` 照常生效。
- `initialize` 返回的 `instructions`，pi 会放进系统提示词：

  ```text
  DocHarbor 只读访问公司 Git 仓库中的文档和代码。先用 list_repos / list_docs / search_docs 定位，再用 read 读原文。ref 默认取智能最新（文档）或默认分支（代码）。
  ```

### 4.2 通用约定

- **仓库标识**：参数 `repo` 接受仓库 slug、名称或数字 ID；输出统一用 slug。
- **ref 参数**：
  - 省略或 `latest`：文档按智能最新（`doc_latest`）解析到具体分支，非文档按默认分支 HEAD 解析。
  - 也可以传分支名或 commit SHA。
  - 输出中一律写**解析后的分支名**；按 commit 访问时写短 SHA。
- **引用键**：每条结果都带引用键 `slug@ref:path#L起-止`，例如 `游戏服务后端@main:core/handle/order.go#L40-88`。模型按这个格式引用，后端按这个格式核实。
- **输出格式**：MCP `content` 是给模型读的纯文本。**第一行固定是摘要**，例如 `找到 12 处匹配，分布在 3 个文件`、`读取第 1-200 行，共 412 行`。前端的过程卡片显示这一行。
- **截断**：结果被截断时，在末尾说明怎么继续，例如 `还有 212 行，用 start_line=201 继续`、`结果已截断到 60 条，请缩小 path_glob`。
- **安全**：
  - 所有工具只读，只访问 `enabled` 的仓库。
  - 拒绝含 `..` 或以 `/` 开头的路径。
  - 跳过二进制文件和超过 `max_file_size_bytes` 的文件，复用现有的 `aiShouldSkipPath` 和 `aiLooksText`。
  - 每次调用 10 秒超时。
- **错误**：参数错误或找不到时返回 `isError: true`，文本写明原因和可选值，例如“仓库不存在。可用：游戏服务后端、文档服务、用户服务”。

### 4.3 工具清单

P0 必须实现，P1 在 P0 稳定后实现。

| 工具 | 优先级 | 参数 | 输出 | 实现 |
| --- | --- | --- | --- | --- |
| `list_repos` | P0 | 无 | 每行一个仓库：slug、名称、默认分支、扫描目录、追踪分支数、最近扫描时间和状态 | 现有 repo 查询 |
| `list_branches` | P0 | `repo`, `limit=50` | 按 HEAD 时间倒序，每行：分支名、短 SHA、HEAD 时间、标记（默认 / 参与智能最新 / 功能分支候选） | 现有 `repo_refs` 数据和 `aiBranchCandidate` |
| `list_docs` | P0 | `repo?`, `query?`, `limit=100` | 智能最新文档索引，每行：引用键（不带行号）、标题、最后修改时间。`query` 按空格拆词，对标题和路径做不区分大小写的 AND 子串匹配 | `doc_latest` + `doc_versions` |
| `search_docs` | P0 | `query`, `repos?`, `regex=false`, `limit=40` | 在智能最新文档中搜索。按文件分组，组头为 `== 引用键 （标题）`，每条匹配 `L123: 内容`，带前后各 1 行上下文（`L122- …`）；单行最多 300 字符 | 沿用 `searchRepoSmartLatestEvidence` 中按 commit 分组 `git grep` 的做法 |
| `search_code` | P0 | `query`, `repos?`, `ref?`, `path_glob?`, `regex=false`, `limit=60` | 在指定 ref 的整棵树中搜索，格式同上。`repos` 省略时搜全部仓库的默认分支 | `git grep -n -I --heading -C1`；默认 `-F -i`，`regex=true` 时用 `-E` |
| `read` | P0 | `repo`, `path`, `ref?`, `start_line=1`, `end_line?` | 第一行摘要，第二行 `== 引用键`，后面是带行号的内容（`   40\t内容`）。单次最多 400 行或 40 KB | `git show <commit>:<path>` |
| `list_dir` | P0 | `repo`, `path="."`, `ref?` | 每行：类型（dir/file）、路径、大小；最多 300 条 | `git ls-tree` |
| `git_log` | P1 | `repo`, `ref?`, `path?`, `limit=20` | 每行：短 SHA、时间、作者、标题 | `git log` |
| `diff_files` | P1 | `repo`, `base`, `head`, `path_glob?` | `git diff --name-status base...head`，最多 200 条；第一行写清 base 和 head 解析后的分支和 SHA | `git diff` |

`read` 的 `ref` 省略时：如果 `path` 在智能最新文档里，取那个版本所在的分支；否则取默认分支。

### 4.4 工具描述

工具的 `description` 直接影响模型怎么用这个工具，按下面的意思写成中文：

- `list_docs`：“列出仓库的设计文档、接口文档（智能最新版本）。不确定从哪里找时先用它。”
- `search_docs`：“在文档中全文搜索。适合查业务规则、流程、接口说明。中文关键词用词语，不要拆成单字。”
- `search_code`：“在代码中搜索。适合查接口路由、结构体字段、错误码、表名。可以同时搜多个仓库。默认字面匹配、不区分大小写。”
- `read`：“读取文件原文。回答前必须读到支撑结论的原文，不要只凭搜索片段下结论。”

## 5. pi 运行时

### 5.1 隔离目录

```text
$DATA_DIR/ai/
  pi-agent/                 # PI_CODING_AGENT_DIR，由 DocHarbor 生成，每次配置变化后重写
    models.json
    mcp.json
    settings.json
    system-prompt.md        # 第 6 节
  pi-sessions/              # --session-dir
  pi-cwd/                   # 空目录，作为 pi 的工作目录
```

`models.json`（按 AI 供应商配置生成，见第 10 节）：

```json
{
  "providers": {
    "dh-deepseek-pro": {
      "baseUrl": "https://api.deepseek.com",
      "api": "openai-completions",
      "apiKey": "$DH_PROVIDER_KEY_0",
      "models": [{ "id": "deepseek-v4-pro" }]
    }
  }
}
```

`mcp.json`：

```json
{
  "mcpServers": {
    "docharbor": {
      "url": "http://127.0.0.1:<HTTP 端口>/api/mcp",
      "headers": { "Authorization": "Bearer ${DH_MCP_TOKEN}" },
      "exposure": "direct",
      "description": "DocHarbor：公司 Git 仓库文档和代码的只读访问"
    }
  },
  "autoEnableCodemode": false
}
```

`settings.json`：关闭非必需的内置扩展，例如 `"extensions": ["-builtin:llama"]`。保留 MCP。具体键名以 pi `docs/settings.md` 为准。

### 5.2 启动子进程

```text
命令：$PI_BIN --mode rpc --no-tools --no-context-files --no-skills
            --session-dir $DATA_DIR/ai/pi-sessions
            --system-prompt $DATA_DIR/ai/pi-agent/system-prompt.md
            --provider <默认 provider> --model <默认 model>
工作目录：$DATA_DIR/ai/pi-cwd
环境变量：只传白名单，不继承 DocHarbor 的全部环境
  PATH, HOME=$DATA_DIR/ai, PI_CODING_AGENT_DIR, PI_OFFLINE=1, PI_SKIP_VERSION_CHECK=1,
  NO_PROXY=127.0.0.1,localhost(+原值), no_proxy=同上,
  HTTP_PROXY/HTTPS_PROXY（如原环境有，模型 API 可能需要）,
  DH_MCP_TOKEN, DH_PROVIDER_KEY_0..n
```

不能加 `--no-extensions`（第 3 节）。实施时如果 `--system-prompt` 不接受文件路径，就改为传文本内容（`pi --help` 为准）。

### 5.3 RPC 客户端（Go）

- 读 stdout 用 `bufio.Reader.ReadBytes('\n')`，去掉行尾的 `\r`。**不要**用默认的 `bufio.Scanner`：它有 64 KB 的单行上限，而工具结果和消息可能很长。
- 命令带唯一 `id`，用 map 按 id 关联 response。没有 id 的记录都是事件，转给当前 run 的订阅者。
- stderr 单独读，保存最近 200 行环形缓冲，诊断页可以查看。不要把 stderr 当协议数据解析。
- `extension_ui_request` 只记日志。`notify` 类型里出现 MCP 连接失败时，标记该进程为不健康。
- 子进程退出时，当前 run 以失败结束（错误信息带 stderr 尾部），池里补一个新进程。

### 5.4 进程池

| 配置 | 默认值 | 说明 |
| --- | --- | --- |
| `AI_PI_POOL_SIZE` | 3 | 最大进程数，按需启动，最少保留 1 个空闲 |
| `AI_PI_MAX_RUNS_PER_PROCESS` | 100 | 跑满后在空闲时回收重启 |
| `AI_RUN_TIMEOUT_SECONDS` | 300 | 超时发 `abort` |
| `AI_MAX_TOOL_CALLS` | 40 | 单次 run 的工具调用数，超过后发 `abort`，回答末尾提示“检索步骤过多，已停止，可以缩小问题范围后重试” |

规则：

- 一个 run 独占一个进程。没有空闲进程时进入 FIFO 队列，SSE 推送排队位置。
- 同一会话同时最多一个 run。会话正在运行时再发问题，返回 409，前端改走“补充说明”接口。
- AI 配置版本变化时，重新生成 `pi-agent/` 下的文件。空闲进程立即重启，忙碌进程在当前 run 结束后重启。
- 浏览器断开 SSE **不中止** run：继续跑完并保存，用户回来时能看到结果。排队中的请求在客户端断开时取消。
- 健康检查：每个进程启动后先发一次 `get_state`，成功再放进池里。

### 5.5 一次 run 的流程

1. 从池中取一个进程（或排队）。
2. 新会话发 `new_session`；已有会话发 `switch_session`（`sessionPath` 为该会话保存的文件）。
3. 发 `set_model`，使用当前默认 provider 和 model。
4. 发 `prompt`，`message` 为第 6.2 节格式的上下文块加用户原文。
5. 转发事件，直到收到 `agent_settled`：
   - `message_update` 里的 `text_delta` 推给前端。
   - `tool_execution_start` / `tool_execution_end` 推过程卡片，同时记录工具返回过的行范围（第 8 节核实引用要用）。
   - `auto_retry_*` 和 `compaction_*` 推状态提示。
6. 对新会话，用 `get_state` 取 `sessionFile`，保存到会话表。用 `get_session_stats` 取 token 和费用。
7. 核实引用（第 8 节），写入运行记录，推 `done` 事件，把进程放回池里。

中途操作：

- **停止**：发 `abort`。
- **补充说明**：发 `prompt`，带 `"streamingBehavior": "steer"`。

## 6. 系统提示词与消息格式

### 6.1 系统提示词

写入 `system-prompt.md`。修改时同步更新评估集基线。

```text
你是 DocHarbor 问答助手，帮助公司的开发、产品和测试人员理解各服务的文档和代码。
你只能通过 docharbor 工具只读访问 Git 仓库，不能修改任何内容，也看不到数据库、日志和线上环境。

工作方式
- 先定位再回答：用 list_docs / search_docs 找设计和接口文档，用 search_code 找实现，用 read 读到能支撑结论的原文后再回答。不要只凭搜索片段下结论。
- 文档和代码不一致时，以默认分支代码为准，并指出文档哪里过时。
- 默认口径是智能最新。来自功能分支的内容要写明分支名，并说明尚未合入默认分支。
- 测试代码、mock、fixtures 不是业务事实，除非用户问的就是测试。
- 找不到时，说明搜了哪些关键词和范围，给出下一步建议，例如可能相关的服务或文档。不要编造接口、字段、表名或错误码。
- 问题有歧义时，先按上下文做最合理的假设并写明假设；只有无法继续时才反问。

按提问人调整回答（看用户消息开头 <context> 里的 role；没有时根据问题判断）
- 开发：给出接口路径和方法、鉴权、请求参数、返回字段、错误码，字段多时用表格，必要时给请求示例。
- 测试：从现象出发，列出可能原因、对应代码位置、如何判断、如何复现；涉及错误码时说明在哪里抛出。
- 产品：用业务语言讲流程、规则、状态和边界条件，少贴代码；流程用编号步骤，状态用表格。

引用
- 每个关键结论后标注来源，格式与工具结果中的引用键一致，用方括号括起来，例如 [游戏服务后端@main:core/handle/order.go#L40-88]。
- 只引用你实际通过 read 或搜索结果看到过的行。
- 不要在结尾另列参考资料。

格式
- 用中文回答，先给结论再展开细节。不要用“根据现有证据”之类的套话开头。
- 代码、路径、字段名用行内代码。
```

### 6.2 用户消息格式

DocHarbor 发给 pi 的 `message`：

```text
<context>
role: 产品
scope: 当前仓库 游戏服务后端
current_file: 游戏服务后端@dev-1.1.1-seckill-copy:doc/秒杀人工减少库存归还方案.md
</context>

秒杀库存人工归还有哪些限制条件？
```

- `role` 取值：开发 / 产品 / 测试 / 未指定。
- `scope` 取值：全部仓库 / 当前仓库 {名称} / 当前文件。
- `current_file` 只在范围是“当前文件”时出现。
- 范围只是给模型的提示，不在工具层强制限制，因为跨仓库追查经常是必要的。
- 展示历史时，去掉开头的 `<context>…</context>` 块，role 和 scope 改用小标签显示。

## 7. 数据与存储

### 7.1 表结构变更

- `ai_sessions` 新增列：`engine TEXT NOT NULL DEFAULT 'legacy'`（取值 `legacy` / `pi`）、`pi_session_file TEXT NOT NULL DEFAULT ''`（相对 `pi-sessions/` 的文件名）。
- 新表 `ai_runs`：

  | 字段 | 说明 |
  | --- | --- |
  | `id`, `session_id` | |
  | `status` | `queued` / `running` / `succeeded` / `aborted` / `timeout` / `failed` |
  | `question` | 用户原文 |
  | `role`, `scope_json` | |
  | `provider_key`, `model` | |
  | `tool_calls` | 工具调用次数 |
  | `input_tokens`, `output_tokens`, `cache_read_tokens`, `cost` | 来自 `get_session_stats` 的增量 |
  | `citations_json` | 核实结果，见第 8 节 |
  | `error_message`, `stderr_tail` | |
  | `started_at`, `finished_at` | |

- 新表 `ai_feedback`：`run_id`, `rating`（1 / -1）, `comment`, `created_at`。

### 7.2 会话内容

会话内容以 pi 的 JSONL 为准，DocHarbor 不在 `ai_messages` 里重复保存。

展示历史时，Go 直接解析 JSONL：

- 从最后一个 entry 沿 `parentId` 回溯出当前分支。
- 取出 user、assistant（文本和 toolCall）、toolResult 三类消息。
- 遇到 compaction entry 时，展示它之前的原始消息，不展示摘要。
- 按 run 分组：每条 user 消息开启一个新 run，和 `ai_runs` 按顺序对应。

文件头的 `version` 不是已知版本时，在日志里告警，并改用一个空闲 pi 进程执行 `switch_session` + `get_messages` 获取内容。

旧会话（`engine = 'legacy'`）继续从 `ai_messages` 读取，只读。

## 8. 引用核实

1. run 执行过程中，从每个工具结果里解析出引用键和行号，记录“看到过的范围”：`read` 记返回的行区间，搜索记每条匹配行及其上下文行。
2. run 结束后，用正则 `\[([^\[\]@]+)@([^:\]]+):([^\]#]+)#L(\d+)(?:-(\d+))?\]` 从最终回答中提取引用。
3. 每条引用的判定：
   - `verified`：同仓库、同 ref、同路径，且行区间和看到过的范围至少重叠 1 行。
   - `path_only`：这个文件被看到过，但行区间没有重叠。
   - `unverified`：这个文件没有被看到过。
4. 为每条引用生成跳转 URL：
   - 文件是已扫描的文档，并且能找到 `version_id`：`/?repo={id}&version={vid}`。
   - 其他情况：现有 blob 接口（`blobURL(repoID, commit, path)`，commit 用 ref 解析结果）。
5. 结果写入 `ai_runs.citations_json`，并通过 `done` 事件发给前端。

## 9. 前端

### 9.1 接口（替代旧的流式问答接口）

| 方法和路径 | 说明 |
| --- | --- |
| `GET /api/ai/sessions` | 现有接口，增加 `engine` 字段 |
| `POST /api/ai/sessions` | 现有接口；新会话的 `engine` 取当前配置 |
| `GET /api/ai/sessions/:id/transcript` | 返回 `{turns: [{run_id, question, role, scope, status, steps: [{tool, args, summary, is_error, preview}], answer, citations, usage, feedback}]}`；`preview` 为工具结果的前 2000 字符 |
| `POST /api/ai/sessions/:id/runs` | SSE。请求体 `{question, role, scope}`。事件：`queued{position}`、`started{run_id}`、`text{delta}`、`tool_start{call_id, tool, args}`、`tool_end{call_id, summary, is_error, preview}`、`notice{message}`（重试、压缩上下文等）、`done{status, citations, usage}`、`error{message}` |
| `POST /api/ai/sessions/:id/steer` | `{text}`，运行中补充说明 |
| `POST /api/ai/sessions/:id/abort` | 停止 |
| `POST /api/ai/runs/:id/feedback` | `{rating, comment?}` |
| `GET /api/access/ai/runs`、`GET /api/access/ai/runs/:id` | 诊断用，令牌需要 `ai.diagnostics.read`；详情包含完整 transcript（工具结果不截断）、用量、stderr 尾部 |

旧会话的接口保留，供只读展示。旧的诊断接口在旧链路删除时一起删除。

### 9.2 AI 问答页（替代页面重设计文档 §5.6）

布局沿用页面重设计文档 §5.6：会话 240px | 对话 | 右栏 320px，响应式规则也相同。右栏内容改为“引用”和“本次用量”两个标签。

**会话列表**：同页面重设计文档。`engine = 'legacy'` 的会话带“旧版”小标签，继续用页面重设计文档 §5.6 的旧界面渲染：过渡期内可以照常提问；`AI_ENGINE` 切换为 `pi` 后改为只读。`engine = 'pi'` 的会话按本节渲染。

**每轮对话**

- 提问气泡：右对齐，下方用小标签显示 role 和 scope。
- **过程区**：运行中默认展开，完成后折叠成一行，例如“查阅 5 个文件 · 搜索 7 次 · 32 秒”。展开后，每次工具调用一行：
  - 图标和中文名：`list_repos` 查看仓库、`list_branches` 查看分支、`list_docs` 浏览文档目录、`search_docs` 搜索文档、`search_code` 搜索代码、`read` 阅读、`list_dir` 浏览目录、`git_log` 查看提交、`diff_files` 比较分支。
  - 主要参数，例如 `搜索代码 “createOrder” · 游戏服务后端@main`、`阅读 core/handle/order.go 第 40-120 行`。
  - 右侧是摘要（工具结果第一行）。运行中的调用显示转圈。`is_error` 显示红色。
  - 点击展开 `preview`，用等宽字体，最高 320px，可滚动。
- **回答**：Markdown 流式渲染。引用键渲染成小标签，显示为“文件名#L40-88”，悬停显示完整引用键。
  - `verified`：普通样式，点击按第 8 节的 URL 打开（新标签页）。
  - `path_only` 和 `unverified`：warn 样式，悬停提示“这段内容没有在本次检索中读到，请自行核对”。
  - 流式输出过程中，引用先渲染成普通标签；`done` 之后按核实结果更新样式。
- **回答下方**：复制、👍 / 👎（点 👎 时展开一个可选的意见输入框）、“查看过程记录”（打开诊断详情，需要令牌时引导到系统设置）。
- **状态**：
  - 排队：“排队中，前面还有 N 个问题”。
  - 停止后：“已停止”。
  - 超时或工具调用过多：显示原因和“缩小范围后重试”。
  - 模型错误：显示错误和“重试”按钮（用同一个问题发起新 run）。

**输入框**（在页面重设计文档 §5.6 的基础上）

- 上方是角色选择，分段控件“开发 / 产品 / 测试”，选择保存在 localStorage。没选时为“未指定”。
- 范围下拉和原来一致。
- 运行中：发送按钮变成“停止”。此时输入并按 Enter，会作为补充说明发出（走 steer 接口），placeholder 改为“补充说明，会在当前步骤结束后交给助手”。
- Enter 发送、Shift+Enter 换行；输入法组合输入中按 Enter 不发送。

**空状态示例问题**：按当前角色显示 3 条，点击即填入输入框：

- 开发：“SGPS 下单接口的请求参数和返回字段是什么？”“用户服务的 token 怎么校验？”“游戏详情接口返回的发货方式怎么排序？”
- 测试：“CDK 激活失败时，返回的错误码分别代表什么？”“秒杀库存归还失败后会怎么重试？”“礼物订单缺少账号名时会出现什么现象？”
- 产品：“秒杀库存人工归还有哪些限制条件？”“游戏交易从发货到退款有哪些状态？”“第三方卖家接入有几种方式，区别是什么？”

**右栏**

- 引用：本轮所有引用，同一文件合并，标注核实状态，点击跳转。
- 本次用量：模型、耗时、工具调用次数、输入/输出 token、费用（有数据时显示）。

### 9.3 AI 供应商页（修改页面重设计文档 §5.7）

- 删除路由顺序、待应用路由、failover 相关的显示。状态条改为：启用状态、默认供应商和模型、引擎（pi / 旧版）、pi 进程池状态（进程数、忙碌数、排队数）。
- 供应商表格和编辑抽屉保留。
- 删除路由优先级、RPM、成本等级字段，pi 不使用这些字段。超时字段保留，作为 run 超时的参考。

### 9.4 AI 诊断页与系统设置

- **诊断页**（修改页面重设计文档 §5.8）：
  - 列表改为 `ai_runs`：状态、问题、模型、耗时、工具调用次数、token、反馈。
  - 详情为完整过程记录：每次工具调用的参数和完整结果、最终回答、引用核实表、stderr 尾部。
  - 删除任务框架、证据契约、检索轮次、契约覆盖等区块。
  - 筛选增加“只看 👎”。
- **系统设置**（在页面重设计文档 §5.9 基础上新增一节“MCP 接入”）：
  - 显示 MCP 地址（CopyField），按当前访问地址拼出 `/api/mcp`。
  - 签发令牌时可以选 `docs.read`，有效期可选 7 / 30 / 90 天。
  - 给出三段可复制的配置示例：
    - pi：`pi mcp add docharbor --url <地址> --bearer-token-env-var DOCHARBOR_TOKEN`
    - Codex：`~/.codex/config.toml` 片段
    - Claude Code：`claude mcp add --transport http docharbor <地址> --header "Authorization: Bearer …"`

  示例的准确写法以各客户端当前文档为准。

## 10. 供应商配置映射

- 每个 `usable` 的供应商生成一个 pi provider：
  - 名称为 `dh-<provider_key>`。
  - `baseUrl` 取 DocHarbor 的 `base_url`，如果以 `/chat/completions` 结尾就去掉。DocHarbor 现有调用也是在 base_url 后拼 `/chat/completions`，两边语义一致。
  - `api` 为 `openai-completions`。
  - `apiKey` 为 `$DH_PROVIDER_KEY_<序号>`，解密后的 Key 通过环境变量传给子进程。
- 只用默认供应商。模型调用的临时错误交给 pi 自带的自动重试；失败后在前端显示错误和“重试”按钮。不实现多供应商自动切换。
- 也可以把 yym 内部模型网关作为一个 OpenAI 兼容供应商配置进来，不需要特殊处理。
- AI 未启用时，问答页显示“AI 问答未启用”并引导到 AI 供应商页。不再提供旧的“本地检索模式”。

## 11. 部署

### Dockerfile

- 最终镜像增加 Node（>=22.19）和 `@earendil-works/pi-coding-agent@1.0.0`（锁定版本，`npm install -g --omit=dev`）。
- 先在 Alpine 上验证：构建后在容器里执行 `pi --mode rpc --no-tools --no-context-files --no-skills` 加一次 `get_state`。
  - 能跑通：继续用 `alpine`。
  - 因为原生模块或 musl 失败：最终镜像改为 `node:24-bookworm-slim`，再装 git、openssh-client、tini、ca-certificates、tzdata。Go 构建阶段相应改为 glibc 的 `golang:1.26-bookworm`（CGO + sqlite）。
- 预计镜像增加约 200 MB。

### 新增环境变量

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `AI_ENGINE` | `legacy`（步骤 4 后改为 `pi`） | 新建会话使用的引擎 |
| `PI_BIN` | `pi` | pi 可执行文件路径；测试时指向假进程 |
| `AI_PI_POOL_SIZE` 等 | 见第 5.4 节 | |
| `MCP_ENABLED` | `true` | 关闭后 `/api/mcp` 返回 404（不影响内部令牌访问） |

更新 README 的配置表。

## 12. 开发者自用接入

MCP 上线后（第 15 节步骤 1 完成），开发者可以在自己的 agent 里直接使用：

```bash
export DOCHARBOR_TOKEN=<系统设置里签发的 docs.read 令牌>
pi mcp add docharbor --url https://<docharbor>/api/mcp --bearer-token-env-var DOCHARBOR_TOKEN
```

如果用的是 pi，在 `/mcp` 里把 docharbor 的暴露方式改为 `direct`（或在 `mcp.json` 中设置），否则在没有 codemode 的配置下调用不到这些工具。这段说明写进使用手册。

## 13. 测试

### Go

- **MCP 工具**：用临时 git 仓库做 fixture，包含多分支、智能最新选中非默认分支的文档、二进制文件、超大文件、中文路径。逐个工具测试参数校验、ref 解析、截断提示、引用键格式和路径安全（`..`、绝对路径）。
- **鉴权**：无令牌、过期令牌、缺少 `docs.read` 能力、内部令牌。
- **RPC 客户端和进程池**：用一个 Go 写的假 pi（`PI_BIN` 指向它，按脚本输出事件）测试：
  - 命令和响应的关联。
  - 单行超过 1 MB 的事件。
  - 子进程意外退出。
  - 超时 abort 和工具调用数超限 abort。
  - 排队顺序，以及排队中客户端断开。
  - 同一会话并发 run 返回 409。
  - 配置变化后的滚动重启。
- **会话 JSONL 解析**：用 pi 实际生成的会话文件做 golden 测试（含工具调用、compaction、steer），文件放在 `testdata/`。
- **引用核实**：三种判定都要覆盖，包括路径中含中文和空格。

### 前端

- 单测：引用键解析和渲染、过程区摘要、context 块剥离。
- e2e：mock SSE 事件序列，覆盖排队 → 工具调用 → 流式回答 → `done`（含 unverified 引用）；运行中补充说明；停止；👎 反馈；旧会话只读打开。

### 真实冒烟（手动，步骤 2 结束时）

用真实模型对第 14 节的评估集跑一遍，记录每题耗时、工具调用次数、token 和费用。

## 14. 评估与切换

- **评估集** `testdata/ai-eval/questions.jsonl`，每行 `{id, role, question, expect}`。`expect` 写判定要点，例如“必须提到 SGPS 和通用商户两种对接方式”。来源：
  - 现有 `ai_agent_eval_test.go` 中的用例。
  - 生产环境 `ai_messages` 里的真实问题（由维护者导出后人工整理）。
  - 每个角色至少 10 题。
- **评估工具** `go run ./cmd/ai-eval -engine pi|legacy -file …`：调用本地 API，把每题的回答、过程、用量写到 `out/ai-eval/<时间>/`，并生成一个便于人工打分的 Markdown 对照表。
- **切换条件**：pi 引擎在评估集上人工判定的正确率明显高于旧链路，并且 P90 耗时可以接受（建议 ≤ 60 秒）。满足后把 `AI_ENGINE` 默认值改为 `pi`。
- **上线后**：定期把 👎 的问题补进评估集。

## 15. 实施步骤

每一步结束时，`go test ./...`、`npm test`、`npm run build`、`npm run check:bundle`、`npm run test:e2e` 都要通过，然后单独提交。

1. **MCP server**：实现 P0 工具、鉴权（`docs.read` 能力、内部令牌）、`MCP_ENABLED`，加 Go 测试。完成后用本机 pi 连上 `/api/mcp` 手动问几个问题，验证工具是否顺手（这一步不需要改任何问答功能）。
2. **pi 运行时**：实现配置生成、RPC 客户端、进程池、run 流程、引用核实、新表和新接口（`AI_ENGINE` 仍默认为 `legacy`）。Docker 镜像加入 pi 并完成 Alpine 验证。用真实模型跑评估集。
   - **检查点**：评估结果交给维护者确认后，再进入步骤 3。
3. **前端**：按第 9 节实现问答页、供应商页、诊断页、系统设置的 MCP 接入，并完成 e2e。
4. **切换**：满足第 14 节条件后，`AI_ENGINE` 默认改为 `pi`。更新使用手册和 README。
5. **删除旧链路**（切换稳定两周后）：删除旧的检索、任务框架、证据契约、证据整理、契约检查、回答校验、查询规划、服务候选相关代码和测试，以及旧的诊断接口和 `ai_agent_runs` / `ai_agent_steps` / `ai_service_candidates` 的写入。表保留只读，不删除数据。设计文档《DocHarbor AI Agent 架构重构设计文档》《执行过程》在文首标注“已被本文取代”。
6. **P1 工具**：`git_log`、`diff_files`。

## 16. 验收清单

- [ ] 外部 pi 用 `docs.read` 令牌能连上 `/api/mcp` 并调用全部 P0 工具；没有令牌时返回 401。
- [ ] pi 子进程的命令行和环境符合第 5.2 节：没有内置工具，没有继承无关环境变量，Key 不落盘（检查 `pi-agent/` 下的文件不含明文 Key）。
- [ ] 连续发起超过 `AI_PI_POOL_SIZE` 个问题时会正确排队；中途杀掉一个 pi 进程，当前 run 报错，后续 run 正常。
- [ ] 运行中补充说明、停止、超时都按预期工作，状态写入 `ai_runs`。
- [ ] 回答中的引用全部可点击，unverified 引用有警示样式。
- [ ] 旧会话可以只读打开。
- [ ] 评估集结果已存档，并经维护者确认。
- [ ] 第 13 节的测试全部通过。

## 17. 风险与注意事项

- **模型工具调用能力**：弱模型会反复搜索或不读原文就作答。先用默认的 DeepSeek 模型验证，必要时换成 yym 网关上更强的模型。系统提示词和工具描述是主要调优手段，不要回到在 Go 里堆规则的老路。
- **成本**：每题预计 5–20 次工具调用，token 用量是旧链路的数倍。用 `ai_runs` 的用量字段持续观察。
- **pi 版本**：锁定 1.0.0。升级 pi 时，重新跑会话 JSONL 的 golden 测试和评估集。
- **提示词注入**：仓库内容可能包含针对模型的指令。因为只有只读工具，影响仅限于“读到更多本来就能读的内容”或回答被误导。引用核实和用户反馈是兜底手段。
- **大仓库搜索性能**：`git grep` 跨全部仓库时注意总耗时。按仓库并发执行，每仓库 10 秒超时，超时的仓库在结果里注明。
