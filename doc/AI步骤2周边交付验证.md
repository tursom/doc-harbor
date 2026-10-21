# AI 步骤 2 周边交付验证

记录日期：2026-10-21。仅记录 CLI、题集、镜像运行时及文档交付；不代表部署、真实模型质量或人工切换门槛已通过。

## 离线验证

- `go test ./cmd/ai-eval`：通过。
- `go test -race ./cmd/ai-eval`：通过。
- `go vet ./cmd/ai-eval`：通过。
- `node --check scripts/pi-rpc-smoke.mjs`：通过。
- `cd ../demo-server/go-gateway/web && npm test`：331 项通过，0 失败、0 跳过；包含 change-log 结构、日期、唯一标识和发布文档链接校验。
- `git diff --check`：本交付文件无空白错误。只使用 Git 只读命令，没有 add/commit/checkout 等写操作。

CLI 的 HTTP/SSE mock 覆盖：原版响应没有 `engine`、legacy `scope_override` 请求、`message_done` 最终消息覆盖草稿、pi `runs`/角色/用量、工具循环的中间文本不进入最终评分答案、超过 1 MB 单行、前置问题、CRLF/多行 data/注释、错误或提前结束的流、引擎不匹配时不提问、失败产物及 Markdown 表格转义。题集校验确认开发/产品/测试各至少 10 题、ID 唯一且来源明确。全部测试使用本机 mock，不请求真实模型。

## Alpine / RPC

实际执行 Dockerfile 的独立阶段：

```bash
docker build --progress=plain --target pi-runtime -t docharbor-pi-runtime:local .
docker run --rm --network none --entrypoint node \
  docharbor-pi-runtime:local /usr/local/lib/docharbor/pi-rpc-smoke.mjs
```

两次检查均返回：

```json
{"node":"24.22.0","pi":"1.0.0","get_state":"passed","model_calls":0}
```

镜像基础为 `node:24-alpine3.23`，安装命令为 `npm install -g --omit=dev @earendil-works/pi-coding-agent@1.0.0`。初次 npm 下载发生多次 `ECONNRESET`，最终约 15 分钟完成；这是包下载等待，没有出现 musl/原生模块启动失败。`pi-runtime` 导出成功，随后断网容器复验成功，因此按实施文档保留 Alpine，无需 glibc fallback。没有构建或启动完整业务服务镜像，没有挂载真实数据/凭据，也没有部署。

宿主机独立运行 `node scripts/pi-rpc-smoke.mjs` 同样通过（Node 26.12.0、pi 1.0.0）。脚本只用临时 HOME/agent 目录和白名单环境，不读取个人 pi 认证或向模型发送 prompt。

## 评估边界与后续检查点

`ai_agent_eval_test.go` 有 6 个核心原题，场景来自 widget/QuantumDelivery 的合成 Git fixture。交付题集保留原题并扩写为每角色 10 题，逐题在 `source` 标明改写依据；不是 30 个独立生产问题。现有实例若没有对应已扫描 fixture，不能用这些题的“未找到”结果计算业务准确率。`metric_value`/`metric_cents` 分属两个 fixture 版本，应按题设置范围，或由维护者改用真实导出的题集和人工判定要点。

客户端只读取会话 `engine` 并选择对应协议，不能覆盖服务端新会话配置；主会话保持 `AI_ENGINE=legacy` 默认。评估 pi 时由维护者提供对应引擎的本地评估实例，真实模型批次、正确率和 P90 人工确认由主会话负责。本交付不进行前端重构、默认切换或旧链路删除。
