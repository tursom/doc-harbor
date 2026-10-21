// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, type App } from 'vue'
import { useTheme } from '../../composables/useTheme'

const apps: App[] = []
let change: ((event: { matches: boolean }) => void) | undefined
const remove = vi.fn()

function mountTheme() {
  let state!: ReturnType<typeof useTheme>
  const host = document.createElement('div')
  document.body.append(host)
  const app = createApp(defineComponent({ setup() { state = useTheme(); return () => h('div') } }))
  app.mount(host)
  apps.push(app)
  return state
}

beforeEach(() => {
  // Node 自带的 localStorage 与 jsdom 版本可能不同，测试明确提供浏览器存储接口。
  const saved = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => saved.get(key) ?? null,
    setItem: (key: string, value: string) => { saved.set(key, value) }
  })
  document.documentElement.removeAttribute('data-theme')
  vi.stubGlobal('matchMedia', vi.fn(() => ({
    matches: true,
    addEventListener: (_: string, listener: typeof change) => { change = listener },
    removeEventListener: remove
  })))
})
afterEach(() => {
  apps.forEach(app => app.unmount())
  apps.length = 0
  document.body.replaceChildren()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('共享主题', () => {
  it('跟随系统变化，显式主题优先，并同步多个实例', () => {
    const first = mountTheme()
    const second = mountTheme()
    expect(first.theme.value).toBe('system')
    expect(first.isDark.value).toBe(true)
    expect(window.matchMedia).toHaveBeenCalledTimes(1)
    first.setTheme('light')
    expect(second.theme.value).toBe('light')
    expect(second.isDark.value).toBe(false)
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(localStorage.getItem('docharbor-theme')).toBe('light')
    first.setTheme('system')
    change?.({ matches: false })
    expect(first.isDark.value).toBe(false)
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
  })

  it('读取持久化偏好，处理跨标签更新与删除', () => {
    localStorage.setItem('docharbor-theme', 'light')
    const state = mountTheme()
    expect(state.theme.value).toBe('light')
    window.dispatchEvent(new StorageEvent('storage', { key: 'docharbor-theme', newValue: 'dark' }))
    expect(state.isDark.value).toBe(true)
    window.dispatchEvent(new StorageEvent('storage', { key: 'docharbor-theme', newValue: null }))
    expect(state.theme.value).toBe('system')
  })

  it('localStorage 读写被拒绝时仍能切换主题', () => {
    vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    vi.spyOn(window.localStorage, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    const state = mountTheme()
    expect(() => state.setTheme('dark')).not.toThrow()
    expect(state.isDark.value).toBe(true)
    expect(document.documentElement.dataset.theme).toBe('dark')
  })
})
