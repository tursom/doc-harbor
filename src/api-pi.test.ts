import { afterEach, describe, expect, it, vi } from 'vitest'
import { parsePiSSEBlock, piApi } from './api-pi'
import type { PiStreamEvent } from './ai-pi-types'

const input = { question: '接口参数', role: '开发' as const, scope: { repo_mode: 'global', repo_ids: [], source_mode: 'smart_latest', file_types: ['all'] } }
const frame = (type: string, data: unknown) => `event: ${type}\r\ndata: ${JSON.stringify(data)}\r\n\r\n`
const done = { status: 'succeeded', citations: [], usage: { input_tokens: 30, output_tokens: 10, cache_read_tokens: 0 } }
afterEach(() => vi.unstubAllGlobals())

describe('pi API transport', () => {
  it('keeps the outer event name when a retry notice contains its own type', () => {
    expect(parsePiSSEBlock('event: notice\ndata: {"type":"auto_retry_start","message":"正在重试"}')).toEqual({ type: 'notice', message: '正在重试' })
    expect(parsePiSSEBlock(': heartbeat')).toBeNull()
    expect(parsePiSSEBlock('event: unknown\ndata: {}')).toBeNull()
  })

  it('decodes split Unicode, CRLF and a tool event larger than 1 MB without losing the done event', async () => {
    const text = frame('queued', { position: 2 }) + frame('text', { delta: '中文答案' }) + frame('tool_end', { call_id: 'read', summary: '读取原文', preview: '大'.repeat(400000), is_error: false }) + frame('done', done)
    const bytes = new TextEncoder().encode(text)
    const fetchMock = vi.fn(async (_url: RequestInfo | URL, _options?: RequestInit) => new Response(new ReadableStream({ start(controller) {
      for (const [start, end] of [[0, 61], [61, 67], [67, 123], [123, bytes.length]]) controller.enqueue(bytes.slice(start, end))
      controller.close()
    } }), { headers: { 'Content-Type': 'text/event-stream' } }))
    vi.stubGlobal('fetch', fetchMock)
    const events: PiStreamEvent[] = []
    await piApi.streamRun(8, input, (event) => { events.push(event) })
    expect(events.map(event => event.type)).toEqual(['queued', 'text', 'tool_end', 'done'])
    expect(events[1]).toMatchObject({ delta: '中文答案' })
    expect(events[2]).toMatchObject({ preview: '大'.repeat(400000) })
    expect(fetchMock).toHaveBeenCalledWith('/api/ai/sessions/8/runs', expect.objectContaining({ method: 'POST', body: JSON.stringify(input) }))
  })

  it('preserves HTTP 409 for steering recovery and rejects an incomplete stream', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{"error":"已有运行"}', { status: 409 })))
    await expect(piApi.streamRun(8, input, vi.fn())).rejects.toMatchObject({ status: 409, message: '已有运行' })
    vi.stubGlobal('fetch', vi.fn(async () => new Response(frame('text', { delta: '部分回答' }), { headers: { 'Content-Type': 'text/event-stream' } })))
    await expect(piApi.streamRun(8, input, vi.fn())).rejects.toThrow('问答可能仍在继续')
  })

  it('uses distinct control and feedback routes and keeps diagnostics authorization out of the URL', async () => {
    const fetchMock = vi.fn(async (_url: RequestInfo | URL, _options?: RequestInit) => new Response('{}'))
    vi.stubGlobal('fetch', fetchMock)
    await piApi.steer(8, '补充说明')
    await piApi.abort(8)
    await piApi.feedback(9, { rating: -1, comment: '缺少参数' })
    await piApi.diagnosticsRuns('fictional-token', { rating: '-1', cursor: '4' })
    expect(fetchMock.mock.calls.map(call => String(call[0]))).toEqual([
      '/api/ai/sessions/8/steer', '/api/ai/sessions/8/abort', '/api/ai/runs/9/feedback', '/api/access/ai/runs?rating=-1&cursor=4'
    ])
    expect(fetchMock.mock.calls[3]?.[1]).toMatchObject({ headers: { Authorization: 'Bearer fictional-token' } })
  })
})
