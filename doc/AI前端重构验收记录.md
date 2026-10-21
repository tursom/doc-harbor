# AI 前端重构验收记录

日期：2026-10-21。对应《DocHarbor AI问答重构实施文档》步骤 3。用户明确决定「跳过评估阶段，实现前端」，本次据此完成前端；真实模型质量未验收，默认引擎仍为 `legacy`。

## 交付范围

- 问答按会话 `engine` 分流。pi 展示工具过程、Markdown 回答、引用核实和用量，提供角色、范围、停止、运行中补充说明、复制、反馈、重试和指定 run 的诊断链接。配置 pi 后旧会话只读；AI 未启用时暂停新提问，保留历史查看。
- SSE 支持分段 UTF-8、LF/CRLF、大于 1 MB 的消息与明确终态。离页只断开接收，不停止已经开始的后台 run；重开运行中的会话轮询恢复结果。同会话并发返回 409 时读取后台运行，将本次输入转为补充说明。
- 引用按文件合并，可点击原文，显示 verified/path_only/unverified；未读到的行范围带提示。普通历史保留原问题和实际进入 pi 消息记录的补充说明，工具预览最多 2000 字符；诊断保留完整工具输出与原始消息。
- AI 诊断改为 run 列表和详情，提供状态、关键词、负反馈筛选、分页、完整过程、引用核实、用量、反馈、stderr 和指定 run 深链接。令牌默认只存内存，可显式选择标签页会话存储。
- 供应商页移除路由、优先级、配额和成本等旧管线编辑项，显示默认模型、引擎和进程池；保留草稿保存后应用的后端流程。供应商 PATCH 不提交只用于路径的 provider_key。
- 系统设置新增 docs.read、7/30/90 天选择和 MCP 地址，提供 pi、Codex、Claude Code 配置。仅 docs.read 可超过 24 小时；切到混合能力后保留已选期限并阻止不合法签发，不静默缩短。示例仅引用环境变量，不嵌入已签发令牌。
- transcript 增加可选的模型、起止时间、耗时、工具次数、错误信息和 supplements，同步 README、CHANGELOG、go-gateway 接口文档及文档修正记录。

## 自动化验证

| 检查 | 结果 |
| --- | --- |
| `npm test` | 21 个文件、123 项全部通过 |
| `npm run build` | 通过，0 个错误；现有 keyCode 弃用提示和大块构建提示不阻断构建 |
| `npm run check:bundle` | 所有页面通过；AI 问答 70.5 KiB、供应商 55.5 KiB、诊断 55.4 KiB gzip |
| `DOCHARBOR_SCREENSHOTS=1 npm run test:e2e` | 86 项全部通过，包含新 pi 问答、设置、诊断和原有页面回归 |
| `go test ./...` | 通过 |
| `go test -race ./internal/app ./internal/pirpc` | 通过 |
| `go vet ./...` | 通过 |
| go-gateway `npm test` | 394 项全部通过 |

新浏览器用例覆盖排队、工具开始/结束、流式正文、引用跳转真实文档页面、用量、Enter 补充说明、Shift+Enter 和输入法保护、显式停止、反馈持久化、离页后恢复、失败重试、双引擎只读分流、长期令牌限制、供应商提交字段、诊断过滤与完整结果。

独立审查发现的三项问题均已修复并加入回归：最终历史对账完成前禁止开始下一问，避免旧响应覆盖新队列；反馈保存后按 run_id 更新当前历史对象；补充说明从 JSONL/RPC 历史恢复。后端原生 JSONL golden 同步验证 supplements，不改原始 fixture。

测试日志位于本机 `/tmp/docharbor-frontend-step3-*.log`、`/tmp/docharbor-step3-*.log`。浏览器 API 使用虚构 fixture，不调用真实模型。

## 视觉检查

新增四个页面 × 1440/390 宽度 × 浅色/深色，共 16 张截图，已用无头浏览器渲染并检查。页面无横向溢出；桌面引用/用量位于右侧，窄屏改为下方标签页。窄屏供应商状态区改为网格，避免状态文字挤成长列。

代表截图（位于本机 out/ai-ui，未加入源码）：

- [pi 问答，桌面浅色](../out/ai-ui/pi-chat-1440-light.png)
- [pi 问答，窄屏深色](../out/ai-ui/pi-chat-390-dark.png)
- [供应商，窄屏深色](../out/ai-ui/pi-providers-390-dark.png)
- [诊断，桌面深色](../out/ai-ui/pi-diagnostics-1440-dark.png)
- [诊断，窄屏浅色](../out/ai-ui/pi-diagnostics-390-light.png)
- [MCP 设置，窄屏浅色](../out/ai-ui/mcp-settings-390-light.png)

重新生成：先运行 `npm run build`，再执行 `DOCHARBOR_SCREENSHOTS=1 npx playwright test e2e/ai-pi-visual.spec.ts`。现有重设计截图用例继续通过。

## 保留的检查点

真实供应商此前返回 401，详见 [后端阶段验收记录](AI问答重构阶段验收记录.md)。用户跳过评估只授权推进前端，没有形成回答准确率、引用正确率或 P90 耗时达标的证据。

未切换默认引擎、未删除旧链路、未部署、未执行工作仓库 Git 写操作。步骤 4 的质量条件、步骤 5 的两周稳定期和后续 P1 工具仍按原实施文档执行。
