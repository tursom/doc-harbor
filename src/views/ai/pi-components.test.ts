// @vitest-environment jsdom
import { createApp, h, nextTick, reactive, type App, type Component } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { piApi } from '../../api-pi'
import { usePiChat } from '../../composables/usePiChat'
import type { PiTurn } from '../../ai-pi-types'
import Composer from './Composer.vue'
import PiChat from './PiChat.vue'
import PiProcess from './PiProcess.vue'
import PiTurnItem from './PiTurnItem.vue'
import { ROLE_STORAGE_KEY } from './pi-chat'

let app: App | null
let container: HTMLDivElement
function mount(component: Component, props: Record<string, unknown>) {
  container = document.createElement('div')
  document.body.append(container)
  app = createApp({ render: () => h(component, props) })
  app.mount(container)
}
const scope = { repo_mode: 'global', repo_ids: [], source_mode: 'smart_latest', file_types: ['all'] }
beforeEach(() => {
  // Node 26 的原生 localStorage 可能遮住 jsdom，显式提供当前测试的浏览器存储接口。
  const saved = new Map<string, string>()
  vi.stubGlobal('localStorage', { getItem: (key: string) => saved.get(key) ?? null, setItem: (key: string, value: string) => saved.set(key, value), clear: () => saved.clear() })
  app = null
})
afterEach(() => { app?.unmount(); container?.remove(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('pi input and role controls', () => {
  it('sends Enter as a supplement while running, keeps Shift+Enter and IME input, and uses the button to stop', async () => {
    const send = vi.fn(); const stop = vi.fn()
    mount(Composer, { pi: true, role: '开发', question: '补充条件', scope: 'global', includeBranches: false, canAskFile: false, busy: true, streaming: true, onSend: send, onStop: stop })
    const textarea = container.querySelector('textarea')!
    expect(textarea.placeholder).toContain('补充说明')
    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', shiftKey: true, bubbles: true }))
    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', isComposing: true, bubbles: true }))
    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', keyCode: 229, bubbles: true }))
    textarea.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }))
    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    expect(send).not.toHaveBeenCalled()
    textarea.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true }))
    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    expect(send).toHaveBeenCalledTimes(1)
    container.querySelector<HTMLButtonElement>('[aria-label="停止生成"]')!.click()
    expect(stop).toHaveBeenCalledTimes(1)
    expect(send).toHaveBeenCalledTimes(1)
    await nextTick()
  })

  it('restores role from localStorage, offers three matching examples and fills rather than sends them', async () => {
    localStorage.setItem(ROLE_STORAGE_KEY, '产品')
    const updateQuestion = vi.fn()
    const stream = vi.spyOn(piApi, 'streamRun')
    mount(PiChat, { session: null, question: '', scope: 'global', canAskFile: false, enabled: true, contextLoading: false, diagnosticsHref: '/ai-diagnostics/', buildScope: () => scope, ensureSession: vi.fn(), 'onUpdate:question': updateQuestion })
    await nextTick()
    expect(container.querySelector('[aria-pressed="true"]')!.textContent).toBe('产品')
    const examples = container.querySelectorAll<HTMLButtonElement>('.pi-empty button')
    expect(examples).toHaveLength(3)
    expect(examples[0]!.textContent).toContain('秒杀库存人工归还')
    examples[0]!.click()
    expect(updateQuestion).toHaveBeenCalledWith('秒杀库存人工归还有哪些限制条件？')
    expect(stream).not.toHaveBeenCalled()
    const testing = [...container.querySelectorAll<HTMLButtonElement>('.role-row button')].find(b => b.textContent === '测试')!
    testing.click(); await nextTick()
    expect(localStorage.getItem(ROLE_STORAGE_KEY)).toBe('测试')
    expect(container.querySelectorAll('.pi-empty button')[0]!.textContent).toContain('CDK 激活失败')
    testing.click(); await nextTick()
    expect(localStorage.getItem(ROLE_STORAGE_KEY)).toBe('未指定')
  })
})

describe('pi process and feedback components', () => {
  it('expands the process during a run, closes on completion and keeps previews available', async () => {
    const turn = reactive<PiTurn>({ run_id: 42, question: '问题', role: '开发', scope, status: 'running', answer: '', citations: [], usage: { input_tokens: 0, output_tokens: 0, cache_read_tokens: 0 }, feedback: null, steps: [{ call_id: 'read-1', tool: 'mcp_docharbor_read', args: { path: 'doc/a.md' }, summary: '读取 20 行', preview: '第一行\n第二行', is_error: false }] })
    mount(PiProcess, { turn })
    const process = container.querySelector<HTMLDetailsElement>('.pi-process')!
    expect(process.open).toBe(true)
    expect(container.textContent).toContain('阅读')
    expect(container.querySelector('.tool-detail')!.textContent).toContain('第一行\n第二行')
    turn.status = 'succeeded'; await nextTick()
    expect(process.open).toBe(false)
    process.open = true; await nextTick()
    expect(process.open).toBe(true)
  })

  it('saves negative feedback immediately and permits an optional comment, with the correct diagnostic run link', async () => {
    const feedback = vi.spyOn(piApi, 'feedback').mockResolvedValue({ saved: true })
    const chat = usePiChat()
    const turn = reactive({ run_id: 42, question: '问题', role: '开发', scope, status: 'succeeded', steps: [], answer: '回答', citations: [], usage: { input_tokens: 1, output_tokens: 1, cache_read_tokens: 0 }, feedback: null } as PiTurn)
    mount(PiTurnItem, { turn, renderer: null, diagnosticsHref: '/ai-diagnostics/', canRetry: true, feedback: chat.feedback })
    container.querySelector<HTMLButtonElement>('[aria-label="没有帮助"]')!.click()
    await vi.waitFor(() => expect(container.querySelector('textarea')).not.toBeNull())
    expect(feedback).toHaveBeenCalledWith(42, { rating: -1 })
    const textarea = container.querySelector('textarea')!
    textarea.value = '缺少错误码'; textarea.dispatchEvent(new Event('input', { bubbles: true }))
    container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await vi.waitFor(() => expect(feedback).toHaveBeenLastCalledWith(42, { rating: -1, comment: '缺少错误码' }))
    expect(container.querySelector('a')!.getAttribute('href')).toBe('/ai-diagnostics/?run=42')
    expect(container.querySelector('a')!.title).toContain('系统设置')
    chat.disconnect()
  })
})
