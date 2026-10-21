// @vitest-environment jsdom
import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api'
import { piApi } from '../api-pi'
import type { AIMessage, AIMessageCitation, AISettingsSummary, AIStreamEvent, AISession, FileContent, Repository } from '../types'
import { useAIChat } from './useAIChat'

function session(id: number): AISession {
  return { id, title: `会话 ${id}`, viewer_key: '', scope_json: '{}', created_at: '', updated_at: '', archived_at: '' }
}
function message(id: number, sessionID: number, content = '', status = 'completed'): AIMessage {
  return { id, session_id: sessionID, role: 'assistant', content, status, model: 'test-model', provider_name: 'test-provider', model_route_json: '', prompt_tokens: 0, completion_tokens: 0, latency_ms: 0, error_message: '', created_at: '' }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}
const emptyMessages = { items: [], citations: [], service_candidates: [] }
let chat: ReturnType<typeof useAIChat>
beforeEach(() => {
  window.history.replaceState(null, '', '/ai/')
  const repo = { id: 7, name: 'Docs' } as Repository
  chat = useAIChat(ref([repo]), ref(repo))
  vi.spyOn(api, 'aiSettings').mockResolvedValue({ enabled: true, engine: 'legacy' } as AISettingsSummary)
  vi.spyOn(piApi, 'session').mockImplementation(async id => session(id))
  vi.spyOn(api, 'aiSessions').mockResolvedValue({ items: [session(1), session(2)] })
  // 每次 HTTP 响应都是新数组，测试桩也保持这一点，避免一次流式 push 污染下一会话。
  vi.spyOn(api, 'aiMessages').mockImplementation(async () => ({ items: [], citations: [], service_candidates: [] }))
})
afterEach(() => {
  chat.stopGeneration()
  vi.restoreAllMocks()
})

describe('AI URL restoration', () => {
  it('restores explicit sessions outside the first history page and current-file scope without sending', async () => {
    window.history.replaceState(null, '', '/ai/?repo=7&version=42&scope=file&session=99&question=解释接口')
    vi.spyOn(api, 'content').mockResolvedValue({ repo_id: 7, version_id: 42, branch: 'release', source_commit_sha: 'abcdef', file_path: 'doc/api.md', file_size: 200 } as FileContent)
    const stream = vi.spyOn(api, 'streamAI')
    await chat.initialize()
    expect(piApi.session).toHaveBeenCalledWith(99)
    expect(api.aiMessages).toHaveBeenCalledWith(99)
    expect(chat.selectedSession.value?.id).toBe(99)
    expect(chat.question.value).toBe('解释接口')
    expect(chat.buildScope()).toMatchObject({ repo_mode: 'current_file', repo_ids: [7], current_file: { version_id: 42, file_path: 'doc/api.md', branch: 'release' } })
    expect(stream).not.toHaveBeenCalled()
  })

  it('preserves the 1 MiB current-file limit and keeps URL questions in global scope', async () => {
    window.history.replaceState(null, '', '/ai/?repo=7&version=42&scope=file&question=提问')
    vi.spyOn(api, 'content').mockResolvedValue({ file_size: 1024 * 1024 + 1 } as FileContent)
    await chat.initialize()
    expect(chat.canAskWithCurrentFile.value).toBe(false)
    expect(chat.scopeMode.value).toBe('global')
    expect(chat.question.value).toBe('提问')
  })

  it('restores repository scope and rejects raw HTML before rendering Markdown', async () => {
    window.history.replaceState(null, '', '/ai/?repo=7&scope=repo&question=test')
    await chat.initialize()
    expect(chat.buildScope()).toMatchObject({ repo_mode: 'current_repo', repo_ids: [7] })
    expect(chat.markdownRenderer.value?.('<img src=x onerror="alert(1)"><script>alert(2)</script>')).not.toMatch(/onerror|<script/)
  })
})

describe('AI request lifecycle', () => {
  it('reads the real engine for URL sessions outside the history window without requesting legacy messages', async () => {
    window.history.replaceState(null, '', '/ai/?session=99')
    vi.mocked(piApi.session).mockResolvedValue({ ...session(99), engine: 'pi' })
    await chat.initialize()
    expect(chat.isPi.value).toBe(true)
    expect(chat.selectedSession.value?.engine).toBe('pi')
    expect(api.aiMessages).not.toHaveBeenCalled()
    await chat.refresh()
    expect(api.aiMessages).not.toHaveBeenCalled()
  })

  it('routes pi selection away from legacy messages and enforces legacy read-only after config switches', async () => {
    await chat.initialize()
    vi.mocked(api.aiMessages).mockClear()
    await chat.selectSession({ ...session(3), engine: 'pi' })
    expect(api.aiMessages).not.toHaveBeenCalled()
    chat.settings.value = { enabled: true, engine: 'pi' } as AISettingsSummary
    await chat.selectSession({ ...session(1), engine: 'legacy' })
    expect(chat.isPi.value).toBe(false)
    expect(chat.legacyReadOnly.value).toBe(true)
    const stream = vi.spyOn(api, 'streamAI')
    chat.question.value = '不可发送'
    await chat.sendQuestion()
    expect(stream).not.toHaveBeenCalled()
    expect(chat.question.value).toBe('不可发送')
  })

  it('does not offer local retrieval when AI is disabled', async () => {
    await chat.initialize()
    chat.settings.value!.enabled = false
    const stream = vi.spyOn(api, 'streamAI')
    chat.question.value = '不可发送'
    await chat.sendQuestion()
    expect(stream).not.toHaveBeenCalled()
  })

  it('ignores stale session reads after a faster selection', async () => {
    const first = deferred<typeof emptyMessages>()
    vi.mocked(api.aiMessages).mockImplementation((id) => id === 1 ? first.promise : Promise.resolve({ ...emptyMessages, items: [message(22, 2, 'new')] }))
    const slow = chat.selectSession(session(1))
    await chat.selectSession(session(2))
    first.resolve({ ...emptyMessages, items: [] })
    await slow
    expect(chat.messages.value.map((item) => item.content)).toEqual(['new'])
    expect(new URLSearchParams(window.location.search).get('session')).toBe('2')
  })

  it('stops transport and ignores buffered old events after changing sessions', async () => {
    await chat.initialize()
    let emit!: (event: AIStreamEvent) => void | Promise<void>
    let signal: AbortSignal | undefined
    const transport = deferred<void>()
    vi.spyOn(api, 'streamAI').mockImplementation(async (_id, _text, _scope, callback, abortSignal) => {
      emit = callback
      signal = abortSignal
      await callback({ type: 'answer_delta', message_id: 11, delta: 'partial' })
      await transport.promise
    })
    chat.question.value = '问题'
    const sending = chat.sendQuestion()
    await vi.waitFor(() => expect(chat.messages.value[0]?.content).toBe('partial'))
    chat.stopGeneration()
    expect(signal?.aborted).toBe(true)
    expect(chat.messages.value[0]?.status).toBe('stopped')
    expect(chat.streaming.value).toBe(false)
    await chat.selectSession(session(2))
    await emit({ type: 'answer_delta', message_id: 11, delta: 'late event' })
    transport.resolve()
    await sending
    expect(chat.selectedSession.value?.id).toBe(2)
    expect(chat.messages.value).toEqual([])
  })

  it('preserves older citation mappings when a follow-up streams and completes', async () => {
    const old = { id: 10, message_id: 11, file_path: 'old.md' } as AIMessageCitation
    vi.mocked(api.aiMessages).mockResolvedValue({ ...emptyMessages, items: [message(11, 1, '[C1]')], citations: [old] })
    await chat.initialize()
    const recent = { id: 12, message_id: 13, file_path: 'new.md' } as AIMessageCitation
    vi.spyOn(api, 'streamAI').mockImplementation(async (_id, _text, _scope, emit) => {
      await emit({ type: 'citations', items: [recent] })
      await emit({ type: 'answer_delta', message_id: 13, delta: '回答 [C1]' })
      await emit({ type: 'message_done', message: message(13, 1, '回答 [C1]'), citations: [recent], service_candidates: [], run: {} } as AIStreamEvent)
    })
    chat.question.value = '追问'
    await chat.sendQuestion()
    expect(chat.numberedCitations.value.map((item) => [item.key, item.number])).toEqual([['13:12', 1], ['11:10', 1]])
    expect(chat.messages.value[1]?.content).toBe('回答 [C1]')
    expect(chat.streaming.value).toBe(false)
  })

  it('does not remain generating after EOF without message_done', async () => {
    await chat.initialize()
    vi.spyOn(api, 'streamAI').mockImplementation(async (_id, _text, _scope, emit) => {
      await emit({ type: 'answer_delta', message_id: 11, delta: '未完成' })
    })
    chat.question.value = '问题'
    await chat.sendQuestion()
    expect(chat.messages.value[0]?.status).toBe('partial')
    expect(chat.streaming.value).toBe(false)
  })

  it('does not send after cancellation while creating the first session', async () => {
    vi.mocked(api.aiSessions).mockResolvedValue({ items: [] })
    await chat.initialize()
    const creation = deferred<AISession>()
    vi.spyOn(api, 'createAISession').mockReturnValue(creation.promise)
    const stream = vi.spyOn(api, 'streamAI')
    chat.question.value = '保留问题'
    const sending = chat.sendQuestion()
    await vi.waitFor(() => expect(api.createAISession).toHaveBeenCalled())
    chat.stopGeneration()
    creation.resolve(session(3))
    await sending
    expect(stream).not.toHaveBeenCalled()
    expect(chat.question.value).toBe('保留问题')
    expect(chat.selectedSession.value).toBeNull()
  })
})
