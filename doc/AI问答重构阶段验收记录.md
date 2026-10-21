# AI 问答重构阶段验收记录

日期：2026-10-21。对应《DocHarbor AI问答重构实施文档》步骤 1 和步骤 2。后端代码已实现，真实模型质量验收被供应商凭据阻断；本记录不代表步骤 3、默认切换或部署已完成。

后续决定：用户已明确回复「跳过评估阶段，实现前端」。据此继续步骤 3，本记录保留前一阶段评估失败的事实；不再以该检查点阻挡前端实现。默认切换、部署和旧链路删除仍未授权或未满足原定条件。

## 已交付

- `/api/mcp`：七项 P0 只读工具、官方 Go SDK v1.8.0、智能最新/分支/SHA 解析、引用键、上下文和截断、路径校验、文本及大小限制、10 秒超时；外部 `docs.read` 与内存内部令牌，保留 CIDR 限制。SDK 的最低版本要求使 Go 升至 1.25。
- pi：1.0.0 JSONL RPC，超过 1 MB 的事件、命令响应关联、stderr 尾部、启动健康检查、进程异常补位；FIFO 队列、取消、运行超时、工具次数限制、独占会话、停止与补充说明。已开始的 run 在 SSE 断开后继续落盘；排队中的请求取消。
- 配置：白名单环境、禁用内置工具/上下文文件/skills、只连接 DocHarbor MCP、direct 暴露、关闭 codemode、回环 NO_PROXY；供应商密钥仅进入环境，配置更新产生不可变快照并回收空闲/已归还的旧进程。
- 存储：会话 `engine` / `pi_session_file`，`ai_runs`、`ai_feedback`；消息以 pi JSONL 为准，树状回放、compaction 原文、steer 同轮、未知版本 RPC fallback。`ai_runs.pi_entry_id` 记录 user entry ID，避免准备失败后同题重试错配；这是实施规格表结构之外新增的一个关联字段，没有增加额外表。
- API：runs SSE、transcript、steer、abort、反馈、`ai.diagnostics.read` 的运行列表/完整诊断，沿用 viewer scope；引用按实际返回的行分为 verified/path_only/unverified，并生成文档或 blob URL。
- 评估 CLI、两份每角色 10 题的题集、README、CHANGELOG，以及 go-gateway 中的接口说明和文档修正记录。

实现使用 pi 原生 `steer` RPC 代替规格示意的 `prompt + streamingBehavior=steer`。两者在运行时都可补充说明，但后者在恰好空闲时会启动额外一轮；原生 steer 仅入队，结合控制命令与租约归还互斥、完成时清队列，避免影响下一会话。HTTP 协议保持规格定义不变。

## 验证结果

| 项目 | 结果 |
| --- | --- |
| `go test ./...` | 通过 |
| `go test -race ./... -count=1` | 通过 |
| `go vet ./...` | 通过 |
| `npm test` | 14 个文件、71 项通过 |
| `npm run build` | 通过；现有 `keyCode` 弃用提示及大块构建提示不阻断构建 |
| `npm run check:bundle` | 所有页面通过预算检查 |
| `npm run test:e2e` | 26 项通过；32 项截图用例默认按配置跳过 |
| `DOCHARBOR_SCREENSHOTS=1 npm run test:e2e` | 58 项全部通过，包括桌面/移动、亮色/暗色截图 |
| go-gateway `npm test` | 331 项通过（含新增文档修正记录） |
| Alpine pi 运行阶段构建和断网容器 `get_state` | Node 24.22.0、pi 1.0.0 通过，无模型调用，保留 Alpine |
| 外部 pi + `docs.read` | `pi mcp list --json` 成功，七项工具可见；实际调用仓库和文档列表成功 |

后端回归测试覆盖：并发同会话返回 409，准备阶段停止不会提交 prompt，准备/运行超时和工具次数限制，进程退出后的下一轮恢复，排队断开与开始后断开，steer/abort，模型配置更新期间排队、旧配置快照不变，同题重试关联，unknown-version 读取目标会话，引用三种状态、中文空格路径，JSONL 树与 compaction/steer，诊断能力及 viewer scope，Key 不落盘及环境隔离。

原始测试日志保存在本机 `/tmp/docharbor-*.log`；镜像周边验证详见《AI步骤2周边交付验证》。没有构建完整业务服务镜像，没有部署，没有执行工作仓库的 Git 写操作。Git fixture 的 init/add/commit 仅发生在自动化测试创建的临时目录中。

## 真实模型评估阻塞

从现有测试数据做 SQLite 一致性副本，复制加密主密钥，仅对镜像仓库只读访问，并在副本关闭定时扫描。临时测试监听 `127.0.0.1:18089`、显式设 `AI_ENGINE=pi`；没有修改现有服务配置。验证结束后已关闭临时服务及其 pi 子进程。

使用 `testdata/ai-eval/questions-real.jsonl` 发起 30 题批次。默认 DeepSeek 供应商返回 **HTTP 401 / Authentication Fails / invalid API key**。全部题目在进入工具检索前失败；带前置问题的题目也可能在前置轮即失败。因此没有可用于判断准确率、P90 检索耗时或有效 token/费用的答案，不能把这些失败计作模型回答质量分数。

本机归档：

- `out/ai-eval/20261021-142924-pi-1877536280/scores.md`：逐题人工评分表、失败原因和材料链接。
- 同目录 `results.json`、逐题 `result.json`、`events.jsonl` 和前置轮记录。
- `out/ai-eval/mcp-smoke.json`：外部 pi 连通性、initialize 与只读工具实际结果，不含令牌。

归档位于已忽略的 `out/`，避免将业务仓库检索结果默认加入源码。题集是实施规格示例及扩展，不是生产问答导出；仍需维护者补充生产真实问题。

## 后续检查点

实施规格 §15 步骤 2 明确要求：“评估结果交给维护者确认后，再进入步骤 3。”当前因凭据不可用未满足该条件。

真实质量评估仍可在维护者更新测试环境默认供应商的有效配置后，在隔离数据副本上重跑业务题集并进行 legacy 对照。前端阶段已按用户新决定推进；无需在聊天中提交密钥。默认值仍为 `legacy`，未删除旧链路；步骤 4 的切换条件及步骤 5 的两周稳定期仍适用。P1 工具 `git_log` / `diff_files` 按文档顺序留待步骤 6。
