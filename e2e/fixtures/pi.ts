import { expect, type Page } from '@playwright/test'
import type { PiCitation, PiStreamEvent, PiTurn } from '../../src/ai-pi-types'
import type { PiRunInput } from '../../src/api-pi'
import type { AISession } from '../../src/types'
import { aiSettings, mockRedesign, piTurn, sessions } from './redesign'

// 仅复用仓库、旧消息等虚构数据；每个测试独立创建 pi 会话和后台 transcript。
export const piSession: AISession = { ...sessions[1]!, id: 71, title: 'pi 发布检查', engine: 'pi' }
export const piCitations: PiCitation[] = structuredClone(piTurn.citations)
export const piAnswer = '发布前先确认健康检查。[' + piCitations[0]!.key + ']\n\n回滚条件仍需核对。[' + piCitations[1]!.key + ']'
export const piUsage = { input_tokens: 460, output_tokens: 110, cache_read_tokens: 100, cost: 0.0042 }

interface BrowserStreamControl {
  has: (runID: number) => boolean
  push: (runID: number, frame: string) => void
  end: (runID: number) => void
}
declare global {
  interface Window {
    __piE2E: BrowserStreamControl
    __piE2EClosed: (runID: number, reason: string) => Promise<void>
  }
}

/**
 * route.fulfill 只返回一次性 body，无法在流中测试补充和停止。
 * 这里只替换 POST runs 的响应体；请求先经过 originalFetch，由 Node 路由记账。
 * 其他 API 仍使用真实 fetch + Playwright route，未知请求由 mockRedesign 拒绝。
 */
export async function mockPi(page: Page) {
  const base = await mockRedesign(page)
  const state = {
    base,
    sessions: [structuredClone(piSession), ...sessions.map(session => ({ ...session, engine: 'legacy' as const }))],
    histories: new Map<number, PiTurn[]>([[piSession.id, []]]),
    requests: [] as { sessionID: number; runID: number; input: PiRunInput }[],
    steers: [] as { sessionID: number; text: string }[],
    aborts: [] as number[],
    feedbacks: [] as { runID: number; rating: number; comment?: string }[],
    transcriptReads: [] as number[],
    disconnects: [] as { runID: number; reason: string }[],
    created: [] as AISession[]
  }
  let nextRunID = 101
  const turnFor = (runID: number) => {
    const turn = [...state.histories.values()].flat().find(turn => turn.run_id === runID)
    if (!turn) throw new Error(`虚构 run #${runID} 尚未创建`)
    return turn
  }
  const latestID = () => {
    const request = state.requests.at(-1)
    if (!request) throw new Error('尚未发起 pi run')
    return request.runID
  }

  await page.exposeFunction('__piE2EClosed', (runID: number, reason: string) => {
    state.disconnects.push({ runID, reason })
  })
  await page.addInitScript(() => {
    const originalFetch = window.fetch.bind(window)
    const streams = new Map<number, ReadableStreamDefaultController<Uint8Array>>()
    window.__piE2E = {
      has: runID => streams.has(runID),
      push(runID, frame) {
        const stream = streams.get(runID)
        if (!stream) throw new Error(`SSE #${runID} 未连接`)
        const bytes = new TextEncoder().encode(frame)
        // 分开 event 行、JSON 和最后一个分隔符，也切开中文 UTF-8 字节。
        for (let offset = 0; offset < bytes.length; offset += 7) stream.enqueue(bytes.slice(offset, offset + 7))
      },
      end(runID) {
        streams.get(runID)?.close()
        streams.delete(runID)
      }
    }
    window.fetch = async (input, init) => {
      const url = new URL(input instanceof Request ? input.url : String(input), location.href)
      const method = init?.method || (input instanceof Request ? input.method : 'GET')
      if (!/^\/api\/ai\/sessions\/\d+\/runs$/.test(url.pathname) || method !== 'POST') return originalFetch(input, init)
      const response = await originalFetch(input, init)
      if (!response.ok) return response
      const { run_id: runID } = await response.json() as { run_id: number }
      const signal = init?.signal || (input instanceof Request ? input.signal : undefined)
      const aborted = () => {
        const stream = streams.get(runID)
        if (!stream) return
        streams.delete(runID)
        stream.error(new DOMException('浏览器停止接收 SSE', 'AbortError'))
        void window.__piE2EClosed(runID, 'signal_abort')
      }
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          streams.set(runID, controller)
          signal?.addEventListener('abort', aborted, { once: true })
          if (signal?.aborted) aborted()
        },
        cancel() {
          streams.delete(runID)
          signal?.removeEventListener('abort', aborted)
          void window.__piE2EClosed(runID, 'reader_cancel')
        }
      })
      return new Response(body, { headers: { 'Content-Type': 'text/event-stream' } })
    }
  })

  // 后注册的具体路由优先；其余 API 交回完整的基础 fixture。
  await page.route('**/api/ai/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
    if (path === '/api/ai/settings' && method === 'GET') return json({ ...aiSettings, engine: 'pi' })
    if (path === '/api/ai/sessions') {
      if (method === 'GET') return json({ items: state.sessions })
      if (method === 'POST') {
        const session = { ...piSession, id: 72 + state.created.length, title: request.postDataJSON().title }
        state.sessions.unshift(session)
        state.created.push(session)
        state.histories.set(session.id, [])
        return json(session, 201)
      }
    }
    const match = /^\/api\/ai\/sessions\/(\d+)(?:\/(transcript|runs|steer|abort))?$/.exec(path)
    if (match) {
      const sessionID = Number(match[1])
      const action = match[2]
      const session = state.sessions.find(session => session.id === sessionID)
      if (!action && method === 'GET' && session) return json(session)
      if (session?.engine === 'pi') {
        if (action === 'transcript' && method === 'GET') {
          state.transcriptReads.push(sessionID)
          return json({ turns: state.histories.get(sessionID) || [] })
        }
        if (action === 'runs' && method === 'POST') {
          const input = request.postDataJSON() as PiRunInput
          const runID = nextRunID++
          state.requests.push({ sessionID, runID, input })
          state.histories.get(sessionID)!.push({ run_id: runID, ...input, status: 'queued', steps: [], answer: '', citations: [],
            usage: { input_tokens: 0, output_tokens: 0, cache_read_tokens: 0 }, feedback: null, model: 'demo-chat-v1', provider_key: 'demo-primary' })
          return json({ run_id: runID })
        }
        if (action === 'steer' && method === 'POST') {
          const { text } = request.postDataJSON() as { text: string }
          state.steers.push({ sessionID, text })
          const turn = state.histories.get(sessionID)!.at(-1)!
          turn.supplements = [...(turn.supplements || []), text]
          return json({ accepted: true })
        }
        if (action === 'abort' && method === 'POST') {
          state.aborts.push(sessionID)
          // accepted 只确认收到命令；终态必须等测试推进 done，不假装同步完成。
          return json({ accepted: true })
        }
      }
    }
    const feedback = /^\/api\/ai\/runs\/(\d+)\/feedback$/.exec(path)
    if (feedback && method === 'POST') {
      const runID = Number(feedback[1])
      const payload = request.postDataJSON() as { rating: 1 | -1; comment?: string }
      state.feedbacks.push({ runID, ...payload })
      turnFor(runID).feedback = { rating: payload.rating, comment: payload.comment || '' }
      return json({ saved: true })
    }
    return route.fallback()
  })

  /** 后台先保存，再发送事件，保证前端 done 后发起的真实历史对账能读到结果。 */
  function persist(event: PiStreamEvent, runID: number) {
    const turn = turnFor(runID)
    switch (event.type) {
      case 'queued': turn.status = 'queued'; break
      case 'started': turn.status = 'running'; turn.started_at = '2026-10-21T03:40:00Z'; break
      case 'text': turn.answer += event.delta; break
      case 'tool_start': turn.steps.push({ call_id: event.call_id, tool: event.tool, args: event.args, summary: '', is_error: false, preview: '' }); break
      case 'tool_end': Object.assign(turn.steps.find(step => step.call_id === event.call_id)!, { summary: event.summary, is_error: event.is_error, preview: event.preview }); break
      case 'error': turn.status = 'failed'; turn.error_message = event.message; break
      case 'notice': break
      case 'done':
        Object.assign(turn, { status: event.status, citations: event.citations, usage: event.usage, error_message: event.message || turn.error_message,
          finished_at: '2026-10-21T03:40:02Z', duration_ms: 2000, tool_calls: turn.steps.length })
        break
    }
  }
  return Object.assign(state, {
    turn: turnFor,
    async connected(runID = latestID()) {
      await expect.poll(() => page.evaluate(id => window.__piE2E?.has(id) || false, runID)).toBe(true)
    },
    async emit(event: PiStreamEvent, runID = latestID()) {
      persist(event, runID)
      // 保留 event/data 分帧协议，直接驱动当前浏览器的 fetch reader。
      await page.evaluate(({ id, frame }) => window.__piE2E.push(id, frame), {
        id: runID, frame: `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`
      })
    },
    async start(runID = latestID()) {
      await this.connected(runID)
      await this.emit({ type: 'started', run_id: runID }, runID)
    },
    async finish(overrides: Partial<Extract<PiStreamEvent, { type: 'done' }>> = {}, runID = latestID()) {
      await this.emit({ type: 'done', run_id: runID, status: 'succeeded', citations: piCitations, usage: piUsage, ...overrides }, runID)
    },
    completeInBackground(runID = latestID()) {
      // 离页后的后台任务不会再通过旧浏览器连接发事件；返回页面需从历史恢复。
      turnFor(runID).answer = piAnswer
      persist({ type: 'done', status: 'succeeded', citations: piCitations, usage: piUsage }, runID)
    },
    async end(runID = latestID()) {
      await page.evaluate(id => window.__piE2E.end(id), runID)
    }
  })
}
