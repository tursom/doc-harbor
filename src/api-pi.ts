import { request, throwAPIError } from './api'
import type { AIQuestionScope, AISession } from './types'
import type { PiDiagnosticDetail, PiFeedback, PiRole, PiRun, PiStreamEvent, PiTurn } from './ai-pi-types'

export interface PiRunInput { question: string; role: PiRole; scope: AIQuestionScope }

/** SSE 事件名由 event: 行决定；notice 的数据也可能有 type，不允许覆盖外层协议类型。 */
export function parsePiSSEBlock(block: string): PiStreamEvent | null {
  let name = ''
  const lines: string[] = []
  for (const line of block.split(/\r?\n/)) {
    if (line.startsWith('event:')) name = line.slice(6).trim()
    if (line.startsWith('data:')) lines.push(line.slice(5).replace(/^ /, ''))
  }
  if (!name || !lines.length) return null
  if (!['queued', 'started', 'text', 'tool_start', 'tool_end', 'notice', 'done', 'error'].includes(name)) return null
  const payload: unknown = JSON.parse(lines.join('\n'))
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) throw new Error('问答事件格式无效')
  return { ...payload, type: name } as PiStreamEvent
}

export const piApi = {
  session(id: number, signal?: AbortSignal) {
    return request<AISession>(`/api/ai/sessions/${id}`, { signal })
  },
  transcript(id: number, signal?: AbortSignal) {
    return request<{ turns: PiTurn[] }>(`/api/ai/sessions/${id}/transcript`, { signal })
  },
  steer(id: number, text: string) {
    return request<{ accepted: boolean }>(`/api/ai/sessions/${id}/steer`, { method: 'POST', body: JSON.stringify({ text }) })
  },
  abort(id: number) {
    return request<{ accepted: boolean }>(`/api/ai/sessions/${id}/abort`, { method: 'POST', body: '{}' })
  },
  feedback(id: number, feedback: Pick<PiFeedback, 'rating'> & { comment?: string }) {
    return request<{ saved: boolean }>(`/api/ai/runs/${id}/feedback`, { method: 'POST', body: JSON.stringify(feedback) })
  },
  diagnosticsRuns(token: string, params: Record<string, string> = {}, signal?: AbortSignal) {
    const query = new URLSearchParams(params)
    return request<{ items: PiRun[]; next_cursor?: string }>(`/api/access/ai/runs${query.size ? `?${query}` : ''}`, {
      signal, headers: { Authorization: `Bearer ${token}` }
    })
  },
  diagnosticRun(token: string, id: number, signal?: AbortSignal) {
    return request<PiDiagnosticDetail>(`/api/access/ai/runs/${id}`, { signal, headers: { Authorization: `Bearer ${token}` } })
  },
  async streamRun(id: number, input: PiRunInput, onEvent: (event: PiStreamEvent) => void | Promise<void>, signal?: AbortSignal) {
    const response = await fetch(`/api/ai/sessions/${id}/runs`, {
      method: 'POST', signal, headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' }, body: JSON.stringify(input)
    })
    if (!response.ok) await throwAPIError(response)
    if (!response.body || !response.headers.get('Content-Type')?.startsWith('text/event-stream')) throw new Error('问答接口未返回事件流')
    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let completed = false
    const dispatch = async (block: string) => {
      const event = parsePiSSEBlock(block)
      if (event) { await onEvent(event); if (event.type === 'done') completed = true }
    }
    try {
      while (!completed) {
        const { value, done } = await reader.read()
        if (done) {
          buffer += decoder.decode()
          if (buffer.trim()) await dispatch(buffer)
          break
        }
        buffer += decoder.decode(value, { stream: true })
        const blocks = buffer.split(/\r?\n\r?\n/)
        buffer = blocks.pop() || ''
        for (const block of blocks) { await dispatch(block); if (completed) break }
      }
      if (!completed) throw new Error('连接已中断，问答可能仍在继续，请刷新会话查看结果')
    } finally {
      // 断开接收仅结束浏览器订阅；主动停止由独立 abort API 完成。
      try { await reader.cancel() } catch { /* 网络断开时无需再次覆盖原始错误。 */ }
      reader.releaseLock()
    }
  }
}
