// 用本机 pi 1.0.0 的 SessionManager 原生序列化生成 fixture，不调用模型或网络。
// 运行：node generate.mjs [pi-coding-agent 安装目录]。只写本目录的新 fixture。
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const root = process.argv[2] ?? '/root/.pi/agent/npm/node_modules/@earendil-works/pi-coding-agent';
const version = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8')).version;
if (version !== '1.0.0') throw new Error(`Expected pi 1.0.0, got ${version}`);
const { SessionManager } = await import(pathToFileURL(join(root, 'dist/core/session-manager.js')));
const temp = mkdtempSync(join(tmpdir(), 'docharbor-pi-fixture-'));
const output = dirname(fileURLToPath(import.meta.url));
const timestamp = 1733234401000;
const usage = { input: 10, output: 2, cacheRead: 3, cacheWrite: 1, totalTokens: 16,
  cost: { input: 0.01, output: 0.02, cacheRead: 0, cacheWrite: 0, total: 0.03 } };
const user = content => ({ role: 'user', content, timestamp });
const assistant = (content, stopReason = 'stop') => ({ role: 'assistant', content,
  api: 'openai-completions', provider: 'fixture', model: 'fixture', usage, stopReason, timestamp });
const context = (role, scope, text) => `<context>\nrole: ${role}\nscope: ${scope}\n</context>\n\n${text}`;
try {
  const manager = SessionManager.create('/docharbor/pi-cwd', temp);
  manager.appendModelChange('fixture', 'fixture');
  manager.appendMessage({ role: 'system', content: '', sections: { preamble: '只读问答' }, timestamp });
  manager.appendMessage(user(context('产品', '当前仓库 游戏服务', '秒杀库存有哪些限制？')));
  const firstAssistant = manager.appendMessage(assistant([
    { type: 'thinking', thinking: '该内容只在诊断显示。' },
    { type: 'text', text: '先读取方案。' },
    { type: 'toolCall', id: 'call_read', name: 'mcp_docharbor_read', arguments: { repo: '游戏服务', path: 'doc/库存 方案.md', start_line: 40 } }
  ], 'toolUse'));
  manager.appendMessage({ role: 'toolResult', toolCallId: 'call_read', toolName: 'mcp_docharbor_read',
    content: [{ type: 'text', text: '读取第 40-42 行，共 100 行\n== 游戏服务@main:doc/库存 方案.md#L40-42\n   40\t人工归还有数量限制。\n   41\t仅允许有效订单。' }],
    details: { fixture: '完整诊断字段' }, isError: false, timestamp });
  // _queueSteer 的原生持久化形式：普通 user + text block，没有 steer 字段。
  manager.appendMessage(user([{ type: 'text', text: '也说明异常情况。' }]));
  manager.appendCompaction('这是压缩摘要，不得当作回答。', firstAssistant, 50000, { readFiles: ['doc/库存 方案.md'] });
  manager.appendContextEdit(firstAssistant, null);
  manager.appendUsage('cache_warm', 'fixture', 'fixture', usage);
  const answer = manager.appendMessage(assistant([{ type: 'text', text: '仅允许有效订单。[游戏服务@main:doc/库存 方案.md#L41-45]' }]));
  manager.appendMessage(user(context('开发', '全部仓库', '这条提问属于放弃的分支。')));
  manager.appendMessage(assistant([{ type: 'text', text: '放弃的回答不能出现。' }]));
  manager.branch(answer);
  manager.appendMessage(user(context('测试', '全部仓库', '解释 <context>原文</context>。')));
  manager.appendMessage(assistant([{ type: 'text', text: '保留原文 context。' }], 'aborted'));

  // 只规范化随机 ID 与 entry 时间，不改动任何 pi 生成的结构或消息内容。
  const entries = readFileSync(manager.getSessionFile(), 'utf8').trim().split('\n').map(JSON.parse);
  const ids = new Map(entries.map((entry, i) => [entry.id, i === 0 ? 'fixture-session' : `entry-${i}`]));
  for (const entry of entries) {
    for (const key of ['id', 'parentId', 'firstKeptEntryId', 'targetId', 'fromId']) {
      if (ids.has(entry[key])) entry[key] = ids.get(entry[key]);
    }
    entry.timestamp = '2024-12-03T14:00:00.000Z';
  }
  writeFileSync(join(output, 'pi-1.0.0-v3.jsonl'), entries.map(entry => JSON.stringify(entry)).join('\n') + '\n');
  const messages = entries.filter(entry => entry.type === 'message').map(entry => entry.message);
  // RPC 形状由 rpc-mode.ts get_messages 的 success 包装确定；过滤弃分支。
  const activeMessages = messages.filter(message => !JSON.stringify(message).includes('放弃'));
  writeFileSync(join(output, 'get-messages.json'), JSON.stringify({ id: 'fixture', type: 'response', command: 'get_messages', success: true, data: { messages: activeMessages } }, null, 2) + '\n');
} finally {
  rmSync(temp, { recursive: true, force: true });
}
