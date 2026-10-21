// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, type App } from 'vue'
import { piApi } from '../api-pi'
import { APIRequestError } from '../api'
import { useDiagnostics, diagnosticsTokenStorageKey, diagnosticStatuses, diagnosticStatusLabel, runDuration, runTokens } from './useDiagnostics'
import type { PiDiagnosticDetail, PiRun } from '../ai-pi-types'

const apps: App[] = []
function run(id: number): PiRun {
  return { id, session_id: 3, status: 'succeeded', question: `问题 ${id}`, role: '开发', scope_json: '{}', provider_key: 'dh-test', model: '模型', tool_calls: 2, input_tokens: 100, output_tokens: 20, cache_read_tokens: 10, cost: 0.01, citations_json: '[]', error_message: '', started_at: '2026-10-21T10:00:00Z', finished_at: '2026-10-21T10:00:12Z' }
}
function detail(id: number): PiDiagnosticDetail {
  return { run: run(id), session: { id: 3, title: '排查会话' }, transcript: null } as PiDiagnosticDetail
}
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (reason: unknown) => void; const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail }); return { promise, resolve, reject } }
async function flush() { for (let index = 0; index < 8; index++) await nextTick() }
function mount() {
  let diagnostics!: ReturnType<typeof useDiagnostics>
  const host = document.createElement('div')
  document.body.append(host)
  const app = createApp(defineComponent({ setup() { diagnostics = useDiagnostics(); return () => h('div') } }))
  app.mount(host)
  apps.push(app)
  return diagnostics
}

beforeEach(() => {
  window.history.replaceState(null, '', '/ai/diagnostics/')
  // 提供独立存储，避免 Node 的同名全局存储和 jsdom 混用。
  const values = new Map<string, string>()
  vi.spyOn(window, 'sessionStorage', 'get').mockReturnValue({ getItem: key => values.get(key) ?? null, setItem: (key, value) => { values.set(key, value) }, removeItem: key => { values.delete(key) } } as Storage)
  vi.spyOn(piApi, 'diagnosticsRuns').mockResolvedValue({ items: [] })
  vi.spyOn(piApi, 'diagnosticRun').mockImplementation(async (_, id) => detail(id))
})
afterEach(() => { apps.splice(0).forEach(app => app.unmount()); document.body.replaceChildren(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('pi 诊断数据与深链接', () => {
  it('没有令牌时保留目标且不发请求，输入后读取目标而不是列表首条', async () => {
    window.history.replaceState(null, '', '/ai/diagnostics/?run=42')
    vi.mocked(piApi.diagnosticsRuns).mockResolvedValue({ items: [run(99)] })
    const diagnostics = mount()
    expect(diagnostics.selectedID.value).toBe(42)
    expect(piApi.diagnosticsRuns).not.toHaveBeenCalled()
    expect(piApi.diagnosticRun).not.toHaveBeenCalled()
    diagnostics.token.value = '新令牌'
    await diagnostics.loadRuns()
    await flush()
    expect(piApi.diagnosticRun).toHaveBeenCalledWith('新令牌', 42, expect.any(AbortSignal))
    expect(diagnostics.detail.value?.run.id).toBe(42)
    expect(window.location.search).toBe('?run=42')
    expect(window.sessionStorage.getItem(diagnosticsTokenStorageKey)).toBeNull()
  })

  it('恢复主动保存的会话令牌后自动打开目标，无需该运行出现在列表', async () => {
    window.history.replaceState(null, '', '/ai/diagnostics/?run=42')
    window.sessionStorage.setItem(diagnosticsTokenStorageKey, '会话令牌')
    const diagnostics = mount()
    await flush()
    expect(diagnostics.rememberToken.value).toBe(true)
    expect(diagnostics.detail.value?.run.id).toBe(42)
    expect(piApi.diagnosticRun).toHaveBeenCalledTimes(1)
    diagnostics.rememberToken.value = false
    expect(window.sessionStorage.getItem(diagnosticsTokenStorageKey)).toBeNull()
    expect(diagnostics.token.value).toBe('会话令牌')
  })

  it.each(['-1', '0', '1.5', 'abc', '9007199254740993'])('忽略无效目标 %s', value => {
    window.history.replaceState(null, '', `/ai/diagnostics/?run=${value}`)
    expect(mount().selectedID.value).toBeNull()
  })

  it('👎、关键词和状态按后端参数发送，分页沿用已提交筛选并按 PiRun.id 去重', async () => {
    const diagnostics = mount()
    diagnostics.token.value = '令牌'
    diagnostics.status.value = 'timeout'
    diagnostics.query.value = '  下单  '
    diagnostics.negativeOnly.value = true
    vi.mocked(piApi.diagnosticsRuns).mockResolvedValueOnce({ items: [run(9), run(8)], next_cursor: '8' }).mockResolvedValueOnce({ items: [run(8), run(7), run(7)] })
    await diagnostics.loadRuns()
    expect(piApi.diagnosticsRuns).toHaveBeenLastCalledWith('令牌', { limit: '50', status: 'timeout', q: '下单', rating: '-1' }, expect.any(AbortSignal))
    diagnostics.query.value = '尚未提交的新关键词'
    diagnostics.negativeOnly.value = false
    await diagnostics.loadRuns(true)
    expect(piApi.diagnosticsRuns).toHaveBeenLastCalledWith('令牌', { limit: '50', status: 'timeout', q: '下单', rating: '-1', cursor: '8' }, expect.any(AbortSignal))
    expect(diagnostics.runs.value.map(item => item.id)).toEqual([9, 8, 7])
    expect(diagnostics.nextCursor.value).toBe('')
    expect(diagnostics.detail.value?.run.id).toBe(9)
  })

  it('令牌改变取消列表与详情，迟到的成功和错误都不能覆盖新令牌的页面', async () => {
    const oldList = deferred<{ items: PiRun[]; next_cursor: string }>()
    const oldDetail = deferred<PiDiagnosticDetail>()
    vi.mocked(piApi.diagnosticsRuns).mockReturnValueOnce(oldList.promise).mockResolvedValue({ items: [run(2)] })
    vi.mocked(piApi.diagnosticRun).mockReturnValueOnce(oldDetail.promise).mockResolvedValue(detail(1))
    const diagnostics = mount()
    diagnostics.token.value = '旧令牌'
    const listPromise = diagnostics.loadRuns()
    const detailPromise = diagnostics.openRun(1)
    const listSignal = vi.mocked(piApi.diagnosticsRuns).mock.calls[0]![2]!
    const detailSignal = vi.mocked(piApi.diagnosticRun).mock.calls[0]![2]!
    diagnostics.token.value = '新令牌'
    expect(listSignal.aborted).toBe(true)
    expect(detailSignal.aborted).toBe(true)
    expect(diagnostics.detail.value).toBeNull()
    await diagnostics.loadRuns()
    oldList.resolve({ items: [run(99)], next_cursor: '99' })
    oldDetail.reject(new APIRequestError(403, '旧请求无权访问'))
    await Promise.all([listPromise, detailPromise])
    await flush()
    expect(diagnostics.runs.value.map(item => item.id)).toEqual([2])
    expect(diagnostics.detail.value?.run.id).toBe(1)
    expect(diagnostics.error.value).toBe('')
    expect(diagnostics.detailError.value).toBe('')
    expect(diagnostics.nextCursor.value).toBe('')
    expect(diagnostics.loading.value).toBe(false)
  })

  it('快速切换运行和卸载时取消详情并丢弃迟到响应', async () => {
    const old = deferred<PiDiagnosticDetail>()
    vi.mocked(piApi.diagnosticRun).mockReturnValueOnce(old.promise)
    const diagnostics = mount()
    diagnostics.token.value = '令牌'
    const first = diagnostics.openRun(1)
    const signal = vi.mocked(piApi.diagnosticRun).mock.calls[0]![2]!
    await diagnostics.openRun(2)
    old.resolve(detail(1))
    await first
    expect(signal.aborted).toBe(true)
    expect(diagnostics.detail.value?.run.id).toBe(2)
    const latestSignal = vi.mocked(piApi.diagnosticRun).mock.calls[1]![2]!
    apps.pop()!.unmount()
    expect(latestSignal.aborted).toBe(true)
  })

  it('新筛选取消旧列表请求，旧游标不能污染新结果', async () => {
    const old = deferred<{ items: PiRun[]; next_cursor: string }>()
    vi.mocked(piApi.diagnosticsRuns).mockReturnValueOnce(old.promise).mockResolvedValueOnce({ items: [run(2)] })
    const diagnostics = mount()
    diagnostics.token.value = '令牌'
    const first = diagnostics.loadRuns()
    const signal = vi.mocked(piApi.diagnosticsRuns).mock.calls[0]![2]!
    diagnostics.negativeOnly.value = true
    await diagnostics.loadRuns()
    old.resolve({ items: [run(3)], next_cursor: '3' })
    await first
    expect(signal.aborted).toBe(true)
    expect(diagnostics.runs.value.map(item => item.id)).toEqual([2])
    expect(diagnostics.nextCursor.value).toBe('')
  })

  it.each([401, 403])('明确显示 %s 鉴权失败，保留目标以便更换令牌重试', async code => {
    window.history.replaceState(null, '', '/ai/diagnostics/?run=42')
    vi.mocked(piApi.diagnosticsRuns).mockRejectedValue(new APIRequestError(code, 'unauthorized'))
    vi.mocked(piApi.diagnosticRun).mockRejectedValue(new APIRequestError(code, 'unauthorized'))
    const diagnostics = mount()
    diagnostics.token.value = '无效令牌'
    await diagnostics.loadRuns()
    await flush()
    expect(diagnostics.error.value).toContain(String(code))
    expect(diagnostics.detailError.value).toContain('系统设置')
    if (code === 403) expect(diagnostics.detailError.value).toContain('ai.diagnostics.read')
    expect(diagnostics.selectedID.value).toBe(42)
    expect(window.location.search).toBe('?run=42')
  })

  it('列表失败不阻止目标详情回放，详情失败不隐藏列表', async () => {
    window.history.replaceState(null, '', '/ai/diagnostics/?run=42')
    const diagnostics = mount()
    diagnostics.token.value = '令牌'
    vi.mocked(piApi.diagnosticsRuns).mockRejectedValueOnce(new Error('列表暂不可用'))
    await diagnostics.loadRuns()
    expect(diagnostics.detail.value?.run.id).toBe(42)
    expect(diagnostics.error.value).toBe('列表暂不可用')
    vi.mocked(piApi.diagnosticsRuns).mockResolvedValueOnce({ items: [run(8)] })
    vi.mocked(piApi.diagnosticRun).mockRejectedValueOnce(new Error('详情暂不可用'))
    await diagnostics.loadRuns()
    expect(diagnostics.runs.value[0]?.id).toBe(8)
    expect(diagnostics.detailError.value).toBe('详情暂不可用')
  })

  it('完整工具结果和原始消息按诊断协议保留，不做旧版摘要映射或截断', async () => {
    const response = detail(42)
    const fullText = '中文工具结果🙂'.repeat(1000) + '完整结果末尾'
    response.transcript = { run_id: 42, question: '问题', role: '开发', scope: { repo_mode: 'all', repo_ids: [], source_mode: 'all', file_types: [] }, status: 'succeeded', steps: [{ call_id: 'read-1', tool: 'mcp__docharbor__read', args: { path: 'doc/说明.md' }, summary: '首行摘要', is_error: false, preview: fullText }], answer: '# 最终回答', citations: [], usage: { input_tokens: 10, output_tokens: 5, cache_read_tokens: 0 }, feedback: null, messages: [{ role: 'user', content: '<context>原文</context>问题', unknown_details: { text: fullText } }] }
    vi.mocked(piApi.diagnosticRun).mockResolvedValueOnce(response)
    const diagnostics = mount()
    diagnostics.token.value = '令牌'
    await diagnostics.openRun(42)
    expect(diagnostics.detail.value?.transcript?.steps[0]?.preview).toBe(fullText)
    expect(diagnostics.detail.value?.transcript?.messages).toEqual(response.transcript?.messages)
  })

  it('存储被禁用时仍能用内存令牌读取，不退回 localStorage', async () => {
    vi.spyOn(window.sessionStorage, 'setItem').mockImplementation(() => { throw new Error('禁止存储') })
    const local = vi.fn()
    vi.stubGlobal('localStorage', { setItem: local })
    const diagnostics = mount()
    diagnostics.rememberToken.value = true
    diagnostics.token.value = '令牌'
    await diagnostics.loadRuns()
    expect(diagnostics.storageError.value).toContain('当前页面')
    expect(diagnostics.error.value).toBe('')
    expect(local).not.toHaveBeenCalled()
  })
})

describe('运行摘要', () => {
  it('覆盖全部 pi 状态，运行中不捏造耗时，token 包含缓存读取', () => {
    expect(diagnosticStatuses.map(diagnosticStatusLabel)).toEqual(['排队中', '运行中', '已完成', '已停止', '已超时', '失败'])
    expect(runDuration(run(1))).toBe('12.0 秒')
    expect(runDuration({ ...run(1), finished_at: '' })).toBe('—')
    expect(runDuration({ ...run(1), finished_at: 'invalid' })).toBe('—')
    expect(runTokens(run(1))).toBe(130)
  })
})
