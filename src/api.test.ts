import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'

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
