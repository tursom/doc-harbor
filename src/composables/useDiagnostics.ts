import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { piApi } from '../api-pi'
import type { PiDiagnosticDetail, PiRun, PiRunStatus } from '../ai-pi-types'

// 与默认只在内存中显示令牌的设置页保持一致；只有主动勾选后才写入标签页会话。
export const diagnosticsTokenStorageKey = 'docharbor-ai-diagnostics-token'
export const diagnosticStatuses: PiRunStatus[] = ['queued', 'running', 'succeeded', 'aborted', 'timeout', 'failed']
const statusNames: Record<PiRunStatus, string> = {
  queued: '排队中', running: '运行中', succeeded: '已完成', aborted: '已停止', timeout: '已超时', failed: '失败'
}
export function diagnosticStatusLabel(status: PiRunStatus) { return statusNames[status] }
export function diagnosticStatusVariant(status: PiRunStatus) {
  return ({ queued: 'neutral', running: 'info', succeeded: 'ok', aborted: 'neutral', timeout: 'warn', failed: 'danger' } as const)[status]
}

/** 未结束的运行没有最终耗时，避免把查看页面的时间误当作运行结束时间。 */
export function runDuration(run: PiRun) {
  const start = Date.parse(run.started_at)
  const end = Date.parse(run.finished_at)
  return Number.isFinite(start) && Number.isFinite(end) && end >= start ? `${((end - start) / 1000).toFixed(1)} 秒` : '—'
}
export function runTokens(run: PiRun) { return run.input_tokens + run.output_tokens + run.cache_read_tokens }
export function feedbackLabel(rating?: number) { return rating === -1 ? '👎 不满意' : rating === 1 ? '👍 满意' : '暂无反馈' }

/** URL 仅保存运行 ID，令牌始终不能进入查询参数或浏览器历史。 */
function targetFromURL() {
  const raw = new URL(window.location.href).searchParams.get('run') || ''
  const id = Number(raw)
  return /^\d+$/.test(raw) && Number.isSafeInteger(id) && id > 0 ? id : null
}

export function useDiagnostics() {
  const token = ref('')
  const rememberToken = ref(false)
  const storageError = ref('')
  const status = ref('')
  const query = ref('')
  const negativeOnly = ref(false)
  const runs = ref<PiRun[]>([])
  const detail = ref<PiDiagnosticDetail | null>(null)
  const loading = ref(false)
  const detailLoading = ref(false)
  const error = ref('')
  const detailError = ref('')
  const selectedID = ref<number | null>(null)
  const nextCursor = ref('')
  const hasRead = ref(false)
  let listRequest = 0
  let detailRequest = 0
  let listController: AbortController | undefined
  let detailController: AbortController | undefined
  let lastFilter: Record<string, string> = {}
  const emptyMessage = computed(() => !token.value.trim() ? '输入诊断令牌后读取运行记录' : !hasRead.value ? '点击读取，查看运行记录' : '没有符合条件的运行记录')

  function persistToken() {
    try {
      if (rememberToken.value && token.value.trim()) window.sessionStorage.setItem(diagnosticsTokenStorageKey, token.value.trim())
      else window.sessionStorage.removeItem(diagnosticsTokenStorageKey)
      storageError.value = ''
    } catch { storageError.value = '浏览器不允许会话存储，令牌仅在当前页面使用。' }
  }

  function cancelDetail() {
    detailRequest++
    detailController?.abort()
    detail.value = null
    detailLoading.value = false
    detailError.value = ''
  }

  function invalidate() {
    // AbortSignal 尽早释放连接，序号同时隔离不响应取消的请求及已经排队的回调。
    listRequest++
    listController?.abort()
    cancelDetail()
    runs.value = []; nextCursor.value = ''; loading.value = false; hasRead.value = false; error.value = ''
    // 不清除 selectedID 和 URL，换令牌、签发令牌后仍可打开原来要排查的运行。
  }
  watch(token, () => { invalidate(); persistToken() }, { flush: 'sync' })
  watch(rememberToken, persistToken, { flush: 'sync' })

  function requestError(err: unknown) {
    const code = typeof err === 'object' && err !== null && 'status' in err ? Number(err.status) : 0
    if (code === 401) return '401：诊断令牌无效或已过期，请到系统设置重新签发。'
    if (code === 403) return '403：令牌缺少 ai.diagnostics.read 能力或无权访问此运行，请到系统设置检查令牌能力和 Viewer 范围。'
    return err instanceof Error ? err.message : String(err)
  }

  async function openRun(id: number) {
    if (!Number.isSafeInteger(id) || id <= 0) return
    selectedID.value = id
    const url = new URL(window.location.href)
    url.searchParams.set('run', String(id))
    window.history.replaceState(window.history.state, '', url)
    cancelDetail()
    if (!token.value.trim()) return
    const request = ++detailRequest
    const controller = detailController = new AbortController()
    detailLoading.value = true
    try {
      const response = await piApi.diagnosticRun(token.value.trim(), id, controller.signal)
      if (request === detailRequest) detail.value = response
    } catch (err) {
      if (request === detailRequest && !controller.signal.aborted) detailError.value = requestError(err)
    } finally { if (request === detailRequest) detailLoading.value = false }
  }

  async function loadRuns(more = false) {
    if (!token.value.trim() || (more && (loading.value || !nextCursor.value))) return
    listController?.abort()
    const request = ++listRequest
    const controller = listController = new AbortController()
    if (!more) {
      runs.value = []; nextCursor.value = ''; hasRead.value = false
      lastFilter = {
        limit: '50', ...(status.value ? { status: status.value } : {}),
        ...(query.value.trim() ? { q: query.value.trim() } : {}), ...(negativeOnly.value ? { rating: '-1' } : {})
      }
      // 深链接的详情独立于列表筛选，不会被首条结果替换，也不受列表请求成败影响。
      if (selectedID.value !== null) void openRun(selectedID.value)
    }
    loading.value = true
    error.value = ''
    try {
      const response = await piApi.diagnosticsRuns(token.value.trim(), { ...lastFilter, ...(more ? { cursor: nextCursor.value } : {}) }, controller.signal)
      if (request !== listRequest) return
      const items = response.items || []
      const seen = new Set(more ? runs.value.map(run => run.id) : [])
      const unique = items.filter(run => {
        if (seen.has(run.id)) return false
        seen.add(run.id)
        return true
      })
      runs.value = more ? [...runs.value, ...unique] : unique
      nextCursor.value = response.next_cursor || ''
      hasRead.value = true
      if (!more && selectedID.value === null && runs.value[0]) void openRun(runs.value[0].id)
    } catch (err) {
      if (request === listRequest && !controller.signal.aborted) error.value = requestError(err)
    } finally { if (request === listRequest) loading.value = false }
  }

  function restoreTarget() {
    selectedID.value = targetFromURL()
    cancelDetail()
    if (selectedID.value !== null && token.value.trim()) void openRun(selectedID.value)
  }
  onMounted(() => {
    selectedID.value = targetFromURL()
    try {
      const saved = window.sessionStorage.getItem(diagnosticsTokenStorageKey)
      if (saved) { rememberToken.value = true; token.value = saved }
    } catch { /* 会话存储被禁用时保持默认的内存令牌模式。 */ }
    window.addEventListener('popstate', restoreTarget)
    if (token.value.trim()) void loadRuns()
  })
  onBeforeUnmount(() => { invalidate(); window.removeEventListener('popstate', restoreTarget) })

  return { token, rememberToken, storageError, status, query, negativeOnly, runs, detail, loading, detailLoading, error, detailError, selectedID, nextCursor, emptyMessage, loadRuns, openRun }
}
