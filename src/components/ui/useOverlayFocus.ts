import { nextTick, onBeforeUnmount, watch, type Ref } from 'vue'

const focusableSelector = 'a[href], button, input:not([type="hidden"]), select, textarea, [tabindex], [contenteditable="true"]'
const inputSelector = 'input:not([type="hidden"]), textarea, select, [contenteditable="true"]'
const overlays: symbol[] = []
let scrollLocks = 0
let previousOverflow = ''

/** 过滤隐藏、禁用以及负 tabindex 元素；每次按键重新收集，兼容动态表单。 */
export function focusableElements(panel: HTMLElement): HTMLElement[] {
  return Array.from(panel.querySelectorAll<HTMLElement>(focusableSelector)).filter(element =>
    element.tabIndex >= 0 && !element.matches(':disabled') &&
    !element.closest('[inert], [hidden]') && element.getClientRects().length > 0 &&
    getComputedStyle(element).visibility !== 'hidden'
  )
}

/**
 * 三种浮层共用一个栈：嵌套菜单只处理自己的 Esc/Tab，父抽屉继续保留。
 * DOM 访问延后到打开后，避免 Astro 服务端渲染时访问浏览器全局对象。
 */
export function useOverlayFocus(
  isOpen: () => boolean,
  panel: Ref<HTMLElement | null>,
  close: () => void,
  lockScroll = true
) {
  const key = Symbol('overlay')
  let active = false
  let revision = 0
  let returnFocus: HTMLElement | null = null
  const isTop = () => overlays.at(-1) === key

  function focusFirst() {
    if (!panel.value) return
    const elements = focusableElements(panel.value)
    const target = elements.find(element => element.matches(inputSelector)) ?? elements[0] ?? panel.value
    target.focus({ preventScroll: true })
  }

  function onKeydown(event: KeyboardEvent) {
    if (!isTop() || !panel.value) return
    if (event.key === 'Escape' && !event.isComposing) {
      event.preventDefault()
      // 父浮层的监听器不能再次消费同一次 Esc。
      event.stopImmediatePropagation()
      close()
      return
    }
    if (event.key !== 'Tab') return
    const elements = focusableElements(panel.value)
    const first = elements[0]
    const last = elements.at(-1)
    const current = document.activeElement
    if (!first || !last) {
      event.preventDefault()
      panel.value.focus({ preventScroll: true })
    } else if (!panel.value.contains(current) || current === panel.value) {
      event.preventDefault()
      ;(event.shiftKey ? last : first).focus()
    } else if (event.shiftKey && current === first) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && current === last) {
      event.preventDefault()
      first.focus()
    }
  }

  function onFocusin(event: FocusEvent) {
    if (isTop() && panel.value && !panel.value.contains(event.target as Node)) focusFirst()
  }

  function deactivate() {
    if (!active) return
    const restore = isTop()
    active = false
    const index = overlays.indexOf(key)
    if (index >= 0) overlays.splice(index, 1)
    document.removeEventListener('keydown', onKeydown, true)
    document.removeEventListener('focusin', onFocusin, true)
    if (lockScroll && --scrollLocks === 0) document.body.style.overflow = previousOverflow
    // 若父面板也已移除，不把焦点放回脱离文档的按钮。
    if (restore && returnFocus?.isConnected) returnFocus.focus({ preventScroll: true })
    returnFocus = null
  }

  watch(isOpen, async open => {
    const currentRevision = ++revision
    if (typeof document === 'undefined') return
    if (!open) {
      deactivate()
      return
    }
    returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    await nextTick()
    if (currentRevision !== revision || !isOpen() || !panel.value) return
    active = true
    overlays.push(key)
    if (lockScroll && scrollLocks++ === 0) {
      previousOverflow = document.body.style.overflow
      document.body.style.overflow = 'hidden'
    }
    document.addEventListener('keydown', onKeydown, true)
    document.addEventListener('focusin', onFocusin, true)
    focusFirst()
  }, { immediate: true, flush: 'post' })

  onBeforeUnmount(() => {
    revision++
    deactivate()
  })

  return { isTop }
}
