# 变更记录

## 2026-10-21 · AI 重构步骤 3 前端

- 按用户「跳过评估阶段，实现前端」的决定实现双引擎问答界面：pi 工具过程、Markdown 引用标签、引用核实/用量、角色与范围、停止、补充说明、反馈及失败重试；配置 pi 后旧会话只读。
- 供应商页显示默认供应商、模型、引擎和进程池状态，移除路由顺序、自动切换、优先级/RPM/成本等级编辑；编辑时保留后端旧字段。
- 诊断改读 `ai_runs`，支持负反馈筛选、完整工具结果、引用核实、最终回答和 stderr；系统设置新增 MCP 地址/客户端配置，以及仅 `docs.read` 可用的 7/30/90 天令牌。
- transcript 补充模型、时间、耗时、工具次数、错误信息和同轮补充说明，供普通问答页恢复运行状态和用量；完成后的历史同步结束前保持发送互斥，反馈按 run_id 更新当前记录；同步 go-gateway 文档。默认仍为 `legacy`，真实模型质量未验收，未部署、未删除旧链路。

## 2026-10-21 · MCP 与 pi 后端

- 新增官方 Go MCP SDK v1.8.0 的只读 HTTP 服务及七项 P0 工具，支持智能最新文档、代码搜索、原文阅读和引用键；支持 `docs.read` 90 天令牌、内部令牌、`MCP_ENABLED` 和既有 CIDR 限制。
- 新增隔离的 pi 1.0.0 RPC 运行时、FIFO 进程池、模型配置快照及配置更新回收，支持补充说明、停止、超时、工具次数限制和崩溃恢复。密钥只进入子进程环境。
- 新增 `ai_runs`、`ai_feedback`、会话 engine 与 JSONL 文件关联；运行以 `pi_entry_id` 绑定 user entry，修复同题重试可能错配历史的问题，消息正文不重复写 `ai_messages`。
- 新增 SSE runs、transcript、steer、abort、反馈及带能力鉴权的 runs 诊断接口；三种引用核实状态提供文档或 blob 跳转。既有 legacy 会话保留，显式启用 pi 后旧会话只读。
- 加入业务示例评估题集、后端进程假体与 JSONL golden 测试。默认仍为 `legacy`；真实评估的默认供应商返回 401，未通过人工评估检查点，前端/默认切换/旧链路删除仍待后续阶段。详见阶段验收记录。

## 2026-10-21 · AI 重构步骤 2 周边交付

本日期为本地变更记录日期，不代表部署日期。实施规格以 `doc/DocHarbor AI问答重构实施文档.md` 为准。

- 新增 `cmd/ai-eval`：`-engine pi|legacy -file ... -url ...`，核对服务端会话引擎，分别适配原版 `messages/stream` 和 pi `runs`；逐题保存最终回答、事件过程、API 用量与人工评分表，失败不掩盖、中断保留材料。不会修改服务端配置。
- 新增 `testdata/ai-eval/questions.jsonl`：开发、测试、产品各 10 题。来自 `ai_agent_eval_test.go` 的 6 个核心问题及同一 fixture 的角色改写，逐题有 `source`/`expect`；追问包含前置轮。不把 fixture 扩写题标记成生产真实问题。
- Docker 运行阶段增加 Node 24 和锁定的 `@earendil-works/pi-coding-agent@1.0.0`；新增 `pi-runtime` 构建目标和 `scripts/pi-rpc-smoke.mjs`，隔离目录/白名单环境下只做 `get_state`，不发送模型 prompt。Alpine 原生模块失败时按实施规格整体切换 glibc 构建与运行阶段。
- README 补充 pi/MCP 环境变量、`docs.read` 接入、评估及人工确认步骤；在 `demo-server/go-gateway/docs/docharbor-ai.md` 同步 API 说明及独立文档修正记录。
- `AI_ENGINE` 保持 `legacy` 默认；人工评估确认前不进入前端实施、不切换默认、不删除旧链路。真实模型质量评估由主会话/维护者另行执行，本交付的 mock 与 RPC 冒烟不替代该检查点。
- 验证：CLI race 测试与 vet 通过；go-gateway 前端 331 项测试通过；Docker `pi-runtime` 构建和断网容器 `get_state` 均通过（Node 24.22.0 / pi 1.0.0），保留 Alpine。详见 [步骤 2 周边交付验证](doc/AI步骤2周边交付验证.md)。未部署。
