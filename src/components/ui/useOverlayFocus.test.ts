// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'
import { useOverlayFocus } from './useOverlayFocus'

const apps: App[] = []

function mountOverlay() {
  const open = ref(false)
  const close = vi.fn(() => { open.value = false })
  const host = document.createElement('div')
  document.body.append(host)
  const app = createApp(defineComponent({
    setup() {
      const panel = ref<HTMLElement | null>(null)
      useOverlayFocus(() => open.value, panel, close)
      return () => open.value ? h('section', { ref: panel, tabindex: -1 }, [
        h('button', { class: 'first' }, '关闭'),
        h('input', { class: 'input', disabled: true }),
        h('input', { class: 'editable' }),
        h('button', { class: 'last' }, '保存')
      ]) : null
    }
  }))
  app.mount(host)
  apps.push(app)
  return { open, close, host }
}

async function flush() { await nextTick(); await nextTick() }
function key(key: string, shiftKey = false) {
  document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key, shiftKey, bubbles: true, cancelable: true }))
}

beforeEach(() => {
  // jsdom 不做布局；明确模拟可见尺寸，仍保留工具自身的 hidden/disabled 过滤。
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue([{}] as unknown as DOMRectList)
})
afterEach(() => {
  apps.reverse().forEach(app => app.unmount())
  apps.length = 0
  document.body.replaceChildren()
  document.body.style.overflow = ''
  vi.restoreAllMocks()
})

describe('浮层键盘与生命周期', () => {
  it('打开聚焦可编辑输入，Tab 双向循环，Esc 关闭并恢复触发焦点与滚动', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()
    document.body.style.overflow = 'auto'
    const overlay = mountOverlay()
    overlay.open.value = true
    await flush()
    expect(document.activeElement).toBe(overlay.host.querySelector('.editable'))
    expect(document.body.style.overflow).toBe('hidden')
    overlay.host.querySelector<HTMLButtonElement>('.last')!.focus()
    key('Tab')
    expect(document.activeElement).toBe(overlay.host.querySelector('.first'))
    key('Tab', true)
    expect(document.activeElement).toBe(overlay.host.querySelector('.last'))
    key('Escape')
    await flush()
    expect(overlay.close).toHaveBeenCalledOnce()
    expect(document.activeElement).toBe(trigger)
    expect(document.body.style.overflow).toBe('auto')
  })

  it('嵌套时 Esc 只关闭顶层，底层继续限制焦点与滚动', async () => {
    const parent = mountOverlay()
    const child = mountOverlay()
    parent.open.value = true
    await flush()
    child.open.value = true
    await flush()
    key('Escape')
    await flush()
    expect(child.close).toHaveBeenCalledOnce()
    expect(parent.close).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(parent.host.querySelector('.editable'))
    expect(document.body.style.overflow).toBe('hidden')
    key('Escape')
    await flush()
    expect(parent.close).toHaveBeenCalledOnce()
    expect(document.body.style.overflow).toBe('')
  })

  it('快速打开后关闭不会留下监听器或滚动锁', async () => {
    const overlay = mountOverlay()
    overlay.open.value = true
    overlay.open.value = false
    await flush()
    expect(document.body.style.overflow).toBe('')
    key('Escape')
    expect(overlay.close).not.toHaveBeenCalled()
  })
})
