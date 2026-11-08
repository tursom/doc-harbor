import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'

describe('后台扫描请求契约', () => {
  afterEach(() => vi.unstubAllGlobals())
  it('202 后立即返回任务，明确区分普通增量、完整重建和取消', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ id: 91, status: 'running' }), { status: 202 }))
    vi.stubGlobal('fetch', fetchMock)
    expect(await api.scanRepo(7)).toMatchObject({ status: 'running' })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/repos/7/scan', expect.objectContaining({ method: 'POST', body: '{"force":false}' }))
    await api.scanRepo(7, true)
    expect(fetchMock).toHaveBeenLastCalledWith('/api/repos/7/scan', expect.objectContaining({ body: '{"force":true}' }))
    await api.cancelScan(7)
    expect(fetchMock).toHaveBeenLastCalledWith('/api/repos/7/scan/cancel', expect.objectContaining({ method: 'POST' }))
  })
})

describe('AI stream transport', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('passes through AbortSignal and rejects when the caller cancels the stream', async () => {
    const fetchMock = vi.fn((_url: string, options: RequestInit = {}) => {
      return new Promise<Response>((_resolve, reject) => {
        options.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
      })
    })
    vi.stubGlobal('fetch', fetchMock)
    const controller = new AbortController()
    const result = api.streamAI(
      9,
      'question',
      { repo_mode: 'global', repo_ids: [], source_mode: 'smart_latest', file_types: ['all'] },
      vi.fn(),
      controller.signal
    )

    controller.abort()

    await expect(result).rejects.toMatchObject({ name: 'AbortError' })
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/ai/sessions/9/messages/stream',
      expect.objectContaining({ signal: controller.signal })
    )
  })
})
