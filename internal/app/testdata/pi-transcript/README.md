本目录 fixture 由本机 `@earendil-works/pi-coding-agent@1.0.0` 的
`dist/core/session-manager.js` 原生序列化生成，文件头为 session version 3。
没有调用模型，也没有复制开发者的真实会话或凭据。

`generate.mjs` 使用 appendMessage、appendCompaction、appendContextEdit、
appendUsage 和 branch 构造工具调用、普通 user 形式的 steer、压缩和弃分支。
生成后只规范化随机 ID 与 entry 时间，保留 pi 的实际字段结构。
`get-messages.json` 使用 RPC get_messages 的真实 response 包装形状，内容为
同一原始分支的消息，用于验证两种入口的展示一致性；它不是压缩后 RPC 的抓包。

运行 `node internal/app/testdata/pi-transcript/generate.mjs <安装目录>` 可重建。
golden 是人工检查过的预期展示结果；Go 测试不依赖 Node 或本机 pi 安装。
