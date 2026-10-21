// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { piApi } from '../api-pi'
import { APIRequestError } from '../api'
import type { PiStreamEvent, PiTurn } from '../ai-pi-types'
import { usePiChat } from './usePiChat'

const scope = { repo_mode: 'global', repo_ids: [], source_mode: 'smart_latest', file_types: ['all'] }
const usage = { input_tokens: 12, output_tokens: 6, cache_read_tokens: 0 }
function turn(status: PiTurn['status'] = 'running', id = 1): PiTurn {
  return { run_id: id, question: '原问题', role: '测试', scope, status, steps: [], answer: '', citations: [], usage, feedback: null }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
let chat: ReturnType<typeof usePiChat>
beforeEach(() => {
  chat = usePiChat()
  vi.spyOn(piApi, 'transcript').mockImplementation(async () => ({ turns: [] }))
  vi.spyOn(piApi, 'abort').mockResolvedValue({ accepted: true })
  vi.spyOn(piApi, 'steer').mockResolvedValue({ accepted: true })
  vi.spyOn(piApi, 'feedback').mockResolvedValue({ saved: true })
})
afterEach(() => { chat.disconnect(); vi.restoreAllMocks(); vi.useRealTimers() })

describe('pi run lifecycle', () => {
  it('keeps sending blocked until the final transcript reconciliation settles', async () => {
    await chat.openSession(7)
    const history = deferred<{ turns: PiTurn[] }>()
    vi.mocked(piApi.transcript).mockReturnValueOnce(history.promise)
    const stream = vi.spyOn(piApi, 'streamRun').mockImplementation(async (_id, _input, emit) => {
      await emit({ type: 'started', run_id: 42 })
      await emit({ type: 'done', run_id: 42, status: 'succeeded', citations: [], usage })
    })
    const first = chat.sendQuestion('第一问', '开发', scope)
    await vi.waitFor(() => expect(chat.turns.value[0]?.status).toBe('succeeded'))
    expect(chat.busy.value).toBe(true)
    await chat.sendQuestion('第二问', '开发', scope)
    expect(stream).toHaveBeenCalledTimes(1)
    history.resolve({ turns: [{ ...turn('succeeded', 42), question: '第一问' }] })
    await first
    expect(chat.busy.value).toBe(false)
    await chat.sendQuestion('第二问', '开发', scope)
    expect(stream).toHaveBeenCalledTimes(2)
  })

  it('updates the current feedback object when history is replaced during save', async () => {
    vi.mocked(piApi.transcript).mockResolvedValue({ turns: [turn('succeeded', 42)] })
    await chat.openSession(7)
    const pending = deferred<{ saved: boolean }>()
    vi.mocked(piApi.feedback).mockReturnValueOnce(pending.promise)
    const oldTurn = chat.turns.value[0]!
    const saving = chat.feedback(oldTurn, -1, '缺少异常分支')
    await chat.refresh()
    expect(chat.turns.value[0]).not.toBe(oldTurn)
    pending.resolve({ saved: true })
    expect(await saving).toBe(true)
    expect(chat.turns.value[0]?.feedback).toEqual({ rating: -1, comment: '缺少异常分支' })
  })

  it('restores persisted supplements after switching away and reopening', async () => {
    vi.mocked(piApi.transcript).mockResolvedValue({ turns: [{ ...turn('succeeded', 42), supplements: ['只看库存逻辑', '包含异常情况'] }] })
    await chat.openSession(7)
    expect(chat.turns.value[0]?.supplements).toEqual(['只看库存逻辑', '包含异常情况'])
    await chat.openSession(null)
    await chat.openSession(7)
    expect(chat.turns.value[0]?.supplements).toEqual(['只看库存逻辑', '包含异常情况'])
  })

  it('turns a raced HTTP 409 into a supplement for the server active run', async () => {
    await chat.openSession(7)
    vi.mocked(piApi.transcript).mockResolvedValue({ turns: [turn('running', 42)] })
    vi.spyOn(piApi, 'streamRun').mockRejectedValue(new APIRequestError(409, '同一会话已有运行中的问题'))
    await chat.sendQuestion('补充新的条件', '开发', scope)
    expect(piApi.steer).toHaveBeenCalledExactlyOnceWith(7, '补充新的条件')
    expect(chat.turns.value).toHaveLength(1)
    expect(chat.activeTurn.value?.run_id).toBe(42)
    expect(chat.activeTurn.value?.supplements).toEqual(['补充新的条件'])
  })

  it('keeps the composer busy until done even after receiving an error event', async () => {
    await chat.openSession(7)
    const pending = deferred<void>()
    const stream = vi.spyOn(piApi, 'streamRun').mockImplementation(async (_id, _input, emit) => { await emit({ type: 'error', message: '模型错误' }); await pending.promise })
    const first = chat.sendQuestion('第一个问题', '测试', scope)
    await vi.waitFor(() => expect(chat.turns.value[0]?.status).toBe('failed'))
    expect(chat.busy.value).toBe(true)
    await chat.sendQuestion('不能并发的第二个问题', '测试', scope)
    expect(stream).toHaveBeenCalledTimes(1)
    pending.resolve(); await first
    expect(chat.busy.value).toBe(false)
  })
  it('renders queue → tools → text → done, preserves protocol data and reads final metadata', async () => {
    await chat.openSession(7)
    vi.mocked(piApi.transcript).mockResolvedValue({ turns: [{ ...turn('succeeded', 42), answer: '回答', model: 'deepseek', duration_ms: 32000 }] })
    vi.spyOn(piApi, 'streamRun').mockImplementation(async (_id, _input, emit) => {
      await emit({ type: 'queued', position: 2 })
      expect(chat.activeTurn.value?.queue_position).toBe(2)
      await emit({ type: 'started', run_id: 42 })
      await emit({ type: 'tool_start', call_id: 'read-1', tool: 'read', args: { path: 'a.go' } })
      expect(chat.turns.value[0]!.steps[0]!.running).toBe(true)
      await emit({ type: 'tool_end', call_id: 'read-1', summary: '读取 20 行', is_error: false, preview: '完整预览' })
      await emit({ type: 'text', delta: '回' }); await emit({ type: 'text', delta: '答' })
      expect(chat.turns.value[0]!.answer).toBe('回答')
      expect(chat.turns.value[0]!.steps[0]).toMatchObject({ summary: '读取 20 行', preview: '完整预览', running: false })
      await emit({ type: 'done', status: 'succeeded', citations: [], usage })
    })
    await chat.sendQuestion('原问题', '测试', scope)
    expect(piApi.streamRun).toHaveBeenCalledWith(7, { question: '原问题', role: '测试', scope }, expect.any(Function), expect.any(AbortSignal))
    expect(chat.turns.value[0]).toMatchObject({ run_id: 42, model: 'deepseek', duration_ms: 32000 })
    expect(chat.busy.value).toBe(false)
  })

  it('reopens running history and polls through read errors until a terminal status', async () => {
    vi.useFakeTimers()
    vi.mocked(piApi.transcript).mockResolvedValueOnce({ turns: [turn()] }).mockRejectedValueOnce(new Error('temporary')).mockResolvedValueOnce({ turns: [{ ...turn('succeeded'), answer: '后台已完成' }] })
    await chat.openSession(7)
    expect(chat.busy.value).toBe(true)
    await vi.advanceTimersByTimeAsync(1500)
    expect(chat.error.value).toContain('temporary')
    await vi.advanceTimersByTimeAsync(1500)
    expect(chat.turns.value[0]!.answer).toBe('后台已完成')
    expect(chat.busy.value).toBe(false)
    await vi.advanceTimersByTimeAsync(5000)
    expect(piApi.transcript).toHaveBeenCalledTimes(3)
  })

  it('uses steer for supplements, never starts a second run, and calls abort only on explicit stop', async () => {
    vi.mocked(piApi.transcript).mockResolvedValue({ turns: [turn()] })
    const stream = vi.spyOn(piApi, 'streamRun')
    await chat.openSession(7)
    expect(await chat.steer('补充条件')).toBe(true)
    expect(piApi.steer).toHaveBeenCalledWith(7, '补充条件')
    expect(chat.activeTurn.value?.supplements).toEqual(['补充条件'])
    await chat.sendQuestion('不能并发', '开发', scope)
    expect(stream).not.toHaveBeenCalled()
    await chat.stop()
    expect(piApi.abort).toHaveBeenCalledExactlyOnceWith(7)
    chat.disconnect()
    expect(piApi.abort).toHaveBeenCalledTimes(1)
  })

  it('disconnects SSE on navigation without abort and ignores all late events', async () => {
    await chat.openSession(7)
    let emit!: (event: PiStreamEvent) => void | Promise<void>
    let signal!: AbortSignal
    const transport = deferred<void>()
    vi.spyOn(piApi, 'streamRun').mockImplementation(async (_id, _input, callback, abortSignal) => {
      emit = callback; signal = abortSignal!
      await callback({ type: 'started', run_id: 42 })
      await transport.promise
    })
    const sending = chat.sendQuestion('原问题', '开发', scope)
    await vi.waitFor(() => expect(chat.activeTurn.value?.run_id).toBe(42))
    await chat.openSession(8)
    expect(signal.aborted).toBe(true)
    expect(piApi.abort).not.toHaveBeenCalled()
    await emit({ type: 'text', delta: '不能串进新会话' })
    transport.resolve(); await sending
    expect(chat.turns.value).toEqual([])
  })

  it('recovers a disconnected stream by polling rather than marking a running server task failed', async () => {
    vi.useFakeTimers()
    await chat.openSession(7)
    vi.mocked(piApi.transcript).mockResolvedValueOnce({ turns: [turn('running', 42)] }).mockResolvedValueOnce({ turns: [{ ...turn('timeout', 42), error_message: '已超时' }] })
    vi.spyOn(piApi, 'streamRun').mockImplementation(async (_id, _input, emit) => { await emit({ type: 'started', run_id: 42 }); throw new Error('断网') })
    await chat.sendQuestion('原问题', '测试', scope)
    expect(chat.activeTurn.value?.run_id).toBe(42)
    expect(piApi.abort).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1500)
    expect(chat.turns.value[0]!.status).toBe('timeout')
    expect(chat.busy.value).toBe(false)
  })

  it('ignores a stale transcript that resolves after another session is opened', async () => {
    const slow = deferred<{ turns: PiTurn[] }>()
    vi.mocked(piApi.transcript).mockImplementation(id => id === 7 ? slow.promise : Promise.resolve({ turns: [turn('succeeded', 8)] }))
    const first = chat.openSession(7)
    await chat.openSession(8)
    slow.resolve({ turns: [turn('running', 7)] }); await first
    expect(chat.turns.value.map(t => t.run_id)).toEqual([8])
  })

  it('retains retryable errors before a run is accepted and does not fake successful feedback', async () => {
    await chat.openSession(7)
    vi.spyOn(piApi, 'streamRun').mockRejectedValue(new Error('模型不可用'))
    await chat.sendQuestion('原问题', '开发', scope)
    expect(chat.turns.value[0]).toMatchObject({ status: 'failed', error_message: '模型不可用', question: '原问题' })
    const savedTurn = turn('succeeded', 42)
    vi.mocked(piApi.feedback).mockRejectedValueOnce(new Error('反馈保存失败'))
    expect(await chat.feedback(savedTurn, -1, '缺少错误码')).toBe(false)
    expect(savedTurn.feedback).toBeNull()
    expect(await chat.feedback(savedTurn, -1, '缺少错误码')).toBe(true)
    expect(piApi.feedback).toHaveBeenLastCalledWith(42, { rating: -1, comment: '缺少错误码' })
    expect(savedTurn.feedback).toMatchObject({ rating: -1, comment: '缺少错误码' })
  })
})
