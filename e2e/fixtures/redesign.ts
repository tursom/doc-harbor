import type { Page } from '@playwright/test'
import type { PiDiagnosticDetail, PiRun, PiTurn } from '../../src/ai-pi-types'
import type {
  AIAgentRun, AIDiagnosticsRunDetailResponse, AIMessage, AIMessageCitation, AISession,
  AISettingsSummary, CommitDetail, DocVersion, FileContent, FileEntry, Repository, ScanRun
} from '../../src/types'

// 全部数据均为虚构；不要从本地敏感原型或真实服务复制数据到公开仓库。
export const fixtureSecret = 'fictional-webhook-secret-for-e2e-only'
export const fixtureToken = 'fictional-access-token-for-e2e-only'
export const fixtureSHA = 'abcdef1234567890abcdef1234567890abcdef1234'
const timestamp = '2026-10-21T03:40:00Z'
export const scanRuns: ScanRun[] = [
  {
    id: 81, repo_id: 7, trigger_type: 'manual', status: 'partial_success', branch_count: 6,
    file_count: 27, skipped_count: 3, error_count: 2, started_at: timestamp,
    finished_at: '2026-10-21T03:40:12Z', error_message: '演示扫描错误：docs/archive/large.md 超过索引限制',
    detail_json: JSON.stringify({ errors: [
      { path: 'docs/archive/large.md', reason: 'file too large', branch: 'main' },
      { path: 'docs/legacy/broken.md', reason: 'missing git object', branch: 'release/1' }
    ] })
  },
  {
    id: 80, repo_id: 7, trigger_type: 'scheduled', status: 'success', branch_count: 6,
    file_count: 25, skipped_count: 0, error_count: 0, started_at: '2026-10-20T03:40:00Z',
    finished_at: '2026-10-20T03:40:09Z', error_message: '', detail_json: '{}'
  },
  {
    id: 79, repo_id: 7, trigger_type: 'webhook', status: 'success', branch_count: 4,
    file_count: 23, skipped_count: 1, error_count: 0, started_at: '2026-10-19T03:40:00Z',
    finished_at: '2026-10-19T03:40:08Z', error_message: '', detail_json: '{}'
  },
  {
    id: 78, repo_id: 7, trigger_type: 'startup', status: 'failed', branch_count: 0,
    file_count: 0, skipped_count: 0, error_count: 1, started_at: '2026-10-18T03:40:00Z',
    finished_at: '2026-10-18T03:40:30Z', error_message: '示例远程仓库连接超时',
    detail_json: JSON.stringify({ stage: 'fetch', retryable: true })
  }
]
export const repository: Repository = {
  id: 7, name: '灯塔示例文档', slug: 'lighthouse-demo', repo_url: 'https://git.example.test/lighthouse/docs.git',
  default_branch: 'main', tracked_branches: ['main', 'release/*', 'feature/*'],
  latest_include_branches: ['*'], latest_exclude_branches: ['archive/*', 'backup/*'], stale_branch_days: 30,
  branch_priority: ['main', 'release/*', 'feature/*'], credential_ref: '', enabled: true,
  sync_interval_seconds: 3600, max_file_size_bytes: 2097152, created_at: timestamp, updated_at: timestamp,
  scan_paths: ['docs', 'handbook'].map((path, index) => ({
    id: index + 1, repo_id: 7, path, include_globs: ['**/*.md', '**/*.html'], exclude_globs: [], enabled: true
  })), latest_scan: scanRuns[0]
}
const version: DocVersion = {
  id: 42, repo_id: 7, document_id: 11, branch: 'feature/release-guide', head_commit_sha: fixtureSHA,
  scan_path: 'docs', file_path: 'docs/deploy.md', previous_path: 'docs/release.md', dir_path: 'docs',
  file_name: 'deploy.md', extension: '.md', mime_type: 'text/markdown', file_size: 1842,
  blob_sha: 'fictional-blob', status: 'active', title: '灯塔发布操作手册', previewable: true,
  download_enabled: true, last_commit_sha: fixtureSHA, last_commit_time: timestamp,
  delete_commit_sha: '', delete_commit_time: '', rename_score: 98, participates_latest: true
}
export const documentContent: FileContent = {
  version_id: 42, document_id: 11, repo_id: 7, branch: version.branch, file_path: version.file_path,
  title: version.title, extension: '.md', mime_type: 'text/markdown', file_size: 1842,
  blob_sha: version.blob_sha, source_commit_sha: fixtureSHA, last_commit_time: timestamp,
  previewable: true, download_enabled: true, too_large: false,
  versions: [version, { ...version, id: 43, branch: 'main', previous_path: '', last_commit_time: '2026-10-20T03:40:00Z' }],
  content: '# 灯塔发布操作手册\n\n本文使用虚构服务演示文档阅读和分支版本。\n\n## 发布前检查\n\n确认 `lighthouse-api` 的健康检查通过，并核对发布分支。\n\n| 检查项 | 预期结果 |\n| --- | --- |\n| 健康检查 | ready |\n| 文档索引 | 27 个文件 |\n\n## 执行发布\n\n1. 备份当前配置。\n2. 发布候选版本。\n3. 观察扫描记录和服务指标。\n\n```sh\ncurl https://service.example.test/health\n```\n\n## 回滚与验证\n\n回滚后重新检查服务状态，并记录操作时间。\n'
}
// 可视验收包含真实 Mermaid 渲染，确保深色主题不只验证普通文本。
documentContent.content += '\n## 发布路径\n\n```mermaid\ngraph LR\n  A[检查] --> B[发布] --> C[验证]\n```\n'
export const files: FileEntry[] = [
  { kind: 'dir', name: 'archive', path: 'docs/archive' },
  { kind: 'file', name: 'deploy.md', path: 'docs/deploy.md', title: version.title, version_id: 42,
    document_id: 11, source_branch: version.branch, previewable: true },
  { kind: 'file', name: 'README.md', path: 'docs/README.md', title: '项目概览', version_id: 43, source_branch: 'main' },
  { kind: 'file', name: 'troubleshooting.md', path: 'docs/troubleshooting.md', title: '故障排查与恢复流程',
    version_id: 44, source_branch: 'release/1' },
  ...['健康检查与监控', '配置参数参考', '数据备份与恢复', 'Webhook 接入说明', '接口调用示例', '发布变更记录'].map((title, index): FileEntry => ({
    kind: 'file', name: `guide-${index + 1}.md`, path: `docs/guide-${index + 1}.md`, title,
    version_id: 45 + index, source_branch: index % 2 ? 'main' : 'release/1'
  }))
]
export const commits: CommitDetail[] = [
  {
    sha: fixtureSHA, parents: ['1234567890abcdef1234567890abcdef1234567890'], author: '林舟',
    author_email: 'lin@example.test', commit_time: timestamp, decorations: 'HEAD -> main, tag: v1.4.0',
    message: '完善灯塔发布检查\n\n补充回滚步骤，并统一文档目录名称。',
    files: [
      { status: 'M', path: 'docs/deploy.md' }, { status: 'A', path: 'docs/health.md' },
      { status: 'R098', path: 'docs/overview.md', old_path: 'docs/intro.md', new_path: 'docs/overview.md' },
      { status: 'D', path: 'docs/obsolete.md' }
    ]
  },
  {
    sha: '1234567890abcdef1234567890abcdef1234567890', parents: ['2222222222222222222222222222222222222222'], author: '周遥',
    author_email: 'zhou@example.test', commit_time: '2026-10-20T03:40:00Z',
    decorations: 'release/1', message: '建立文档索引与故障排查指南', files: [{ status: 'A', path: 'docs/README.md' }]
  },
  ...['补充健康检查示例', '整理配置参数', '新增数据备份指南', '初始化灯塔文档仓库'].map((message, index): CommitDetail => ({
    sha: String(index + 2).repeat(40), parents: index === 3 ? [] : [String(index + 3).repeat(40)],
    author: index % 2 ? '林舟' : '周遥', author_email: 'writer@example.test',
    commit_time: `2026-10-${19 - index}T03:40:00Z`, decorations: index === 1 ? 'tag: v1.3.0' : '',
    message, files: [{ status: 'M', path: 'docs/README.md' }]
  }))
]
export const aiSettings: AISettingsSummary = {
	engine: 'legacy', pi_pool: { total: 2, busy: 1, idle: 1, starting: 0, queued: 0 },
  enabled: true, status: 'enabled', default_provider_key: 'demo-primary', default_provider_name: '示例云模型',
  default_model: 'demo-chat-v1', route_provider_keys: ['demo-primary', 'demo-backup'],
  active_route_provider_keys: ['demo-primary', 'demo-backup'], has_unapplied_changes: false, encryption_ready: true,
  last_test: { status: 'pass', message: '连接成功', latency_ms: 218, tested_at: timestamp },
  route_providers: [
    { provider_key: 'demo-primary', name: '示例云模型', model: 'demo-chat-v1', priority: 1 },
    { provider_key: 'demo-backup', name: '示例备用模型', model: 'demo-chat-lite', priority: 2 }
  ],
  providers: ['demo-primary', 'demo-backup'].map((key, index) => ({
    provider_key: key, name: index ? '示例备用模型' : '示例云模型', provider_type: 'openai_compatible',
    base_url: `https://${index ? 'backup' : 'api'}.example.test/v1`, model: index ? 'demo-chat-lite' : 'demo-chat-v1',
    api_key_configured: true, api_key_last4: index ? '5678' : '1234', is_default: !index, route_order: index + 1,
    usable: true, last_test_status: 'pass', last_test_message: '连接成功', timeout_seconds: 20,
    max_rpm: 60, priority: index + 1, cost_tier: 'low'
  }))
}
const scope = { repo_mode: 'all', repo_ids: [], source_mode: 'smart_latest', file_types: ['markdown', 'code'] }
export const sessions: AISession[] = [
  { id: 32, title: '索引状态检查', viewer_key: 'demo-viewer', scope_json: JSON.stringify(scope),
    created_at: timestamp, updated_at: timestamp, archived_at: '' },
  { id: 31, title: '灯塔发布与回滚', viewer_key: 'demo-viewer', scope_json: JSON.stringify(scope),
    created_at: '2026-10-20T03:40:00Z', updated_at: '2026-10-20T03:45:00Z', archived_at: '' }
]
export const messages: AIMessage[] = [
  { id: 61, session_id: 31, role: 'user', content: '灯塔服务发布前要检查什么？', model: '', provider_name: '',
    model_route_json: '{}', prompt_tokens: 0, completion_tokens: 0, latency_ms: 0, status: 'completed',
    error_message: '', created_at: timestamp },
  { id: 62, session_id: 31, role: 'assistant', content: '发布前需要确认健康检查通过，并核对候选分支。[C1]\n\n随后备份配置、发布候选版本，观察服务指标；异常时执行回滚。[C1]',
    model: 'demo-chat-v1', provider_name: '示例云模型', model_route_json: '{}', prompt_tokens: 460,
    completion_tokens: 110, latency_ms: 1240, status: 'completed', error_message: '', created_at: timestamp }
]
const citations: AIMessageCitation[] = [{
  id: 91, message_id: 62, repo_id: 7, repo_name: repository.name, version_id: 42, source_scope: 'smart_latest',
  branch: version.branch, commit_sha: fixtureSHA, file_path: version.file_path, line_start: 5, line_end: 18,
  quote_text: '确认健康检查通过，并核对发布分支。', score: 0.94, created_at: timestamp
}]
const candidates = [{
  id: 1, run_id: 51, message_id: 62, repo_id: 7, repo_name: repository.name, service_name: 'lighthouse-api',
  matched_terms: ['发布', '回滚'], confidence: 'high', reason: '发布手册和健康检查文档匹配',
  score: 0.93, evidence_count: 2, created_at: timestamp
}]
const run: AIAgentRun = {
  id: 51, session_id: 31, user_message_id: 61, assistant_message_id: 62, status: 'succeeded', current_state: 'done',
  intent: 'deployment', scope_json: JSON.stringify(scope), retrieval_plan_json: '{}', service_candidate_count: 1,
  evidence_count: 2, code_evidence_count: 0, memory_count: 0, unconfirmed_count: 0, verification_status: 'succeeded',
  verification_report_json: '{}', checkpoint_json: '{}', index_snapshot_id: 1, config_version: 1,
  config_hash: 'fictional-config', model: 'demo-chat-v1', provider_name: '示例云模型', provider_failover_json: '[]',
  model_route_json: '{}', escalation_count: 0, estimated_cost_json: '{}', started_at: timestamp,
  finished_at: '2026-10-21T03:40:02Z', error_message: ''
}
const diagnostics: AIDiagnosticsRunDetailResponse = {
  session: sessions[1]!, user_message: messages[0]!, assistant_message: messages[1]!, run,
  service_candidates: candidates, citations,
  agent_workflow: {
    task_frame: { intent: 'deployment', user_goal: '确认发布前检查与回滚步骤', answer_shape: 'steps',
      scope_strategy: 'repository', target_artifacts: ['docs/deploy.md'], must_not: [], known_terms: ['灯塔'], generated_terms: ['发布'] },
    evidence_contract: { contract_id: 'deployment-v1', required_keys: ['release.precheck'], recommended_keys: ['release.rollback'] },
    evidence_bundle: { bundle_id: 'bundle-demo-51', coverage: { 'release.precheck': 'covered' }, group_count: 2, excluded_count: 0 },
    verification_report: { status: 'succeeded', details: ['引用覆盖发布检查步骤'], passed_checks: ['citation_coverage'],
      failed_checks: [], next_action: 'answer', rewrite_attempted: false },
    retrieval_rounds: [{ round: 1, reason: '检索发布检查与回滚文档', searches: [{ tool: 'search', query: '灯塔 发布 回滚' }],
      new_evidence_count: 2, missing_contract_keys: [], coverage_delta: { 'release.precheck': 'covered' } }],
    contract_coverage: { status: 'covered', missing_required: [], next_action: 'answer', items: [{
      key: 'release.precheck', requirement: '发布前检查', status: 'covered', evidence_ids: [91],
      reason: '操作手册包含完整检查步骤', missing_detail: '', confidence: 0.96
    }] }
  },
  steps: [{ id: 1, run_id: 51, parent_step_id: 0, agent_name: 'retriever', step_type: 'search', status: 'succeeded',
    tool_name: 'search', task_class: 'retrieve', model: 'demo-chat-v1', provider_name: '示例云模型', model_route_reason: '',
    escalated_from_step_id: 0, token_input: 120, token_output: 45, estimated_cost: '0', latency_ms: 218,
    error_message: '', summary: { query: '灯塔 发布 回滚', evidence_count: 2 }, created_at: timestamp, finished_at: timestamp }],
  data_sources: {
    scope, indexing: { default_scan_roots: ['docs'], exclude_globs: ['**/vendor/**'], max_file_size: 2097152 },
    repositories: [{ ...repository, candidate_targets: [], scan_paths: [{ id: 1, path: 'docs',
      include_globs: ['**/*.md'], exclude_globs: [], created_at: timestamp, updated_at: timestamp }] }]
  }
}

export const piRun: PiRun = {
  id: 51, session_id: 31, status: 'succeeded', question: messages[0]!.content, role: '开发', scope_json: JSON.stringify(scope),
  provider_key: 'demo-primary', model: 'demo-chat-v1', tool_calls: 2, input_tokens: 460, output_tokens: 110, cache_read_tokens: 100,
  cost: 0.0042, citations_json: '[]', error_message: '', stderr_tail: '示例运行没有异常。', started_at: timestamp, finished_at: '2026-10-21T03:40:02Z',
  feedback: { rating: -1, comment: '希望补充回滚条件。' }
}
export const piTurn: PiTurn = {
  run_id: 51, question: messages[0]!.content, role: '开发', scope, status: 'succeeded',
  steps: [{ call_id: 'search-1', tool: 'mcp__docharbor__search_docs', args: { query: '灯塔 发布' }, summary: '找到 2 处匹配，分布在 1 个文件', is_error: false, preview: '== lighthouse-demo@main:docs/deploy.md#L5-18\nL5: 发布检查' },
    { call_id: 'read-1', tool: 'mcp__docharbor__read', args: { repo: 'lighthouse-demo', path: 'docs/deploy.md', start_line: 5, end_line: 18 }, summary: '读取第 5-18 行，共 30 行', is_error: false, preview: '读取第 5-18 行，共 30 行\n== lighthouse-demo@main:docs/deploy.md#L5-18\n5\t确认健康检查通过，并核对发布分支。' }],
  answer: '发布前需要确认健康检查通过，并核对候选分支。[lighthouse-demo@main:docs/deploy.md#L5-18]\n\n回滚条件需要进一步核对。[lighthouse-demo@main:docs/deploy.md#L25-28]',
  citations: [{ key: 'lighthouse-demo@main:docs/deploy.md#L5-18', repo: 'lighthouse-demo', ref: 'main', path: 'docs/deploy.md', line_start: 5, line_end: 18, status: 'verified', url: '/?repo=7&version=42' },
    { key: 'lighthouse-demo@main:docs/deploy.md#L25-28', repo: 'lighthouse-demo', ref: 'main', path: 'docs/deploy.md', line_start: 25, line_end: 28, status: 'unverified', url: '/?repo=7&version=42' }],
  usage: { input_tokens: 460, output_tokens: 110, cache_read_tokens: 100, cost: 0.0042 }, feedback: piRun.feedback!, model: piRun.model,
  provider_key: piRun.provider_key, started_at: timestamp, finished_at: piRun.finished_at, duration_ms: 2000, tool_calls: 2
}
piRun.citations_json = JSON.stringify(piTurn.citations)
export const piDiagnostics: PiDiagnosticDetail = { run: piRun, session: { ...sessions[1]!, engine: 'pi' }, transcript: { ...piTurn, messages: [{ role: 'user', content: piTurn.question }, { role: 'toolResult', content: [{ type: 'text', text: piTurn.steps[1]!.preview }] }] } }

/** 每个测试独立保存状态；所有 /api 请求在浏览器端拦截，绝不读写真实服务数据。 */
export async function mockRedesign(page: Page) {
  const state = {
    repo: structuredClone(repository), patches: [] as Partial<Repository>[], secretRotations: 0,
    questions: [] as { question: string; scope_override: unknown }[], unexpected: [] as string[],
    repoReads: 0, messageReads: [] as number[], secret: fixtureSecret,
    createdRepos: [] as Repository[], scannedRepos: [] as number[], tokenRequests: [] as Record<string, unknown>[]
  }
  await page.route('**/api/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    const method = request.method()
    const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
    if (path === '/api/repos' && method === 'GET') {
      state.repoReads++
      return json({ items: [state.repo, { ...repository, id: 8, name: '星图示例服务', slug: 'star-map', latest_scan: scanRuns[1] }, ...state.createdRepos] })
    }
    if (path === '/api/repos/7' && method === 'PATCH') {
      const patch = request.postDataJSON() as Partial<Repository>
      state.patches.push(patch)
      state.repo = { ...state.repo, ...patch }
      return json(state.repo)
    }
    if (path === '/api/repos' && method === 'POST') {
      const repo = { ...repository, ...request.postDataJSON(), id: 9 }
      state.createdRepos.push(repo)
      return json(repo, 201)
    }
    if (/\/repos\/\d+\/scan$/.test(path) && method === 'POST') {
      state.scannedRepos.push(Number(path.split('/')[3]))
      return json(scanRuns[1])
    }
    if (/\/repos\/\d+\/branches$/.test(path)) return json({ items: ['main', version.branch, 'release/1'].map((ref_name, i) => ({
      id: i + 1, repo_id: 7, ref_type: 'branch', ref_name, commit_sha: fixtureSHA, commit_time: timestamp, last_scanned_at: timestamp
    })) })
    if (/\/repos\/\d+\/(files|tree|search)$/.test(path)) return json({ items: files })
    if (/\/repos\/\d+\/versions\/\d+\/content$/.test(path)) return json(documentContent)
    if (/\/repos\/\d+\/documents\/\d+\/history$/.test(path)) return json({
      document: { id: 11 }, versions: documentContent.versions, events: [{ id: 1, repo_id: 7, document_id: 11,
        branch: version.branch, event_type: 'rename', old_path: 'docs/release.md', new_path: 'docs/deploy.md',
        commit_sha: fixtureSHA, commit_time: timestamp, rename_score: 98 }]
    })
    if (/\/repos\/\d+\/documents\/\d+\/versions$/.test(path)) return json({ items: documentContent.versions })
    if (/\/repos\/\d+\/history$/.test(path)) return json({ items: commits })
    if (/\/repos\/\d+\/commits\//.test(path)) return json(commits.find((item) => path.endsWith(item.sha)) || commits[0])
    if (/\/repos\/\d+\/scan-runs$/.test(path)) return json({ items: scanRuns })
    if (path === '/api/webhooks/github/secret') {
      if (method === 'POST') { state.secretRotations++; state.secret = `${fixtureSecret}-rotated` }
      return json({ configured: true, secret: state.secret })
    }
    if (path === '/api/tokens' && method === 'POST') {
      state.tokenRequests.push(request.postDataJSON())
      return json({ token: fixtureToken, expires_at: '2026-10-21T11:40:00Z', ...request.postDataJSON() })
    }
    if (path === '/api/ai/settings') return json(aiSettings)
    if (path === '/api/ai/sessions') return json(method === 'POST' ? sessions[1] : { items: sessions })
    if (/\/api\/ai\/sessions\/\d+$/.test(path) && method === 'GET') return json(sessions.find(item => item.id === Number(path.split('/')[4])) || { ...sessions[1], id: Number(path.split('/')[4]) })
    if (/\/api\/ai\/sessions\/\d+\/messages$/.test(path) && method === 'GET') {
      const sessionID = Number(path.split('/')[4])
      state.messageReads.push(sessionID)
      return json({ items: sessionID === 31 ? messages : [], service_candidates: sessionID === 31 ? candidates : [],
        citations: sessionID === 31 ? citations : [] })
    }
    if (/\/api\/ai\/sessions\/\d+\/messages\/stream$/.test(path) && method === 'POST') {
      const payload = request.postDataJSON()
      state.questions.push(payload)
      const events = [
        ['user_message', { message: { ...messages[0], id: 63, content: payload.question } }],
        ['run_started', { run, assistant_message: { ...messages[1], id: 64, content: '', status: 'streaming' } }],
        ['answer_delta', { message_id: 64, delta: '模拟流式回答完成。' }],
        ['message_done', { run, message: { ...messages[1], id: 64, content: '模拟流式回答完成。' }, service_candidates: [], citations: [] }],
        ['done', { ok: true }]
      ]
      return route.fulfill({ contentType: 'text/event-stream', body: events.map(([event, data]) =>
        `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`).join('') })
    }
    if (path === '/api/access/ai/diagnostics/runs') return json({ items: [{ run, session: sessions[1],
      user_message_id: 61, user_question: messages[0]!.content, assistant_message_id: 62,
      assistant_status: 'completed', duration_ms: 2000 }] })
    if (path === '/api/access/ai/diagnostics/runs/51') return json(diagnostics)
    if (path === '/api/access/ai/diagnostics/data-sources') return json(diagnostics.data_sources)
    if (path === '/api/access/ai/runs') return json({ items: [piRun] })
    if (path === '/api/access/ai/runs/51') return json(piDiagnostics)
    // 未知接口直接失败并由测试断言，避免返回空对象掩盖页面与 API 契约不匹配。
    state.unexpected.push(`${method} ${path}`)
    return json({ error: `Unmocked endpoint: ${method} ${path}` }, 501)
  })
  return state
}
