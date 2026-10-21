import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

export type Theme = 'system' | 'light' | 'dark'
const storageKey = 'docharbor-theme'
// 同一页面中的侧栏、Markdown/Mermaid 共用状态，避免各实例各自监听系统设置。
const theme = ref<Theme>('system')
const systemDark = ref(false)
const isDark = computed(() => theme.value === 'dark' || (theme.value === 'system' && systemDark.value))
let users = 0
let media: MediaQueryList | undefined

function parseTheme(value: string | null): Theme {
  return value === 'light' || value === 'dark' ? value : 'system'
}

function applyTheme() {
  if (typeof document === 'undefined') return
  // system 移除属性，让纯 CSS 的 prefers-color-scheme 仍能在无 JS 页面工作。
  if (theme.value === 'system') document.documentElement.removeAttribute('data-theme')
  else document.documentElement.dataset.theme = theme.value
}

function onSystemChange(event: MediaQueryListEvent) {
  systemDark.value = event.matches
}

function onStorage(event: StorageEvent) {
  if (event.key !== storageKey && event.key !== null) return
  theme.value = parseTheme(event.newValue)
  applyTheme()
}

function setTheme(value: Theme) {
  theme.value = parseTheme(value)
  applyTheme()
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(storageKey, theme.value)
  } catch {
    // 隐私模式或存储被禁用时，当前页面仍然可以切换主题。
  }
}

/** SSR 使用 system 初值，挂载后读取首屏脚本的设置；不在服务端访问 localStorage。 */
export function useTheme() {
  let mounted = false
  onMounted(() => {
    mounted = true
    if (users++ > 0) return
    try {
      theme.value = parseTheme(window.localStorage.getItem(storageKey))
    } catch {
      theme.value = parseTheme(document.documentElement.getAttribute('data-theme'))
    }
    if (typeof window.matchMedia === 'function') {
      media = window.matchMedia('(prefers-color-scheme: dark)')
      systemDark.value = media.matches
      media.addEventListener('change', onSystemChange)
    }
    window.addEventListener('storage', onStorage)
    applyTheme()
  })
  onBeforeUnmount(() => {
    if (!mounted || --users > 0) return
    media?.removeEventListener('change', onSystemChange)
    media = undefined
    window.removeEventListener('storage', onStorage)
  })
  return { theme, isDark, setTheme }
}
